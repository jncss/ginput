// Package ginput – Form: interactive multi-field terminal input.
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

// formField is implemented by any interactive field that can be placed in a Form.
type formField interface {
	fieldInit()                                                    // reset to configured default
	fieldRender(w *bufio.Writer)                                   // draw with decorations + position cursor
	fieldKey(b []byte, pending *[]byte, w *bufio.Writer) keyResult // process a keystroke
	fieldRenderClean(w *bufio.Writer)                              // draw without decorations
	fieldValue() string                                            // current value as string
	fieldSetValue(val string)                                      // set current value programmatically
	setMarginLeft(n int)                                           // set left column offset
}

// textField wraps *Input with its editing state, implementing formField.
type textField struct {
	inp    *Input
	buf    []rune
	cursor int
}

func (f *textField) fieldInit() {
	runes := []rune(f.inp.defaultVal)
	if cap(runes) < f.inp.maxLen {
		tmp := make([]rune, len(runes), f.inp.maxLen)
		copy(tmp, runes)
		runes = tmp
	}
	f.buf = runes
	f.cursor = len(runes)
}

func (f *textField) fieldRender(w *bufio.Writer)      { f.inp.redraw(w, f.buf, f.cursor) }
func (f *textField) fieldRenderClean(w *bufio.Writer) { f.inp.redrawClean(w, f.buf) }
func (f *textField) fieldValue() string               { return string(f.buf) }
func (f *textField) setMarginLeft(n int)              { f.inp.marginLeft = n }

func (f *textField) fieldSetValue(val string) {
	runes := []rune(val)
	if len(runes) > f.inp.maxLen {
		runes = runes[:f.inp.maxLen]
	}
	if cap(runes) < f.inp.maxLen {
		tmp := make([]rune, len(runes), f.inp.maxLen)
		copy(tmp, runes)
		runes = tmp
	}
	f.buf = runes
	f.cursor = len(runes)
}

func (f *textField) fieldKey(b []byte, pending *[]byte, w *bufio.Writer) keyResult {
	return f.inp.handleKey(b, &f.buf, &f.cursor, pending, w)
}

// formEntry holds the key and field state for one form field.
type formEntry struct {
	key         string
	field       formField
	interactive bool                                       // false for Label and separatorField entries
	offsetX     int                                        // extra left offset for this field only (added to form-level offsets)
	onEnter     func(key string, values map[string]string) // fired when focus arrives
	onExit      func(key string, values map[string]string) // fired when focus leaves
	onChange    func(key string, value string)             // fired on each value change
}

// ── Static / read-only form items ────────────────────────────────────────────

// Label is a read-only display item that can be placed in a Form.
// It shows an optional prompt prefix and a message text, neither editable.
// Its text can be updated programmatically (e.g., from an OnSubmit callback);
// the new value is shown the next time the form redraws.
type Label struct {
	label      string
	text       string
	labelColor Color
	textColor  Color
	marginLeft int // set by the parent Form via setMarginLeft
}

// NewLabel creates a Label with the given prefix (label) and message (text).
// Either string can be empty. Colors default to the terminal default.
func NewLabel(label, text string) *Label {
	return &Label{label: label, text: text}
}

// Set updates the message part of the label.
// It is safe to call from inside an OnSubmit callback; the new text appears
// when the form is redrawn by WithStayOnForm.
func (l *Label) Set(text string) { l.text = text }

// WithLabelColor sets the ANSI color for the prompt/label prefix.
// Returns the same *Label for chaining.
func (l *Label) WithLabelColor(c Color) *Label { l.labelColor = c; return l }

// WithTextColor sets the ANSI color for the message text.
// Returns the same *Label for chaining.
func (l *Label) WithTextColor(c Color) *Label { l.textColor = c; return l }

// formField implementation for Label.
func (l *Label) fieldInit()                                              {}
func (l *Label) fieldValue() string                                      { return "" }
func (l *Label) fieldSetValue(val string)                                { l.text = val }
func (l *Label) fieldKey(_ []byte, _ *[]byte, _ *bufio.Writer) keyResult { return keyNext }
func (l *Label) setMarginLeft(n int)                                     { l.marginLeft = n }
func (l *Label) fieldRender(w *bufio.Writer) {
	fmt.Fprint(w, "\r")
	if l.marginLeft > 0 {
		fmt.Fprintf(w, "\033[%dC", l.marginLeft)
	}
	labelText, msgText := l.label, l.text
	if cols := termColsOrZero(int(os.Stdout.Fd())); cols > 0 {
		avail := cols - l.marginLeft
		labelRunes := []rune(labelText)
		if len(labelRunes) >= avail {
			labelText = string(labelRunes[:avail])
			msgText = ""
		} else {
			msgText = truncateLine(msgText, avail-len(labelRunes))
		}
	}
	colorWrap(w, l.labelColor, labelText)
	colorWrap(w, l.textColor, msgText)
	fmt.Fprint(w, "\033[K") // erase to end of line so shorter updates don't leave stale text
}
func (l *Label) fieldRenderClean(w *bufio.Writer) { l.fieldRender(w) }

