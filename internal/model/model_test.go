package model_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enuan/enuan-soap-gen/internal/clientgen"
	"github.com/enuan/enuan-soap-gen/internal/config"
	"github.com/enuan/enuan-soap-gen/internal/model"
	"github.com/enuan/enuan-soap-gen/internal/testutil"
)

var corpusCases = []struct {
	name      string
	wantOps   int
	wantTypes []string // nested types that must be reachable
}{
	{name: "chat_service", wantOps: 4},
	{name: "tagliandi", wantOps: 5, wantTypes: []string{"BaseContext"}},
}

// TestBuildCorpus checks the invariants that the gateway depends on, using the
// committed soap-go client fixtures.
func TestBuildCorpus(t *testing.T) {
	for _, tc := range corpusCases {
		t.Run(tc.name, func(t *testing.T) {
			wsdl := testutil.CorpusPath(t, tc.name)
			cfg := &config.Config{
				Service: tc.name,
				WSDL:    wsdl,
				Module:  "example.com/" + tc.name + "-gw",
				Naming:  config.Naming{JSON: "camel"},
			}
			m, err := model.Build(cfg, testutil.ClientFixture(t, tc.name))
			if err != nil {
				t.Fatalf("model.Build: %v", err)
			}

			if len(m.Operations) != tc.wantOps {
				t.Fatalf("operations = %d, want %d", len(m.Operations), tc.wantOps)
			}
			if m.Endpoint == "" {
				t.Error("endpoint is empty")
			}
			for _, op := range m.Operations {
				if op.Method == "" {
					t.Errorf("%s: empty client method", op.Name)
				}
				if op.SOAPAction == "" {
					t.Errorf("%s: empty SOAPAction", op.Name)
				}
				if op.Input == nil || op.Output == nil {
					t.Fatalf("%s: missing input/output message", op.Name)
				}
				if op.Input.GoType == "" || !strings.HasSuffix(op.Input.GoType, "Wrapper") {
					t.Errorf("%s: unexpected input Go type %q", op.Name, op.Input.GoType)
				}
				for _, f := range op.Input.Fields {
					if f.XMLName == "" {
						t.Errorf("%s: input field %s without XML name", op.Name, f.GoName)
					}
					if f.JSONName == "" || f.JSONName[0] < 'a' || f.JSONName[0] > 'z' {
						t.Errorf("%s: JSON name %q is not camelCase", op.Name, f.JSONName)
					}
				}
			}

			byName := map[string]*model.Type{}
			for _, ty := range m.Types {
				byName[ty.Name] = ty
			}
			for _, want := range tc.wantTypes {
				ty, ok := byName[want]
				if !ok {
					t.Errorf("nested type %s not reachable from the selected operations", want)
					continue
				}
				if len(ty.Fields) == 0 {
					t.Errorf("nested type %s has no fields", want)
				}
			}
		})
	}
}

// TestBuildInheritedFields is a regression test for the soap-go inheritance
// fix: fields declared by a base type must reach the derived DTOs.
func TestBuildInheritedFields(t *testing.T) {
	cfg := &config.Config{
		Service: "tagliandi",
		WSDL:    testutil.CorpusPath(t, "tagliandi"),
		Module:  "example.com/tagliandi-gw",
		Naming:  config.Naming{JSON: "camel"},
	}
	m, err := model.Build(cfg, testutil.ClientFixture(t, "tagliandi"))
	if err != nil {
		t.Fatalf("model.Build: %v", err)
	}
	// BaseContext inherits SessionId from BaseRequest; it is reachable and its
	// DTO must expose it with the camelCase JSON name.
	for _, ty := range m.Types {
		if ty.Name != "BaseContext" {
			continue
		}
		for _, f := range ty.Fields {
			if f.XMLName == "SessionId" {
				if f.JSONName != "sessionId" {
					t.Errorf("SessionId JSON name = %q, want sessionId", f.JSONName)
				}
				return
			}
		}
		t.Fatal("BaseContext has no SessionId field (inheritance regression)")
	}
	t.Fatal("BaseContext not reachable from the selected operations")
}

