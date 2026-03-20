// Package ginput provides a single-line text input widget for text terminals.
//
// It puts the terminal into raw mode, enforces a maximum rune count, and
// supports basic line-editing keys:
//
//   - Enter           – confirm input
//   - Ctrl-C          – return ErrInterrupt
//   - Ctrl-D          – return ErrEOF (only on empty input)
//   - Backspace       – delete character before the cursor
//   - Delete          – delete character under the cursor
//   - ← / →           – move cursor left / right
//   - Home / Ctrl-A   – move cursor to start
//   - End  / Ctrl-E   – move cursor to end
//   - Ctrl-K          – delete from cursor to end of line
//   - Ctrl-U          – delete the entire line
package ginput

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

// Sentinel errors returned by Read.
var (
	// ErrInterrupt is returned when the user presses Ctrl-C.
	ErrInterrupt = errors.New("interrupted")
	// ErrEOF is returned when the user presses Ctrl-D on an empty line.
	ErrEOF = errors.New("EOF")
)

// Color is an ANSI terminal color / SGR style attribute.
// The zero value (empty string) means "no color, use the terminal default".
// Use the predefined Color* constants or Color256 for 256-color entries.
type Color string

// colorReset is the ANSI escape sequence that resets all SGR attributes.
const colorReset = "\033[0m"

// Predefined foreground colors (standard ANSI 16-color palette).
const (
	ColorDefault Color = "" // no escape sequence; terminal default
	ColorBold    Color = "\033[1m"

	ColorBlack   Color = "\033[30m"
	ColorRed     Color = "\033[31m"
	ColorGreen   Color = "\033[32m"
	ColorYellow  Color = "\033[33m"
	ColorBlue    Color = "\033[34m"
	ColorMagenta Color = "\033[35m"
	ColorCyan    Color = "\033[36m"
	ColorWhite   Color = "\033[37m"

	ColorBrightBlack   Color = "\033[90m"
	ColorBrightRed     Color = "\033[91m"
	ColorBrightGreen   Color = "\033[92m"
	ColorBrightYellow  Color = "\033[93m"
	ColorBrightBlue    Color = "\033[94m"
	ColorBrightMagenta Color = "\033[95m"
	ColorBrightCyan    Color = "\033[96m"
	ColorBrightWhite   Color = "\033[97m"
)

// Color256 returns the foreground Color for a 256-color palette entry (0–255).
func Color256(n int) Color { return Color(fmt.Sprintf("\033[38;5;%dm", n)) }

// colorWrap writes c's escape code, the text s, and the reset sequence to w.
// When c is ColorDefault (empty string) it just writes s with no escape codes.
func colorWrap(w io.Writer, c Color, s string) {
	if c == "" {
		fmt.Fprint(w, s)
		return
	}
	fmt.Fprint(w, string(c))
	fmt.Fprint(w, s)
	fmt.Fprint(w, colorReset)
}

// keyResult is the signal returned by handleKey to the calling read loop.
type keyResult int

const (
	keyNone      keyResult = iota
	keyConfirm             // Enter – confirm the current value
	keyInterrupt           // Ctrl-C – abort
	keyEOF                 // Ctrl-D on empty buffer
	keyNext                // Tab – move to the next field (Form only)
	keyPrev                // Shift-Tab – move to the previous field (Form only)
	keyUp                  // ↑ arrow – move to the previous field (Form only)
	keyDown                // ↓ arrow – move to the next field (Form only)
)

// Input is a single-line text input field that limits the number of runes
// the user may enter to a configurable maximum.
type Input struct {
	maxLen       int
	prompt       string
	defaultVal   string
	mask         rune                    // 0 means no masking
	placeholder  rune                    // fill char for empty positions; 0 = continuous underline (default)
	showField    bool                    // show placeholder chars up to maxLen
	showBrackets bool                    // surround the field with '[' and ']'
	validator    func(rune, []rune) bool // optional per-rune validator
	promptColor  Color                   // ANSI color for the prompt text
	inputColor   Color                   // ANSI color for the editable area
	in           *os.File                // input source (default os.Stdin)
	out          io.Writer               // output destination (default os.Stdout)
	marginLeft   int                     // left column offset applied by the parent Form
}

// New returns an Input that accepts at most maxLen runes.
// maxLen must be >= 1.
func New(maxLen int) *Input {
	if maxLen < 1 {
		maxLen = 1
	}
	return &Input{
		maxLen:    maxLen,
		showField: true,
		in:        os.Stdin,
		out:       os.Stdout,
	}
}