// separatorField is a blank terminal line used to visually separate groups of
// fields in a Form. It is not editable and is skipped during navigation.
type separatorField struct{}

func (s *separatorField) fieldInit()                                              {}
func (s *separatorField) fieldValue() string                                      { return "" }
func (s *separatorField) fieldSetValue(_ string)                                  {}
func (s *separatorField) fieldKey(_ []byte, _ *[]byte, _ *bufio.Writer) keyResult { return keyNext }
func (s *separatorField) setMarginLeft(_ int)                                     {}
func (s *separatorField) fieldRender(_ *bufio.Writer)                             {}
func (s *separatorField) fieldRenderClean(_ *bufio.Writer)                        {}

// Form renders multiple Input fields on consecutive terminal lines and lets
// the user navigate between them interactively.
//
// Navigation keys:
//   - Tab / ↓ arrow    : move focus to the next field
//   - Shift-Tab / ↑ arrow : move focus to the previous field
//   - Enter (default) : advance to the next field; submit on the last field
//   - Ctrl-C          : return ErrInterrupt
//   - Ctrl-D          : return ErrEOF (only when the active field is empty)
//
// The submit key can be changed with WithSubmitKey. When a custom submit key
// is set, Enter always advances to the next field (wrapping) from any position.
//
// All single-field editing keys (←/→, Home, End, Ctrl-A/E/K/U,
// Backspace, Delete) apply to the currently focused field.
//
// Use WithStayOnForm to keep the form active after submission (for repeated
// data entry). In that mode, Read only returns on Ctrl-C, Ctrl-D, or when
// the OnSubmit callback returns a non-nil error.
type Form struct {
	entries        []*formEntry
	onSubmit       func(map[string]string) error
	in             *os.File
	out            io.Writer
	submitSeqs     [][]byte // sequences that trigger submission
	header         string   // optional static text rendered above the fields
	footer         string   // optional static text rendered below the fields
	headerColor    Color
	footerColor    Color
	labelColor     Color    // default prompt color for all fields
	inputColor     Color    // default editable-area color for all fields
	stayOnForm     bool     // keep the form active after each submit
	clearKeys      []string // fields to reset on stay-on-form submit (nil = all)
	focusKey       string   // key of the field to activate after a stay-on-form redraw
	offsetX        int      // columns of left margin for the entire form (header, fields, footer)
	offsetY        int      // blank lines printed above the form before the first render
	contentOffsetX int      // additional left margin applied only to input fields (not header/footer)

	// status line (rendered below the footer)
	statusMsg     string    // current message; empty means the line is blank
	statusColor   Color     // ANSI color for the status message
	hasStatus     bool      // true once the status area has been configured
	statusClearAt time.Time // zero = no auto-clear; non-zero = deadline for clearing

	// key event handlers
	fnHandlers   map[int]func(map[string]string) error  // OnFn: per-Fn-key callbacks (1–12)
	ctrlHandlers map[byte]func(map[string]string) error // OnCtrl: per-Ctrl-letter callbacks
}

// NewForm creates an empty Form that reads from os.Stdin and writes to os.Stdout.
func NewForm() *Form {
	return &Form{in: os.Stdin, out: os.Stdout, submitSeqs: [][]byte{{'\r'}}}
}

// Add appends a named text field to the form.
// The same *Input options (WithPrompt, WithBrackets, WithDefault, …) apply.
// Returns the same *Form so calls can be chained.
func (f *Form) Add(key string, inp *Input) *Form {
	if f.labelColor != "" && inp.promptColor == "" {
		inp.WithPromptColor(f.labelColor)
	}
	if f.inputColor != "" && inp.inputColor == "" {
		inp.WithInputColor(f.inputColor)
	}
	f.entries = append(f.entries, &formEntry{key: key, field: &textField{inp: inp}, interactive: true})
	return f
}

// AddNumeric appends a numeric field to the form.
// The same *NumericInput options (WithPrompt, WithBrackets, WithNegative, …) apply.
// Returns the same *Form so calls can be chained.
func (f *Form) AddNumeric(key string, n *NumericInput) *Form {
	if f.labelColor != "" && n.promptColor == "" {
		n.WithPromptColor(f.labelColor)
	}
	if f.inputColor != "" && n.inputColor == "" {
		n.WithInputColor(f.inputColor)
	}
	f.entries = append(f.entries, &formEntry{key: key, field: &numericField{n: n}, interactive: true})
	return f
}

// AddLabel appends a *Label to the form.
// Labels are displayed but not editable; they are skipped during navigation
// and do not appear in the results map.
// The optional key allows the label to be retrieved later with Form.GetLabel(key).
// Returns the same *Form so calls can be chained.
func (f *Form) AddLabel(key string, l *Label) *Form {
	f.entries = append(f.entries, &formEntry{key: key, field: l})
	return f
}

