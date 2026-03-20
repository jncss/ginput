// Example: multi-page form loaded entirely from a JSON definition file.
//
// The form structure (pages, fields, colors, header, footer …) is defined in
// form.json and parsed with ginput.NewMultiFormFromJSON.
// Runtime callbacks (page-change hints, submit validation) are registered in
// Go code after the form is built.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/jncss/ginput"
)

// mf is package-level so the OnPageChange / OnSubmit closures can reach it.
var mf *ginput.MultiForm

func main() {
	// ── Load form definition from JSON ────────────────────────────────────
	data, err := os.ReadFile("form.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot read form.json: %v\n", err)
		os.Exit(1)
	}

	mf, err = ginput.NewMultiFormFromJSON(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid form definition: %v\n", err)
		os.Exit(1)
	}

	// ── Runtime callbacks (cannot be expressed in JSON) ───────────────────

	// Show a page-specific hint in the status area on every page switch.
	hints := map[string]string{
		"personal": "Page 1/3 – fill in your personal details",
		"address":  "Page 2/3 – enter your postal address",
		"account":  "Page 3/3 – choose login credentials",
	}
	mf.OnPageChange(func(pageKey string, _ map[string]map[string]string) {
		if h, ok := hints[pageKey]; ok {
			mf.SetStatus(h, 0)
		}
	})

	// Validate on submit: both password fields must match.
	mf.OnSubmit(func(all map[string]map[string]string) error {
		acc := all["account"]
		if acc["password"] != acc["password2"] {
			mf.SetStatus("ERROR: passwords do not match – please correct.", 5)
			mf.SetValue("account", "password", "")
			mf.SetValue("account", "password2", "")
			return fmt.Errorf("passwords mismatch")
		}
		return nil
	})

	// ── Run ───────────────────────────────────────────────────────────────
	mf.ClearScreen()
	results, err := mf.Read()
	switch {
	case err == nil:
		// success – print collected values grouped by page
		fmt.Println("\n── Results ──────────────────────────────────────────")
		pageOrder := []string{"personal", "address", "account"}
		for _, pk := range pageOrder {
			fmt.Printf("\n[%s]\n", pk)
			for k, v := range results[pk] {
				fmt.Printf("  %-12s = %s\n", k, v)
			}
		}
	case errors.Is(err, ginput.ErrInterrupt):
		fmt.Fprintln(os.Stderr, "\nCancelled.")
		os.Exit(1)
	case errors.Is(err, ginput.ErrEOF):
		fmt.Fprintln(os.Stderr, "\nEOF.")
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
		os.Exit(1)
	}
}
