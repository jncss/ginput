// Package ginput – MultiForm: multi-page interactive terminal form.
package ginput

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// pageSeqNext is the raw byte sequence sent by terminals for the PageDown key.
var pageSeqNext = []byte("\x1b[6~")

// pageSeqPrev is the raw byte sequence sent by terminals for the PageUp key.
var pageSeqPrev = []byte("\x1b[5~")

// Page is a single page inside a MultiForm. It wraps a *Form (used only for
// its field list, label color, input color, and per-field callbacks) together
// with page-level metadata.
//
// Only the fields of the wrapped Form are used; its header, footer, status,
// offsets, submit key, OnSubmit, and StayOnForm configuration are all ignored —
// those are owned by the parent MultiForm.
type Page struct {
	key         string // unique identifier for this page
	pageHeader  string // short title shown between the global header and the fields
	headerColor Color  // ANSI color for the page header line
	form        *Form  // holds the field list and per-field callbacks
}

// NewPage creates a Page with the given key. The wrapped Form is a plain
// NewForm() that shares the parent MultiForm's stdin/stdout once Read is called.
func NewPage(key string) *Page {
	return &Page{key: key, form: NewForm()}
}

// WithPageHeader sets the per-page title line rendered between the global
// header and the fields. Use \n for multi-line titles.
// Returns the same *Page for chaining.
func (p *Page) WithPageHeader(text string) *Page {
	p.pageHeader = text
	return p
}

// WithPageHeaderColor sets the ANSI color for the page header line.
// Returns the same *Page for chaining.
func (p *Page) WithPageHeaderColor(c Color) *Page {
	p.headerColor = c
	return p
}

// Add appends a named text field to the page.
// Returns the same *Page for chaining.
func (p *Page) Add(key string, inp *Input) *Page {
	p.form.Add(key, inp)
	return p
}

// AddNumeric appends a numeric field to the page.
// Returns the same *Page for chaining.
func (p *Page) AddNumeric(key string, n *NumericInput) *Page {
	p.form.AddNumeric(key, n)
	return p
}

// AddLabel appends a read-only label to the page.
// Returns the same *Page for chaining.
func (p *Page) AddLabel(key string, l *Label) *Page {
	p.form.AddLabel(key, l)
	return p
}

// AddSeparator appends a blank separator line to the page.
// Returns the same *Page for chaining.
func (p *Page) AddSeparator() *Page {
	p.form.AddSeparator()
	return p
}

// GetLabel returns the *Label with the given key on this page, or nil.
func (p *Page) GetLabel(key string) *Label {
	return p.form.GetLabel(key)
}

// WithLabelColor sets the default prompt color for all fields on this page.
// Returns the same *Page for chaining.
func (p *Page) WithLabelColor(c Color) *Page {
	p.form.WithLabelColor(c)
	return p
}

// WithInputColor sets the default editable-area color for all fields on this page.
// Returns the same *Page for chaining.
func (p *Page) WithInputColor(c Color) *Page {
	p.form.WithInputColor(c)
	return p
}

// WithContentOffsetX sets an additional left margin for the fields on this page
// (added on top of the MultiForm-level offset).
// Returns the same *Page for chaining.
func (p *Page) WithContentOffsetX(n int) *Page {
	p.form.WithContentOffsetX(n)
	return p
}

// OnEnter registers a focus-enter callback for the named field on this page.
// Returns the same *Page for chaining.
func (p *Page) OnEnter(key string, fn func(key string, values map[string]string)) *Page {
	p.form.OnEnter(key, fn)
	return p
}

// OnExit registers a focus-exit callback for the named field on this page.
// Returns the same *Page for chaining.
func (p *Page) OnExit(key string, fn func(key string, values map[string]string)) *Page {
	p.form.OnExit(key, fn)
	return p
}

// OnChange registers a value-change callback for the named field on this page.
// Returns the same *Page for chaining.
func (p *Page) OnChange(key string, fn func(key string, value string)) *Page {
	p.form.OnChange(key, fn)
	return p
}