// AddSeparator appends a blank line to the form.
// Separators are skipped during navigation and add no entry to the results map.
// Returns the same *Form so calls can be chained.
func (f *Form) AddSeparator() *Form {
	f.entries = append(f.entries, &formEntry{field: &separatorField{}})
	return f
}

// GetLabel returns the *Label with the given key, or nil if not found.
// Useful when the form was built from a JSON definition and the caller needs
// a reference to update the label text (e.g., from an OnSubmit callback).
func (f *Form) GetLabel(key string) *Label {
	for _, e := range f.entries {
		if e.key == key {
			if l, ok := e.field.(*Label); ok {
				return l
			}
		}
	}
	return nil
}

// WithOffsetX sets a left-column margin applied to the entire form: header,
// all input fields, and footer are all shifted right by n columns.
// Returns the same *Form so calls can be chained.
func (f *Form) WithOffsetX(n int) *Form {
	f.offsetX = n
	return f
}

// WithOffsetY sets the number of blank lines printed above the form before
// the initial render. This shifts the form down on the terminal.
// Returns the same *Form so calls can be chained.
func (f *Form) WithOffsetY(n int) *Form {
	f.offsetY = n
	return f
}

// WithContentOffsetX sets an additional left margin applied only to the
// input-field rows (not the header or footer). This is added on top of any
// form-level WithOffsetX margin.
// Returns the same *Form so calls can be chained.
func (f *Form) WithContentOffsetX(n int) *Form {
	f.contentOffsetX = n
	return f
}

// WithFieldOffset sets an extra left-column margin on a single named field,
// added on top of the form-level and content-level offsets.
// key must match the key passed to Add, AddNumeric, or AddLabel.
// Returns the same *Form so calls can be chained.
func (f *Form) WithFieldOffset(key string, x int) *Form {
	for _, e := range f.entries {
		if e.key == key {
			e.offsetX = x
			return f
		}
	}
	return f
}

// OnSubmit registers a validation callback called when the user presses Enter.
// If the callback returns a non-nil error, Read returns that error immediately
// without clearing the form from the screen.
// Returns the same *Form so calls can be chained.
func (f *Form) OnSubmit(fn func(map[string]string) error) *Form {
	f.onSubmit = fn
	return f
}

// WithSubmitKey sets the byte that triggers form submission.
// The default is '\r' (Enter): Enter on the last field submits, and Enter on
// any other field advances to the next one.
// Pass any other byte (e.g. 19 for Ctrl-S) to make that key always submit
// from any field; Enter will then always advance to the next field (wrapping).
// Returns the same *Form so calls can be chained.
func (f *Form) WithSubmitKey(key byte) *Form {
	if key == 0 {
		key = '\r'
	}
	f.submitSeqs = [][]byte{{key}}
	return f
}

// WithSubmitFn sets function key n (1–12) as the submit trigger.
// Pressing that key submits the form from any field.
// Enter will then always advance to the next field (wrapping).
// Returns the same *Form so calls can be chained.
func (f *Form) WithSubmitFn(n int) *Form {
	seqs := fnKeySeqs(n)
	if seqs != nil {
		f.submitSeqs = seqs
	}
	return f
}

// isEnterSubmit reports whether the configured submit trigger is the default Enter key.
func (f *Form) isEnterSubmit() bool {
	return len(f.submitSeqs) == 1 && len(f.submitSeqs[0]) == 1 &&
		(f.submitSeqs[0][0] == '\r' || f.submitSeqs[0][0] == '\n')
}

// matchesSubmit reports whether b equals any of the configured submit sequences.
func (f *Form) matchesSubmit(b []byte) bool {
	for _, seq := range f.submitSeqs {
		if bytes.Equal(b, seq) {
			return true
		}
	}
	return false
}

// WithHeader sets a static text line (or multi-line block) rendered above
// the input fields while the form is active. The header is also kept on
// screen after submission. Use \n to separate multiple lines.
// Returns the same *Form so calls can be chained.
func (f *Form) WithHeader(text string) *Form {
	f.header = text
	return f
}

// WithFooter sets a static text line (or multi-line block) rendered below
// the input fields while the form is active. The footer is also kept on
// screen after submission. Use \n to separate multiple lines.
// Returns the same *Form so calls can be chained.
func (f *Form) WithFooter(text string) *Form {
	f.footer = text
	return f
}

// WithHeaderColor sets the ANSI color used to render the header text.
// Returns the same *Form so calls can be chained.
func (f *Form) WithHeaderColor(c Color) *Form {
	f.headerColor = c
	return f
}

// WithFooterColor sets the ANSI color used to render the footer text.
// Returns the same *Form so calls can be chained.
func (f *Form) WithFooterColor(c Color) *Form {
	f.footerColor = c
	return f
}