// TestBuildSelectOperations checks that include/exclude filtering works and
// that the preserve naming mode leaves the JSON names untouched.
func TestBuildSelectOperations(t *testing.T) {
	cfg := &config.Config{
		Service:    "tagliandi",
		WSDL:       testutil.CorpusPath(t, "tagliandi"),
		Module:     "example.com/tagliandi-gw",
		Naming:     config.Naming{JSON: "preserve"},
		Operations: config.Operations{Include: []string{"Operation2"}},
	}
	m, err := model.Build(cfg, testutil.ClientFixture(t, "tagliandi"))
	if err != nil {
		t.Fatalf("model.Build: %v", err)
	}
	if len(m.Operations) != 1 || m.Operations[0].Name != "Operation2" {
		t.Fatalf("operations = %+v, want only Operation2", m.Operations)
	}
	for _, f := range m.Operations[0].Input.Fields {
		if f.JSONName != f.XMLName {
			t.Errorf("naming=preserve: JSON name %q != XML name %q", f.JSONName, f.XMLName)
		}
	}
}

// TestBuildDriftDetection proves that a mismatch between the WSDL and the
// generated client is fatal (codegen drift).
func TestBuildDriftDetection(t *testing.T) {
	clientDir := testutil.CopyDir(t, testutil.ClientFixture(t, "tagliandi"), filepath.Join(t.TempDir(), "soap"))

	// Drop the inherited SessionId field from the generated client: the WSDL
	// still declares it, so the model must refuse to build.
	removed := 0
	files, err := filepath.Glob(filepath.Join(clientDir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var kept []string
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "\tSessionId ") {
				removed++
				continue
			}
			kept = append(kept, line)
		}
		if err := os.WriteFile(file, []byte(strings.Join(kept, "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if removed == 0 {
		t.Fatal("no SessionId field found in the generated client")
	}

	cfg := &config.Config{
		Service: "tagliandi",
		WSDL:    testutil.CorpusPath(t, "tagliandi"),
		Module:  "example.com/tagliandi-gw",
		Naming:  config.Naming{JSON: "camel"},
	}
	_, err = model.Build(cfg, clientDir)
	if err == nil || !strings.Contains(err.Error(), "codegen drift") {
		t.Fatalf("expected codegen drift error, got %v", err)
	}
}

// TestBuildUnknownOperation checks the error for an operation that has no
// generated client method.
func TestBuildUnknownOperation(t *testing.T) {
	cfg := &config.Config{
		Service:    "tagliandi",
		WSDL:       testutil.CorpusPath(t, "tagliandi"),
		Module:     "example.com/tagliandi-gw",
		Naming:     config.Naming{JSON: "camel"},
		Operations: config.Operations{Include: []string{"NoSuchOperation"}},
	}
	if _, err := model.Build(cfg, testutil.ClientFixture(t, "tagliandi")); err == nil {
		t.Fatal("expected error for an operation without a generated method")
	}
}

// TestClientFixturesMatchGenerator (SOAPGEN_E2E=1) regenerates the soap-go
// clients with the pinned CLI and compares the result with the committed
// fixtures. With UPDATE_FIXTURES=1 it rewrites them instead.
func TestClientFixturesMatchGenerator(t *testing.T) {
	if !testutil.E2E() {
		t.Skip("set SOAPGEN_E2E=1 to run end-to-end tests")
	}
	soapBin := testutil.SoapBinary(t)
	for _, tc := range corpusCases {
		t.Run(tc.name, func(t *testing.T) {
			generated := filepath.Join(t.TempDir(), tc.name)
			if err := clientgen.Run(context.Background(), clientgen.Options{
				WSDLPath: testutil.CorpusPath(t, tc.name),
				Package:  tc.name,
				OutDir:   generated,
				SoapBin:  soapBin,
			}); err != nil {
				t.Fatalf("clientgen: %v", err)
			}
			fixture := testutil.ClientFixture(t, tc.name)
			if testutil.UpdateFixtures() {
				if err := os.RemoveAll(fixture); err != nil {
					t.Fatal(err)
				}
				testutil.CopyDir(t, generated, fixture)
				return
			}
			compareTrees(t, fixture, generated)
		})
	}
}

func compareTrees(t *testing.T, want, got string) {
	t.Helper()
	entries, err := os.ReadDir(want)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		wantData, err := os.ReadFile(filepath.Join(want, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		gotData, err := os.ReadFile(filepath.Join(got, e.Name()))
		if err != nil {
			t.Errorf("missing generated file %s: %v", e.Name(), err)
			continue
		}
		if string(wantData) != string(gotData) {
			t.Errorf("%s differs from the committed fixture (run make testdata-regen)", e.Name())
		}
	}
	gotEntries, err := os.ReadDir(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotEntries) != len(entries) {
		t.Errorf("generated %d files, fixture has %d", len(gotEntries), len(entries))
	}
}