// GetValue returns the current runtime value of the named field on this page.
func (p *Page) GetValue(key string) string {
	return p.form.GetValue(key)
}

// SetValue sets the current runtime value of the named field on this page.
func (p *Page) SetValue(key string, val string) {
	p.form.SetValue(key, val)
}

// ── MultiForm ─────────────────────────────────────────────────────────────────

// MultiForm renders a multi-page interactive terminal form. Each page groups a
// set of fields under a per-page header; the user navigates between pages with
// PageUp (previous) and PageDown (next). A common global header, footer, and
// status line are shared across all pages.
//
// Navigation:
//   - Tab / ↓         : next field within the current page
//   - Shift-Tab / ↑   : previous field within the current page
//   - PageDown        : next page (wraps)
//   - PageUp          : previous page (wraps)
//   - Enter           : advance field; submit on the last field of the last page
//   - Ctrl-C          : ErrInterrupt
//   - Ctrl-D          : ErrEOF (only when the active field is empty)
//
// Results are returned as map[string]map[string]string keyed first by page key,
// then by field key. Field keys may be reused across different pages without
// conflict.
type MultiForm struct {
	pages         []*Page
	in            *os.File
	out           io.Writer
	header        string
	footer        string
	headerColor   Color
	footerColor   Color
	offsetX       int
	offsetY       int
	submitSeqs    [][]byte
	onSubmit      func(map[string]map[string]string) error
	onPageChange  func(pageKey string, values map[string]map[string]string)
	stayOnForm    bool
	focusPageKey  string // page to focus after a stay-on-form redraw
	focusFieldKey string // field within that page to focus

	// status line
	statusMsg     string
	statusColor   Color
	hasStatus     bool
	statusClearAt time.Time

	// global key event handlers
	fnHandlers   map[int]func(map[string]map[string]string) error
	ctrlHandlers map[byte]func(map[string]map[string]string) error

	// computed at Read() start for fixed-layout rendering
	maxPHLines int // max page-header rows across all pages
	maxFields  int // max field count across all pages
}

// NewMultiForm creates an empty MultiForm that reads from os.Stdin and writes
// to os.Stdout.
func NewMultiForm() *MultiForm {
	return &MultiForm{
		in:         os.Stdin,
		out:        os.Stdout,
		submitSeqs: [][]byte{{'\r'}},
	}
}

// AddPage appends a page to the MultiForm.
// Returns the same *MultiForm for chaining.
func (mf *MultiForm) AddPage(p *Page) *MultiForm {
	mf.pages = append(mf.pages, p)
	return mf
}

// WithHeader sets a static text rendered above all pages.
// Returns the same *MultiForm for chaining.
func (mf *MultiForm) WithHeader(text string) *MultiForm {
	mf.header = text
	return mf
}

// WithHeaderColor sets the ANSI color for the global header.
// Returns the same *MultiForm for chaining.
func (mf *MultiForm) WithHeaderColor(c Color) *MultiForm {
	mf.headerColor = c
	return mf
}

// WithFooter sets a static text rendered below all pages.
// Returns the same *MultiForm for chaining.
func (mf *MultiForm) WithFooter(text string) *MultiForm {
	mf.footer = text
	return mf
}

// WithFooterColor sets the ANSI color for the global footer.
// Returns the same *MultiForm for chaining.
func (mf *MultiForm) WithFooterColor(c Color) *MultiForm {
	mf.footerColor = c
	return mf
}

// WithOffsetX sets the left-column margin for the entire MultiForm.
// Returns the same *MultiForm for chaining.
func (mf *MultiForm) WithOffsetX(n int) *MultiForm {
	mf.offsetX = n
	return mf
}

// WithOffsetY sets the number of blank lines printed above the form.
// Returns the same *MultiForm for chaining.
func (mf *MultiForm) WithOffsetY(n int) *MultiForm {
	mf.offsetY = n
	return mf
}

// WithStatusColor configures the status line color and reserves the status area.
// Returns the same *MultiForm for chaining.
func (mf *MultiForm) WithStatusColor(c Color) *MultiForm {
	mf.statusColor = c
	mf.hasStatus = true
	return mf
}

