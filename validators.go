// Package ginput – predefined per-rune validators for use with WithValidator.
package ginput

import "unicode"

// ── Simple validators ─────────────────────────────────────────────────────────
//
// Each variable holds a func(rune, []rune) bool ready to pass to WithValidator.
// They ignore the current buffer contents.
//
// Example:
//
//	ginput.New(6).WithValidator(ginput.ValidDigits).Read()

// ValidDigits accepts only ASCII digit characters (0–9).
var ValidDigits = func(r rune, _ []rune) bool {
	return r >= '0' && r <= '9'
}

// ValidLetters accepts only Unicode letters.
var ValidLetters = func(r rune, _ []rune) bool {
	return unicode.IsLetter(r)
}

// ValidAlphaNum accepts Unicode letters or ASCII digits.
var ValidAlphaNum = func(r rune, _ []rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// ValidUppercase accepts only uppercase Unicode letters.
var ValidUppercase = func(r rune, _ []rune) bool {
	return unicode.IsUpper(r)
}

// ValidLowercase accepts only lowercase Unicode letters.
var ValidLowercase = func(r rune, _ []rune) bool {
	return unicode.IsLower(r)
}

// ValidASCII accepts any printable ASCII character (U+0020–U+007E).
var ValidASCII = func(r rune, _ []rune) bool {
	return r >= 0x20 && r <= 0x7E
}

// ValidHex accepts hexadecimal digit characters (0–9, a–f, A–F).
var ValidHex = func(r rune, _ []rune) bool {
	return (r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'f') ||
		(r >= 'A' && r <= 'F')
}

// ValidNoSpace rejects any Unicode whitespace character.
var ValidNoSpace = func(r rune, _ []rune) bool {
	return !unicode.IsSpace(r)
}

// ── Factory validators ────────────────────────────────────────────────────────
//
// These functions return a new validator configured with the given parameters.

// ValidAllowRunes returns a validator that accepts only runes present in chars.
//
//	ginput.New(10).WithValidator(ginput.ValidAllowRunes("aeiouAEIOU")).Read()
func ValidAllowRunes(chars string) func(rune, []rune) bool {
	set := make(map[rune]struct{}, len([]rune(chars)))
	for _, r := range chars {
		set[r] = struct{}{}
	}
	return func(r rune, _ []rune) bool {
		_, ok := set[r]
		return ok
	}
}

// ValidRejectRunes returns a validator that rejects any rune present in chars.
//
//	ginput.New(20).WithValidator(ginput.ValidRejectRunes(`/\:*?"<>|`)).Read()
func ValidRejectRunes(chars string) func(rune, []rune) bool {
	set := make(map[rune]struct{}, len([]rune(chars)))
	for _, r := range chars {
		set[r] = struct{}{}
	}
	return func(r rune, _ []rune) bool {
		_, ok := set[r]
		return !ok
	}
}

// ValidInteger returns a validator that accepts signed integers:
// digits 0–9, plus an optional '-' as the very first character.
//
//	ginput.New(10).WithValidator(ginput.ValidInteger()).Read()
func ValidInteger() func(rune, []rune) bool {
	return func(r rune, buf []rune) bool {
		if r == '-' {
			return len(buf) == 0
		}
		return r >= '0' && r <= '9'
	}
}

// ValidDecimal returns a validator that accepts decimal numbers:
// digits 0–9, an optional '-' as the very first character, and
// at most one occurrence of sep as the decimal separator.
//
//	ginput.New(12).WithValidator(ginput.ValidDecimal('.')).Read()
func ValidDecimal(sep rune) func(rune, []rune) bool {
	return func(r rune, buf []rune) bool {
		if r == '-' {
			return len(buf) == 0
		}
		if r == sep {
			for _, c := range buf {
				if c == sep {
					return false // already has a separator
				}
			}
			return true
		}
		return r >= '0' && r <= '9'
	}
}

// ── Combinators ───────────────────────────────────────────────────────────────

// ValidAll returns a validator that accepts a rune only when every one of the
// given validators accepts it (logical AND).
//
//	ginput.New(20).WithValidator(ginput.ValidAll(ginput.ValidASCII, ginput.ValidNoSpace)).Read()
func ValidAll(vs ...func(rune, []rune) bool) func(rune, []rune) bool {
	return func(r rune, buf []rune) bool {
		for _, v := range vs {
			if !v(r, buf) {
				return false
			}
		}
		return true
	}
}

// ValidAny returns a validator that accepts a rune when at least one of the
// given validators accepts it (logical OR).
//
//	ginput.New(20).WithValidator(ginput.ValidAny(ginput.ValidLetters, ginput.ValidDigits)).Read()
func ValidAny(vs ...func(rune, []rune) bool) func(rune, []rune) bool {
	return func(r rune, buf []rune) bool {
		for _, v := range vs {
			if v(r, buf) {
				return true
			}
		}
		return false
	}
}