// WithStatusColor configures the ANSI color for the status line and reserves
// a dedicated status area below the footer. Calling this method is not
// required before using SetStatus, but doing so lets you set the color at
// build time and ensures the status area is always visible (even when the
// message is empty).
// Returns the same *Form so calls can be chained.
func (f *Form) WithStatusColor(c Color) *Form {
	f.statusColor = c
	f.hasStatus = true
	return f
}

// SetStatus sets the status line message.
//
// If clearAfterSecs > 0 the message is automatically cleared after that many
// seconds; the clearing is driven by the internal read loop and happens
// promptly even if the user is not actively typing. A clearAfterSecs of 0
// leaves the message visible until the next call to SetStatus.
//
// SetStatus is safe to call from an OnSubmit callback; the updated text
// is rendered on the next form redraw (which happens immediately after
// OnSubmit returns in WithStayOnForm mode).
func (f *Form) SetStatus(msg string, clearAfterSecs int) {
	f.statusMsg = msg
	f.hasStatus = true
	if clearAfterSecs > 0 {
		f.statusClearAt = time.Now().Add(time.Duration(clearAfterSecs) * time.Second)
	} else {
		f.statusClearAt = time.Time{}
	}
}

// ClearStatus clears the status line message immediately.
// Equivalent to SetStatus("", 0).
func (f *Form) ClearStatus() {
	f.statusMsg = ""
	f.statusClearAt = time.Time{}
}

// WithLabelColor sets the default ANSI color for the prompt/label of every
// field added after this call. A field that explicitly calls WithPromptColor
// keeps its own color.
// Returns the same *Form so calls can be chained.
func (f *Form) WithLabelColor(c Color) *Form {
	f.labelColor = c
	return f
}

// WithInputColor sets the default ANSI color for the editable area of every
// field added after this call. A field that explicitly calls WithInputColor
// keeps its own color.
// Returns the same *Form so calls can be chained.
func (f *Form) WithInputColor(c Color) *Form {
	f.inputColor = c
	return f
}

// GetValue returns the current runtime value of the named interactive field.
// For text fields it is the typed string; for numeric fields the formatted
// decimal string (e.g. "9.99"). Returns "" if key is not found.
func (f *Form) GetValue(key string) string {
	for _, e := range f.entries {
		if e.key == key && e.interactive {
			return e.field.fieldValue()
		}
	}
	return ""
}

// SetValue sets the current runtime value of the named interactive field.
// For text fields the string is clamped to the field's maxLen; for numeric
// fields it is parsed the same way as WithDefault.
// If key is not found or the field is not interactive, the call is a no-op.
// When called from a field callback (OnEnter, OnExit, OnChange) the field is
// redrawn automatically after the callback returns. When called from OnSubmit
// or OnFn/OnCtrl the new value is visible from the next keypress.
func (f *Form) SetValue(key string, val string) {
	for _, e := range f.entries {
		if e.key == key && e.interactive {
			e.field.fieldSetValue(val)
			return
		}
	}
}

// ClearScreen writes the ANSI clear-screen sequence to the form's output,
// placing the cursor at the top-left corner. Useful before calling Read to
// present the form on a clean terminal.
func (f *Form) ClearScreen() {
	fmt.Fprint(f.out, "\033[2J\033[H")
}

// OnEnter registers a callback fired when focus moves INTO the named field.
// fn receives the field key and a snapshot of all current form values.
// Useful for pre-filling related fields or showing contextual status messages.
// Returns the same *Form so calls can be chained.
func (f *Form) OnEnter(key string, fn func(key string, values map[string]string)) *Form {
	for _, e := range f.entries {
		if e.key == key {
			e.onEnter = fn
			return f
		}
	}
	return f
}

// OnExit registers a callback fired when focus moves OUT of the named field.
// fn receives the field key and a snapshot of all current form values.
// Useful for per-field validation feedback or status messages.
// Returns the same *Form so calls can be chained.
func (f *Form) OnExit(key string, fn func(key string, values map[string]string)) *Form {
	for _, e := range f.entries {
		if e.key == key {
			e.onExit = fn
			return f
		}
	}
	return f
}

// OnChange registers a callback fired after each keystroke that changes the
// value of the named field. fn receives the field key and the new value.
// Returns the same *Form so calls can be chained.
func (f *Form) OnChange(key string, fn func(key string, value string)) *Form {
	for _, e := range f.entries {
		if e.key == key {
			e.onChange = fn
			return f
		}
	}
	return f
}

// OnFn registers a callback fired when function key n (1–12) is pressed from
// any field. The callback receives a snapshot of the current form values.
// If fn returns a non-nil error the form exits immediately with that error;
// returning nil keeps the form active.
// F-key handlers take priority over a submit trigger set with WithSubmitFn.
// Returns the same *Form so calls can be chained.
func (f *Form) OnFn(n int, fn func(map[string]string) error) *Form {
	if f.fnHandlers == nil {
		f.fnHandlers = make(map[int]func(map[string]string) error)
	}
	f.fnHandlers[n] = fn
	return f
}

