// Example: multi-page form using ginput.MultiForm.
//
// Three pages (Personal / Address / Account) navigate with PageUp/PageDown.
// A shared header, footer, and status line are shown on every page.
// OnPageChange fires whenever the user switches pages and shows a hint in
// the status area. OnSubmit receives the full map[pageKey]map[fieldKey]value.
package main

import (
	"fmt"
	"os"

	"github.com/jncss/ginput"
)

// mf is a package-level variable so the OnPageChange and OnSubmit closures
// can call mf.SetStatus / mf.SetValue after mf is fully built.
var mf *ginput.MultiForm

func main() {
	// ── Page 1: Personal data ─────────────────────────────────────────────
	personal := ginput.NewPage("personal").
		WithPageHeader("Personal data").
		WithPageHeaderColor(ginput.ColorCyan).
		Add("name", ginput.New(40).WithPrompt("Full name    : ")).
		Add("email", ginput.New(60).WithPrompt("E-mail       : ")).
		Add("phone", ginput.New(20).WithPrompt("Phone        : "))

	// ── Page 2: Address ───────────────────────────────────────────────────
	address := ginput.NewPage("address").
		WithPageHeader("Address").
		WithPageHeaderColor(ginput.ColorGreen).
		Add("street", ginput.New(60).WithPrompt("Street       : ")).
		Add("city", ginput.New(40).WithPrompt("City         : ")).
		Add("country", ginput.New(30).WithPrompt("Country      : ").WithDefault("Spain")).
		Add("zip", ginput.New(10).WithPrompt("ZIP code     : "))

	// ── Page 3: Account ───────────────────────────────────────────────────
	account := ginput.NewPage("account").
		WithPageHeader("Account").
		WithPageHeaderColor(ginput.ColorYellow).
		Add("username", ginput.New(30).WithPrompt("Username     : ")).
		Add("password", ginput.New(30).WithPrompt("Password     : ").WithMask('*')).
		Add("password2", ginput.New(30).WithPrompt("Confirm pwd  : ").WithMask('*'))

	// ── MultiForm ─────────────────────────────────────────────────────────
	mf = ginput.NewMultiForm().
		WithHeader("══════════════════════════════════════\n  User registration – multi-page form\n══════════════════════════════════════").
		WithHeaderColor(ginput.ColorWhite).
		WithFooter("Tab/↑↓ navigate fields  │  PageUp/PageDown change page  │  Enter on last field submits").
		WithFooterColor(ginput.ColorBlue).
		WithStatusColor(ginput.ColorMagenta).
		WithOffsetX(2).
		WithOffsetY(1).
		AddPage(personal).
		AddPage(address).
		AddPage(account).
		// Show a contextual hint whenever the active page changes.
		OnPageChange(func(pageKey string, _ map[string]map[string]string) {
			hints := map[string]string{
				"personal": "Page 1/3 – fill in your personal details",
				"address":  "Page 2/3 – enter your postal address",
				"account":  "Page 3/3 – choose login credentials",
			}
			if h, ok := hints[pageKey]; ok {
				mf.SetStatus(h, 0)
			}
		}).
		// Validate on submit: passwords must match.
		OnSubmit(func(all map[string]map[string]string) error {
			acc := all["account"]
			if acc["password"] != acc["password2"] {
				mf.SetStatus("ERROR: passwords do not match, please correct.", 5)
				mf.SetValue("account", "password", "")
				mf.SetValue("account", "password2", "")
				return fmt.Errorf("passwords mismatch")
			}
			return nil
		})

	// Clear the screen before showing the form.
	mf.ClearScreen()

	results, err := mf.Read()
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
		os.Exit(1)
	}

	// Print results grouped by page.
	fmt.Println("\n── Results ──────────────────────────────────────────")
	pages := []string{"personal", "address", "account"}
	for _, pk := range pages {
		fmt.Printf("\n[%s]\n", pk)
		for k, v := range results[pk] {
			fmt.Printf("  %-12s = %s\n", k, v)
		}
	}
}