// WithPrompt sets the text displayed before the editable area.
// Returns the same *Input so calls can be chained.
func (inp *Input) WithPrompt(prompt string) *Input {
	inp.prompt = prompt
	return inp
}

// WithMask sets the rune printed in place of every typed character (e.g. '*'
// for password fields).  Pass 0 to disable masking (the default).
// Returns the same *Input so calls can be chained.
func (inp *Input) WithMask(mask rune) *Input {
	inp.mask = mask
	return inp
}

// WithField enables the fixed-width field display: the input area always
// occupies maxLen columns, with typed characters followed by a continuous
// underline for the remaining positions (or a custom rune if WithPlaceholder
// has been called).
// Returns the same *Input so calls can be chained.
func (inp *Input) WithField() *Input {
	inp.showField = true
	return inp
}

// WithBrackets surrounds the input field with '[' and ']' delimiters.
// It implicitly enables the fixed-width field display (same as WithField).
// Returns the same *Input so calls can be chained.
func (inp *Input) WithBrackets() *Input {
	inp.showBrackets = true
	inp.showField = true
	return inp
}

// WithDefault sets the initial value pre-filled in the input field.
// The user can edit or delete it before confirming.
// If the value is longer than maxLen it is truncated to maxLen runes.
// Returns the same *Input so calls can be chained.
func (inp *Input) WithDefault(val string) *Input {
	runes := []rune(val)
	if len(runes) > inp.maxLen {
		runes = runes[:inp.maxLen]
	}
	inp.defaultVal = string(runes)
	return inp
}

// WithPlaceholder sets the rune used to fill empty positions when field display
// is active. By default (when this method is not called) empty positions are
// rendered as a continuous ANSI underline on spaces, which looks like a solid
// underline bar. Pass any rune (e.g. '·' or '_') to use a literal character
// instead.
// Returns the same *Input so calls can be chained.
func (inp *Input) WithPlaceholder(r rune) *Input {
	inp.placeholder = r
	return inp
}

// WithValidator sets a function called before each character is inserted.
// If it returns false the character is silently rejected.
// The function receives the candidate rune and the current buffer contents.
// Returns the same *Input so calls can be chained.
func (inp *Input) WithValidator(fn func(rune, []rune) bool) *Input {
	inp.validator = fn
	return inp
}

// WithPromptColor sets the ANSI color used to render the prompt text.
// Use one of the Color* constants or Color256.
// Returns the same *Input so calls can be chained.
func (inp *Input) WithPromptColor(c Color) *Input {
	inp.promptColor = c
	return inp
}

// WithInputColor sets the ANSI color used to render the editable area
// (brackets, typed characters, and placeholder characters).
// Returns the same *Input so calls can be chained.
func (inp *Input) WithInputColor(c Color) *Input {
	inp.inputColor = c
	return inp
}

// WithInput sets the file used for reading keystrokes (default: os.Stdin).
// Must be a *os.File because terminal raw mode requires a file descriptor.
// Returns the same *Input so calls can be chained.
func (inp *Input) WithInput(f *os.File) *Input {
	inp.in = f
	return inp
}

// WithOutput sets the writer used for rendering the input field (default: os.Stdout).
// Returns the same *Input so calls can be chained.
func (inp *Input) WithOutput(w io.Writer) *Input {
	inp.out = w
	return inp
}

