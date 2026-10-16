package diagnose_test

import (
	"bytes"
	"encoding/xml"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enuan/enuan-soap-gen/internal/diagnose"
	"github.com/enuan/enuan-soap-gen/internal/testutil"
	"github.com/enuan/soap-go/wsdl"
)

var update = flag.Bool("update", false, "update golden files")

func loadDefinitions(t *testing.T, path string) (*wsdl.Definitions, []byte) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var defs wsdl.Definitions
	if err := xml.Unmarshal(raw, &defs); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return &defs, raw
}

func checkFile(t *testing.T, path string) *diagnose.Report {
	t.Helper()
	defs, raw := loadDefinitions(t, path)
	return diagnose.Check(defs, raw)
}

// TestGoldenReports pins the full report of every corpus and synthetic case.
func TestGoldenReports(t *testing.T) {
	root := testutil.ModuleRoot(t)
	cases := map[string]string{}
	for _, dir := range []string{
		filepath.Join(root, "testdata", "corpus"),
		filepath.Join(root, "internal", "diagnose", "testdata", "synthetic"),
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".wsdl") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".wsdl")
			cases[name] = filepath.Join(dir, e.Name())
		}
	}
	if len(cases) < 8 {
		t.Fatalf("corpus unexpectedly small: %d cases", len(cases))
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			report := checkFile(t, input)
			var buf bytes.Buffer
			if err := report.WriteText(&buf); err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join(root, "testdata", "golden", "diagnose", name+".txt")
			if *update {
				if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run with -update to create)", err)
			}
			if got := buf.String(); got != string(want) {
				t.Fatalf("report mismatch for %s (-update to regenerate)\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
			}
		})
	}
}

// TestSyntheticFindings asserts the rules that must fire for the synthetic
// cases, independently of exact golden text.
func TestSyntheticFindings(t *testing.T) {
	root := testutil.ModuleRoot(t)
	cases := map[string][]string{
		"rpc_encoded":  {"unsupported/rpc-style", "unsupported/encoded-use"},
		"soap12_only":  {"unsupported/soap12-only"},
		"wsdl_import":  {"unsupported/wsdl-import"},
		"http_binding": {"info/http-binding-ignored"},
		"choice_any": {
			"unsupported/xsd-choice",
			"unsupported/xsd-any",
			"unsupported/substitution-group",
			"unsupported/abstract-type",
			"warning/xsd-list-union",
			"warning/one-way-operation",
			"warning/untyped-element",
			"warning/empty-message",
			"config-hint/session-field",
			"config-hint/state-field",
		},
	}
	for name, wantRules := range cases {
		t.Run(name, func(t *testing.T) {
			report := checkFile(t, filepath.Join(root, "internal", "diagnose", "testdata", "synthetic", name+".wsdl"))
			got := map[string]bool{}
			for _, f := range report.Findings {
				got[f.Rule] = true
			}
			for _, rule := range wantRules {
				if !got[rule] {
					t.Errorf("missing finding %q (rules: %v)", rule, sortedRules(report))
				}
			}
		})
	}
}

func sortedRules(report *diagnose.Report) []string {
	var out []string
	for _, f := range report.Findings {
		out = append(out, string(f.Severity)+" "+f.Rule)
	}
	return out
}

// TestCorpusPitfalls documents the historic codegen traps that the corpus must
// keep exercising.
func TestCorpusPitfalls(t *testing.T) {
	root := testutil.ModuleRoot(t)
	corpus := func(name string) *diagnose.Report {
		return checkFile(t, filepath.Join(root, "testdata", "corpus", name+".wsdl"))
	}

	chat := corpus("chat_service")
	for _, rule := range []string{
		"known-pitfall/ms-serialization-schema",
		"known-pitfall/simple-type-shadowing",
		"known-pitfall/duplicate-soap11-bindings",
	} {
		if !hasRule(chat, rule) {
			t.Errorf("chat_service: missing %s", rule)
		}
	}
	if chat.HasErrors() {
		t.Errorf("chat_service: unexpected errors: %v", sortedRules(chat))
	}

	rtws := corpus("rtws")
	if got := chatExtensionCount(rtws); got != 42 {
		t.Errorf("rtws: complexContent extensions = %d, want 42", got)
	}
	if !hasRule(rtws, "config-hint/state-field") {
		t.Error("rtws: missing config-hint/state-field (State/MethodState)")
	}

	tagliandi := corpus("tagliandi")
	if !hasRule(tagliandi, "config-hint/session-field") {
		t.Error("tagliandi: missing config-hint/session-field (SessionId)")
	}
	if !hasRule(tagliandi, "config-hint/state-field") {
		t.Error("tagliandi: missing config-hint/state-field (Successo/CodiceErrore)")
	}
}

func hasRule(report *diagnose.Report, rule string) bool {
	for _, f := range report.Findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

func chatExtensionCount(report *diagnose.Report) int {
	return report.Stats.ComplexExtensions
}
