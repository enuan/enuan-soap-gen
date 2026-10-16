package model_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/enuan/enuan-soap-gen/internal/config"
	"github.com/enuan/enuan-soap-gen/internal/model"
	"github.com/enuan/enuan-soap-gen/internal/testutil"
)

// TestBuildSchemaDocs checks the WSDL-derived enrichment: xs:documentation on
// elements, simple types and portType operations, plus the XSD facets that map
// onto JSON Schema keywords (pattern, length, inclusive bounds, default).
func TestBuildSchemaDocs(t *testing.T) {
	clientDir := filepath.Join(testutil.ModuleRoot(t), "internal", "model", "testdata", "schema_docs")
	cfg := &config.Config{
		Service: "schemadocs",
		WSDL:    filepath.Join(clientDir, "definitions.wsdl"),
		Module:  "example.com/schemadocs",
	}
	m, err := model.Build(cfg, clientDir)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(m.Operations) != 1 {
		t.Fatalf("got %d operations, want 1", len(m.Operations))
	}
	op := m.Operations[0]
	if op.Name != "GetVehicle" {
		t.Fatalf("operation name = %q, want GetVehicle", op.Name)
	}
	if want := "Looks up a vehicle by plate or VIN."; !strings.Contains(op.Docs, want) {
		t.Errorf("operation docs = %q, want %q", op.Docs, want)
	}

	in := map[string]*model.Field{}
	for _, f := range op.Input.Fields {
		in[f.GoName] = f
	}
	out := map[string]*model.Field{}
	for _, f := range op.Output.Fields {
		out[f.GoName] = f
	}

	plate := in["Plate"]
	if plate == nil {
		t.Fatal("input has no Plate field")
	}
	if !strings.Contains(plate.Doc, `"AA123BB"`) || !strings.Contains(plate.Doc, "format.") {
		t.Errorf("plate doc = %q, want the element documentation", plate.Doc)
	}
	if want := `[A-Z]{2}[0-9]{3}[A-Z]{2}`; plate.Pattern != want {
		t.Errorf("plate pattern = %q, want %q", plate.Pattern, want)
	}
	if plate.Default != "AA000AA" {
		t.Errorf("plate default = %q, want AA000AA", plate.Default)
	}
	if plate.Required {
		t.Error("plate is optional (minOccurs=0), but Required is true")
	}

	vin := in["Vin"]
	if vin == nil {
		t.Fatal("input has no Vin field")
	}
	if vin.MinLength != 17 || vin.MaxLength != 17 {
		t.Errorf("vin length = [%d,%d], want [17,17]", vin.MinLength, vin.MaxLength)
	}
	if !strings.Contains(vin.Doc, "exactly 17 characters") {
		t.Errorf("vin doc = %q", vin.Doc)
	}

	year := in["Year"]
	if year == nil {
		t.Fatal("input has no Year field")
	}
	if year.Minimum != "1900" || year.Maximum != "2100" {
		t.Errorf("year bounds = [%q,%q], want [1900,2100]", year.Minimum, year.Maximum)
	}

	if result := out["Result"]; result == nil || !strings.Contains(result.Doc, "Business XML payload") {
		t.Errorf("result field = %+v, want documentation from the WSDL", result)
	}
}
