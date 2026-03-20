// Package ginput – FormDef: JSON-driven form builder.
package ginput

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FieldDef describes a single form field in a JSON definition.
//
// JSON example (text field):
//
//	{ "key": "email", "prompt": "Email: ", "maxLen": 40, "brackets": true }
//
// JSON example (numeric field):
//
//	{ "key": "price", "prompt": "Price: ", "type": "numeric", "maxIntegers": 5, "decimals": 2, "brackets": true }
//
// JSON example (label):
//
//	{ "type": "label", "key": "status", "prompt": "Status: ", "promptColor": "brightBlack", "text": "ready", "textColor": "green" }
//
// JSON example (separator):
//
//	{ "type": "separator" }
type FieldDef struct {
	Key         string   `json:"key"`
	Type        string   `json:"type"` // "text" (default), "numeric", "label", or "separator"
	Prompt      string   `json:"prompt"`
	Text        string   `json:"text,omitempty"`      // label: message content
	TextColor   string   `json:"textColor,omitempty"` // label: ANSI color for message
	MaxLen      int      `json:"maxLen"`              // text: max runes; numeric: fallback for maxIntegers if omitted
	MaxIntegers int      `json:"maxIntegers"`         // numeric: max digits before the decimal point
	Decimals    int      `json:"decimals"`            // numeric: digits after the decimal point
	Negative    bool     `json:"negative"`            // numeric: allow negative values
	Brackets    bool     `json:"brackets"`
	Field       bool     `json:"field"`
	Placeholder string   `json:"placeholder"` // single rune as string; omit for default ('_')
	Mask        string   `json:"mask"`        // single rune as string; omit or "" for no mask
	Default     string   `json:"default"`
	Validators  []string `json:"validators,omitempty"` // validator expressions applied to text fields
	PromptColor string   `json:"promptColor,omitempty"`
	InputColor  string   `json:"inputColor,omitempty"`
	OffsetX     int      `json:"offsetX,omitempty"` // extra left-column margin for this field only
}

// FormDef is the top-level JSON structure for defining a form.
//
// JSON example:
//
//	{
//	  "fields": [
//	    { "key": "user", "prompt": "User: ", "maxLen": 20, "brackets": true },
//	    { "key": "pass", "prompt": "Pass: ", "maxLen": 32, "brackets": true, "mask": "*" }
//	  ]
//	}
//
// submitKey is the ASCII code of the key that submits the form.
// Omit or set to 0 to use the default (13 = Enter).
// Use submitFn (1–12) to set an F-key instead of a printable key.
type FormDef struct {
	SubmitKey      int        `json:"submitKey,omitempty"`
	SubmitFn       int        `json:"submitFn,omitempty"`       // F-key number 1-12
	Header         string     `json:"header,omitempty"`         // optional static text rendered above the fields
	HeaderColor    string     `json:"headerColor,omitempty"`    // ANSI color name for the header
	Footer         string     `json:"footer,omitempty"`         // optional static text rendered below the fields
	FooterColor    string     `json:"footerColor,omitempty"`    // ANSI color name for the footer
	StatusColor    string     `json:"statusColor,omitempty"`    // ANSI color for the status line; setting this also reserves the status area
	LabelColor     string     `json:"labelColor,omitempty"`     // default prompt color for all fields
	InputColor     string     `json:"inputColor,omitempty"`     // default editable-area color for all fields
	OffsetX        int        `json:"offsetX,omitempty"`        // left-column margin applied to the entire form
	OffsetY        int        `json:"offsetY,omitempty"`        // blank lines printed above the form
	ContentOffsetX int        `json:"contentOffsetX,omitempty"` // extra left margin applied only to fields (not header/footer)
	Fields         []FieldDef `json:"fields"`
}

// parseJSONColor converts a human-readable color name (as used in JSON form
// definitions) to a Color value. Accepted names are the standard 16 ANSI
// foreground colors plus "bold". An unrecognised string is passed through as a
// raw ANSI escape sequence. An empty string returns ColorDefault.
func parseJSONColor(s string) Color {
	switch strings.ToLower(s) {
	case "", "default":
		return ColorDefault
	case "bold":
		return ColorBold
	case "black":
		return ColorBlack
	case "red":
		return ColorRed
	case "green":
		return ColorGreen
	case "yellow":
		return ColorYellow
	case "blue":
		return ColorBlue
	case "magenta":
		return ColorMagenta
	case "cyan":
		return ColorCyan
	case "white":
		return ColorWhite
	case "brightblack", "gray", "grey":
		return ColorBrightBlack
	case "brightred":
		return ColorBrightRed
	case "brightgreen":
		return ColorBrightGreen
	case "brightyellow":
		return ColorBrightYellow
	case "brightblue":
		return ColorBrightBlue
	case "brightmagenta":
		return ColorBrightMagenta
	case "brightcyan":
		return ColorBrightCyan
	case "brightwhite":
		return ColorBrightWhite
	default:
		// treat as a raw ANSI escape sequence
		return Color(s)
	}
}

