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
	return projectConfig(t, root, yaml)
}

// projectConfig builds the config and the model from an explicit soapgen.yaml.
func projectConfig(t *testing.T, root, yaml string) (*config.Config, *model.Model) {
	t.Helper()
	cfgPath := filepath.Join(root, "soapgen.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	m, err := model.Build(cfg, testutil.ClientFixture(t, cfg.PackageName()))
	if err != nil {
		t.Fatal(err)
	}
	return cfg, m
}

// tagliandiAuthYAML configures the tagliandi corpus with login auth and state
// mapping; it is the F3 reference configuration.
func tagliandiAuthYAML(t *testing.T) string {
	return "service: tagliandi\n" +
		"wsdl: " + testutil.CorpusPath(t, "tagliandi") + "\n" +
		"module: example.com/tagliandi-gw\n" +
		"gateway: tagliandi-gateway\n" +
		"auth:\n" +
		"  strategy: login\n" +
		"  login:\n" +
		"    operation: Operation1\n" +
		"    token_field: LoginResult.SessionId\n" +
		"    username_field: Request.CompanyLogin\n" +
		"    password_field: Request.Password\n" +
		"    inject: Context.SessionId\n" +
		"error_map:\n" +
		"  state:\n" +
		"    success_field: Successo\n" +
		"    code_field: CodiceErrore\n" +
		"    message_field: Messaggio\n"
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
		filepath.Join("cmd", "chat_service-gateway", "main.go"),
		"Makefile",
		"go.mod",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("missing generated file %s: %v", rel, err)
		}
	}

	assertGolden(t, filepath.Join(root, "internal", "api", "operations_gen.go"),
		filepath.Join("testdata", "golden", "emit", "chat_service.operations_gen.go"))

	// The Makefile installs a static binary named after the configured gateway.
	makefile := readFile(t, filepath.Join(root, "Makefile"))
	for _, want := range []string{
		".PHONY: gen gen-client verify install",
		"CGO_ENABLED=0 go install -trimpath ./cmd/chat_service-gateway",
	} {
		if !strings.Contains(makefile, want) {
			t.Errorf("Makefile is missing %q", want)
		}
	}

	// The generated config defaults to 127.0.0.1:8080 when no confoo document
	// is present.
	configGen := readFile(t, filepath.Join(root, "internal", "config", "config_gen.go"))
	for _, want := range []string{`Host: "127.0.0.1"`, "Port: 8080"} {
		if !strings.Contains(configGen, want) {
			t.Errorf("config_gen.go is missing the default %s", want)
		}
	}

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

