// Package model builds the single internal model of a SOAP service from two
// sources of truth: the WSDL (actions, wrapper elements, optionality, enums,
// docs, endpoint) and the generated Go client read via go/ast (real type and
// field names). A mismatch between the two is a fatal error: it signals codegen
// drift.
package model

import (
	"fmt"
	"sort"
	"strings"

	"github.com/enuan/enuan-soap-gen/internal/config"
	"github.com/enuan/enuan-soap-gen/internal/ingest"
)

// Model is the complete service model for the selected operations.
type Model struct {
	Service    string
	Namespace  string
	Endpoint   string
	Operations []*Operation
	Types      []*Type // referenced struct types, sorted by name
}

// Operation is one SOAP operation exposed over REST.
type Operation struct {
	Name       string
	Method     string // generated client method name
	SOAPAction string
	Docs       string
	Input      *Message
	Output     *Message
}

// Message is the input or output wrapper of an operation.
type Message struct {
	Element string // wrapper element local name
	GoType  string // generated Go type name
	Fields  []*Field
}

// FieldKind classifies a field for DTO conversion.
type FieldKind int

const (
	FieldScalar FieldKind = iota
	FieldEnum
	FieldStruct
)

// Field is one payload field.
type Field struct {
	XMLName     string
	JSONName    string
	GoName      string
	GoType      string // type expression in the generated client package
	Kind        FieldKind
	StructType  string // for FieldStruct: Go type name without pointer/slice
	Pointer     bool
	Array       bool
	ElemPointer bool
	Required    bool
	MaxLength   int
	Enum        []string
	Doc         string
}

// Type is a referenced struct type that needs a REST DTO.
type Type struct {
	Name   string
	Fields []*Field
}

// Build reads the WSDL and the generated client and produces the model for the
// operations selected in cfg.
func Build(cfg *config.Config, clientDir string) (*Model, error) {
	src, err := ingest.Load(cfg.WSDLPath())
	if err != nil {
		return nil, err
	}
	defer src.Close()
	defs, err := src.Parse()
	if err != nil {
		return nil, err
	}

	wsdlOps, endpoint := extractOperations(defs)
	if len(wsdlOps) == 0 {
		return nil, fmt.Errorf("%s: no portType operations found", cfg.WSDLPath())
	}
	ix := newWSDLIndex(defs)
	pkg, err := parseClientDir(clientDir)
	if err != nil {
		return nil, err
	}

	m := &Model{Service: defs.Name, Namespace: targetNamespace(defs), Endpoint: endpoint}
	if m.Service == "" {
		m.Service = cfg.Service
	}

	seenTypes := map[string]bool{}
	var queue []string
	selected := 0
	for _, wop := range wsdlOps {
		if !cfg.Included(wop.Name) {
			continue
		}
		method, ok := findMethod(pkg, wop.Name)
		if !ok {
			return nil, fmt.Errorf("operation %q: no generated client method (available: %s)",
				wop.Name, strings.Join(pkg.methodsSorted(), ", "))
		}
		input, err := buildMessage(ix, pkg, wop.InputElem, method.req, cfg, seenTypes, &queue)
		if err != nil {
			return nil, fmt.Errorf("operation %q input: %w", wop.Name, err)
		}
		output, err := buildMessage(ix, pkg, wop.OutputElem, method.resp, cfg, seenTypes, &queue)
		if err != nil {
			return nil, fmt.Errorf("operation %q output: %w", wop.Name, err)
		}
		m.Operations = append(m.Operations, &Operation{
			Name:       wop.Name,
			Method:     method.name,
			SOAPAction: wop.Action,
			Docs:       wop.Doc,
			Input:      input,
			Output:     output,
		})
		selected++
	}
	if selected == 0 {
		return nil, fmt.Errorf("no operations selected by soapgen.yaml (include=%v exclude=%v)",
			cfg.Operations.Include, cfg.Operations.Exclude)
	}

	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		fields, err := buildFields(ix, pkg, name, "", cfg, seenTypes, &queue)
		if err != nil {
			return nil, fmt.Errorf("type %s: %w", name, err)
		}
		m.Types = append(m.Types, &Type{Name: name, Fields: fields})
	}
	sort.Slice(m.Types, func(i, j int) bool { return m.Types[i].Name < m.Types[j].Name })

	return m, nil
}

