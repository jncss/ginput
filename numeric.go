// Package ginput – NumericInput: fixed-decimal right-aligned numeric field.
package ginput

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

// NumericInput is a right-aligned numeric input field with a fixed number of
// decimal places. Digits are entered calculator-style: each new digit shifts
// existing digits left, and Backspace removes the rightmost digit.
//
// Supported keys:
//   - 0-9           : append a digit (shifts existing digits left)
//   - Backspace     : remove the rightmost digit
//   - -             : toggle sign (only when WithNegative is set)
//   - Ctrl-U        : reset to zero
//   - Enter         : confirm
//   - Ctrl-C        : return ErrInterrupt
//   - Ctrl-D        : return ErrEOF (only when value is zero)
//   - Tab / ↓       : next field (Form only)
//   - Shift-Tab / ↑ : previous field (Form only)
type NumericInput struct {
	maxIntegers  int
	decimals     int
	prompt       string
	showBrackets bool
	allowNeg     bool
	defaultVal   string
	promptColor  Color
	inputColor   Color
	in           *os.File
	out          io.Writer
	marginLeft   int // left column offset applied by the parent Form
}

// NewNumeric creates a NumericInput that accepts up to maxIntegers digits
// before the decimal point and exactly decimals digits after it.
// maxIntegers must be >= 1; decimals must be >= 0.
func NewNumeric(maxIntegers, decimals int) *NumericInput {
	if maxIntegers < 1 {
		maxIntegers = 1
	}
	if decimals < 0 {
		decimals = 0
	}
	return &NumericInput{
		maxIntegers: maxIntegers,
		decimals:    decimals,
		in:          os.Stdin,
		out:         os.Stdout,
	}
}

// WithPrompt sets the text displayed before the editable area.
// Returns the same *NumericInput so calls can be chained.
func (n *NumericInput) WithPrompt(prompt string) *NumericInput {
	n.prompt = prompt
	return n
}

// WithBrackets surrounds the field with '[' and ']'.
// Returns the same *NumericInput so calls can be chained.
func (n *NumericInput) WithBrackets() *NumericInput {
	n.showBrackets = true
	return n
}

// WithNegative allows the user to toggle the sign with the '-' key.
// Returns the same *NumericInput so calls can be chained.
func (n *NumericInput) WithNegative() *NumericInput {
	n.allowNeg = true
	return n
}

// WithDefault sets the initial value pre-filled in the field.
// The string is parsed as a decimal number; invalid values are treated as zero.
// Returns the same *NumericInput so calls can be chained.
func (n *NumericInput) WithDefault(val string) *NumericInput {
	n.defaultVal = val
	return n
}

// WithInput sets the file used for reading keystrokes (default: os.Stdin).
// Must be a *os.File because terminal raw mode requires a file descriptor.
// Returns the same *NumericInput so calls can be chained.
func (n *NumericInput) WithInput(f *os.File) *NumericInput {
	n.in = f
	return n
}

// WithOutput sets the writer used for rendering (default: os.Stdout).
// Returns the same *NumericInput so calls can be chained.
func (n *NumericInput) WithOutput(w io.Writer) *NumericInput {
	n.out = w
	return n
}

// WithPromptColor sets the ANSI color used to render the prompt text.
// Returns the same *NumericInput so calls can be chained.
func (n *NumericInput) WithPromptColor(c Color) *NumericInput {
	n.promptColor = c
	return n
}

// WithInputColor sets the ANSI color used to render the editable area
// (brackets and numeric digits).
// Returns the same *NumericInput so calls can be chained.
func (n *NumericInput) WithInputColor(c Color) *NumericInput {
	n.inputColor = c
	return n
}

// Read puts the terminal into raw mode and reads a numeric value.
// Returns the formatted value as a string (e.g. "123.45").
//
// Errors:
//   - ErrInterrupt  when Ctrl-C is pressed.
//   - ErrEOF        when Ctrl-D is pressed with a zero value.
//   - Any OS-level read error.
func (n *NumericInput) Read() (string, error) {
	fd := int(n.in.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return "", fmt.Errorf("ginput: make raw: %w", err)
	}
	defer term.Restore(fd, oldState) //nolint:errcheck

	w := bufio.NewWriter(n.out)
	value := n.parseDefault()
	n.render(w, value)
	w.Flush()

	var pending []byte
	rb := make([]byte, 16)
	for {
		nr, err := n.in.Read(rb)
		if err != nil {
			return "", err
		}
		switch n.handleKey(rb[:nr], &value, &pending, w) {
		case keyConfirm:
			n.renderClean(w, value)
			fmt.Fprint(w, "\r\n")
			w.Flush()
			return n.formatClean(value), nil
		case keyInterrupt:
			fmt.Fprint(w, "\r\n")
			w.Flush()
			return "", ErrInterrupt
		case keyEOF:
			fmt.Fprint(w, "\r\n")
			w.Flush()
			return "", ErrEOF
		}
		w.Flush()
	}
}