// SetStatus sets the status line message. If clearAfterSecs > 0, the message
// auto-clears after that many seconds. Safe to call from any callback.
func (mf *MultiForm) SetStatus(msg string, clearAfterSecs int) {
	mf.statusMsg = msg
	mf.hasStatus = true
	if clearAfterSecs > 0 {
		mf.statusClearAt = time.Now().Add(time.Duration(clearAfterSecs) * time.Second)
	} else {
		mf.statusClearAt = time.Time{}
	}
}

// ClearStatus clears the status line message immediately.
func (mf *MultiForm) ClearStatus() {
	mf.statusMsg = ""
	mf.statusClearAt = time.Time{}
}

// WithSubmitKey sets the byte that triggers form submission.
// Returns the same *MultiForm for chaining.
func (mf *MultiForm) WithSubmitKey(key byte) *MultiForm {
	if key == 0 {
		key = '\r'
	}
	mf.submitSeqs = [][]byte{{key}}
	return mf
}

// WithSubmitFn sets function key n (1–12) as the submit trigger.
// Returns the same *MultiForm for chaining.
func (mf *MultiForm) WithSubmitFn(n int) *MultiForm {
	seqs := fnKeySeqs(n)
	if seqs != nil {
		mf.submitSeqs = seqs
	}
	return mf
}

// OnSubmit registers a callback called when the form is submitted.
// fn receives the full results map (page key → field key → value).
// A non-nil return exits Read with that error.
// Returns the same *MultiForm for chaining.
func (mf *MultiForm) OnSubmit(fn func(map[string]map[string]string) error) *MultiForm {
	mf.onSubmit = fn
	return mf
}

// OnPageChange registers a callback fired whenever the active page changes
// (PageUp/PageDown). fn receives the new page key and a snapshot of all values.
// Returns the same *MultiForm for chaining.
func (mf *MultiForm) OnPageChange(fn func(pageKey string, values map[string]map[string]string)) *MultiForm {
	mf.onPageChange = fn
	return mf
}

// WithStayOnForm enables stay-on-form mode: the form stays active after each
// submit and exits only on Ctrl-C, Ctrl-D, or a non-nil OnSubmit error.
// Returns the same *MultiForm for chaining.
func (mf *MultiForm) WithStayOnForm() *MultiForm {
	mf.stayOnForm = true
	return mf
}

// Focus sets the page and field that receive focus after a stay-on-form redraw.
// If not called, focus goes to the first field of the first page.
// Returns the same *MultiForm for chaining.
func (mf *MultiForm) Focus(pageKey, fieldKey string) *MultiForm {
	mf.focusPageKey = pageKey
	mf.focusFieldKey = fieldKey
	return mf
}

// OnFn registers a callback fired when function key n (1–12) is pressed.
// A non-nil return exits Read with that error; nil keeps the form active.
// Returns the same *MultiForm for chaining.
func (mf *MultiForm) OnFn(n int, fn func(map[string]map[string]string) error) *MultiForm {
	if mf.fnHandlers == nil {
		mf.fnHandlers = make(map[int]func(map[string]map[string]string) error)
	}
	mf.fnHandlers[n] = fn
	return mf
}

// OnCtrl registers a callback fired when Ctrl+char is pressed.
// char ('A'–'Z' / 'a'–'z'); case is ignored. A non-nil return exits Read.
// Returns the same *MultiForm for chaining.
func (mf *MultiForm) OnCtrl(char byte, fn func(map[string]map[string]string) error) *MultiForm {
	if mf.ctrlHandlers == nil {
		mf.ctrlHandlers = make(map[byte]func(map[string]map[string]string) error)
	}
	b := char
	switch {
	case b >= 'a' && b <= 'z':
		b = b - 'a' + 1
	case b >= 'A' && b <= 'Z':
		b = b - 'A' + 1
	}
	mf.ctrlHandlers[b] = fn
	return mf
}

