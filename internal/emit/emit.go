// Package emit generates the REST/JSON gateway sources: Huma DTOs and
// handlers, hooks, configuration, main and test scaffolding.
package emit

import (
	"bytes"
	"embed"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/enuan/enuan-soap-gen/internal/config"
	"github.com/enuan/enuan-soap-gen/internal/model"
)

//go:embed templates/*.tmpl
var templatesFS embed.FS

// dependencyVersions pins the runtime stack of the generated gateway.
var dependencyVersions = []dependency{
	{"github.com/danielgtaylor/huma/v2", "v2.39.1"},
	{"github.com/enuan/gonfoo", "v0.0.0-20260910151220-6fe501af83c6"},
	{"github.com/enuan/m3-go", "v0.0.0-20261014092316-ee743e500f83"},
	{"github.com/enuan/soap-go", "v0.29.2-enuan.1"},
	{"github.com/enuan/web-go", "v0.0.0-20260925140115-58322663abb8"},
	{"github.com/gin-gonic/gin", "v1.12.0"},
}

type dependency struct {
	Path    string
	Version string
}

// Options configures one emission run.
type Options struct {
	Root   string // generated project root
	Config *config.Config
	Model  *model.Model
}

// Result lists the files written or left untouched.
type Result struct {
	Written []string
	Skipped []string
}

// Run writes every generated file. Files that must not be overwritten
// (hooks.go) are written only when missing.
func Run(opts Options) (*Result, error) {
	v, err := newView(opts)
	if err != nil {
		return nil, err
	}
	res := &Result{}
	files := []struct {
		tmpl string
		path string
		isGo bool
		once bool
	}{
		{"operations_gen.go.tmpl", filepath.Join(opts.Root, v.Config.APIDir, "operations_gen.go"), true, false},
		{"hooks.go.tmpl", filepath.Join(opts.Root, v.Config.APIDir, "hooks.go"), true, true},
		{"operations_gen_test.go.tmpl", filepath.Join(opts.Root, v.Config.APIDir, "operations_gen_test.go"), true, false},
		{"config_gen.go.tmpl", filepath.Join(opts.Root, "internal", "config", "config_gen.go"), true, false},
		{"main.go.tmpl", filepath.Join(opts.Root, v.Config.CmdDir, "main.go"), true, false},
		{"Makefile.tmpl", filepath.Join(opts.Root, "Makefile"), false, true},
		{"go.mod.tmpl", filepath.Join(opts.Root, "go.mod"), false, true},
	}
	for _, f := range files {
		out, err := v.render(f.tmpl)
		if err != nil {
			return nil, err
		}
		if f.isGo {
			formatted, err := format.Source(out)
			if err != nil {
				return nil, fmt.Errorf("generated %s does not compile: %w\n%s", f.tmpl, err, out)
			}
			out = formatted
		}
		if f.once {
			if _, err := os.Stat(f.path); err == nil {
				res.Skipped = append(res.Skipped, f.path)
				continue
			}
		}
		if err := writeFile(f.path, out); err != nil {
			return nil, err
		}
		res.Written = append(res.Written, f.path)
	}
	return res, nil
}

func writeFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

// ----- view model -----

type view struct {
	Config       *config.Config
	Model        *model.Model
	Module       string
	ClientImport string
	APIImport    string
	ConfigImport string
	Operations   []*opView
	Converters   []*converter
	Dependencies []dependency
}

type opView struct {
	Operation         *model.Operation
	ID                string
	HTTPMethod        string
	Path              string
	InputType         string
	OutputType        string
	RequestType       string
	ResponseType      string
	RequestConverter  string
	ResponseConverter string
	RequestXMLNames   []string
	Summary           string
}

type converter struct {
	Name     string // echoWrapperFromSOAP
	SOAPType string
	DTOType  string
	Doc      string
	Fields   []convField
}

type convField struct {
	GoName   string
	Doc      string
	FromSOAP []string // lines converting SOAP -> DTO (in: SOAP, out: DTO)
	ToSOAP   []string // lines converting DTO -> SOAP (in: DTO, out: SOAP)
}

func newView(opts Options) (*view, error) {
	cfg := opts.Config
	if cfg == nil || opts.Model == nil {
		return nil, fmt.Errorf("emit: config and model are required")
	}
	v := &view{
		Config:       cfg,
		Model:        opts.Model,
		Module:       cfg.Module,
		ClientImport: importPath(cfg.Module, cfg.ClientDir),
		APIImport:    importPath(cfg.Module, cfg.APIDir),
		ConfigImport: importPath(cfg.Module, filepath.Join("internal", "config")),
		Dependencies: dependencyVersions,
	}

	structNames := map[string]bool{}
	for _, t := range opts.Model.Types {
		structNames[t.Name] = true
	}

	// Converters for operation wrappers.
	seenConv := map[string]bool{}
	addConverter := func(soapType, dtoType, doc string, fields []*model.Field) {
		if soapType == "" || seenConv[soapType] {
			return
		}
		seenConv[soapType] = true
		v.Converters = append(v.Converters, buildConverter(soapType, dtoType, doc, fields))
	}

	for _, op := range opts.Model.Operations {
		reqType := op.Name + "Request"
		respType := op.Name + "Response"
		addConverter(op.Input.GoType, reqType, "converts the REST request into its SOAP wrapper", op.Input.Fields)
		addConverter(op.Output.GoType, respType, "converts the SOAP response into the REST response", op.Output.Fields)
		ov := &opView{
			Operation:         op,
			ID:                lowerCamel(op.Name),
			HTTPMethod:        cfg.RuleFor(op.Name).Method,
			Path:              cfg.PathFor(op.Name),
			InputType:         op.Name + "Input",
			OutputType:        op.Name + "Output",
			RequestType:       reqType,
			ResponseType:      respType,
			RequestConverter:  lowerCamel(op.Input.GoType) + "ToSOAP",
			ResponseConverter: lowerCamel(op.Output.GoType) + "FromSOAP",
			Summary:           summary(op),
		}
		for _, f := range op.Input.Fields {
			if f.XMLName != "" {
				ov.RequestXMLNames = append(ov.RequestXMLNames, f.XMLName)
			}
		}
		v.Operations = append(v.Operations, ov)
	}

	// Converters for nested types.
	for _, t := range opts.Model.Types {
		addConverter(t.Name, t.Name+"DTO", "converts "+t.Name, t.Fields)
	}
	return v, nil
}