func buildMessage(ix *wsdlIndex, pkg *astPackage, element, goType string, cfg *config.Config, seen map[string]bool, queue *[]string) (*Message, error) {
	fields, err := buildFields(ix, pkg, goType, element, cfg, seen, queue)
	if err != nil {
		return nil, err
	}
	return &Message{Element: element, GoType: goType, Fields: fields}, nil
}

// buildFields merges the AST struct fields with the WSDL field specs.
// element is the wrapper element name ("" for nested named types).
func buildFields(ix *wsdlIndex, pkg *astPackage, goType, element string, cfg *config.Config, seen map[string]bool, queue *[]string) ([]*Field, error) {
	astFields, ok := pkg.structFields(goType)
	if !ok {
		return nil, fmt.Errorf("generated type %s is not a struct", goType)
	}
	var specs []*fieldSpec
	if element != "" {
		if s, found := ix.elementFields(element); found {
			specs = s
		}
	} else {
		if s, found := ix.goTypeFields(goType); found {
			specs = s
		}
	}
	specByName := map[string]*fieldSpec{}
	for _, s := range specs {
		specByName[s.XMLName] = s
	}

	var fields []*Field
	astByName := map[string]bool{}
	for _, af := range astFields {
		info := pkg.classify(af.Expr)
		field := &Field{
			XMLName:     af.XMLName,
			GoName:      af.GoName,
			GoType:      af.GoType,
			Kind:        FieldKind(info.Kind),
			StructType:  info.Base,
			Pointer:     info.Pointer,
			Array:       info.Array,
			ElemPointer: info.ElemPointer,
		}
		name := af.XMLName
		if name == "" {
			name = af.GoName
		}
		field.JSONName = jsonName(name, cfg.Naming.JSON)
		if spec := specByName[af.XMLName]; spec != nil {
			astByName[spec.XMLName] = true
			field.Required = spec.MinOccurs != "0" && !spec.Nillable
			field.MaxLength = spec.MaxLength
			field.Enum = spec.Enum
			field.Doc = spec.Doc
		} else {
			field.Required = !info.Pointer && !strings.Contains(af.Options, "omitempty")
		}
		if info.Kind == kindStruct && info.Base != "" && !seen[info.Base] {
			seen[info.Base] = true
			*queue = append(*queue, info.Base)
		}
		fields = append(fields, field)
	}

	// Every WSDL field must exist in the generated struct (drift detection).
	for _, spec := range specs {
		if spec.XMLName != "" && !astByName[spec.XMLName] && !hasASTField(astFields, spec.XMLName) {
			return nil, fmt.Errorf("field %q declared in the WSDL is missing from generated type %s (codegen drift)", spec.XMLName, goType)
		}
	}
	return fields, nil
}

func hasASTField(fields []astField, xmlName string) bool {
	for _, f := range fields {
		if f.XMLName == xmlName {
			return true
		}
	}
	return false
}

// findMethod matches an operation name to a client method (exact, then
// case/identifier-insensitive).
func findMethod(pkg *astPackage, operation string) (method, bool) {
	if m, ok := pkg.methods[operation]; ok {
		return m, true
	}
	want := normalizeIdent(operation)
	for _, m := range pkg.methods {
		if normalizeIdent(m.name) == want {
			return m, true
		}
	}
	return method{}, false
}

func normalizeIdent(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		}
	}
	return b.String()
}

func jsonName(name, mode string) string {
	if mode == "preserve" || name == "" {
		return name
	}
	r := []rune(name)
	if r[0] >= 'A' && r[0] <= 'Z' {
		r[0] = r[0] + ('a' - 'A')
	}
	return string(r)
}