// ClearScreen writes the ANSI clear-screen + cursor-home sequence to the output.
func (mf *MultiForm) ClearScreen() {
	fmt.Fprint(mf.out, "\033[2J\033[H")
}

// GetValue returns the runtime value of the named field on the named page.
// Returns "" if the page or field is not found.
func (mf *MultiForm) GetValue(pageKey, fieldKey string) string {
	for _, p := range mf.pages {
		if p.key == pageKey {
			return p.form.GetValue(fieldKey)
		}
	}
	return ""
}

// SetValue sets the runtime value of the named field on the named page.
// No-op if the page or field is not found.
func (mf *MultiForm) SetValue(pageKey, fieldKey, val string) {
	for _, p := range mf.pages {
		if p.key == pageKey {
			p.form.SetValue(fieldKey, val)
			return
		}
	}
}

// GetPage returns the *Page with the given key, or nil.
func (mf *MultiForm) GetPage(key string) *Page {
	for _, p := range mf.pages {
		if p.key == key {
			return p
		}
	}
	return nil
}

// collectAll returns the full results map indexed by page key, then field key.
// When field keys are duplicated across pages each page has its own namespace.
func (mf *MultiForm) collectAll() map[string]map[string]string {
	all := make(map[string]map[string]string, len(mf.pages))
	for _, p := range mf.pages {
		all[p.key] = p.form.collect()
	}
	return all
}

// ── layout helpers ────────────────────────────────────────────────────────────

// pageHeaderLines returns the number of terminal rows a page header occupies
// (text lines + 1 blank separator line), or 0 when the page has no header.
func pageHeaderLines(p *Page) int {
	if p.pageHeader == "" {
		return 0
	}
	return strings.Count(p.pageHeader, "\n") + 2 // content lines + 1 blank separator
}

// globalHeaderLines returns the number of rows used by the global header.
func (mf *MultiForm) globalHeaderLines() int {
	if mf.header == "" {
		return 0
	}
	return strings.Count(mf.header, "\n") + 2
}

func (mf *MultiForm) footerExtraLines() int {
	if mf.footer == "" {
		return 0
	}
	return strings.Count(mf.footer, "\n") + 2
}

func (mf *MultiForm) statusExtraLines() int {
	if !mf.hasStatus {
		return 0
	}
	return 2
}

// totalRowsFixed returns the fixed total height of the form area (identical
// for all pages because it uses maxPHLines / maxFields instead of per-page
// values).
func (mf *MultiForm) totalRowsFixed() int {
	return mf.globalHeaderLines() +
		mf.maxPHLines +
		mf.maxFields +
		mf.footerExtraLines() +
		mf.statusExtraLines()
}

// fieldRow returns the absolute row index (0-based from the top of the render
// area) of field at fieldIdx.  This is the same value for every page because
// the per-page header area is always padded to maxPHLines rows.
func (mf *MultiForm) fieldRow(fieldIdx int) int {
	return mf.globalHeaderLines() + mf.maxPHLines + fieldIdx
}

func (mf *MultiForm) printIndented(w *bufio.Writer, c Color, text string) {
	if mf.offsetX <= 0 {
		colorWrap(w, c, strings.ReplaceAll(text, "\n", "\r\n"))
		return
	}
	indent := strings.Repeat(" ", mf.offsetX)
	fmt.Fprint(w, indent)
	colorWrap(w, c, strings.ReplaceAll(text, "\n", "\r\n"+indent))
}

func (mf *MultiForm) printStatusContent(w *bufio.Writer) {
	fmt.Fprint(w, "\r")
	if mf.offsetX > 0 {
		fmt.Fprintf(w, "\033[%dC", mf.offsetX)
	}
	colorWrap(w, mf.statusColor, mf.statusMsg)
	fmt.Fprint(w, "\033[K")
}

