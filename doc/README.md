# ginput

Go library for interactive terminal input — single-line text fields, numeric
fields, and multi-field forms with navigation, validation, and a status line.

Terminal is put into *raw* mode; all rendering and editing is managed internally.

---

## Table of contents

1. [Installation](#installation)
2. [Quick start](#quick-start)
3. [Single-field input (`Input`)](#single-field-input-input)
4. [Numeric field (`NumericInput`)](#numeric-field-numericinput)
5. [Validators](#validators)
6. [Interactive forms (`Form`)](#interactive-forms-form)
7. [Form layout](#form-layout)
8. [Labels and separators](#labels-and-separators)
9. [Status line](#status-line)
10. [Stay-on-form mode and `OnSubmit`](#stay-on-form-mode-and-onsubmit)
11. [Field callbacks](#field-callbacks)
12. [JSON-defined forms](#json-defined-forms)
13. [Persisting form values](#persisting-form-values)
14. [Color system](#color-system)
15. [Errors](#errors)
16. [Multi-page forms (`MultiForm`)](#multi-page-forms-multiform)
17. [Project structure](#project-structure)

---

## Installation

```bash
go get github.com/jncss/ginput
```

Only external dependency: [`golang.org/x/term`](https://pkg.go.dev/golang.org/x/term).

---

## Quick start

```go
import "github.com/jncss/ginput"
```

With chained options:

```go
text, err := ginput.New(20).
    WithPrompt("Name: ").
    WithBrackets().
    Read()
```

---

## Single-field input (`Input`)

`Input` is a single-line text field limited to a fixed number of runes.

### Constructor

| Function | Description |
|---|---|
| `New(maxLen int) *Input` | Creates a field accepting at most `maxLen` runes (min 1) |
| `ReadString(maxLen int) (string, error)` | Shorthand for `New(maxLen).Read()` |

### Configuration (chainable)

| Method | Description |
|---|---|
| `WithPrompt(s string)` | Text displayed before the field |
| `WithField()` | Shows placeholder characters up to `maxLen` |
| `WithBrackets()` | Surrounds the field with `[` `]`; implicitly enables `WithField` |
| `WithPlaceholder(r rune)` | Rune for empty positions (default `'_'`) |
| `WithMask(r rune)` | Replaces each typed character with the given rune (e.g. `'*'`). `0` to disable |
| `WithDefault(val string)` | Pre-fills the field (truncated if longer than `maxLen`) |
| `WithValidator(fn func(rune, []rune) bool)` | Per-rune filter; returns `false` to reject |
| `WithPromptColor(c Color)` | ANSI color for the prompt text |
| `WithInputColor(c Color)` | ANSI color for the editable area |
| `WithInput(f *os.File)` | Input source (default `os.Stdin`) |
| `WithOutput(w io.Writer)` | Output destination (default `os.Stdout`) |

### `Read() (string, error)`

Reads input in raw mode. Returns the entered text on **Enter**, or a sentinel error on **Ctrl-C** / **Ctrl-D**.

### Editing keys

| Key | Action |
|---|---|
| **Enter** | Confirms input |
| **Backspace** | Deletes character before cursor |
| **Delete** | Deletes character under cursor |
| **← / →** | Moves cursor left / right |
| **Home** / **Ctrl-A** | Cursor to start |
| **End** / **Ctrl-E** | Cursor to end |
| **Ctrl-K** | Delete from cursor to end |
| **Ctrl-U** | Clear entire line |
| **Ctrl-C** | `ErrInterrupt` |
| **Ctrl-D** | `ErrEOF` (only when empty) |

### Examples

```go
// With brackets and a default.
name, _ := ginput.New(20).
    WithPrompt("Name: ").
    WithBrackets().
    WithDefault("Joan").
    Read()
// Terminal: Name: [Joan________________]
```

```go
// Password.
pass, _ := ginput.New(32).WithPrompt("Password: ").WithMask('*').Read()
// Terminal: Password: ******
```

```go
// Custom placeholder.
code, _ := ginput.New(6).
    WithPrompt("Code: ").
    WithBrackets().
    WithPlaceholder('·').
    WithValidator(ginput.ValidDigits).
    Read()
// Terminal: Code: [12····]
```

---

## Numeric field (`NumericInput`)

Right-to-left numeric entry (calculator-style). Each digit shifts existing
digits left; **Backspace** removes the rightmost digit. The value is always
displayed right-aligned in a fixed-width area.

### Constructor

| Function | Description |
|---|---|
| `NewNumeric(maxIntegers, decimals int) *NumericInput` | Up to `maxIntegers` integer digits + `decimals` decimal places |

### Configuration (chainable)

| Method | Description |
|---|---|
| `WithPrompt(s string)` | Text before the field |
| `WithBrackets()` | Surrounds the field with `[` `]` |
| `WithNegative()` | Allows negative values (`-` key toggles sign) |
| `WithDefault(val string)` | Initial value (e.g. `"9.99"`) |
| `WithPromptColor(c Color)` | ANSI color for the prompt |
| `WithInputColor(c Color)` | ANSI color for the editable area |
| `WithInput(*os.File)` | Input source |
| `WithOutput(io.Writer)` | Output destination |

### `Read() (string, error)`

Returns the formatted value as a string (e.g. `"1234.56"`).

### Keys

| Key | Action |
|---|---|
| **0–9** | Append digit |
| **Backspace** | Remove rightmost digit |
| **-** | Toggle sign (only with `WithNegative`) |
| **Ctrl-U** | Reset to zero |
| **Enter / Tab / ↓** | Confirm / next field |
| **Shift-Tab / ↑** | Previous field |
| **Ctrl-C** | `ErrInterrupt` |
| **Ctrl-D** | `ErrEOF` (only when value is zero) |

### Examples

```go
// Standalone.
price, _ := ginput.NewNumeric(6, 2).
    WithPrompt("Price: ").
    WithBrackets().
    WithDefault("9.99").
    Read()
// Terminal: Price: [ 1234.56]
```

```go
// In a form.
results, _ := ginput.NewForm().
    Add("name",  ginput.New(20).WithPrompt("Name:  ").WithBrackets()).
    AddNumeric("price", ginput.NewNumeric(6, 2).WithPrompt("Price: ").WithBrackets()).
    AddNumeric("qty",   ginput.NewNumeric(4, 0).WithPrompt("Qty:   ").WithBrackets()).
    Read()
```

```json
// Via JSON.
{ "key": "price", "type": "numeric", "prompt": "Price: ",
  "maxIntegers": 6, "decimals": 2, "brackets": true, "default": "9.99" }
```

---

## Validators

Validators are `func(rune, []rune) bool` functions passed to `WithValidator`.
They receive the candidate rune and the current buffer; return `false` to reject.

### Predefined validators

| Variable | Accepts |
|---|---|
| `ValidDigits` | ASCII digits 0–9 |
| `ValidLetters` | Unicode letters |
| `ValidAlphaNum` | Unicode letters or ASCII digits |
| `ValidUppercase` | Unicode uppercase letters |
| `ValidLowercase` | Unicode lowercase letters |
| `ValidASCII` | Printable ASCII (U+0020–U+007E) |
| `ValidHex` | Hexadecimal digits (0–9, a–f, A–F) |
| `ValidNoSpace` | Any non-whitespace character |

### Factory validators

| Function | Description |
|---|---|
| `ValidAllowRunes(chars string)` | Accepts only runes in `chars` |
| `ValidRejectRunes(chars string)` | Rejects runes in `chars` |
| `ValidInteger()` | Digits + optional leading `-` |
| `ValidDecimal(sep rune)` | Digits + optional `-` + at most one `sep` |

### Combinators

| Function | Description |
|---|---|
| `ValidAll(vs ...)` | AND — all must accept |
| `ValidAny(vs ...)` | OR — any may accept |

### Validator expressions (JSON)

The `validators` array on a text field accepts expression strings.
Multiple expressions are AND-combined.

| Expression | Equivalent |
|---|---|
| `"digits"` | `ValidDigits` |
| `"letters"` | `ValidLetters` |
| `"alphaNum"` | `ValidAlphaNum` |
| `"uppercase"` | `ValidUppercase` |
| `"lowercase"` | `ValidLowercase` |
| `"ascii"` | `ValidASCII` |
| `"hex"` | `ValidHex` |
| `"noSpace"` | `ValidNoSpace` |
| `"integer"` | `ValidInteger()` |
| `"decimal"` | `ValidDecimal('.')` |
| `"decimal:<sep>"` | `ValidDecimal(sep)` |
| `"allow:<chars>"` | `ValidAllowRunes(chars)` |
| `"reject:<chars>"` | `ValidRejectRunes(chars)` |

### Examples

```go
// Digits only.
ginput.New(6).WithBrackets().WithValidator(ginput.ValidDigits).Read()

// Decimal with comma separator.
ginput.New(12).WithValidator(ginput.ValidDecimal(',')).Read()

// Printable ASCII without spaces.
ginput.New(20).WithValidator(ginput.ValidAll(ginput.ValidASCII, ginput.ValidNoSpace)).Read()

// Filename: reject illegal characters.
ginput.New(40).WithValidator(ginput.ValidRejectRunes(`/\:*?"<>|`)).Read()
```

---

## Interactive forms (`Form`)

`Form` renders multiple fields on consecutive terminal lines and lets the
user navigate between them.

### Constructor

```go
form := ginput.NewForm()
```

### Adding fields

| Method | Description |
|---|---|
| `Add(key string, *Input) *Form` | Appends a text field |
| `AddNumeric(key string, *NumericInput) *Form` | Appends a numeric field |
| `AddLabel(key string, *Label) *Form` | Appends a read-only label (see [Labels](#labels-and-separators)) |
| `AddSeparator() *Form` | Appends a blank separator line |
| `GetLabel(key string) *Label` | Retrieves a label by key for dynamic updates |

### Form configuration (chainable)

| Method | Description |
|---|---|
| `OnSubmit(fn func(map[string]string) error)` | Callback on submit; non-nil error → `Read` returns it |
| `WithSubmitKey(key byte)` | Submit trigger key (default `'\r'` = Enter) |
| `WithSubmitFn(n int)` | F-key 1–12 as submit trigger; Enter always advances |
| `WithStayOnForm(clearKeys ...string)` | Keep form active after each submit (see [Stay-on-form](#stay-on-form-mode-and-onsubmit)) |
| `Focus(key string)` | Field to focus after stay-on-form redraw |
| `OnEnter(key string, fn)` | Callback fired when focus arrives at a field (see [Field callbacks](#field-callbacks)) |
| `OnExit(key string, fn)` | Callback fired when focus leaves a field |
| `OnChange(key string, fn)` | Callback fired on every value change in a field |
| `OnFn(n int, fn)` | Callback fired when F-key n (1–12) is pressed from any field |
| `OnCtrl(char byte, fn)` | Callback fired when Ctrl+char is pressed from any field |
| `WithHeader(text string)` | Static text above the fields |
| `WithHeaderColor(c Color)` | Color for the header |
| `WithFooter(text string)` | Static text below the fields |
| `WithFooterColor(c Color)` | Color for the footer |
| `WithStatusColor(c Color)` | Color for the status line; reserves the status area (see [Status line](#status-line)) |
| `WithLabelColor(c Color)` | Default prompt color for all fields |
| `WithInputColor(c Color)` | Default editable-area color for all fields |
| `WithOffsetX(n int)` | Left margin for the entire form |
| `WithOffsetY(n int)` | Blank lines above the form |
| `WithContentOffsetX(n int)` | Extra left margin for fields only (not header/footer) |
| `WithFieldOffset(key string, x int)` | Extra left margin for a single field |
| `WithInput(*os.File)` | Input source (default `os.Stdin`) |
| `WithOutput(io.Writer)` | Output destination (default `os.Stdout`) |

### Status line methods

| Method | Description |
|---|---|
| `SetStatus(msg string, clearAfterSecs int)` | Sets the status message; auto-clears after `n` seconds (0 = permanent) |
| `ClearStatus()` | Clears the status message immediately |

### `Read() (map[string]string, error)`

Renders the form and reads all fields. Returns a `map[string]string` keyed
by the names passed to `Add`/`AddNumeric`. Labels and separators are omitted.

### Form navigation keys

| Key | Action |
|---|---|
| **Tab** / **↓** | Next field (circular) |
| **Shift-Tab** / **↑** | Previous field (circular) |
| **Enter** | Next field; submit on last field (or always advance if a custom submit key is set) |
| **Ctrl-C** | `ErrInterrupt` |
| **Ctrl-D** | `ErrEOF` (only when active field is empty) |

All single-field editing keys (←/→, Home, End, Ctrl-A/E/K/U,
Backspace, Delete) apply to the active field.

### Basic example

```go
results, err := ginput.NewForm().
    Add("first", ginput.New(20).WithPrompt("First name: ").WithBrackets()).
    Add("last",  ginput.New(20).WithPrompt("Last name:  ").WithBrackets()).
    Add("email", ginput.New(40).WithPrompt("Email:      ").WithBrackets()).
    Add("pass",  ginput.New(32).WithPrompt("Password:   ").WithBrackets().WithMask('*')).
    OnSubmit(func(v map[string]string) error {
        if v["first"] == "" {
            return fmt.Errorf("first name is required")
        }
        return nil
    }).
    Read()
```

Terminal appearance:
```
First name: [Joan________________]
Last name:  [____________________]
Email:      [_______________________________________]
Password:   [********************************]
```

---

## Form layout

### Offsets

Three offset levels control positioning:

| Level | Method / JSON | Effect |
|---|---|---|
| **Entire form** | `WithOffsetX(n)` / `"offsetX"` | Shifts header, fields, footer, and status right by `n` columns |
| **Entire form** | `WithOffsetY(n)` / `"offsetY"` | Inserts `n` blank lines above the form |
| **Fields only** | `WithContentOffsetX(n)` / `"contentOffsetX"` | Extra left margin for fields (not header/footer); stacks with `offsetX` |
| **Single field** | `WithFieldOffset(key, x)` / field `"offsetX"` | Extra margin for one field; stacks with both |

Effective field margin: `offsetX + contentOffsetX + field.offsetX`

```go
form.WithOffsetX(2).WithFieldOffset("table", 2)
```

```json
{
  "offsetX": 2,
  "fields": [
    { "key": "host",  "prompt": "Host:  ", "maxLen": 32, "brackets": true },
    { "key": "table", "prompt": "Table: ", "maxLen": 32, "brackets": true, "offsetX": 2 }
  ]
}
```

Result (`.` = space):
```
..Host:  [                              ]
..Table:   [                              ]
```

### Vertical anatomy of a rendered form

```
  [blank lines ← offsetY]
  Header                          ← WithHeader
  (blank separator line)
  Field 1                         ← fields area
  Field 2
  ...
  (blank separator line)
  Footer                          ← WithFooter
  (blank separator line)
  Status message                  ← status line (SetStatus / WithStatusColor)
```

---

## Labels and separators

Two non-interactive items can be placed anywhere in the field list.
Both are skipped during Tab/↑↓ navigation and produce no entry in the results map.

### Separator

A blank line that visually separates groups of fields.

```go
form.AddSeparator()
```

JSON: `{ "type": "separator" }`

### Label

A read-only line with a prompt prefix and a message text that can be updated
programmatically.

```go
status := ginput.NewLabel("Status: ", "ready").
    WithLabelColor(ginput.ColorBrightBlack).
    WithTextColor(ginput.ColorCyan)

form.AddLabel("status", status)

// Update from OnSubmit:
status.Set("Done!")
```

| Method | Description |
|---|---|
| `NewLabel(label, text string) *Label` | Creates a label |
| `Set(text string)` | Updates the message text |
| `WithLabelColor(c Color) *Label` | Color for the prefix |
| `WithTextColor(c Color) *Label` | Color for the message |

When the form was built from JSON, use `form.GetLabel("status")` to get the reference.

JSON:
```json
{ "type": "label", "key": "status",
  "prompt": "Status: ", "promptColor": "brightBlack",
  "text": "ready",      "textColor": "cyan" }
```

---

## Status line

The status line is a dedicated area rendered **below the footer** for
transient messages (success/error feedback, progress, etc.). Unlike a
`Label`, which is part of the field list, the status line sits outside
the form's field area and supports automatic timed clearing.

### Enabling the status area

Call `WithStatusColor` at build time to reserve the area and set its color:

```go
form.WithStatusColor(ginput.ColorCyan)
```

Or in JSON: `"statusColor": "cyan"`.

Calling `SetStatus` also implicitly enables the area if it hasn't been configured yet.

### Setting a message

```go
form.SetStatus("Saved → output.sql", 4)  // auto-clears after 4 seconds
form.SetStatus("Error: not found", 0)     // stays until next SetStatus / ClearStatus
form.ClearStatus()                        // clears immediately
```

`SetStatus` is safe to call from inside an `OnSubmit` callback. The message
is rendered on the next form redraw (which happens immediately in
`WithStayOnForm` mode).

### Auto-clear behaviour

When `clearAfterSecs > 0`, the read loop sets a short read-deadline on the
input file so the message disappears promptly even if the user does not press
any key.

### Example with `OnSubmit`

```go
form.OnSubmit(func(v map[string]string) error {
    out, err := process(v)
    if err != nil {
        form.SetStatus("Error: "+err.Error(), 4)
    } else {
        form.SetStatus("Saved → "+out, 4)
    }
    return nil
})
form.WithStayOnForm("query").Focus("query")
form.WithStatusColor(ginput.ColorCyan)
```

Terminal appearance (status visible for 4 seconds after each submit):

```
  MySQL · Export CREATE TABLE

    Host:     [localhost_______________________]
    Port:     [3306_]
    User:     [root____________________________]
    Password: [********************************]
    Database: [mydb____________________________]

    Table:    [________________________________]

  Enter on last field to submit  ·  Tab/↑↓ to navigate  ·  Ctrl-C to cancel

  Saved → CLIENTS.sql
```

---

## Stay-on-form mode and `OnSubmit`

### `OnSubmit`

`OnSubmit` registers a callback invoked when the form is submitted.
If the callback returns a non-nil error, `Read` returns that error immediately.

```go
form.OnSubmit(func(v map[string]string) error {
    if v["name"] == "" {
        return fmt.Errorf("name is required") // Read returns this error
    }
    return nil
})
```

### `WithStayOnForm`

Keeps the form active after each successful submit. The optional key list
names which fields are reset; if empty, all fields are reset.

```go
form.WithStayOnForm("table")   // reset only "table" after each submit
form.Focus("table")            // put cursor on "table" after redraw
```

The form only exits via **Ctrl-C**, **Ctrl-D**, or when `OnSubmit` returns a
non-nil error.

### `Focus`

Sets which field receives focus after each stay-on-form redraw. Can be called
at build time (chainable) or from inside `OnSubmit` for dynamic focus control.

### Complete pattern (MySQL export example)

```go
form.OnSubmit(func(v map[string]string) error {
    // Save connection values (never the password).
    toSave := make(map[string]string, len(v))
    for k, val := range v {
        if k != "pass" && k != "table" {
            toSave[k] = val
        }
    }
    _ = ginput.SaveValues("settings.json", toSave)

    outFile, err := exportTable(v)
    if err != nil {
        form.SetStatus("Error: "+err.Error(), 4)
    } else {
        form.SetStatus("Saved → "+outFile, 4)
    }
    return nil // returning nil keeps the form active
})
form.WithStayOnForm("table").Focus("table")
```

---

## Field callbacks

Five event hooks let you react to field-level and keyboard events without
leaving the form active. All callbacks are registered at build time and
are triggered automatically by the `Read` loop.

### Focus callbacks — `OnEnter` / `OnExit`

Fired when focus **arrives at** or **leaves** a named interactive field.
The callback receives the field key and a snapshot of all current field values.
Return value: none (effects only).

```go
func (f *Form) OnEnter(key string, fn func(key string, values map[string]string)) *Form
func (f *Form) OnExit (key string, fn func(key string, values map[string]string)) *Form
```

Typical uses:
- Show a contextual help message in the status line when entering a field.
- Validate the previous field's value when leaving it.
- Pre-fill related fields dynamically.

```go
form.
    OnEnter("email", func(key string, vals map[string]string) {
        form.SetStatus("Enter a valid e-mail address", 0)
    }).
    OnExit("email", func(key string, vals map[string]string) {
        if !strings.Contains(vals["email"], "@") {
            form.SetStatus("Invalid e-mail format", 3)
        } else {
            form.ClearStatus()
        }
    })
```

### Value-change callback — `OnChange`

Fired **after every keystroke** that modifies the value of a named field.
The callback receives the field key and the new string value.

```go
func (f *Form) OnChange(key string, fn func(key string, value string)) *Form
```

Typical uses:
- Live preview in a label.
- Real-time character counting in the status line.
- Cross-field conditional enabling.

```go
preview := ginput.NewLabel("Preview: ", "")
form.AddLabel("preview", preview)

form.OnChange("name", func(key, val string) {
    preview.Set(strings.ToUpper(val))
})
```

### Function-key callback — `OnFn`

Fired when the user presses **F1–F12** from any field.
The callback receives a snapshot of all current field values.
Returning a non-nil error exits `Read` with that error; returning `nil`
keeps the form active.

```go
func (f *Form) OnFn(n int, fn func(map[string]string) error) *Form
```

`OnFn` handlers have priority over a submit trigger registered with
`WithSubmitFn`.

```go
form.
    OnFn(2, func(vals map[string]string) error {   // F2 → save draft
        saveDraft(vals)
        form.SetStatus("Draft saved (F2)", 3)
        return nil  // stay on form
    }).
    OnFn(10, func(vals map[string]string) error {  // F10 → submit and exit
        return process(vals)  // nil = ok, non-nil = exit with error
    })
```

### Control-key callback — `OnCtrl`

Fired when the user presses **Ctrl+letter** from any field.
`char` can be uppercase or lowercase; both map to the same control byte
(e.g. `'S'` and `'s'` both catch **Ctrl-S**).
Returning a non-nil error exits `Read`; returning `nil` keeps the form active.

```go
func (f *Form) OnCtrl(char byte, fn func(map[string]string) error) *Form
```

> **Note:** `OnCtrl` has priority over the built-in editing shortcuts.
> Avoid registering `'A'`, `'E'`, `'K'`, or `'U'` unless you intentionally
> want to override `Home`, `End`, delete-to-end, and clear-line.

```go
form.OnCtrl('S', func(vals map[string]string) error {
    if err := save(vals); err != nil {
        form.SetStatus("Save error: "+err.Error(), 4)
        return nil // stay — let user correct the data
    }
    return fmt.Errorf("saved") // exit Read; caller handles this
})
```

### Combining callbacks

All five callback types can be freely combined on the same form:

```go
form.
    OnEnter("sku", func(k string, vals map[string]string) {
        form.SetStatus("Type the article SKU", 0)
    }).
    OnExit("sku", func(k string, vals map[string]string) {
        if vals[k] == "" {
            form.SetStatus("SKU is required", 3)
        }
    }).
    OnChange("price", func(k, v string) {
        labelTotal.Set(computeTotal(v, currentQty))
    }).
    OnFn(5, func(vals map[string]string) error {
        clearAll()
        return nil
    }).
    OnCtrl('S', func(vals map[string]string) error {
        return saveAndExit(vals)
    })
```

---

## JSON-defined forms

`NewFormFromJSON` builds a complete `*Form` from a JSON byte slice.

### Constructor functions

| Function | Description |
|---|---|
| `NewFormFromJSON(data []byte) (*Form, error)` | Parses JSON and returns a configured `*Form` |
| `NewFormFromDef(def FormDef) (*Form, error)` | Builds a `*Form` from a `FormDef` struct |

After creation, you can still chain `OnSubmit`, `WithStayOnForm`, `Focus`,
`SetStatus`, `WithInput`, `WithOutput`, etc. before calling `Read()`.

### Top-level JSON keys

| Key | Type | Description |
|---|---|---|
| `header` | string | Static text above the fields |
| `headerColor` | string | Color for the header |
| `footer` | string | Static text below the fields |
| `footerColor` | string | Color for the footer |
| `statusColor` | string | Color for the status line; enables the status area |
| `labelColor` | string | Default prompt color for all fields |
| `inputColor` | string | Default editable-area color for all fields |
| `offsetX` | int | Left margin for the entire form |
| `offsetY` | int | Blank lines above the form |
| `contentOffsetX` | int | Extra left margin for fields only |
| `submitKey` | int | ASCII code of the submit key (0 = Enter) |
| `submitFn` | int | F-key number 1–12 as submit trigger |
| `fields` | array | Field definitions (see below) |

### Field JSON keys

| Key | Type | Applies to | Description |
|---|---|---|---|
| `key` | string | text, numeric | Field identifier (required) |
| `type` | string | all | `"text"` (default), `"numeric"`, `"label"`, `"separator"` |
| `prompt` | string | all except separator | Text before the field |
| `maxLen` | int | text | Maximum runes (required, ≥ 1) |
| `maxIntegers` | int | numeric | Max integer digits (≥ 1) |
| `decimals` | int | numeric | Decimal places (0 = integer) |
| `negative` | bool | numeric | Allow negative values |
| `brackets` | bool | text, numeric | Surround with `[` `]` |
| `field` | bool | text | Show empty positions without brackets |
| `placeholder` | string | text | Single character for empty positions |
| `mask` | string | text | Single character mask (e.g. `"*"`) |
| `default` | string | text, numeric | Pre-filled value |
| `validators` | []string | text | Validator expressions (AND-combined) |
| `promptColor` | string | all except separator | Color for prompt/label prefix |
| `inputColor` | string | text, numeric | Color for the editable area |
| `text` | string | label | Message content |
| `textColor` | string | label | Color for the message |
| `offsetX` | int | all except separator | Extra left margin for this field |

### `ApplyDefaults` / `LoadAndApplyDefaults`

Pre-fill field defaults from a previously saved values file:

```go
var def ginput.FormDef
json.Unmarshal([]byte(formJSON), &def)
ginput.LoadAndApplyDefaults("saved.json", &def)  // no-op if file doesn't exist
form, _ := ginput.NewFormFromDef(def)
```

### Full JSON example

```json
{
  "header": "MySQL · Export CREATE TABLE",
  "headerColor": "cyan",
  "footer": "Enter on last field to submit  ·  Tab/↑↓ to navigate  ·  Ctrl-C to cancel",
  "footerColor": "brightBlack",
  "statusColor": "cyan",
  "labelColor": "green",
  "offsetX": 2,
  "contentOffsetX": 2,
  "fields": [
    { "key": "host",  "prompt": "Host:     ", "maxLen": 32, "brackets": true, "default": "localhost" },
    { "key": "port",  "prompt": "Port:     ", "maxLen": 5,  "brackets": true, "default": "3306", "validators": ["digits"] },
    { "key": "user",  "prompt": "User:     ", "maxLen": 32, "brackets": true },
    { "key": "pass",  "prompt": "Password: ", "maxLen": 32, "brackets": true, "mask": "*" },
    { "key": "db",    "prompt": "Database: ", "maxLen": 32, "brackets": true },
    { "type": "separator" },
    { "key": "table", "prompt": "Table:    ", "maxLen": 32, "brackets": true, "validators": ["noSpace"] }
  ]
}
```

---

## Persisting form values

| Function | Description |
|---|---|
| `SaveValues(path string, values map[string]string) error` | Writes the map as indented JSON (perms `0600`) |
| `LoadValues(path string) (map[string]string, error)` | Reads a JSON file back into a map |
| `LoadAndApplyDefaults(path string, *FormDef) error` | Loads values and sets field defaults in one step (no-op if file missing) |

```go
// Save.
results, _ := form.Read()
ginput.SaveValues("settings.json", results)

// Restore.
values, _ := ginput.LoadValues("settings.json")
```

Produced file:
```json
{
	"city": "Barcelona",
	"country": "ES",
	"zip": "08001"
}
```

---

## Color system

`Color` is a `string` typedef wrapping ANSI SGR escape sequences.

### Predefined constants

| Constant | Description |
|---|---|
| `ColorDefault` | No escape; terminal default |
| `ColorBold` | Bold text |
| `ColorBlack` … `ColorWhite` | Standard 8 foreground colors |
| `ColorBrightBlack` … `ColorBrightWhite` | Bright (high-intensity) 8 colors |

### 256-color palette

```go
c := ginput.Color256(208) // orange
```

### Color names (JSON)

In JSON definitions, colors are specified as strings mapped to constants:

`"default"`, `"bold"`, `"black"`, `"red"`, `"green"`, `"yellow"`, `"blue"`,
`"magenta"`, `"cyan"`, `"white"`, `"brightBlack"` (or `"gray"`/`"grey"`),
`"brightRed"`, `"brightGreen"`, `"brightYellow"`, `"brightBlue"`,
`"brightMagenta"`, `"brightCyan"`, `"brightWhite"`.

Unrecognised strings are passed through as raw ANSI escape sequences.

---

## Errors

```go
var ErrInterrupt = errors.New("interrupted")   // Ctrl-C
var ErrEOF       = errors.New("EOF")           // Ctrl-D on empty
```

```go
if errors.Is(err, ginput.ErrInterrupt) { ... }
```

Full error handling:

```go
results, err := form.Read()
switch {
case err == nil:
    fmt.Println("Results:", results)
case errors.Is(err, ginput.ErrInterrupt):
    fmt.Println("Cancelled.")
case errors.Is(err, ginput.ErrEOF):
    fmt.Println("EOF.")
default:
    fmt.Println("Error:", err)
}
```

---

## Multi-page forms (`MultiForm`)

A `MultiForm` groups several `Page` objects into one interactive session. The
user navigates between pages with **PageUp / PageDown**. A common global
header, footer, and status line are displayed on every page; each page can also
define its own per-page title.

Because field keys may be reused across different pages, the result of
`Read()` is `map[string]map[string]string` — the outer key identifies the
page, the inner key identifies the field.

---

### `Page` — constructing a page

#### Constructor

```go
func NewPage(key string) *Page
```

`key` is the page's unique identifier used in the result map and in page-level
callbacks.

#### Adding fields

The same field methods available on `Form` are mirrored on `Page`:

| Method | Description |
|--------|-------------|
| `Add(key string, inp *Input) *Page` | Text field |
| `AddNumeric(key string, n *NumericInput) *Page` | Numeric field |
| `AddLabel(key string, l *Label) *Page` | Read-only label |
| `AddSeparator() *Page` | Blank separator line |
| `GetLabel(key string) *Label` | Retrieve a label at runtime |

#### Page configuration (chainable)

| Method | Description |
|--------|-------------|
| `WithPageHeader(text string) *Page` | Title shown above the page's fields (may contain `\n`) |
| `WithPageHeaderColor(c Color) *Page` | ANSI color for the page title |
| `WithLabelColor(c Color) *Page` | Default prompt color for this page's fields |
| `WithInputColor(c Color) *Page` | Default input-area color for this page's fields |
| `WithContentOffsetX(n int) *Page` | Extra left margin added on top of the MultiForm offset |

#### Per-field callbacks on a page

| Method | Description |
|--------|-------------|
| `OnEnter(key, fn) *Page` | Fired when focus enters the field. `fn(key string, vals map[string]string)` |
| `OnExit(key, fn) *Page` | Fired when focus leaves the field. Same signature |
| `OnChange(key, fn) *Page` | Fired on every character change. `fn(key, value string)` |

`vals` is a snapshot of **this page's** field values.

#### Runtime helpers on a page

```go
func (p *Page) GetValue(key string) string
func (p *Page) SetValue(key string, val string)
```

---

### `MultiForm` — constructing the multi-page form

#### Constructor

```go
func NewMultiForm() *MultiForm
```

Reads from `os.Stdin`, writes to `os.Stdout`.

#### Building the form (chainable)

| Method | Description |
|--------|-------------|
| `AddPage(p *Page) *MultiForm` | Append a page |
| `WithHeader(text string) *MultiForm` | Global header shown above every page |
| `WithHeaderColor(c Color) *MultiForm` | Color for the global header |
| `WithFooter(text string) *MultiForm` | Global footer shown below every page |
| `WithFooterColor(c Color) *MultiForm` | Color for the global footer |
| `WithOffsetX(n int) *MultiForm` | Left-column margin for the whole form |
| `WithOffsetY(n int) *MultiForm` | Blank lines printed above the form |
| `WithStatusColor(c Color) *MultiForm` | Enable status area with the given color |
| `WithSubmitKey(key byte) *MultiForm` | Override the submit byte (default `\r`) |
| `WithSubmitFn(n int) *MultiForm` | Use function key F*n* as the submit trigger |
| `WithStayOnForm() *MultiForm` | Stay active after each submit (exit only on Ctrl-C / Ctrl-D / non-nil OnSubmit error) |
| `Focus(pageKey, fieldKey string) *MultiForm` | Focus target after a stay-on-form redraw |

#### Callbacks (chainable)

| Method | Signature | Description |
|--------|-----------|-------------|
| `OnSubmit(fn) *MultiForm` | `fn(all map[string]map[string]string) error` | Called when the form is submitted. Non-nil error exits `Read`. |
| `OnPageChange(fn) *MultiForm` | `fn(pageKey string, all map[string]map[string]string)` | Fired on every page switch. |
| `OnFn(n int, fn) *MultiForm` | `fn(all map[string]map[string]string) error` | Fired when F*n* is pressed. |
| `OnCtrl(char byte, fn) *MultiForm` | `fn(all map[string]map[string]string) error` | Fired when Ctrl+*char* is pressed. |

#### Status line methods

```go
func (mf *MultiForm) SetStatus(msg string, clearAfterSecs int)
func (mf *MultiForm) ClearStatus()
```

#### Runtime helpers

```go
func (mf *MultiForm) GetValue(pageKey, fieldKey string) string
func (mf *MultiForm) SetValue(pageKey, fieldKey, val string)
func (mf *MultiForm) GetPage(key string) *Page
func (mf *MultiForm) ClearScreen()
```

#### `Read() (map[string]map[string]string, error)`

Puts the terminal in raw mode and starts the interactive session.
Returns `map[pageKey]map[fieldKey]value` on success.

Errors: `ginput.ErrInterrupt` (Ctrl-C) and `ginput.ErrEOF` (Ctrl-D on empty).

---

### Navigation keys

| Key | Action |
|-----|--------|
| Tab, ↓ Arrow | Next field within the current page |
| Shift-Tab, ↑ Arrow | Previous field within the current page |
| PageDown | Next page (wraps) |
| PageUp | Previous page (wraps) |
| Enter | Advance field; on last field of a non-last page moves to next page; on last field of last page submits |
| Ctrl-C | Return `ErrInterrupt` |
| Ctrl-D | Return `ErrEOF` (only when active field is empty) |

---

### Complete example

See `example/pages/main.go` for the runnable version. Abbreviated:

```go
var mf *ginput.MultiForm

func main() {
    personal := ginput.NewPage("personal").
        WithPageHeader("Personal data").
        WithPageHeaderColor(ginput.ColorCyan).
        Add("name",  ginput.New(40).WithPrompt("Full name: ")).
        Add("email", ginput.New(60).WithPrompt("E-mail:    "))

    address := ginput.NewPage("address").
        WithPageHeader("Address").
        WithPageHeaderColor(ginput.ColorGreen).
        Add("street",  ginput.New(60).WithPrompt("Street:  ")).
        Add("country", ginput.New(30).WithPrompt("Country: ").WithDefault("Spain"))

    mf = ginput.NewMultiForm().
        WithHeader("=== Registration ===").
        WithFooter("PageUp/PageDown to switch pages  │  Enter on last field submits").
        WithStatusColor(ginput.ColorMagenta).
        WithOffsetX(2).
        AddPage(personal).
        AddPage(address).
        OnPageChange(func(pageKey string, _ map[string]map[string]string) {
            mf.SetStatus("Now on page: "+pageKey, 3)
        }).
        OnSubmit(func(all map[string]map[string]string) error {
            fmt.Println("name:", all["personal"]["name"])
            fmt.Println("street:", all["address"]["street"])
            return nil
        })

    mf.ClearScreen()
    results, err := mf.Read()
    // … handle err, use results
}
```

---

## Project structure

```
ginput/
├── go.mod
├── go.sum
├── ginput.go        ← Input + Color system
├── validators.go    ← predefined validators
├── numeric.go       ← NumericInput
├── form.go          ← Form + Label + status line
├── formjson.go      ← NewFormFromJSON / NewFormFromDef
├── formvalues.go    ← SaveValues / LoadValues / LoadAndApplyDefaults
├── fnkeys.go        ← F1–F12 key sequences
├── multiform.go     ← MultiForm + Page
├── doc/
│   └── README.md    ← this file
└── example/
    ├── main.go      ← single-field + form examples
    ├── json/
    │   └── main.go  ← JSON form with F10 submit + SaveValues
    ├── mysql/
    │   └── main.go  ← MySQL export: OnSubmit + StayOnForm + SetStatus
    └── pages/
        └── main.go  ← multi-page form: 3 pages + OnPageChange + OnSubmit validation
```
