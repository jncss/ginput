package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/jncss/ginput"
)

const valuesFile = "form_values.json"

func main() {
	const formJSON = `{
	"submitFn": 10,
	"header": "Address Data",
	"headerColor": "cyan",
	"footer": "F10 to submit  ·  Tab / ↑↓ to navigate  ·  Ctrl-C to cancel",
	"footerColor": "brightBlack",
	"labelColor": "yellow",
	"fields": [
		{ "key": "city",    "prompt": "City:     ", "maxLen": 30, "brackets": true },
		{ "key": "country", "prompt": "Country:  ", "maxLen": 20, "brackets": true, "default": "ES" },
		{ "key": "zip",     "prompt": "ZIP code: ", "maxLen":  5, "brackets": true, "placeholder": ".", "validators": ["digits"] },
		{ "key": "price",   "prompt": "Price:    ", "type": "numeric", "maxIntegers": 6, "decimals": 2, "brackets": true, "inputColor": "green" },
		{ "key": "pass",    "prompt": "Password: ", "maxLen": 32, "brackets": true, "mask": "*" }
	]
}`

	// Parse the definition, inject any previously saved defaults, then build.
	var def ginput.FormDef
	if err := json.Unmarshal([]byte(formJSON), &def); err != nil {
		fmt.Fprintln(os.Stderr, "form JSON error:", err)
		os.Exit(1)
	}
	if err := ginput.LoadAndApplyDefaults(valuesFile, &def); err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not load saved values:", err)
	}

	form, err := ginput.NewFormFromDef(def)
	if err != nil {
		fmt.Fprintln(os.Stderr, "form error:", err)
		os.Exit(1)
	}

	results, err := form.Read()
	if err != nil {
		switch {
		case errors.Is(err, ginput.ErrInterrupt):
			fmt.Println("\nCancelled.")
		case errors.Is(err, ginput.ErrEOF):
			fmt.Println("\nEOF.")
		default:
			fmt.Fprintln(os.Stderr, "\nError:", err)
			os.Exit(1)
		}
		return
	}

	fmt.Printf("\nCity: %s  Country: %s  ZIP: %s  Price: %s\n",
		results["city"], results["country"], results["zip"], results["price"])

	// Persist the results for the next run.
	if err := ginput.SaveValues(valuesFile, results); err != nil {
		fmt.Fprintln(os.Stderr, "save error:", err)
	} else {
		fmt.Printf("(values saved to %s)\n", valuesFile)
	}
}