// updateStatusLine redraws the status line in its fixed position, then
// restores the cursor to activeFieldIdx and re-renders that field.
func (mf *MultiForm) updateStatusLine(w *bufio.Writer, p *Page, activeFieldIdx int) {
	if !mf.hasStatus {
		return
	}
	curRow := mf.fieldRow(activeFieldIdx)
	// Status is always at: globalHeader + maxPHLines + maxFields + footerExtra + 1
	statusRow := mf.globalHeaderLines() + mf.maxPHLines + mf.maxFields + mf.footerExtraLines() + 1
	dist := statusRow - curRow
	if dist > 0 {
		fmt.Fprintf(w, "\033[%dB", dist)
	} else if dist < 0 {
		fmt.Fprintf(w, "\033[%dA", -dist)
	}
	mf.printStatusContent(w)
	if dist > 0 {
		fmt.Fprintf(w, "\033[%dA", dist)
	} else if dist < 0 {
		fmt.Fprintf(w, "\033[%dB", -dist)
	}
	p.form.entries[activeFieldIdx].field.fieldRender(w)
}

// ── full-page render ──────────────────────────────────────────────────────────

// renderPage draws the complete fixed-height form:
//
//	global header  (always the same height)
//	page-header area padded to maxPHLines rows
//	fields area     padded to maxFields rows
//	footer          (always the same height)
//	status          (always the same height)
//
// The total height is totalRowsFixed() regardless of which page is rendered.
func (mf *MultiForm) renderPage(w *bufio.Writer, p *Page) {
	// Global header.
	if mf.header != "" {
		mf.printIndented(w, mf.headerColor, mf.header)
		fmt.Fprint(w, "\r\n\r\n")
	}
	// Per-page header area – padded to maxPHLines rows so the fields below
	// always start on the same terminal row.
	phLines := pageHeaderLines(p)
	if p.pageHeader != "" {
		mf.printIndented(w, p.headerColor, p.pageHeader)
		fmt.Fprint(w, "\r\n\r\n")
	}
	for i := phLines; i < mf.maxPHLines; i++ {
		fmt.Fprint(w, "\033[2K\r\n")
	}
	// Fields area – padded to maxFields rows so footer and status always
	// appear at the same position across all pages.
	for _, e := range p.form.entries {
		e.field.fieldRender(w)
		fmt.Fprint(w, "\r\n")
	}
	for i := len(p.form.entries); i < mf.maxFields; i++ {
		fmt.Fprint(w, "\033[2K\r\n")
	}
	// Footer.
	if mf.footer != "" {
		fmt.Fprint(w, "\r\n")
		mf.printIndented(w, mf.footerColor, mf.footer)
		fmt.Fprint(w, "\r\n")
	}
	// Status.
	if mf.hasStatus {
		fmt.Fprint(w, "\r\n")
		mf.printStatusContent(w)
		fmt.Fprint(w, "\r\n")
	}
}

// clearAndRenderPage redraws only the variable area (page-header + fields)
// for newPage without touching the global header, footer, or status line.
// Returns the index of the first active field on the new page.
func (mf *MultiForm) clearAndRenderPage(w *bufio.Writer, newPage *Page, activeFieldIdx int) int {
	// Move cursor up to the start of the variable area (first row after the
	// global header).
	curRow := mf.fieldRow(activeFieldIdx)
	varAreaTop := mf.globalHeaderLines()
	dist := curRow - varAreaTop
	if dist > 0 {
		fmt.Fprintf(w, "\033[%dA", dist)
	}
	fmt.Fprint(w, "\r")

	// Per-page header, padded to maxPHLines.
	phLines := pageHeaderLines(newPage)
	if newPage.pageHeader != "" {
		fmt.Fprint(w, "\033[2K\r") // clear line before writing header
		mf.printIndented(w, newPage.headerColor, newPage.pageHeader)
		fmt.Fprint(w, "\r\n\033[2K\r\n") // end of text + clear blank separator line
	}
	for i := phLines; i < mf.maxPHLines; i++ {
		fmt.Fprint(w, "\033[2K\r\n")
	}

	// Fields, padded to maxFields.
	for _, e := range newPage.form.entries {
		fmt.Fprint(w, "\033[2K\r") // clear line before rendering
		e.field.fieldRender(w)
		fmt.Fprint(w, "\r\n")
	}
	for i := len(newPage.form.entries); i < mf.maxFields; i++ {
		fmt.Fprint(w, "\033[2K\r\n")
	}

	// Cursor is now at fieldRow(maxFields) = first row of footer.
	// Move up to the first active field.
	target := newPage.form.firstActive()
	distUp := mf.maxFields - target
	if distUp > 0 {
		fmt.Fprintf(w, "\033[%dA", distUp)
	}
	fmt.Fprint(w, "\r")
	newPage.form.entries[target].field.fieldRender(w)
	return target
}