// ── internal helpers ──────────────────────────────────────────────────────────

func (n *NumericInput) promptRuneLen() int {
	return utf8.RuneCountInString(n.prompt)
}

func (n *NumericInput) bracketOffset() int {
	if n.showBrackets {
		return 1
	}
	return 0
}

// fieldWidth returns the display width of the editable area (excluding prompt and brackets).
func (n *NumericInput) fieldWidth() int {
	w := n.maxIntegers
	if n.decimals > 0 {
		w += 1 + n.decimals // '.' separator + decimal digits
	}
	if n.allowNeg {
		w++ // sign column
	}
	return w
}

// scale returns 10^decimals.
func (n *NumericInput) scale() int64 {
	s := int64(1)
	for i := 0; i < n.decimals; i++ {
		s *= 10
	}
	return s
}

// maxAbsValue is the largest storable absolute value.
// E.g. maxIntegers=3, decimals=2 → 99999 (represents 999.99).
func (n *NumericInput) maxAbsValue() int64 {
	intMax := int64(1)
	for i := 0; i < n.maxIntegers; i++ {
		intMax *= 10
	}
	intMax-- // e.g. maxIntegers=3 → 999
	sc := n.scale()
	return intMax*sc + (sc - 1)
}

// parseDefault converts n.defaultVal to an internal int64 value.
func (n *NumericInput) parseDefault() int64 {
	s := strings.TrimSpace(n.defaultVal)
	if s == "" {
		return 0
	}
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	parts := strings.SplitN(s, ".", 2)

	var intPart int64
	for _, c := range parts[0] {
		if c >= '0' && c <= '9' {
			intPart = intPart*10 + int64(c-'0')
		}
	}

	var decPart int64
	if len(parts) == 2 {
		decStr := parts[1]
		if len(decStr) > n.decimals {
			decStr = decStr[:n.decimals]
		}
		for _, c := range decStr {
			if c >= '0' && c <= '9' {
				decPart = decPart*10 + int64(c-'0')
			}
		}
		// right-pad with zeros for missing decimal places
		for i := len(decStr); i < n.decimals; i++ {
			decPart *= 10
		}
	}

	sc := n.scale()
	v := intPart*sc + decPart
	max := n.maxAbsValue()
	if v > max {
		v = max
	}
	if neg && n.allowNeg {
		v = -v
	}
	return v
}

// formatDisplay formats value as a right-aligned string of exactly fieldWidth() runes.
func (n *NumericInput) formatDisplay(value int64) string {
	neg := value < 0
	abs := value
	if neg {
		abs = -abs
	}
	sc := n.scale()
	var s string
	if n.decimals > 0 {
		intPart := abs / sc
		decPart := abs % sc
		s = fmt.Sprintf("%d.%0*d", intPart, n.decimals, decPart)
	} else {
		s = fmt.Sprintf("%d", abs)
	}
	if neg {
		s = "-" + s
	} else if n.allowNeg {
		s = " " + s // reserve sign column for positive values
	}
	fw := n.fieldWidth()
	for utf8.RuneCountInString(s) < fw {
		s = " " + s
	}
	return s
}

// formatClean returns the value as a plain decimal string without leading spaces
// (e.g. "123.45", "-0.50"). Used for the Form result map and standalone Read.
func (n *NumericInput) formatClean(value int64) string {
	neg := value < 0
	abs := value
	if neg {
		abs = -abs
	}
	sc := n.scale()
	var s string
	if n.decimals > 0 {
		intPart := abs / sc
		decPart := abs % sc
		s = fmt.Sprintf("%d.%0*d", intPart, n.decimals, decPart)
	} else {
		s = fmt.Sprintf("%d", abs)
	}
	if neg {
		s = "-" + s
	}
	return s
}

