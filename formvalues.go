// Package ginput – helpers to persist and restore form values.
package ginput

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// SaveValues writes a form result map to a JSON file at path.
// The file is created or truncated with permissions 0600.
//
//	results, _ := form.Read()
//	err := ginput.SaveValues("data.json", results)
func SaveValues(path string, values map[string]string) error {
	data, err := json.MarshalIndent(values, "", "\t")
	if err != nil {
		return fmt.Errorf("ginput: marshal values: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("ginput: write values %q: %w", path, err)
	}
	return nil
}

// LoadAndApplyDefaults is a convenience helper that combines LoadValues and
// FormDef.ApplyDefaults into a single call.
//
// It reads the JSON file at path, then sets each field's Default to the saved
// value for that key. If the file does not exist the call is a no-op and nil
// is returned. Any other read or parse error is returned.
//
//	var def ginput.FormDef
//	json.Unmarshal(data, &def)
//	ginput.LoadAndApplyDefaults("saved.json", &def)
//	form, _ := ginput.NewFormFromDef(def)
func LoadAndApplyDefaults(path string, def *FormDef) error {
	values, err := LoadValues(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	def.ApplyDefaults(values)
	return nil
}

// LoadValues reads a JSON file previously written by SaveValues and returns
// the form result map.
//
//	values, err := ginput.LoadValues("data.json")
func LoadValues(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ginput: read values %q: %w", path, err)
	}
	var values map[string]string
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("ginput: parse values %q: %w", path, err)
	}
	return values, nil
}