// handleKey processes one raw byte sequence for the given mutable state.
// Any partial UTF-8 multi-byte sequence is preserved in *pending and
// prepended on the next call. Output is written to w; the caller must flush w.
// Tab returns keyNext; Shift-Tab (ESC[Z) returns keyPrev.
// ↑ arrow (ESC[A) returns keyUp; ↓ arrow (ESC[B) returns keyDown.
func (inp *Input) handleKey(b []byte, buf *[]rune, cursor *int, pending *[]byte, w *bufio.Writer) keyResult {
	// Prepend any previously accumulated partial sequence.
	if len(*pending) > 0 {
		b = append(*pending, b...)
		*pending = nil
	}
	n := len(b)

	switch {
	// ── Confirm ──────────────────────────────────────────────────────────────
	case n == 1 && (b[0] == '\r' || b[0] == '\n'):
		return keyConfirm

	// ── Ctrl-C ───────────────────────────────────────────────────────────────
	case n == 1 && b[0] == 3:
		return keyInterrupt

	// ── Ctrl-D (EOF on empty buffer) ─────────────────────────────────────────
	case n == 1 && b[0] == 4:
		if len(*buf) == 0 {
			return keyEOF
		}

	// ── Tab → next field ─────────────────────────────────────────────────────
	case n == 1 && b[0] == 9:
		return keyNext

	// ── Shift-Tab → previous field: ESC [ Z ──────────────────────────────────
	case n == 3 && b[0] == 27 && b[1] == '[' && b[2] == 'Z':
		return keyPrev

	// ── Backspace ────────────────────────────────────────────────────────────
	case n == 1 && (b[0] == 127 || b[0] == 8):
		if *cursor > 0 {
			*buf = append((*buf)[:*cursor-1], (*buf)[*cursor:]...)
			*cursor--
			inp.redraw(w, *buf, *cursor)
		}

	// ── Ctrl-A → Home ────────────────────────────────────────────────────────
	case n == 1 && b[0] == 1:
		*cursor = 0
		inp.moveCursorAbs(w, inp.promptRuneLen()+inp.bracketOffset())

	// ── Ctrl-E → End ─────────────────────────────────────────────────────────
	case n == 1 && b[0] == 5:
		*cursor = len(*buf)
		inp.moveCursorAbs(w, inp.promptRuneLen()+inp.bracketOffset()+*cursor)

	// ── Ctrl-K → kill to end ─────────────────────────────────────────────────
	case n == 1 && b[0] == 11:
		*buf = (*buf)[:*cursor]
		inp.redraw(w, *buf, *cursor)

	// ── Ctrl-U → kill line ───────────────────────────────────────────────────
	case n == 1 && b[0] == 21:
		*buf = (*buf)[:0]
		*cursor = 0
		inp.redraw(w, *buf, *cursor)

	// ── ANSI escape sequences ─────────────────────────────────────────────────
	case n >= 3 && b[0] == 27 && b[1] == '[':
		base := inp.promptRuneLen() + inp.bracketOffset()
		switch b[2] {
		case 'A': // ↑ up arrow → previous field
			return keyUp
		case 'B': // ↓ down arrow → next field
			return keyDown
		case 'D': // ← left arrow
			if *cursor > 0 {
				*cursor--
				inp.moveCursorAbs(w, base+*cursor)
			}
		case 'C': // → right arrow
			if *cursor < len(*buf) {
				*cursor++
				inp.moveCursorAbs(w, base+*cursor)
			}
		case 'H': // Home (xterm)
			*cursor = 0
			inp.moveCursorAbs(w, base)
		case 'F': // End (xterm)
			*cursor = len(*buf)
			inp.moveCursorAbs(w, base+*cursor)
		case '1': // Home (rxvt: ESC [ 1 ~)
			if n >= 4 && b[3] == '~' {
				*cursor = 0
				inp.moveCursorAbs(w, base)
			}
		case '4': // End (rxvt: ESC [ 4 ~)
			if n >= 4 && b[3] == '~' {
				*cursor = len(*buf)
				inp.moveCursorAbs(w, base+*cursor)
			}
		case '3': // Delete key: ESC [ 3 ~
			if n >= 4 && b[3] == '~' && *cursor < len(*buf) {
				*buf = append((*buf)[:*cursor], (*buf)[*cursor+1:]...)
				inp.redraw(w, *buf, *cursor)
			}
		}

	// ── Regular printable character ───────────────────────────────────────────
	default:
		if len(*buf) < inp.maxLen {
			// Accumulate partial UTF-8 multi-byte sequences.
			if !utf8.FullRune(b) {
				*pending = append((*pending)[:0], b...)
				return keyNone
			}
			r, size := utf8.DecodeRune(b)
			if r != utf8.RuneError && size > 0 && r >= 32 {
				if inp.validator == nil || inp.validator(r, *buf) {
					*buf = append(*buf, 0)
					copy((*buf)[*cursor+1:], (*buf)[*cursor:])
					(*buf)[*cursor] = r
					*cursor++
					inp.redraw(w, *buf, *cursor)
				}
			}
		}
	}
	return keyNone
}