// render draws the full field line (prompt + optional brackets + right-aligned value)
// and positions the terminal cursor at the right edge of the numeric area.
func (n *NumericInput) render(w *bufio.Writer, value int64) {
	fmt.Fprint(w, "\r")
	if n.marginLeft > 0 {
		fmt.Fprintf(w, "\033[%dC", n.marginLeft)
	}
	colorWrap(w, n.promptColor, n.prompt)
	if n.inputColor != "" {
		fmt.Fprint(w, string(n.inputColor))
	}
	if n.showBrackets {
		fmt.Fprint(w, "[")
	}
	// Render with continuous underline so the full field width is always visible.
	fmt.Fprintf(w, "\033[4m%s\033[24m", n.formatDisplay(value))
	if n.showBrackets {
		fmt.Fprint(w, "]")
	}
	if n.inputColor != "" {
		fmt.Fprint(w, colorReset)
	}
	fmt.Fprint(w, "\033[K")
	// position cursor at the rightmost digit column
	col := n.promptRuneLen() + n.bracketOffset() + n.fieldWidth()
	fmt.Fprint(w, "\r")
	if total := n.marginLeft + col; total > 0 {
		fmt.Fprintf(w, "\033[%dC", total)
	}
}

// renderClean draws the field without placeholder decorations or brackets.
func (n *NumericInput) renderClean(w *bufio.Writer, value int64) {
	fmt.Fprint(w, "\r")
	if n.marginLeft > 0 {
		fmt.Fprintf(w, "\033[%dC", n.marginLeft)
	}
	colorWrap(w, n.promptColor, n.prompt)
	if n.inputColor != "" {
		fmt.Fprint(w, string(n.inputColor))
	}
	fmt.Fprint(w, n.formatClean(value))
	if n.inputColor != "" {
		fmt.Fprint(w, colorReset)
	}
	fmt.Fprint(w, "\033[K")
}

// handleKey processes one raw keystroke and updates value.
func (n *NumericInput) handleKey(b []byte, value *int64, pending *[]byte, w *bufio.Writer) keyResult {
	if len(*pending) > 0 {
		b = append(*pending, b...)
		*pending = nil
	}
	nb := len(b)
	switch {
	case nb == 1 && (b[0] == '\r' || b[0] == '\n'):
		return keyConfirm
	case nb == 1 && b[0] == 3:
		return keyInterrupt
	case nb == 1 && b[0] == 4:
		if *value == 0 {
			return keyEOF
		}
	case nb == 1 && b[0] == 9:
		return keyNext
	case nb == 3 && b[0] == 27 && b[1] == '[' && b[2] == 'Z':
		return keyPrev
	case nb >= 3 && b[0] == 27 && b[1] == '[' && b[2] == 'A':
		return keyUp
	case nb >= 3 && b[0] == 27 && b[1] == '[' && b[2] == 'B':
		return keyDown
	case nb == 1 && (b[0] == 127 || b[0] == 8): // Backspace
		*value /= 10
		n.render(w, *value)
	case nb == 1 && b[0] == 21: // Ctrl-U: clear
		*value = 0
		n.render(w, *value)
	case nb == 1 && b[0] == '-' && n.allowNeg:
		*value = -*value
		n.render(w, *value)
	case nb == 1 && b[0] >= '0' && b[0] <= '9':
		digit := int64(b[0] - '0')
		var newVal int64
		if *value < 0 {
			newVal = *value*10 - digit
		} else {
			newVal = *value*10 + digit
		}
		abs := newVal
		if abs < 0 {
			abs = -abs
		}
		if abs <= n.maxAbsValue() {
			*value = newVal
			n.render(w, *value)
		}
	}
	return keyNone
}

// ── formField implementation ──────────────────────────────────────────────────

// numericField wraps *NumericInput with its mutable state for use inside a Form.
type numericField struct {
	n     *NumericInput
	value int64
}

func (f *numericField) fieldInit() {
	f.value = f.n.parseDefault()
}

func (f *numericField) fieldRender(w *bufio.Writer) {
	f.n.render(w, f.value)
}

func (f *numericField) fieldKey(b []byte, pending *[]byte, w *bufio.Writer) keyResult {
	return f.n.handleKey(b, &f.value, pending, w)
}

func (f *numericField) fieldRenderClean(w *bufio.Writer) {
	f.n.renderClean(w, f.value)
}

func (f *numericField) fieldValue() string {
	return f.n.formatClean(f.value)
}

func (f *numericField) fieldSetValue(val string) {
	saved := f.n.defaultVal
	f.n.defaultVal = val
	f.value = f.n.parseDefault()
	f.n.defaultVal = saved
}

func (f *numericField) setMarginLeft(n int) { f.n.marginLeft = n }