// TestRunSchemaDocs renders a fixture whose WSDL carries documentation and
// XSD facets, and checks they are emitted as DTO field tags.
func TestRunSchemaDocs(t *testing.T) {
	root := t.TempDir()
	clientDir := filepath.Join(testutil.ModuleRoot(t), "internal", "model", "testdata", "schema_docs")
	yaml := "service: schemadocs\n" +
		"wsdl: " + filepath.Join(clientDir, "definitions.wsdl") + "\n" +
		"module: example.com/schemadocs-gw\n" +
		"gateway: schemadocs-gateway\n"
	cfgPath := filepath.Join(root, "soapgen.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	m, err := model.Build(cfg, clientDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := emit.Run(emit.Options{Root: root, Config: cfg, Model: m}); err != nil {
		t.Fatalf("emit.Run: %v", err)
	}
	ops := readFile(t, filepath.Join(root, "internal", "api", "operations_gen.go"))
	for _, want := range []string{
		`doc:"Vehicle plate in 'AA123BB' format."`,
		`pattern:"[A-Z]{2}[0-9]{3}[A-Z]{2}"`,
		`minLength:"17"`,
		`maxLength:"17"`,
		`minimum:"1900"`,
		`maximum:"2100"`,
		`default:"AA000AA"`,
		`doc:"VIN: exactly 17 characters."`,
		`doc:"Business XML payload."`,
	} {
		if !strings.Contains(ops, want) {
			t.Errorf("operations_gen.go is missing %s", want)
		}
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

// TestRunAuthState checks the generated session/login and state-mapping code
// with the tagliandi reference configuration, and that the generated project
// builds and its generated tests pass (SOAPGEN_E2E=1).
func TestRunAuthState(t *testing.T) {
	root := t.TempDir()
	cfg, m := projectConfig(t, root, tagliandiAuthYAML(t))
	res, err := emit.Run(emit.Options{Root: root, Config: cfg, Model: m})
	if err != nil {
		t.Fatalf("emit.Run: %v", err)
	}
	written := map[string]bool{}
	for _, f := range res.Written {
		written[filepath.Base(f)] = true
	}
	if !written["auth_gen.go"] {
		t.Fatal("auth_gen.go was not generated for auth.strategy=login")
	}

	apiDir := filepath.Join(root, "internal", "api")
	auth := readFile(t, filepath.Join(apiDir, "auth_gen.go"))
	for _, want := range []string{
		"func NewSessionProvider(client *soapclient.Client, username, password string) SessionProvider",
		"s.client.Operation1(ctx, loginWrapperToSOAP(req))",
		"req.Request = &LoginRequestDTO{}",
		"req.Request.CompanyLogin = &s.username",
		"req.Request.Password = &s.password",
		"if resp.LoginResult != nil && resp.LoginResult.SessionId != nil {",
		"if token == " + "\"\" {",
	} {
		if !strings.Contains(auth, want) {
			t.Errorf("auth_gen.go is missing %q", want)
		}
	}

	ops := readFile(t, filepath.Join(apiDir, "operations_gen.go"))
	for _, want := range []string{
		"func (s *Service) applySessionOperation2(ctx context.Context, req *Operation2Request) error",
		"req.Context = &BaseContextDTO{}",
		"req.Context.SessionId = &token",
		"if err := out.Body.CheckState(); err != nil {",
		"func (r *IntervalsResultDTO) CheckState() error",
		"func (r *Operation2Response) CheckState() error",
		"func stateMessage(parts ...any) string",
	} {
		if !strings.Contains(ops, want) {
			t.Errorf("operations_gen.go is missing %q", want)
		}
	}
	if strings.Contains(ops, "applySessionOperation1") {
		t.Error("the login operation must not inject its own token")
	}

	tests := readFile(t, filepath.Join(apiDir, "operations_gen_test.go"))
	for _, want := range []string{
		"func TestSessionProviderLogin(t *testing.T)",
		"func TestOperation2StateError(t *testing.T)",
		"type stubSession struct{ token string }",
		"svc.Hooks = Hooks{Session: &stubSession{token: \"test-token\"}}",
	} {
		if !strings.Contains(tests, want) {
			t.Errorf("operations_gen_test.go is missing %q", want)
		}
	}

	main := readFile(t, filepath.Join(root, cfg.CmdDir, "main.go"))
	if !strings.Contains(main, "svc.Hooks = restapi.Hooks{Session: restapi.NewSessionProvider(client, cfg.Username, cfg.Password)}") {
		t.Error("main.go does not wire the session provider")
	}
	configGen := readFile(t, filepath.Join(root, "internal", "config", "config_gen.go"))
	for _, want := range []string{"Username   string", "Password   string"} {
		if !strings.Contains(configGen, want) {
			t.Errorf("config_gen.go is missing %q", want)
		}
	}

	if !testutil.E2E() {
		t.Skip("generated project build requires SOAPGEN_E2E=1")
	}
	testutil.CopyDir(t, testutil.ClientFixture(t, "tagliandi"), filepath.Join(root, "internal", "soap", "tagliandi"))
	goRun(t, root, "mod", "tidy")
	goRun(t, root, "build", "./...")
	goRun(t, root, "vet", "./...")
	goRun(t, root, "test", "./...")
}

// TestRunAuthStateErrors checks the configuration errors of auth/error_map.
func TestRunAuthStateErrors(t *testing.T) {
	base := tagliandiAuthYAML(t)
	cases := []struct {
		name   string
		yaml   string
		errSub string
	}{
		{
			name:   "unknown login operation",
			yaml:   strings.Replace(base, "operation: Operation1", "operation: Nope", 1),
			errSub: "not among the generated operations",
		},
		{
			name:   "unknown token field",
			yaml:   strings.Replace(base, "token_field: LoginResult.SessionId", "token_field: LoginResult.Nope", 1),
			errSub: "LoginSeviceResultDTO has no field Nope",
		},
		{
			name:   "inject path missing on an operation",
			yaml:   strings.Replace(base, "inject: Context.SessionId", "inject: Nope.SessionId", 1),
			errSub: "auth.login.inject for operation Operation2",
		},
		{
			name: "state success field must be a bool",
			yaml: strings.Replace(strings.Replace(base,
				"success_field: Successo", "success_field: CodiceErrore", 1),
				"code_field: CodiceErrore", "code_field: Successo", 1),
			errSub: "a bool (or *bool) is required",
		},
		{
			name:   "unknown state field",
			yaml:   strings.Replace(base, "success_field: Successo", "success_field: Nope", 1),
			errSub: "no generated type has a Nope field",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			cfgPath := filepath.Join(root, "soapgen.yaml")
			if err := os.WriteFile(cfgPath, []byte(tc.yaml), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				t.Fatal(err)
			}
			m, err := model.Build(cfg, testutil.ClientFixture(t, "tagliandi"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = emit.Run(emit.Options{Root: root, Config: cfg, Model: m})
			if err == nil || !strings.Contains(err.Error(), tc.errSub) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.errSub)
			}
		})
	}
}

// TestRunHeaderAuth checks the generated HTTP header interceptor.
func TestRunHeaderAuth(t *testing.T) {
	root := t.TempDir()
	yaml := tagliandiAuthYAML(t)
	yaml = strings.Replace(yaml, "  strategy: login", "  strategy: header\n  header: X-Api-Key", 1)
	yaml = strings.Replace(yaml, "token_field: LoginResult.SessionId", "token_field: Unused", 1)
	yaml = strings.Replace(yaml, "    username_field: Request.CompanyLogin\n", "", 1)
	yaml = strings.Replace(yaml, "    password_field: Request.Password\n", "", 1)
	yaml = strings.Replace(yaml, "    inject: Context.SessionId\n", "", 1)
	cfg, m := projectConfig(t, root, yaml)
	res, err := emit.Run(emit.Options{Root: root, Config: cfg, Model: m})
	if err != nil {
		t.Fatalf("emit.Run: %v", err)
	}
	for _, f := range res.Written {
		if filepath.Base(f) == "auth_gen.go" {
			t.Error("auth_gen.go must not be generated for auth.strategy=header")
		}
	}
	main := readFile(t, filepath.Join(root, cfg.CmdDir, "main.go"))
	for _, want := range []string{"authHeaderInterceptor", `req.Header.Set("X-Api-Key", value)`, "cfg.APIKey"} {
		if !strings.Contains(main, want) {
			t.Errorf("main.go is missing %q", want)
		}
	}
}

// TestRunEndToEnd (SOAPGEN_E2E=1) generates complete gateway projects and
// builds and tests them, proving that the generated code compiles and that the
// generated tests pass. It needs the network (module downloads).
func TestRunEndToEnd(t *testing.T) {
	if !testutil.E2E() {
		t.Skip("set SOAPGEN_E2E=1 to run end-to-end tests")
	}
	cases := []struct {
		name string
		yaml func(*testing.T) string
	}{
		{name: "chat_service", yaml: func(t *testing.T) string { return "" }},
		{name: "tagliandi", yaml: tagliandiAuthYAML},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			var cfg *config.Config
			var m *model.Model
			if tc.yaml(t) == "" {
				cfg, m = project(t, root, "chat_service")
			} else {
				cfg, m = projectConfig(t, root, tc.yaml(t))
			}
			testutil.CopyDir(t, testutil.ClientFixture(t, tc.name), filepath.Join(root, "internal", "soap", tc.name))
			if _, err := emit.Run(emit.Options{Root: root, Config: cfg, Model: m}); err != nil {
				t.Fatalf("emit.Run: %v", err)
			}
			goRun(t, root, "mod", "tidy")
			goRun(t, root, "build", "./...")
			goRun(t, root, "vet", "./...")
			goRun(t, root, "test", "./...")
		})
	}
}

func goRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	if out, err := cmd.Output(); err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
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