// Read puts the terminal into raw mode and reads a line of at most maxLen
// runes.  The entered text is returned when the user presses Enter.
//
// Errors:
//   - ErrInterrupt  when Ctrl-C is pressed.
//   - ErrEOF        when Ctrl-D is pressed on an empty input.
//   - Any OS-level read error.
func (inp *Input) Read() (string, error) {
	fd := int(inp.in.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return "", fmt.Errorf("ginput: make raw: %w", err)
	}
	defer term.Restore(fd, oldState) //nolint:errcheck

	w := bufio.NewWriter(inp.out)

	buf := []rune(inp.defaultVal)
	if cap(buf) < inp.maxLen {
		tmp := make([]rune, len(buf), inp.maxLen)
		copy(tmp, buf)
		buf = tmp
	}
	cursor := len(buf)

	fmt.Fprint(w, inp.prompt)
	inp.redraw(w, buf, cursor)
	w.Flush()

	var pending []byte
	rb := make([]byte, 16)
	for {
		n, err := inp.in.Read(rb)
		if err != nil {
			return string(buf), err
		}
		switch inp.handleKey(rb[:n], &buf, &cursor, &pending, w) {
		case keyConfirm:
			if inp.showField {
				inp.redrawClean(w, buf)
			}
			fmt.Fprint(w, "\r\n")
			w.Flush()
			return string(buf), nil
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

// ReadString is a package-level convenience wrapper.
// It creates an Input with the given maximum length and calls Read.
func ReadString(maxLen int) (string, error) {
	return New(maxLen).Read()
}

// ClearScreen writes the ANSI clear-screen + cursor-home sequence to w,
// presenting a blank terminal. Typically called before Form.Read() to start
// on a clean screen. Pass os.Stdout for the default terminal output.
func ClearScreen(w io.Writer) {
	fmt.Fprint(w, "\033[2J\033[H")
}

// ── internal helpers ─────────────────────────────────────────────────────────

func (inp *Input) promptRuneLen() int {
	return utf8.RuneCountInString(inp.prompt)
}

// bracketOffset returns 1 when brackets are enabled (accounts for the leading '[').
func (inp *Input) bracketOffset() int {
	if inp.showBrackets {
		return 1
	}
	return 0
}

// redrawClean redraws the field showing only the prompt and entered text,
// without placeholder characters or bracket delimiters.
// Used on confirmation to remove UI chrome before the final newline.
func (inp *Input) redrawClean(w io.Writer, buf []rune) {
	fmt.Fprint(w, "\r")
	if inp.marginLeft > 0 {
		fmt.Fprintf(w, "\033[%dC", inp.marginLeft)
	}
	colorWrap(w, inp.promptColor, inp.prompt)
	if inp.inputColor != "" {
		fmt.Fprint(w, string(inp.inputColor))
	}
	if inp.mask != 0 {
		for range buf {
			fmt.Fprintf(w, "%c", inp.mask)
		}
	} else {
		fmt.Fprint(w, string(buf))
	}
	if inp.inputColor != "" {
		fmt.Fprint(w, colorReset)
	}
	fmt.Fprint(w, "\033[K")
}

// redraw repaints the full input line and repositions the cursor at `cursor`.
func (inp *Input) redraw(w io.Writer, buf []rune, cursor int) {
	fmt.Fprint(w, "\r")
	if inp.marginLeft > 0 {
		fmt.Fprintf(w, "\033[%dC", inp.marginLeft)
	}
	colorWrap(w, inp.promptColor, inp.prompt)
	if inp.inputColor != "" {
		fmt.Fprint(w, string(inp.inputColor))
	}
	if inp.showBrackets {
		fmt.Fprint(w, "[")
	}
	if inp.mask != 0 {
		for range buf {
			fmt.Fprintf(w, "%c", inp.mask)
		}
	} else {
		fmt.Fprint(w, string(buf))
	}
	if inp.showField {
		remaining := inp.maxLen - len(buf)
		if remaining > 0 {
			if inp.placeholder == 0 {
				// Default: continuous underline on spaces.
				fmt.Fprintf(w, "\033[4m%s\033[24m", strings.Repeat(" ", remaining))
			} else {
				for i := 0; i < remaining; i++ {
					fmt.Fprintf(w, "%c", inp.placeholder)
				}
			}
		}
		if inp.showBrackets {
			fmt.Fprint(w, "]")
		}
		if inp.inputColor != "" {
			fmt.Fprint(w, colorReset)
		}
	} else {
		if inp.inputColor != "" {
			fmt.Fprint(w, colorReset)
		}
		fmt.Fprint(w, "\033[K")
	}
	inp.moveCursorAbs(w, inp.promptRuneLen()+inp.bracketOffset()+cursor)
}

// moveCursorAbs positions the terminal cursor at column col (0-based),
// offset by the field's marginLeft.
func (inp *Input) moveCursorAbs(w io.Writer, col int) {
	fmt.Fprint(w, "\r")
	if total := inp.marginLeft + col; total > 0 {
		fmt.Fprintf(w, "\033[%dC", total)
	}
}