// NewFormFromJSON parses a JSON form definition and returns a configured *Form
// ready to call Read() on.
//
// Returns an error if the JSON is malformed or any field definition is invalid.
func NewFormFromJSON(data []byte) (*Form, error) {
	var def FormDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("ginput: parse form JSON: %w", err)
	}
	return NewFormFromDef(def)
}

// ApplyDefaults sets the Default field of each FieldDef whose key exists in
// values. It modifies the receiver in place and returns it for chaining.
//
//	var def ginput.FormDef
//	json.Unmarshal(data, &def)
//	def.ApplyDefaults(saved)
//	form, err := ginput.NewFormFromDef(def)
func (def *FormDef) ApplyDefaults(values map[string]string) *FormDef {
	for i := range def.Fields {
		if v, ok := values[def.Fields[i].Key]; ok {
			def.Fields[i].Default = v
		}
	}
	return def
}

// NewFormFromDef builds a *Form from a FormDef value.
// Useful when constructing the definition programmatically instead of from raw JSON.
func NewFormFromDef(def FormDef) (*Form, error) {
	f := NewForm()
	if def.SubmitKey != 0 {
		f.WithSubmitKey(byte(def.SubmitKey))
	} else if def.SubmitFn >= 1 && def.SubmitFn <= 12 {
		f.WithSubmitFn(def.SubmitFn)
	}
	if def.Footer != "" {
		f.WithFooter(def.Footer)
	}
	if def.FooterColor != "" {
		f.WithFooterColor(parseJSONColor(def.FooterColor))
	}
	if def.StatusColor != "" {
		f.WithStatusColor(parseJSONColor(def.StatusColor))
	}
	if def.Header != "" {
		f.WithHeader(def.Header)
	}
	if def.HeaderColor != "" {
		f.WithHeaderColor(parseJSONColor(def.HeaderColor))
	}
	if def.LabelColor != "" {
		f.WithLabelColor(parseJSONColor(def.LabelColor))
	}
	if def.InputColor != "" {
		f.WithInputColor(parseJSONColor(def.InputColor))
	}
	if def.OffsetX != 0 {
		f.WithOffsetX(def.OffsetX)
	}
	if def.OffsetY != 0 {
		f.WithOffsetY(def.OffsetY)
	}
	if def.ContentOffsetX != 0 {
		f.WithContentOffsetX(def.ContentOffsetX)
	}
	for i, fd := range def.Fields {
		// key is required only for interactive (text/numeric) fields
		if fd.Key == "" && fd.Type != "label" && fd.Type != "separator" {
			return nil, fmt.Errorf("ginput: field %d: key is required", i)
		}
		switch fd.Type {
		case "", "text":
			if fd.MaxLen < 1 {
				return nil, fmt.Errorf("ginput: field %q: maxLen must be >= 1", fd.Key)
			}
			inp := New(fd.MaxLen)
			if fd.Prompt != "" {
				inp.WithPrompt(fd.Prompt)
			}
			if fd.Brackets {
				inp.WithBrackets()
			} else if fd.Field {
				inp.WithField()
			}
			if fd.Placeholder != "" {
				r := []rune(fd.Placeholder)
				if len(r) != 1 {
					return nil, fmt.Errorf("ginput: field %q: placeholder must be a single character", fd.Key)
				}
				inp.WithPlaceholder(r[0])
			}
			if fd.Mask != "" {
				r := []rune(fd.Mask)
				if len(r) != 1 {
					return nil, fmt.Errorf("ginput: field %q: mask must be a single character", fd.Key)
				}
				inp.WithMask(r[0])
			}
			if fd.Default != "" {
				inp.WithDefault(fd.Default)
			}
			if len(fd.Validators) > 0 {
				v, err := resolveValidators(fd.Key, fd.Validators)
				if err != nil {
					return nil, err
				}
				inp.WithValidator(v)
			}
			if fd.PromptColor != "" {
				inp.WithPromptColor(parseJSONColor(fd.PromptColor))
			}
			if fd.InputColor != "" {
				inp.WithInputColor(parseJSONColor(fd.InputColor))
			}
			f.Add(fd.Key, inp)
			if fd.OffsetX != 0 {
				f.WithFieldOffset(fd.Key, fd.OffsetX)
			}

		case "numeric":
			maxInt := fd.MaxIntegers
			if maxInt < 1 {
				maxInt = fd.MaxLen
			}
			if maxInt < 1 {
				return nil, fmt.Errorf("ginput: field %q: maxIntegers (or maxLen) must be >= 1 for numeric type", fd.Key)
			}
			n := NewNumeric(maxInt, fd.Decimals)
			if fd.Prompt != "" {
				n.WithPrompt(fd.Prompt)
			}
			if fd.Brackets {
				n.WithBrackets()
			}
			if fd.Negative {
				n.WithNegative()
			}
			if fd.Default != "" {
				n.WithDefault(fd.Default)
			}
			if fd.PromptColor != "" {
				n.WithPromptColor(parseJSONColor(fd.PromptColor))
			}
			if fd.InputColor != "" {
				n.WithInputColor(parseJSONColor(fd.InputColor))
			}
			f.AddNumeric(fd.Key, n)
			if fd.OffsetX != 0 {
				f.WithFieldOffset(fd.Key, fd.OffsetX)
			}

		case "label":
			lbl := NewLabel(fd.Prompt, fd.Text)
			if fd.PromptColor != "" {
				lbl.WithLabelColor(parseJSONColor(fd.PromptColor))
			}
			if fd.TextColor != "" {
				lbl.WithTextColor(parseJSONColor(fd.TextColor))
			}
			f.AddLabel(fd.Key, lbl)
			if fd.OffsetX != 0 {
				f.WithFieldOffset(fd.Key, fd.OffsetX)
			}

		case "separator":
			f.AddSeparator()

		default:
			return nil, fmt.Errorf("ginput: field %q: unknown type %q (want \"text\" or \"numeric\")", fd.Key, fd.Type)
		}
	}
	return f, nil
}

