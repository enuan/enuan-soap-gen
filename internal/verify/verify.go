// Package verify validates a generated gateway: the soap-go client, the Huma
// layer and the auth/state wiring. It is the last pipeline stage and is also
// exposed through "enuan-soap-gen verify".
package verify

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/enuan/enuan-soap-gen/internal/config"
	"github.com/enuan/enuan-soap-gen/internal/ingest"
)

// Status is the outcome of one check.
type Status string

// Check statuses.
const (
	OK   Status = "ok"
	Warn Status = "warn"
	Fail Status = "fail"
)

// Check is a single verification result.
type Check struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Report is the result of a verification run.
type Report struct {
	Service string  `json:"service"`
	Root    string  `json:"root"`
	Checks  []Check `json:"checks"`
}

// Add appends a check.
func (r *Report) Add(name string, status Status, format string, args ...any) {
	r.Checks = append(r.Checks, Check{Name: name, Status: status, Detail: fmt.Sprintf(format, args...)})
}

// Failed reports whether any check failed.
func (r *Report) Failed() bool {
	for _, c := range r.Checks {
		if c.Status == Fail {
			return true
		}
	}
	return false
}

// Counts returns the number of checks per status.
func (r *Report) Counts() (ok, warn, fail int) {
	for _, c := range r.Checks {
		switch c.Status {
		case OK:
			ok++
		case Warn:
			warn++
		case Fail:
			fail++
		}
	}
	return ok, warn, fail
}

// Options configures a verification run.
type Options struct {
	Config *config.Config
	Root   string // generated project root (defaults to the soapgen.yaml dir)
	// RunTests executes "go test ./..." in the project, asserting the
	// generated marshal/behavior tests.
	RunTests bool
}

// Run verifies the generated project.
func Run(opts Options) (*Report, error) {
	cfg := opts.Config
	if cfg == nil {
		return nil, fmt.Errorf("verify: config is required")
	}
	root := opts.Root
	if root == "" {
		root = cfg.Dir
	}
	r := &Report{Service: cfg.Service, Root: root}

	methods, err := clientMethods(filepath.Join(root, cfg.ClientDir))
	if err != nil {
		r.Add("client", Fail, "cannot read the generated client: %v", err)
		return r, nil
	}
	ops, err := wsdlOperations(cfg)
	if err != nil {
		r.Add("wsdl", Fail, "%v", err)
		return r, nil
	}

	checkClient(r, cfg, methods, ops)
	checkAPI(r, cfg, root, ops)
	checkAuth(r, cfg, root, ops)
	checkState(r, cfg, root)
	if opts.RunTests {
		checkTests(r, root)
	}
	return r, nil
}

// clientMethod is a method of the generated *Client.
type clientMethod struct {
	Name string
	File string
	Line int
}

// clientMethods lists the generated client methods (all of them, so duplicates
// can be detected).
func clientMethods(dir string) ([]clientMethod, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []clientMethod
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
				continue
			}
			recv := fn.Recv.List[0].Type
			if star, ok := recv.(*ast.StarExpr); ok {
				recv = star.X
			}
			if ident, ok := recv.(*ast.Ident); !ok || ident.Name != "Client" {
				continue
			}
			out = append(out, clientMethod{Name: fn.Name.Name, File: e.Name(), Line: fset.Position(fn.Pos()).Line})
		}
	}
	return out, nil
}

