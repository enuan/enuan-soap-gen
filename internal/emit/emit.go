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

type outFile struct {
	tmpl string
	path string
	isGo bool
	once bool
}

// Run writes every generated file. Files that must not be overwritten
// (hooks.go, Makefile, go.mod) are written only when missing.
func Run(opts Options) (*Result, error) {
	v, err := newView(opts)
	if err != nil {
		return nil, err
	}
	res := &Result{}
	files := []outFile{
		{"operations_gen.go.tmpl", filepath.Join(opts.Root, v.Config.APIDir, "operations_gen.go"), true, false},
		{"hooks.go.tmpl", filepath.Join(opts.Root, v.Config.APIDir, "hooks.go"), true, true},
		{"operations_gen_test.go.tmpl", filepath.Join(opts.Root, v.Config.APIDir, "operations_gen_test.go"), true, false},
		{"config_gen.go.tmpl", filepath.Join(opts.Root, "internal", "config", "config_gen.go"), true, false},
		{"main.go.tmpl", filepath.Join(opts.Root, v.Config.CmdDir, "main.go"), true, false},
		{"Makefile.tmpl", filepath.Join(opts.Root, "Makefile"), false, true},
		{"go.mod.tmpl", filepath.Join(opts.Root, "go.mod"), false, true},
	}
	if v.Session != nil {
		files = append(files, outFile{"auth_gen.go.tmpl", filepath.Join(opts.Root, v.Config.APIDir, "auth_gen.go"), true, false})
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
	Session      *sessionView
	State        *stateView
	HeaderAuth   *headerAuthView
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
	ResponseToSOAP    string
	RequestXMLNames   []string
	ResponseJSONName  string
	Summary           string
	Description       string
	SessionInject     *accessor  // nil when the token must not be injected
	ChecksState       bool       // response body has a generated CheckState
	StateTest         *stateTest // fake failing response for the generated test
}

// headerAuthView drives the HTTP header interceptor (auth.strategy=header).
type headerAuthView struct {
	Header string
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

	ix := buildDTOIndex(opts.Model)
	session, err := buildSession(cfg, opts.Model, ix)
	if err != nil {
		return nil, err
	}
	v.Session = session
	if cfg.Auth.Strategy == "header" {
		v.HeaderAuth = &headerAuthView{Header: cfg.Auth.Header}
	}
	state, err := buildState(cfg, opts.Model, ix)
	if err != nil {
		return nil, err
	}
	v.State = state

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
			ResponseToSOAP:    lowerCamel(op.Output.GoType) + "ToSOAP",
			Summary:           summary(op),
			Description:       opDescription(op),
		}
		if session != nil && op.Name != session.LoginName && !contains(cfg.Auth.Login.SkipInject, op.Name) && cfg.Auth.Login.Inject != "" {
			acc, err := ix.resolvePath(reqType, cfg.Auth.Login.Inject)
			if err != nil {
				return nil, fmt.Errorf("auth.login.inject for operation %s: %w (add the operation to auth.login.skip_inject if it takes no session)", op.Name, err)
			}
			ov.SessionInject = acc
		}
		if state != nil {
			ov.ChecksState = state.needsCheck(respType)
			ov.StateTest = state.Tests[respType]
		}
		for _, f := range op.Input.Fields {
			if f.XMLName != "" {
				ov.RequestXMLNames = append(ov.RequestXMLNames, f.XMLName)
			}
		}
		for _, f := range op.Output.Fields {
			if f.JSONName != "" && !f.Array {
				ov.ResponseJSONName = f.JSONName
				break
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

// RequestJSON renders a JSON request body for the generated HTTP test: every
// required field and every plain string field (nested structs/slices are
// skipped).
func (v *view) RequestJSON(fields []*model.Field) string {
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		if f.Array || f.Kind == model.FieldStruct {
			continue
		}
		if !f.Required && !isStringField(f) {
			continue
		}
		val, ok := jsonLiteral(f)
		if !ok {
			continue
		}
		parts = append(parts, fmt.Sprintf("%q: %s", f.JSONName, val))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// jsonLiteral returns a deterministic test value for a scalar DTO field.
func jsonLiteral(f *model.Field) (string, bool) {
	if f.Kind == model.FieldEnum {
		if len(f.Enum) > 0 {
			return fmt.Sprintf("%q", f.Enum[0]), true
		}
		return "", false
	}
	base := strings.TrimPrefix(f.GoType, "*")
	switch {
	case base == "string":
		return `"x"`, true
	case base == "bool":
		return "true", true
	case strings.HasPrefix(base, "int"), strings.HasPrefix(base, "uint"), strings.HasPrefix(base, "float"):
		return "1", true
	}
	return "", false
}

// opDescription documents the SOAP operation behind the REST endpoint.
func opDescription(op *model.Operation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "SOAP operation `%s` (SOAP 1.1, document/literal).\n\n", op.Name)
	if op.SOAPAction != "" {
		fmt.Fprintf(&b, "SOAP action: `%s`\n\n", op.SOAPAction)
	}
	b.WriteString("SOAP faults are mapped to `502 Bad Gateway`, timeouts to `504 Gateway Timeout`.")
	return b.String()
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
	if f.MinLength > 0 {
		tags = append(tags, fmt.Sprintf(`minLength:"%d"`, f.MinLength))
	}
	if f.Pattern != "" {
		tags = append(tags, fmt.Sprintf(`pattern:%q`, f.Pattern))
	}
	if f.Minimum != "" {
		tags = append(tags, fmt.Sprintf(`minimum:%q`, f.Minimum))
	}
	if f.Maximum != "" {
		tags = append(tags, fmt.Sprintf(`maximum:%q`, f.Maximum))
	}
	if f.Default != "" {
		tags = append(tags, fmt.Sprintf(`default:%q`, f.Default))
	}
	if len(f.Enum) > 0 && !enumHasComma(f.Enum) {
		tags = append(tags, fmt.Sprintf(`enum:"%s"`, strings.Join(f.Enum, ",")))
		if !strings.Contains(f.Enum[0], `"`) {
			tags = append(tags, fmt.Sprintf(`example:%q`, f.Enum[0]))
		}
	}
	if format := xsdFormat(f); format != "" {
		tags = append(tags, fmt.Sprintf(`format:%q`, format))
	}
	if doc := docTag(f.Doc); doc != "" {
		tags = append(tags, fmt.Sprintf(`doc:%q`, doc))
	}
	return strings.Join(tags, " ")
}

// docTag makes WSDL documentation safe for a Go struct tag: quotes and
// backticks would break the generated raw string literal, and newlines or
// runs of spaces would make the emitted tags unreadable.
func docTag(doc string) string {
	doc = strings.ReplaceAll(doc, `"`, "'")
	doc = strings.ReplaceAll(doc, "`", "'")
	return strings.Join(strings.Fields(doc), " ")
}

func enumHasComma(values []string) bool {
	for _, v := range values {
		if strings.Contains(v, ",") {
			return true
		}
	}
	return false
}

// xsdFormat maps the soap-go XSD helper types to JSON Schema formats.
func xsdFormat(f *model.Field) string {
	switch strings.TrimLeft(f.GoType, "*[]") {
	case "soap.XSDDateTime", "time.Time":
		// xs:dateTime maps to soap.XSDDateTime, xs:date/xs:time to time.Time;
		// both marshal to RFC 3339 in JSON.
		return "date-time"
	}
	return ""
}

// commentText makes WSDL documentation safe for a Go line comment.
func commentText(doc string) string {
	return strings.Join(strings.Fields(doc), " ")
}

func (v *view) render(name string) ([]byte, error) {
	tmpl, err := template.New(name).Funcs(template.FuncMap{
		"lowerCamel":  lowerCamel,
		"dtoExpr":     v.DTOExpr,
		"tags":        v.Tags,
		"quote":       func(s string) string { return fmt.Sprintf("%q", s) },
		"soapGo":      func() string { return config.SoapGoModule },
		"comment":     commentText,
		"join":        strings.Join,
		"indentLines": indentLines,
		"allocLines":  allocLines,
		"requestJSON": v.RequestJSON,
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

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

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