// resolveValidators resolves a slice of validator expressions for the field
// identified by key and returns a single combined validator.
// Multiple expressions are AND-combined via ValidAll.
//
// Supported expressions:
//
//	"digits"           → ValidDigits
//	"letters"          → ValidLetters
//	"alphaNum"         → ValidAlphaNum
//	"uppercase"        → ValidUppercase
//	"lowercase"        → ValidLowercase
//	"ascii"            → ValidASCII
//	"hex"              → ValidHex
//	"noSpace"          → ValidNoSpace
//	"integer"          → ValidInteger()
//	"decimal"          → ValidDecimal('.') (dot separator)
//	"decimal:<sep>"    → ValidDecimal with the given single-character separator
//	"allow:<chars>"    → ValidAllowRunes(chars)
//	"reject:<chars>"   → ValidRejectRunes(chars)
func resolveValidators(key string, exprs []string) (func(rune, []rune) bool, error) {
	vs := make([]func(rune, []rune) bool, 0, len(exprs))
	for _, expr := range exprs {
		v, err := resolveValidator(expr)
		if err != nil {
			return nil, fmt.Errorf("ginput: field %q: %w", key, err)
		}
		vs = append(vs, v)
	}
	if len(vs) == 1 {
		return vs[0], nil
	}
	return ValidAll(vs...), nil
}

func resolveValidator(expr string) (func(rune, []rune) bool, error) {
	switch {
	case expr == "digits":
		return ValidDigits, nil
	case expr == "letters":
		return ValidLetters, nil
	case expr == "alphaNum":
		return ValidAlphaNum, nil
	case expr == "uppercase":
		return ValidUppercase, nil
	case expr == "lowercase":
		return ValidLowercase, nil
	case expr == "ascii":
		return ValidASCII, nil
	case expr == "hex":
		return ValidHex, nil
	case expr == "noSpace":
		return ValidNoSpace, nil
	case expr == "integer":
		return ValidInteger(), nil
	case expr == "decimal":
		return ValidDecimal('.'), nil
	case strings.HasPrefix(expr, "decimal:"):
		sep := []rune(strings.TrimPrefix(expr, "decimal:"))
		if len(sep) != 1 {
			return nil, fmt.Errorf("validator %q: separator must be a single character", expr)
		}
		return ValidDecimal(sep[0]), nil
	case strings.HasPrefix(expr, "allow:"):
		return ValidAllowRunes(strings.TrimPrefix(expr, "allow:")), nil
	case strings.HasPrefix(expr, "reject:"):
		return ValidRejectRunes(strings.TrimPrefix(expr, "reject:")), nil
	default:
		return nil, fmt.Errorf("unknown validator %q", expr)
	}
}