// wsdlOperations returns the portType operations of the WSDL.
func wsdlOperations(cfg *config.Config) ([]string, error) {
	src, err := ingest.Load(cfg.WSDLPath())
	if err != nil {
		return nil, err
	}
	defer src.Close()
	defs, err := src.Parse()
	if err != nil {
		return nil, err
	}
	var out []string
	seen := map[string]bool{}
	for _, pt := range defs.PortType {
		for _, op := range pt.Operations {
			if !seen[op.Name] {
				seen[op.Name] = true
				out = append(out, op.Name)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no portType operations in %s", cfg.WSDL)
	}
	return out, nil
}

func checkClient(r *Report, cfg *config.Config, methods []clientMethod, ops []string) {
	counts := map[string]int{}
	for _, m := range methods {
		counts[m.Name]++
	}
	selected := 0
	for _, op := range ops {
		if !cfg.Included(op) {
			continue
		}
		selected++
		switch n := counts[op]; {
		case n == 0:
			r.Add("client/"+op, Fail, "no generated client method")
		case n > 1:
			r.Add("client/"+op, Fail, "%d generated client methods with the same name", n)
		default:
			r.Add("client/"+op, OK, "one generated client method")
		}
	}
	r.Add("client", OK, "%d methods for %d selected operations", len(methods), selected)
}

func checkAPI(r *Report, cfg *config.Config, root string, ops []string) {
	path := filepath.Join(root, cfg.APIDir, "operations_gen.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		r.Add("api", Fail, "cannot parse %s: %v", path, err)
		return
	}

	declared := map[string]bool{}
	for _, d := range file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			if ts, ok := spec.(*ast.TypeSpec); ok {
				declared[ts.Name.Name] = true
			}
		}
	}
	missing := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || !strings.HasSuffix(id.Name, "DTO") || declared[id.Name] {
			return true
		}
		missing[id.Name] = true
		return true
	})
	if len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for name := range missing {
			names = append(names, name)
		}
		sort.Strings(names)
		r.Add("api/dtos", Fail, "referenced but not declared: %s", strings.Join(names, ", "))
	} else {
		r.Add("api/dtos", OK, "every referenced DTO is declared")
	}

	// One huma.Register per selected operation, with unique operation IDs.
	registered := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Register" {
			return true
		}
		if len(call.Args) < 2 {
			return true
		}
		lit, ok := call.Args[1].(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "OperationID" {
				continue
			}
			if bl, ok := kv.Value.(*ast.BasicLit); ok {
				registered[strings.Trim(bl.Value, `"`)] = true
			}
		}
		return true
	})
	for _, op := range ops {
		if !cfg.Included(op) {
			continue
		}
		if registered[lowerFirst(op)] {
			r.Add("api/register/"+op, OK, "Huma operation registered")
		} else {
			r.Add("api/register/"+op, Fail, "no huma.Register for the operation")
		}
	}
}

// lowerFirst matches the operation ID convention of the generator.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'A' && r[0] <= 'Z' {
		r[0] += 'a' - 'A'
	}
	return string(r)
}

func checkAuth(r *Report, cfg *config.Config, root string, ops []string) {
	switch cfg.Auth.Strategy {
	case "none":
		return
	case "header":
		main := readSource(root, filepath.Join(cfg.CmdDir, "main.go"))
		if strings.Contains(main, "authHeaderInterceptor") && strings.Contains(main, cfg.Auth.Header) {
			r.Add("auth/header", OK, "interceptor sets %s from cfg.APIKey", cfg.Auth.Header)
		} else {
			r.Add("auth/header", Fail, "main.go does not install the header interceptor")
		}
		return
	case "login":
	}

	auth := readSource(root, filepath.Join(cfg.APIDir, "auth_gen.go"))
	if auth == "" {
		r.Add("auth/login", Fail, "auth_gen.go is missing")
		return
	}
	main := readSource(root, filepath.Join(cfg.CmdDir, "main.go"))
	if !strings.Contains(main, "NewSessionProvider") {
		r.Add("auth/login", Fail, "main.go does not wire the session provider")
	}

	wanted := 0
	missing := 0
	if cfg.Auth.Login.Inject != "" {
		api := readSource(root, filepath.Join(cfg.APIDir, "operations_gen.go"))
		for _, op := range ops {
			if !cfg.Included(op) || op == cfg.Auth.Login.Operation || contains(cfg.Auth.Login.SkipInject, op) {
				continue
			}
			wanted++
			if !strings.Contains(api, "func (s *Service) applySession"+op+"(") {
				r.Add("auth/inject/"+op, Fail, "no applySession%s helper", op)
				missing++
			}
		}
	}
	if missing == 0 {
		r.Add("auth/login", OK, "session provider generated, token injected into %d operations", wanted)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func checkState(r *Report, cfg *config.Config, root string) {
	if !cfg.ErrorMap.State.Enabled() {
		return
	}
	api := readSource(root, filepath.Join(cfg.APIDir, "operations_gen.go"))
	switch {
	case !strings.Contains(api, "func stateMessage("):
		r.Add("error_map/state", Fail, "stateMessage helper is missing")
	case !strings.Contains(api, "CheckState() error"):
		r.Add("error_map/state", Fail, "no CheckState method generated")
	case !strings.Contains(api, "out.Body.CheckState()"):
		r.Add("error_map/state", Fail, "handlers do not call CheckState")
	default:
		r.Add("error_map/state", OK, "CheckState generated and applied by the handlers")
	}
}

func checkTests(r *Report, root string) {
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.Add("tests", Fail, "go test ./... failed: %v\n%s", err, tail(string(out), 20))
		return
	}
	r.Add("tests", OK, "go test ./... passed")
}

func readSource(root, rel string) string {
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return ""
	}
	return string(data)
}

func tail(s string, lines int) string {
	parts := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n")
}
