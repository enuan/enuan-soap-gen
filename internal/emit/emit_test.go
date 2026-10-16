package emit_test

import (
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enuan/enuan-soap-gen/internal/config"
	"github.com/enuan/enuan-soap-gen/internal/emit"
	"github.com/enuan/enuan-soap-gen/internal/model"
	"github.com/enuan/enuan-soap-gen/internal/testutil"
)

var update = flag.Bool("update", false, "update golden files")

// project builds the config and the model of a corpus service.
func project(t *testing.T, root, name string) (*config.Config, *model.Model) {
	t.Helper()
	yaml := "service: " + name + "\n" +
		"wsdl: " + testutil.CorpusPath(t, name) + "\n" +
		"module: example.com/" + name + "-gw\n" +
		"gateway: " + name + "-gateway\n"
	cfgPath := filepath.Join(root, "soapgen.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	m, err := model.Build(cfg, testutil.ClientFixture(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return cfg, m
}

func TestRunChatService(t *testing.T) {
	root := t.TempDir()
	cfg, m := project(t, root, "chat_service")

	first, err := emit.Run(emit.Options{Root: root, Config: cfg, Model: m})
	if err != nil {
		t.Fatalf("emit.Run: %v", err)
	}
	if len(first.Written) == 0 {
		t.Fatal("no files written")
	}
	for _, rel := range []string{
		filepath.Join("internal", "api", "operations_gen.go"),
		filepath.Join("internal", "api", "hooks.go"),
		filepath.Join("internal", "api", "operations_gen_test.go"),
		filepath.Join("internal", "config", "config_gen.go"),
		filepath.Join("cmd", "chat_service_gateway", "main.go"),
		"Makefile",
		"go.mod",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("missing generated file %s: %v", rel, err)
		}
	}

	assertGolden(t, filepath.Join(root, "internal", "api", "operations_gen.go"),
		filepath.Join("testdata", "golden", "emit", "chat_service.operations_gen.go"))

	// hooks.go is written once and never overwritten.
	hooks := filepath.Join(root, "internal", "api", "hooks.go")
	custom := "// custom hook wiring\n"
	appendFile(t, hooks, custom)
	if _, err := emit.Run(emit.Options{Root: root, Config: cfg, Model: m}); err != nil {
		t.Fatalf("second emit.Run: %v", err)
	}
	data, err := os.ReadFile(hooks)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), custom) {
		t.Error("hooks.go was overwritten by a second run")
	}

	// Everything else must be byte-identical across runs.
	before := readFile(t, filepath.Join(root, "internal", "api", "operations_gen.go"))
	if _, err := emit.Run(emit.Options{Root: root, Config: cfg, Model: m}); err != nil {
		t.Fatal(err)
	}
	after := readFile(t, filepath.Join(root, "internal", "api", "operations_gen.go"))
	if before != after {
		t.Error("operations_gen.go is not deterministic")
	}
}

// TestRunNestedTypes checks that every DTO type referenced by the generated
// converters is declared (regression: missing declarations for nested types).
func TestRunNestedTypes(t *testing.T) {
	root := t.TempDir()
	cfg, m := project(t, root, "tagliandi")
	if len(m.Types) == 0 {
		t.Fatal("tagliandi model has no nested types")
	}
	if _, err := emit.Run(emit.Options{Root: root, Config: cfg, Model: m}); err != nil {
		t.Fatalf("emit.Run: %v", err)
	}

	file := filepath.Join(root, "internal", "api", "operations_gen.go")
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	declared := map[string]bool{}
	for _, d := range parsed.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
			for _, spec := range gd.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					declared[ts.Name.Name] = true
				}
			}
		}
	}
	missing := map[string]bool{}
	ast.Inspect(parsed, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || !strings.HasSuffix(id.Name, "DTO") {
			return true
		}
		if !declared[id.Name] {
			missing[id.Name] = true
		}
		return true
	})
	if len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for name := range missing {
			names = append(names, name)
		}
		t.Errorf("undefined DTO types referenced by the generated code: %s", strings.Join(names, ", "))
	}
	for _, ty := range m.Types {
		if !declared[ty.Name+"DTO"] {
			t.Errorf("DTO for nested type %s was not generated", ty.Name)
		}
	}
}

// TestRunEndToEnd (SOAPGEN_E2E=1) generates a complete gateway project and
// builds and tests it, proving that the generated code compiles and that the
// generated tests pass. It needs the network (module downloads).
func TestRunEndToEnd(t *testing.T) {
	if !testutil.E2E() {
		t.Skip("set SOAPGEN_E2E=1 to run end-to-end tests")
	}
	root := t.TempDir()
	cfg, m := project(t, root, "chat_service")
	testutil.CopyDir(t, testutil.ClientFixture(t, "chat_service"), filepath.Join(root, "internal", "soap", "chat_service"))
	if _, err := emit.Run(emit.Options{Root: root, Config: cfg, Model: m}); err != nil {
		t.Fatalf("emit.Run: %v", err)
	}
	for _, args := range [][]string{
		{"mod", "tidy"},
		{"build", "./..."},
		{"vet", "./..."},
		{"test", "./..."},
	} {
		cmd := exec.Command("go", args...)
		cmd.Dir = root
		cmd.Stderr = os.Stderr
		if out, err := cmd.Output(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

func assertGolden(t *testing.T, gotPath, goldenRel string) {
	t.Helper()
	golden := filepath.Join(testutil.ModuleRoot(t), goldenRel)
	got, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden: %v (run go test ./internal/emit -update)", err)
	}
	if string(got) != string(want) {
		t.Errorf("%s differs from %s (run go test ./internal/emit -update)", gotPath, goldenRel)
	}
}

func appendFile(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
