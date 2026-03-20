package main

import (
	"errors"
	"fmt"

	"github.com/jncss/ginput"
)

func main() {
	// ── Single-field examples ─────────────────────────────────────────────────

	// 1. Name with brackets and a default value.
	name, err := ginput.New(20).
		WithPrompt("Name:     ").
		WithBrackets().
		WithDefault("World").
		Read()
	if err != nil {
		handleErr(err)
		return
	}
	fmt.Printf("Hello, %s!\n", name)

	// 2. Digits-only code with a custom placeholder.
	code, err := ginput.New(6).
		WithPrompt("Code:     ").
		WithBrackets().
		WithPlaceholder('.').
		WithValidator(ginput.ValidDigits).
		Read()
	if err != nil {
		handleErr(err)
		return
	}
	fmt.Printf("Code: %s\n", code)

	// ── Form example ──────────────────────────────────────────────────────────

	fmt.Println()
	fmt.Println("--- Registration form (Tab / Shift-Tab / Enter to navigate, Enter on last field to submit) ---")
	fmt.Println()

	results, err := ginput.NewForm().
		Add("first", ginput.New(20).WithPrompt("First name: ").WithBrackets()).
		Add("last", ginput.New(20).WithPrompt("Last name:  ").WithBrackets()).
		Add("email", ginput.New(40).WithPrompt("Email:      ").WithBrackets()).
		Add("pass", ginput.New(32).WithPrompt("Password:   ").WithBrackets().WithMask('*')).
		OnSubmit(func(v map[string]string) error {
			if v["first"] == "" {
				return fmt.Errorf("first name is required")
			}
			if len(v["pass"]) < 4 {
				return fmt.Errorf("password must be at least 4 characters")
			}
			return nil
		}).
		Read()
	if err != nil {
		handleErr(err)
		return
	}
	fmt.Printf("\nWelcome, %s %s! (email: %s)\n", results["first"], results["last"], results["email"])
}

func handleErr(err error) {
	switch {
	case errors.Is(err, ginput.ErrInterrupt):
		fmt.Println("\nCancelled.")
	case errors.Is(err, ginput.ErrEOF):
		fmt.Println("\nEOF.")
	default:
		fmt.Printf("\nError: %v\n", err)
	}
}