// OnCtrl registers a callback fired when Ctrl+char is pressed from any field.
// char must be a letter ('A'–'Z' or 'a'–'z'); case is ignored (Ctrl-S and
// Ctrl-s are the same). The callback receives a snapshot of the current
// form values. If fn returns a non-nil error the form exits with that error;
// returning nil keeps the form active.
// Ctrl handlers take priority over the built-in editing shortcuts (Ctrl-A/E/
// K/U), so avoid registering those letters unless you intend to override them.
// Returns the same *Form so calls can be chained.
func (f *Form) OnCtrl(char byte, fn func(map[string]string) error) *Form {
	if f.ctrlHandlers == nil {
		f.ctrlHandlers = make(map[byte]func(map[string]string) error)
	}
	b := char
	switch {
	case b >= 'a' && b <= 'z':
		b = b - 'a' + 1
	case b >= 'A' && b <= 'Z':
		b = b - 'A' + 1
	}
	f.ctrlHandlers[b] = fn
	return f
}

// footerExtraLines returns the number of terminal lines occupied by the footer
// (including a blank separator line), or 0 when no footer is set.
func (f *Form) footerExtraLines() int {
	if f.footer == "" {
		return 0
	}
	return strings.Count(f.footer, "\n") + 2 // 1 blank separator + content lines
}

// statusExtraLines returns the number of terminal lines reserved for the
// status area (blank separator + status line), or 0 when no status area is
// configured.
func (f *Form) statusExtraLines() int {
	if !f.hasStatus {
		return 0
	}
	return 2 // 1 blank separator + 1 status line
}

// printStatusContent renders the status message on the current terminal line.
// The caller is responsible for positioning the cursor on the correct row
// before calling this helper.
func (f *Form) printStatusContent(w *bufio.Writer) {
	fmt.Fprint(w, "\r")
	if f.offsetX > 0 {
		fmt.Fprintf(w, "\033[%dC", f.offsetX)
	}
	msg := f.statusMsg
	if cols := termColsOrZero(int(f.in.Fd())); cols > 0 {
		msg = truncateLine(msg, cols-f.offsetX)
	}
	colorWrap(w, f.statusColor, msg)
	fmt.Fprint(w, "\033[K") // erase to end of line so shorter updates clean up stale text
}

// updateStatusLine moves the cursor from the active field row down to the
// status line, redraws the status content, and then moves the cursor back and
// redraws the active field to restore the correct cursor column.
// Used for asynchronous status updates (e.g. auto-clear timer) while the read
// loop is waiting for input.
func (f *Form) updateStatusLine(w *bufio.Writer, active int) {
	if !f.hasStatus {
		return
	}
	n := len(f.entries)
	// Distance from the active-field row to the status content row:
	//   n fields end at row n-1; blank-sep at n; footer at n+1..n+footerLines;
	//   blank-sep before status at n+footerExtraLines(); status at n+footerExtraLines()+1.
	dist := n + f.footerExtraLines() + 1 - active
	fmt.Fprintf(w, "\033[%dB", dist) // move down to status row
	f.printStatusContent(w)
	fmt.Fprintf(w, "\033[%dA", dist)       // move back up to active-field row
	f.entries[active].field.fieldRender(w) // restore cursor column
}

// printIndented writes text to w, prefixing every line with f.offsetX spaces.
// It replaces \n with \r\n (required in raw terminal mode).
// When offsetX is zero it is equivalent to colorWrap.
func (f *Form) printIndented(w *bufio.Writer, c Color, text string) {
	cols := termColsOrZero(int(f.in.Fd()))
	if avail := cols - f.offsetX; avail > 0 {
		lines := strings.Split(text, "\n")
		for i, line := range lines {
			lines[i] = truncateLine(line, avail)
		}
		text = strings.Join(lines, "\n")
	}
	if f.offsetX <= 0 {
		colorWrap(w, c, strings.ReplaceAll(text, "\n", "\r\n"))
		return
	}
	indent := strings.Repeat(" ", f.offsetX)
	fmt.Fprint(w, indent)
	colorWrap(w, c, strings.ReplaceAll(text, "\n", "\r\n"+indent))
}

// WithInput sets the file used for reading keystrokes (default: os.Stdin).
// Must be a *os.File because terminal raw mode requires a file descriptor.
// Returns the same *Form so calls can be chained.
func (f *Form) WithInput(file *os.File) *Form {
	f.in = file
	return f
}

// WithOutput sets the writer used for rendering (default: os.Stdout).
// Returns the same *Form so calls can be chained.
func (f *Form) WithOutput(w io.Writer) *Form {
	f.out = w
	return f
}

