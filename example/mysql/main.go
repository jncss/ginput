package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	_ "github.com/go-sql-driver/mysql"

	"github.com/jncss/ginput"
)

const valuesFile = "mysql_values.json"

// formJSON defines the complete form layout: connection fields, a separator,
// the table field, another separator, and a status label.
// The separator and label types are built-in ginput JSON field types.
const formJSON = `{
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
}`

// exportTable connects to MySQL and writes the CREATE TABLE statement for
// the given table to a .sql file. It returns the output filename on success.
func exportTable(v map[string]string) (string, error) {
	host := v["host"]
	port := v["port"]
	user := v["user"]
	pass := v["pass"]
	db := v["db"]
	table := v["table"]

	if port == "" {
		port = "3306"
	}

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", user, pass, host, port, db)

	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	defer conn.Close()

	// Strip backticks before quoting as a safety measure.
	safeTable := strings.ReplaceAll(table, "`", "")
	var tableName, createSQL string
	row := conn.QueryRow("SHOW CREATE TABLE `" + safeTable + "`")
	if err := row.Scan(&tableName, &createSQL); err != nil {
		return "", fmt.Errorf("SHOW CREATE TABLE %s: %w", table, err)
	}

	outFile := table + ".sql"
	if err := os.WriteFile(outFile, []byte(createSQL+";\n"), 0o640); err != nil {
		return "", fmt.Errorf("write %s: %w", outFile, err)
	}
	return outFile, nil
}

func main() {
	var def ginput.FormDef
	if err := json.Unmarshal([]byte(formJSON), &def); err != nil {
		fmt.Fprintln(os.Stderr, "form JSON error:", err)
		os.Exit(1)
	}
	// Pre-fill connection fields from the last run (password is never saved).
	if err := ginput.LoadAndApplyDefaults(valuesFile, &def); err != nil {
		_ = err // not fatal; file may not exist on first run
	}

	form, err := ginput.NewFormFromDef(def)
	if err != nil {
		fmt.Fprintln(os.Stderr, "form error:", err)
		os.Exit(1)
	}

	// OnSubmit: persist connection values and export the requested table.
	// The result (or any error) is shown in the status line instead of
	// printing to stdout, so the terminal display stays intact.
	// The message auto-clears after 4 seconds.
	// Returning nil keeps the form active (WithStayOnForm below).
	form.OnSubmit(func(v map[string]string) error {
		toSave := make(map[string]string, len(v))
		for k, val := range v {
			if k != "pass" && k != "table" {
				toSave[k] = val
			}
		}
		if err := ginput.SaveValues(valuesFile, toSave); err != nil {
			fmt.Fprintln(os.Stderr, "Warning: could not save values:", err)
		}

		outFile, err := exportTable(v)
		if err != nil {
			form.SetStatus("Error: "+err.Error(), 4)
		} else {
			form.SetStatus("Saved \u2192 "+outFile, 4)
		}
		return nil // always stay; user exits with Ctrl-C
	})

	// Ctrl-R clears the table field and moves focus back to it so the user
	// can enter a different table name without Tab-navigating manually.
	form.OnCtrl('R', func(vals map[string]string) error {
		form.SetValue("table", "")
		form.SetStatus("Table field cleared (Ctrl-R)", 3)
		return nil
	})
	// Stay on the form after each submit; clear only the table field and
	// keep the cursor on it so the user can export the next table immediately.
	form.WithStayOnForm("table").Focus("table")

	// Clear the terminal so the form appears at the top of a clean screen.
	form.ClearScreen()

	// Status message shown on startup; auto-clears after 5 seconds.
	form.SetStatus("Enter MySQL connection details and table name.", 5)

	// Run the form. If the user cancels with Ctrl-C or an EOF is received,
	// print a message and exit without an error status. Any other error is
	// printed to stderr and exits with a non-zero status.

	if _, err := form.Read(); err != nil {
		switch {
		case errors.Is(err, ginput.ErrInterrupt):
			fmt.Println("\nCancelled.")
		case errors.Is(err, ginput.ErrEOF):
			fmt.Println("\nEOF.")
		default:
			fmt.Fprintln(os.Stderr, "\nError:", err)
			os.Exit(1)
		}
	}
}