func summary(op *model.Operation) string {
	if op.Docs != "" {
		return op.Docs
	}
	if len(op.Input.Fields) == 0 {
		return "Calls the SOAP operation " + op.Name
	}
	return "Calls the SOAP operation " + op.Name
}

func importPath(module, dir string) string {
	return strings.TrimSuffix(module, "/") + "/" + filepath.ToSlash(strings.TrimPrefix(dir, "./"))
}

func buildConverter(soapType, dtoType, doc string, fields []*model.Field) *converter {
	c := &converter{Name: lowerCamel(soapType) + "FromSOAP", SOAPType: soapType, DTOType: dtoType, Doc: doc}
	for _, f := range fields {
		cf := convField{GoName: f.GoName, Doc: f.Doc}
		cf.FromSOAP = conversionLines(f, "FromSOAP")
		cf.ToSOAP = conversionLines(f, "ToSOAP")
		c.Fields = append(c.Fields, cf)
	}
	return c
}

// conversionLines returns the statements converting one field in the given
// direction. `in` and `out` are the function parameters.
func conversionLines(f *model.Field, direction string) []string {
	in := "in." + f.GoName
	out := "out." + f.GoName
	suffix := direction
	conv := lowerCamel(f.StructType) + suffix
	if f.Kind != model.FieldStruct {
		return []string{fmt.Sprintf("%s = %s", out, in)}
	}
	if !f.Array {
		if f.Pointer {
			return []string{fmt.Sprintf("%s = %s(%s)", out, conv, in)}
		}
		return []string{fmt.Sprintf("%s = *%s(&%s)", out, conv, in)}
	}
	elemType := f.StructType + "DTO"
	if direction == "ToSOAP" {
		elemType = "soapclient." + f.StructType
	}
	if f.ElemPointer {
		elemType = "*" + elemType
	}
	assign := fmt.Sprintf("%s[i] = %s(%s[i])", out, conv, in)
	if !f.ElemPointer {
		assign = fmt.Sprintf("%s[i] = *%s(&%s[i])", out, conv, in)
	}
	return []string{
		fmt.Sprintf("if %s != nil {", in),
		fmt.Sprintf("\t%s = make([]%s, len(%s))", out, elemType, in),
		fmt.Sprintf("\tfor i := range %s {", in),
		"\t\t" + assign,
		"\t}",
		"}",
	}
}

// DTOExpr renders the DTO field type for a model field.
func (v *view) DTOExpr(f *model.Field) string {
	var base string
	switch f.Kind {
	case model.FieldStruct:
		base = f.StructType + "DTO"
	case model.FieldEnum:
		base = "soapclient." + f.StructType
	default:
		return f.GoType
	}
	s := base
	if f.ElemPointer {
		s = "*" + s
	}
	if f.Array {
		s = "[]" + s
	}
	if f.Pointer {
		s = "*" + s
	}
	return s
}

// Tags renders the struct tags of a DTO field.
func (v *view) Tags(f *model.Field) string {
	var tags []string
	name := f.JSONName
	if !f.Required {
		name += ",omitempty"
	}
	tags = append(tags, fmt.Sprintf(`json:%q`, name))
	if f.Required {
		tags = append(tags, `required:"true"`)
	}
	if f.MaxLength > 0 {
		tags = append(tags, fmt.Sprintf(`maxLength:"%d"`, f.MaxLength))
	}
	if len(f.Enum) > 0 && !strings.Contains(strings.Join(f.Enum, ","), ",") {
		tags = append(tags, fmt.Sprintf(`enum:"%s"`, strings.Join(f.Enum, ",")))
	}
	return strings.Join(tags, " ")
}

func (v *view) render(name string) ([]byte, error) {
	tmpl, err := template.New(name).Funcs(template.FuncMap{
		"lowerCamel":  lowerCamel,
		"dtoExpr":     v.DTOExpr,
		"tags":        v.Tags,
		"quote":       func(s string) string { return fmt.Sprintf("%q", s) },
		"join":        strings.Join,
		"indentLines": indentLines,
	}).ParseFS(templatesFS, "templates/"+name)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, v); err != nil {
		return nil, fmt.Errorf("render %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

func indentLines(lines []string) string { return strings.Join(lines, "\n") }

func lowerCamel(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'A' && r[0] <= 'Z' {
		r[0] = r[0] + ('a' - 'A')
	}
	return string(r)
}

// SortedOps returns the operation names (for docs/tests).
func SortedOps(m *model.Model) []string {
	out := make([]string, 0, len(m.Operations))
	for _, op := range m.Operations {
		out = append(out, op.Name)
	}
	sort.Strings(out)
	return out
}