// WithStayOnForm enables "stay on form" mode: after each successful submit the
// form remains active instead of returning. The optional clearKeys list names
// which fields are reset to their configured defaults; if no keys are given,
// all fields are reset. The form only exits via Ctrl-C (ErrInterrupt),
// Ctrl-D (ErrEOF), or when the OnSubmit callback returns a non-nil error.
// Returns the same *Form so calls can be chained.
func (f *Form) WithStayOnForm(clearKeys ...string) *Form {
	f.stayOnForm = true
	f.clearKeys = clearKeys
	return f
}

// Focus sets the field that receives the cursor after each stay-on-form
// submit redraw. key must match the key passed to Add or AddNumeric.
// If Focus is never called (or key is not found), the cursor goes to the
// first interactive field.
// Can be called at build time (chainable) or from inside an OnSubmit callback
// to change the focus dynamically for the next redraw.
// Returns the same *Form so calls can be chained.
func (f *Form) Focus(key string) *Form {
	f.focusKey = key
	return f
}

// Read puts the terminal into raw mode, renders all fields on consecutive
// lines, and lets the user edit them interactively.
//
// When the submit key (default: Enter) is pressed on the last field, all field
// values are collected into a map[string]string keyed by the names passed to
// Add, and returned. On any other field, the same key advances to the next
// field. Tab / Shift-Tab always navigate between fields.
//
// Errors:
//   - ErrInterrupt  when Ctrl-C is pressed on any field.
//   - ErrEOF        when Ctrl-D is pressed on an empty active field.
//   - The error returned by OnSubmit, if set and validation fails.
//   - Any OS-level read error.
func (f *Form) Read() (map[string]string, error) {
	if len(f.entries) == 0 {
		return map[string]string{}, nil
	}

	fd := int(f.in.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, fmt.Errorf("ginput: make raw: %w", err)
	}
	defer term.Restore(fd, oldState) //nolint:errcheck

	w := bufio.NewWriter(f.out)
	n := len(f.entries)

	// Initialise each field from its configured default value.
	for _, e := range f.entries {
		e.field.fieldInit()
	}

	// Apply margin offsets: form-level + content-level + per-field.
	for _, e := range f.entries {
		e.field.setMarginLeft(f.offsetX + f.contentOffsetX + e.offsetX)
	}

	// ── Initial render ────────────────────────────────────────────────────────
	// Emit blank lines for vertical offset.
	for i := 0; i < f.offsetY; i++ {
		fmt.Fprint(w, "\r\n")
	}
	// Draw the optional header above the fields.
	if f.header != "" {
		f.printIndented(w, f.headerColor, f.header)
		fmt.Fprint(w, "\r\n")
		fmt.Fprint(w, "\r\n")
	}
	// Draw all N fields, each terminated with \r\n.  Cursor ends up at column 0
	// on the line below the last field (line N, 0-indexed from top).
	for _, e := range f.entries {
		e.field.fieldRender(w)
		fmt.Fprint(w, "\r\n")
	}
	// Draw the optional footer below the fields.
	if f.footer != "" {
		fmt.Fprint(w, "\r\n")
		f.printIndented(w, f.footerColor, f.footer)
		fmt.Fprint(w, "\r\n")
	}
	// Draw the optional status area below the footer.
	if f.hasStatus {
		fmt.Fprint(w, "\r\n")
		f.printStatusContent(w)
		fmt.Fprint(w, "\r\n")
	}
	// Move up to reach row 0.
	fmt.Fprintf(w, "\033[%dA", n+f.footerExtraLines()+f.statusExtraLines())
	// Move down to the first interactive field and redraw it.
	first := f.firstActive()
	if first > 0 {
		fmt.Fprintf(w, "\033[%dB", first)
	}
	f.entries[first].field.fieldRender(w)
	w.Flush()

	active := first
	// Fire onEnter for the initially focused field.
	if e0 := f.entries[active]; e0.onEnter != nil {
		e0.onEnter(e0.key, f.collect())
		if f.hasStatus {
			f.updateStatusLine(w, active)
			w.Flush()
		}
	}
	var pending []byte

	// keyMsg carries one raw read result from the background reader goroutine.
	type keyMsg struct {
		data []byte
		err  error
	}

	// Launch a dedicated goroutine for blocking reads so we can implement the
	// status auto-clear timer with time.After instead of SetReadDeadline, which
	// does not work for terminal file descriptors on Linux.
	keys := make(chan keyMsg, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		rb := make([]byte, 16)
		for {
			n, readErr := f.in.Read(rb)
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

	for {
		// Build a timer for the status auto-clear; nil channel never fires.
		var clearTimer <-chan time.Time
		if f.hasStatus && !f.statusClearAt.IsZero() {
			if d := time.Until(f.statusClearAt); d <= 0 {
				// Deadline already passed – clear immediately and loop.
				f.statusMsg = ""
				f.statusClearAt = time.Time{}
				f.updateStatusLine(w, active)
				w.Flush()
			} else {
				clearTimer = time.After(d)
			}
		}

		var msg keyMsg
		select {
		case msg = <-keys:
		case <-clearTimer:
			f.statusMsg = ""
			f.statusClearAt = time.Time{}
			f.updateStatusLine(w, active)
			w.Flush()
			continue
		}

		if msg.err != nil {
			return nil, msg.err
		}

		e := f.entries[active]

		// Intercept OnCtrl / OnFn handlers before field or submit logic.
		if f.ctrlHandlers != nil && len(msg.data) == 1 && msg.data[0] >= 1 && msg.data[0] <= 26 {
			if fn, ok := f.ctrlHandlers[msg.data[0]]; ok {
				if err := fn(f.collect()); err != nil {
					f.finalRender(w, active)
					w.Flush()
					return nil, err
				}
				if f.hasStatus {
					f.updateStatusLine(w, active)
				}
				w.Flush()
				continue
			}
		}
		if f.fnHandlers != nil {
			if fn_n := f.matchFnKey(msg.data); fn_n > 0 {
				if fn, ok := f.fnHandlers[fn_n]; ok {
					if err := fn(f.collect()); err != nil {
						f.finalRender(w, active)
						w.Flush()
						return nil, err
					}
					if f.hasStatus {
						f.updateStatusLine(w, active)
					}
					w.Flush()
					continue
				}
			}
		}

		// Intercept a non-Enter submit trigger before handleKey sees it.
		if !f.isEnterSubmit() && f.matchesSubmit(msg.data) {
			results := f.collect()
			if f.onSubmit != nil {
				if err := f.onSubmit(results); err != nil {
					w.Flush()
					return nil, err
				}
			}
			if f.stayOnForm {
				f.fireOnExit(w, active)
				active = f.resetAndStay(w, active)
				f.fireOnEnter(w, active)
				w.Flush()
				continue
			}
			f.finalRender(w, active)
			w.Flush()
			return results, nil
		}

		prevVal := ""
		if e.onChange != nil {
			prevVal = e.field.fieldValue()
		}
		result := e.field.fieldKey(msg.data, &pending, w)
		if e.onChange != nil {
			if nv := e.field.fieldValue(); nv != prevVal {
				e.onChange(e.key, nv)
				if f.hasStatus {
					f.updateStatusLine(w, active)
				}
			}
		}

		switch result {
		case keyConfirm:
			// Enter is the submit key: submit only on the last interactive field.
			if f.isEnterSubmit() && active == f.lastActive() {
				results := f.collect()
				if f.onSubmit != nil {
					if err := f.onSubmit(results); err != nil {
						w.Flush()
						return nil, err
					}
				}
				if f.stayOnForm {
					f.fireOnExit(w, active)
					active = f.resetAndStay(w, active)
					f.fireOnEnter(w, active)
					w.Flush()
					continue
				}
				f.finalRender(w, active)
				w.Flush()
				return results, nil
			}
			// Otherwise advance to the next interactive field (wrapping).
			next := f.nextActive(active)
			f.fireOnExit(w, active)
			f.moveTo(w, active, next)
			active = next
			f.fireOnEnter(w, active)
			f.entries[active].field.fieldRender(w) // reflect any SetValue from OnEnter

		case keyInterrupt:
			f.finalRender(w, active)
			w.Flush()
			return nil, ErrInterrupt

		case keyEOF:
			// Each field only returns keyEOF when its value is already empty/zero.
			f.finalRender(w, active)
			w.Flush()
			return nil, ErrEOF

		case keyNext, keyDown:
			next := f.nextActive(active)
			f.fireOnExit(w, active)
			f.moveTo(w, active, next)
			active = next
			f.fireOnEnter(w, active)
			f.entries[active].field.fieldRender(w) // reflect any SetValue from OnEnter

		case keyPrev, keyUp:
			prev := f.prevActive(active)
			f.fireOnExit(w, active)
			f.moveTo(w, active, prev)
			active = prev
			f.fireOnEnter(w, active)
			f.entries[active].field.fieldRender(w) // reflect any SetValue from OnEnter
		}

		w.Flush()
	}
}

// ── internal helpers ──────────────────────────────────────────────────────────

// collect builds the result map from the current interactive field values.
// Label and separator entries are omitted.
func (f *Form) collect() map[string]string {
	m := make(map[string]string)
	for _, e := range f.entries {
		if e.interactive {
			m[e.key] = e.field.fieldValue()
		}
	}
	return m
}

// firstActive returns the index of the first interactive entry.
func (f *Form) firstActive() int {
	for i, e := range f.entries {
		if e.interactive {
			return i
		}
	}
	return 0
}

// lastActive returns the index of the last interactive entry.
func (f *Form) lastActive() int {
	for i := len(f.entries) - 1; i >= 0; i-- {
		if f.entries[i].interactive {
			return i
		}
	}
	return len(f.entries) - 1
}

// nextActive returns the index of the next interactive entry after cur (wrapping).
func (f *Form) nextActive(cur int) int {
	n := len(f.entries)
	for i := 1; i <= n; i++ {
		idx := (cur + i) % n
		if f.entries[idx].interactive {
			return idx
		}
	}
	return cur
}

// prevActive returns the index of the previous interactive entry before cur (wrapping).
func (f *Form) prevActive(cur int) int {
	n := len(f.entries)
	for i := 1; i <= n; i++ {
		idx := (cur - i + n) % n
		if f.entries[idx].interactive {
			return idx
		}
	}
	return cur
}

// moveTo moves the terminal cursor from field from to field to (by row delta)
// and redraws the target field to position the cursor at the right column.
func (f *Form) moveTo(w *bufio.Writer, from, to int) {
	delta := to - from
	if delta > 0 {
		fmt.Fprintf(w, "\033[%dB", delta)
	} else if delta < 0 {
		fmt.Fprintf(w, "\033[%dA", -delta)
	}
	e := f.entries[to]
	e.field.fieldRender(w)
}

// finalRender moves the cursor to field 0, redraws every field without
// placeholder/bracket decorations, and leaves the cursor on a new line
// below the last field.
func (f *Form) finalRender(w *bufio.Writer, active int) {
	if active > 0 {
		fmt.Fprintf(w, "\033[%dA", active)
	}
	for i, e := range f.entries {
		e.field.fieldRenderClean(w)
		if i < len(f.entries)-1 {
			fmt.Fprint(w, "\r\n")
		}
	}
	fmt.Fprint(w, "\r\n")
	if f.footer != "" {
		fmt.Fprint(w, "\r\n")
		f.printIndented(w, f.footerColor, f.footer)
		fmt.Fprint(w, "\r\n")
	}
	if f.hasStatus && f.statusMsg != "" {
		fmt.Fprint(w, "\r\n")
		f.printStatusContent(w)
		fmt.Fprint(w, "\r\n")
	}
}

// resetAndStay re-initialises the fields that should be cleared after a
// "stay-on-form" submit, redraws all fields, and returns 0 (the new active
// field index).
func (f *Form) resetAndStay(w *bufio.Writer, active int) int {
	n := len(f.entries)

	clearAll := len(f.clearKeys) == 0
	clearSet := make(map[string]bool, len(f.clearKeys))
	for _, k := range f.clearKeys {
		clearSet[k] = true
	}

	// Move cursor to field 0.
	if active > 0 {
		fmt.Fprintf(w, "\033[%dA", active)
	}
	fmt.Fprint(w, "\r")

	// Re-initialise the appropriate fields.
	for _, e := range f.entries {
		if clearAll || clearSet[e.key] {
			e.field.fieldInit()
		}
	}

	// Redraw all fields.
	for _, e := range f.entries {
		e.field.fieldRender(w)
		fmt.Fprint(w, "\r\n")
	}
	// Redraw footer (static content, but must traverse it to reach the status area).
	if f.footer != "" {
		fmt.Fprint(w, "\r\n")
		f.printIndented(w, f.footerColor, f.footer)
		fmt.Fprint(w, "\r\n")
	}
	// Redraw the status line so that a message set by OnSubmit becomes visible.
	if f.hasStatus {
		fmt.Fprint(w, "\r\n")
		f.printStatusContent(w)
		fmt.Fprint(w, "\r\n")
	}

	// Move back up to row 0 and down to the focus/first interactive field.
	fmt.Fprintf(w, "\033[%dA", n+f.footerExtraLines()+f.statusExtraLines())
	target := f.firstActive()
	if f.focusKey != "" {
		for i, e := range f.entries {
			if e.interactive && e.key == f.focusKey {
				target = i
				break
			}
		}
	}
	if target > 0 {
		fmt.Fprintf(w, "\033[%dB", target)
	}
	f.entries[target].field.fieldRender(w)
	return target
}

// fireOnExit fires the onExit callback for the field at idx (if any) and
// refreshes the status line. Must be called while the terminal cursor is
// positioned on row idx.
func (f *Form) fireOnExit(w *bufio.Writer, idx int) {
	e := f.entries[idx]
	if e.onExit == nil {
		return
	}
	e.onExit(e.key, f.collect())
	if f.hasStatus {
		f.updateStatusLine(w, idx)
	}
}

// fireOnEnter fires the onEnter callback for the field at idx (if any) and
// refreshes the status line. Must be called while the terminal cursor is
// positioned on row idx.
func (f *Form) fireOnEnter(w *bufio.Writer, idx int) {
	e := f.entries[idx]
	if e.onEnter == nil {
		return
	}
	e.onEnter(e.key, f.collect())
	if f.hasStatus {
		f.updateStatusLine(w, idx)
	}
}

// matchFnKey returns the function-key number (1–12) if b matches any known
// F-key escape sequence, or 0 if no match is found.
func (f *Form) matchFnKey(b []byte) int {
	for n := 1; n <= 12; n++ {
		for _, seq := range fnKeySeqs(n) {
			if bytes.Equal(b, seq) {
				return n
			}
		}
	}
	return 0
}
