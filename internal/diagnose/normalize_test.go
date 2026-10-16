package diagnose_test

import (
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"

	"github.com/enuan/enuan-soap-gen/internal/diagnose"
	"github.com/enuan/enuan-soap-gen/internal/testutil"
	"github.com/enuan/soap-go/wsdl"
	"github.com/enuan/soap-go/xsd"
)

func corpusRaw(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(testutil.ModuleRoot(t), "testdata", "corpus", name+".wsdl"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestNormalizeChatService(t *testing.T) {
	raw := corpusRaw(t, "chat_service")
	out, stats, err := diagnose.Normalize(raw, diagnose.DefaultNormalizeOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.RemovedSchemas) != 1 {
		t.Errorf("removed schemas = %v, want 1", stats.RemovedSchemas)
	}
	if len(stats.RemovedBindings) != 1 || len(stats.RemovedPorts) != 1 {
		t.Errorf("removed bindings/ports = %v/%v, want 1/1", stats.RemovedBindings, stats.RemovedPorts)
	}
	if bytes.Contains(out, []byte(diagnose.MSSerializationNS)) {
		t.Error("Microsoft serialization schema survived normalization")
	}
	if bytes.Contains(out, []byte("duration")) {
		t.Error("shadowing simple type survived normalization")
	}

	normDefs := parseBytes(t, out)
	if len(normDefs.Binding) != 1 {
		t.Errorf("bindings after normalization = %d, want 1", len(normDefs.Binding))
	}
	if len(normDefs.Types.Schemas) != 1 {
		t.Errorf("schemas after normalization = %d, want 1", len(normDefs.Types.Schemas))
	}

	// Idempotency: normalizing a normalized document must be a no-op.
	out2, stats2, err := diagnose.Normalize(out, diagnose.DefaultNormalizeOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, out2) {
		t.Error("normalization is not idempotent")
	}
	if !stats2.Empty() {
		t.Errorf("second normalization changed something: %+v", stats2)
	}
}

func TestNormalizeFlattensInheritance(t *testing.T) {
	cases := []struct {
		corpus string
		typ    string
		field  string
	}{
		{"tagliandi", "Type1", "SessionId"},
		{"rtws", "RTDaTargaResult", "state"},
	}
	for _, tc := range cases {
		t.Run(tc.corpus, func(t *testing.T) {
			raw := corpusRaw(t, tc.corpus)
			before := parseBytes(t, raw)
			if hasElement(before, tc.typ, tc.field) {
				t.Fatalf("precondition failed: %s already contains %s before normalization (corpus no longer exercises inheritance)", tc.typ, tc.field)
			}

			out, stats, err := diagnose.Normalize(raw, diagnose.DefaultNormalizeOptions())
			if err != nil {
				t.Fatal(err)
			}
			if len(stats.UnflattenedTypes) != 0 {
				t.Errorf("unflattened types: %v", stats.UnflattenedTypes)
			}
			if bytes.Contains(out, []byte("complexContent")) {
				t.Error("complexContent survived normalization")
			}
			after := parseBytes(t, out)
			if !hasElement(after, tc.typ, tc.field) {
				t.Errorf("inherited field %s missing from %s after normalization", tc.field, tc.typ)
			}
		})
	}
}

func TestNormalizeKeepsUnsupportedInheritance(t *testing.T) {
	path := filepath.Join(testutil.ModuleRoot(t), "internal", "diagnose", "testdata", "normalize", "unflattened.wsdl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out, stats, err := diagnose.Normalize(raw, diagnose.DefaultNormalizeOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.UnflattenedTypes) != 1 || stats.UnflattenedTypes[0] != "Derived" {
		t.Errorf("unflattened = %v, want [Derived]", stats.UnflattenedTypes)
	}
	if !bytes.Contains(out, []byte("complexContent")) {
		t.Error("unsupported inheritance should be left intact")
	}
}

func parseBytes(t *testing.T, raw []byte) *wsdl.Definitions {
	t.Helper()
	var defs wsdl.Definitions
	if err := xml.Unmarshal(raw, &defs); err != nil {
		t.Fatalf("parse normalized document: %v", err)
	}
	return &defs
}

func hasElement(defs *wsdl.Definitions, typeName, elemName string) bool {
	if defs.Types == nil {
		return false
	}
	for i := range defs.Types.Schemas {
		s := &defs.Types.Schemas[i]
		for j := range s.ComplexTypes {
			ct := &s.ComplexTypes[j]
			if ct.Name == typeName && sequenceHasElement(ct.Sequence, elemName) {
				return true
			}
		}
	}
	return false
}

func sequenceHasElement(seq *xsd.Sequence, name string) bool {
	if seq == nil {
		return false
	}
	for i := range seq.Elements {
		if seq.Elements[i].Name == name {
			return true
		}
	}
	return false
}
