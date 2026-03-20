// Package ginput – F-key byte sequences for terminal raw mode.
package ginput

// fnKeySeqs returns the raw byte sequences that a terminal sends when the user
// presses function key n (1–12) in raw mode.
//
// Most terminals send one of two sequences for F1–F4 depending on their
// terminfo settings, so both variants are returned for those keys.
//
// Common sequences (xterm / VTE / most Linux terminals):
//
//	F1  → ESC O P        (or ESC [ 1 1 ~)
//	F2  → ESC O Q        (or ESC [ 1 2 ~)
//	F3  → ESC O R        (or ESC [ 1 3 ~)
//	F4  → ESC O S        (or ESC [ 1 4 ~)
//	F5  → ESC [ 1 5 ~
//	F6  → ESC [ 1 7 ~
//	F7  → ESC [ 1 8 ~
//	F8  → ESC [ 1 9 ~
//	F9  → ESC [ 2 0 ~
//	F10 → ESC [ 2 1 ~
//	F11 → ESC [ 2 3 ~
//	F12 → ESC [ 2 4 ~
func fnKeySeqs(n int) [][]byte {
	switch n {
	case 1:
		return [][]byte{[]byte("\x1bOP"), []byte("\x1b[11~")}
	case 2:
		return [][]byte{[]byte("\x1bOQ"), []byte("\x1b[12~")}
	case 3:
		return [][]byte{[]byte("\x1bOR"), []byte("\x1b[13~")}
	case 4:
		return [][]byte{[]byte("\x1bOS"), []byte("\x1b[14~")}
	case 5:
		return [][]byte{[]byte("\x1b[15~")}
	case 6:
		return [][]byte{[]byte("\x1b[17~")}
	case 7:
		return [][]byte{[]byte("\x1b[18~")}
	case 8:
		return [][]byte{[]byte("\x1b[19~")}
	case 9:
		return [][]byte{[]byte("\x1b[20~")}
	case 10:
		return [][]byte{[]byte("\x1b[21~")}
	case 11:
		return [][]byte{[]byte("\x1b[23~")}
	case 12:
		return [][]byte{[]byte("\x1b[24~")}
	default:
		return nil
	}
}