// ── Read ──────────────────────────────────────────────────────────────────────

// Read puts the terminal into raw mode and runs the interactive multi-page form.
// Returns map[pageKey]map[fieldKey]value on success.
func (mf *MultiForm) Read() (map[string]map[string]string, error) {
	if len(mf.pages) == 0 {
		return map[string]map[string]string{}, nil
	}

	fd := int(mf.in.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, fmt.Errorf("ginput: make raw: %w", err)
	}
	defer term.Restore(fd, oldState) //nolint:errcheck

	w := bufio.NewWriter(mf.out)

	// Apply offsets and init all fields on all pages before rendering.
	// Also compute fixed-layout dimensions (max page-header rows and max
	// field count) so all pages share the same viewport height.
	for _, p := range mf.pages {
		p.form.in = mf.in
		p.form.out = mf.out
		for _, e := range p.form.entries {
			e.field.fieldInit()
			e.field.setMarginLeft(mf.offsetX + p.form.contentOffsetX + e.offsetX)
		}
		if ph := pageHeaderLines(p); ph > mf.maxPHLines {
			mf.maxPHLines = ph
		}
		if n := len(p.form.entries); n > mf.maxFields {
			mf.maxFields = n
		}
	}

	// Vertical offset (blank lines above the form).
	for i := 0; i < mf.offsetY; i++ {
		fmt.Fprint(w, "\r\n")
	}

	// Initial render of the first page.
	activePage := 0
	p := mf.pages[activePage]
	mf.renderPage(w, p)

	// Move cursor back to top of render area, then down to first field.
	totalRows := mf.totalRowsFixed()
	if totalRows > 0 {
		fmt.Fprintf(w, "\033[%dA", totalRows)
	}
	fmt.Fprint(w, "\r")
	activeField := p.form.firstActive()
	targetRow := mf.fieldRow(activeField)
	if targetRow > 0 {
		fmt.Fprintf(w, "\033[%dB", targetRow)
	}
	p.form.entries[activeField].field.fieldRender(w)
	w.Flush()

	// Fire onEnter for the initial field.
	if e0 := p.form.entries[activeField]; e0.onEnter != nil {
		e0.onEnter(e0.key, p.form.collect())
		mf.updateStatusLine(w, p, activeField)
		w.Flush()
	}

	var pending []byte

	type keyMsg struct {
		data []byte
		err  error
	}
	keys := make(chan keyMsg, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		rb := make([]byte, 16)
		for {
			n, readErr := mf.in.Read(rb)
			msg := keyMsg{make([]byte, n), readErr}
			copy(msg.data, rb[:n])
			select {
			case keys <- msg:
			case <-done:
				return
			}
			if readErr != nil {
				return
			}
		}
	}()

	isEnterSubmit := func() bool {
		return len(mf.submitSeqs) == 1 && len(mf.submitSeqs[0]) == 1 &&
			(mf.submitSeqs[0][0] == '\r' || mf.submitSeqs[0][0] == '\n')
	}
	matchesSubmit := func(b []byte) bool {
		for _, seq := range mf.submitSeqs {
			if bytes.Equal(b, seq) {
				return true
			}
		}
		return false
	}
	matchFnKey := func(b []byte) int {
		for n := 1; n <= 12; n++ {
			for _, seq := range fnKeySeqs(n) {
				if bytes.Equal(b, seq) {
					return n
				}
			}
		}
		return 0
	}

	// finalRender clears interactive decorations on all fields of the current
	// page and leaves the cursor below the form.
	finalRender := func() {
		// Move up to the first field row.
		curRow := mf.fieldRow(activeField)
		firstFieldRow := mf.fieldRow(0)
		dist := curRow - firstFieldRow
		if dist > 0 {
			fmt.Fprintf(w, "\033[%dA", dist)
		}
		fmt.Fprint(w, "\r")
		// Clean-render all fields of the current page.
		for _, e := range p.form.entries {
			e.field.fieldRenderClean(w)
			fmt.Fprint(w, "\r\n")
		}
		// Advance past padding rows, footer, and status to end of form area.
		remainingRows := (mf.maxFields - len(p.form.entries)) +
			mf.footerExtraLines() + mf.statusExtraLines()
		for i := 0; i < remainingRows; i++ {
			fmt.Fprint(w, "\r\n")
		}
	}

	// switchPage moves to the page at index newIdx, redrawing only the
	// variable area so the global header, footer, and status stay put.
	switchPage := func(newIdx int) {
		oldPage := mf.pages[activePage]
		// fire onExit on active field of current page
		if e := oldPage.form.entries[activeField]; e.onExit != nil {
			e.onExit(e.key, oldPage.form.collect())
		}
		newPage := mf.pages[newIdx]
		activeField = mf.clearAndRenderPage(w, newPage, activeField)
		activePage = newIdx
		p = mf.pages[activePage]
		if mf.onPageChange != nil {
			mf.onPageChange(p.key, mf.collectAll())
			mf.updateStatusLine(w, p, activeField)
		}
		// fire onEnter on new active field
		if e := p.form.entries[activeField]; e.onEnter != nil {
			e.onEnter(e.key, p.form.collect())
			mf.updateStatusLine(w, p, activeField)
		}
	}

	for {
		// Status auto-clear timer.
		var clearTimer <-chan time.Time
		if mf.hasStatus && !mf.statusClearAt.IsZero() {
			if d := time.Until(mf.statusClearAt); d <= 0 {
				mf.statusMsg = ""
				mf.statusClearAt = time.Time{}
				mf.updateStatusLine(w, p, activeField)
				w.Flush()
			} else {
				clearTimer = time.After(d)
			}
		}

		var msg keyMsg
		select {
		case msg = <-keys:
		case <-clearTimer:
			mf.statusMsg = ""
			mf.statusClearAt = time.Time{}
			mf.updateStatusLine(w, p, activeField)
			w.Flush()
			continue
		}
		if msg.err != nil {
			return nil, msg.err
		}

		// PageDown → next page.
		if bytes.Equal(msg.data, pageSeqNext) {
			next := (activePage + 1) % len(mf.pages)
			switchPage(next)
			w.Flush()
			continue
		}
		// PageUp → previous page.
		if bytes.Equal(msg.data, pageSeqPrev) {
			prev := (activePage - 1 + len(mf.pages)) % len(mf.pages)
			switchPage(prev)
			w.Flush()
			continue
		}

		// OnCtrl handlers.
		if mf.ctrlHandlers != nil && len(msg.data) == 1 && msg.data[0] >= 1 && msg.data[0] <= 26 {
			if fn, ok := mf.ctrlHandlers[msg.data[0]]; ok {
				if err := fn(mf.collectAll()); err != nil {
					finalRender()
					w.Flush()
					return nil, err
				}
				mf.updateStatusLine(w, p, activeField)
				w.Flush()
				continue
			}
		}
		// OnFn handlers.
		if mf.fnHandlers != nil {
			if fn_n := matchFnKey(msg.data); fn_n > 0 {
				if fn, ok := mf.fnHandlers[fn_n]; ok {
					if err := fn(mf.collectAll()); err != nil {
						finalRender()
						w.Flush()
						return nil, err
					}
					mf.updateStatusLine(w, p, activeField)
					w.Flush()
					continue
				}
			}
		}

		// Non-Enter submit trigger.
		if !isEnterSubmit() && matchesSubmit(msg.data) {
			results := mf.collectAll()
			if mf.onSubmit != nil {
				if err := mf.onSubmit(results); err != nil {
					w.Flush()
					return nil, err
				}
			}
			if mf.stayOnForm {
				// Reset all fields and go to focus page/field.
				for _, pg := range mf.pages {
					for _, e := range pg.form.entries {
						e.field.fieldInit()
					}
				}
				targetPage := 0
				if mf.focusPageKey != "" {
					for i, pg := range mf.pages {
						if pg.key == mf.focusPageKey {
							targetPage = i
							break
						}
					}
				}
				switchPage(targetPage)
				if mf.focusFieldKey != "" {
					for i, e := range p.form.entries {
						if e.interactive && e.key == mf.focusFieldKey {
							if i != activeField {
								p.form.moveTo(w, activeField, i)
								activeField = i
							}
							break
						}
					}
				}
				w.Flush()
				continue
			}
			finalRender()
			w.Flush()
			return results, nil
		}

		// Handle keystroke.
		e := p.form.entries[activeField]
		prevVal := ""
		if e.onChange != nil {
			prevVal = e.field.fieldValue()
		}
		result := e.field.fieldKey(msg.data, &pending, w)
		if e.onChange != nil {
			if nv := e.field.fieldValue(); nv != prevVal {
				e.onChange(e.key, nv)
				mf.updateStatusLine(w, p, activeField)
			}
		}

		switch result {
		case keyConfirm:
			// Enter as submit: only when on the last field of the last page.
			if isEnterSubmit() && activePage == len(mf.pages)-1 && activeField == p.form.lastActive() {
				results := mf.collectAll()
				if mf.onSubmit != nil {
					if err := mf.onSubmit(results); err != nil {
						w.Flush()
						return nil, err
					}
				}
				if mf.stayOnForm {
					for _, pg := range mf.pages {
						for _, en := range pg.form.entries {
							en.field.fieldInit()
						}
					}
					targetPage := 0
					if mf.focusPageKey != "" {
						for i, pg := range mf.pages {
							if pg.key == mf.focusPageKey {
								targetPage = i
								break
							}
						}
					}
					switchPage(targetPage)
					w.Flush()
					continue
				}
				finalRender()
				w.Flush()
				return results, nil
			}
			// On the last field of a non-last page: advance to next page.
			if isEnterSubmit() && activeField == p.form.lastActive() && activePage < len(mf.pages)-1 {
				switchPage(activePage + 1)
				w.Flush()
				continue
			}
			// Otherwise: advance to next field within the page.
			if e.onExit != nil {
				e.onExit(e.key, p.form.collect())
			}
			next := p.form.nextActive(activeField)
			p.form.moveTo(w, activeField, next)
			activeField = next
			if ne := p.form.entries[activeField]; ne.onEnter != nil {
				ne.onEnter(ne.key, p.form.collect())
				mf.updateStatusLine(w, p, activeField)
			}
			p.form.entries[activeField].field.fieldRender(w)

		case keyInterrupt:
			finalRender()
			w.Flush()
			return nil, ErrInterrupt

		case keyEOF:
			finalRender()
			w.Flush()
			return nil, ErrEOF

		case keyNext, keyDown:
			if e.onExit != nil {
				e.onExit(e.key, p.form.collect())
			}
			next := p.form.nextActive(activeField)
			p.form.moveTo(w, activeField, next)
			activeField = next
			if ne := p.form.entries[activeField]; ne.onEnter != nil {
				ne.onEnter(ne.key, p.form.collect())
				mf.updateStatusLine(w, p, activeField)
			}
			p.form.entries[activeField].field.fieldRender(w)

		case keyPrev, keyUp:
			if e.onExit != nil {
				e.onExit(e.key, p.form.collect())
			}
			prev := p.form.prevActive(activeField)
			p.form.moveTo(w, activeField, prev)
			activeField = prev
			if pe := p.form.entries[activeField]; pe.onEnter != nil {
				pe.onEnter(pe.key, p.form.collect())
				mf.updateStatusLine(w, p, activeField)
			}
			p.form.entries[activeField].field.fieldRender(w)
		}

		w.Flush()
	}
}
