package verify

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// WriteText writes the deterministic text report.
func (r *Report) WriteText(w io.Writer) error {
	ok, warn, fail := r.Counts()
	if _, err := fmt.Fprintf(w, "verify %s (%s): %d ok, %d warnings, %d failed\n", r.Service, r.Root, ok, warn, fail); err != nil {
		return err
	}
	width := 0
	for _, c := range r.Checks {
		if len(c.Name) > width {
			width = len(c.Name)
		}
	}
	for _, c := range r.Checks {
		marker := "ok  "
		switch c.Status {
		case Warn:
			marker = "warn"
		case Fail:
			marker = "FAIL"
		}
		detail := c.Detail
		if detail != "" {
			detail = "  " + detail
		}
		if _, err := fmt.Fprintf(w, "%s  %-*s%s\n", marker, width, c.Name, detail); err != nil {
			return err
		}
	}
	if fail > 0 {
		fmt.Fprintln(w, "\nverification failed")
		return nil
	}
	fmt.Fprintln(w, "\nverification passed")
	return nil
}

// WriteJSON writes the machine-readable report.
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// Summary returns a one-line summary for the gen pipeline.
func (r *Report) Summary() string {
	ok, warn, fail := r.Counts()
	var fails []string
	for _, c := range r.Checks {
		if c.Status == Fail {
			fails = append(fails, c.Name)
		}
	}
	line := fmt.Sprintf("verify: %d ok, %d warnings, %d failed", ok, warn, fail)
	if len(fails) > 0 {
		line += " (" + strings.Join(fails, ", ") + ")"
	}
	return line
}
