package diagnose

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// statsOrder fixes the field order of the rendered stats line.
var statsOrder = []struct {
	name  string
	value func(Stats) int
}{
	{"services", func(s Stats) int { return s.Services }},
	{"ports", func(s Stats) int { return s.Ports }},
	{"portTypes", func(s Stats) int { return s.PortTypes }},
	{"operations", func(s Stats) int { return s.Operations }},
	{"messages", func(s Stats) int { return s.Messages }},
	{"bindings", func(s Stats) int { return s.Bindings }},
	{"soap11", func(s Stats) int { return s.SOAP11Bindings }},
	{"soap12", func(s Stats) int { return s.SOAP12Bindings }},
	{"http", func(s Stats) int { return s.HTTPBindings }},
	{"schemas", func(s Stats) int { return s.Schemas }},
	{"complexTypes", func(s Stats) int { return s.ComplexTypes }},
	{"simpleTypes", func(s Stats) int { return s.SimpleTypes }},
	{"elements", func(s Stats) int { return s.Elements }},
	{"localElements", func(s Stats) int { return s.LocalElements }},
	{"attributes", func(s Stats) int { return s.Attributes }},
	{"enumerations", func(s Stats) int { return s.Enumerations }},
	{"complexExtensions", func(s Stats) int { return s.ComplexExtensions }},
	{"nillable", func(s Stats) int { return s.Nillable }},
	{"unbounded", func(s Stats) int { return s.Unbounded }},
}

// String renders the stats as a single stable line.
func (s Stats) String() string {
	var b strings.Builder
	for i, f := range statsOrder {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%s=%d", f.name, f.value(s))
	}
	return b.String()
}

// WriteText renders a deterministic, reviewable report. It is the format used
// by golden tests and by the CLI's default output.
func (r *Report) WriteText(w io.Writer) error {
	if r.Ref != "" {
		fmt.Fprintf(w, "ref: %s\n", r.Ref)
	}
	if r.SHA256 != "" {
		fmt.Fprintf(w, "sha256: %s\n", r.SHA256)
	}
	fmt.Fprintf(w, "service: %s\n", r.Service)
	fmt.Fprintf(w, "targetNamespace: %s\n", r.TargetNamespace)
	fmt.Fprintf(w, "stats: %s\n", r.Stats.String())
	fmt.Fprintf(w, "findings: %d\n", len(r.Findings))
	for _, f := range r.Findings {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", f.Severity, f.Rule, f.Location, f.Message)
	}
	return nil
}

// WriteJSON renders the report as indented JSON.
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
