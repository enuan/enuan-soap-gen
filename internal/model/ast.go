package model

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"sort"
	"strings"
)

// astPackage is a read-only view of the generated SOAP client package.
type astPackage struct {
	fset    *token.FileSet
	types   map[string]*ast.TypeSpec
	methods map[string]method // by method name
}

type method struct {
	name string
	req  string
	resp string
}

func parseClientDir(dir string) (*astPackage, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse generated client in %s: %w", dir, err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no Go package found in %s", dir)
	}
	p := &astPackage{fset: fset, types: map[string]*ast.TypeSpec{}, methods: map[string]method{}}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				switch d := decl.(type) {
				case *ast.GenDecl:
					if d.Tok != token.TYPE {
						continue
					}
					for _, spec := range d.Specs {
						ts, ok := spec.(*ast.TypeSpec)
						if ok {
							p.types[ts.Name.Name] = ts
						}
					}
				case *ast.FuncDecl:
					if m, ok := parseClientMethod(d); ok {
						p.methods[m.name] = m
					}
				}
			}
		}
	}
	if len(p.types) == 0 {
		return nil, fmt.Errorf("no type declarations found in %s", dir)
	}
	return p, nil
}

// parseClientMethod recognizes func (c *Client) X(ctx, req *Req, ...) (*Resp, error).
func parseClientMethod(fn *ast.FuncDecl) (method, bool) {
	if fn.Recv == nil || len(fn.Recv.List) != 1 || fn.Type.Params == nil || fn.Type.Results == nil {
		return method{}, false
	}
	star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return method{}, false
	}
	recv, ok := star.X.(*ast.Ident)
	if !ok || recv.Name != "Client" {
		return method{}, false
	}
	var m method
	m.name = fn.Name.Name
	for _, param := range fn.Type.Params.List {
		if t, ok := starIdent(param.Type); ok && m.req == "" {
			m.req = t
		}
	}
	for _, res := range fn.Type.Results.List {
		if t, ok := starIdent(res.Type); ok && m.resp == "" {
			m.resp = t
		}
	}
	return m, m.req != "" && m.resp != ""
}

func starIdent(expr ast.Expr) (string, bool) {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return "", false
	}
	ident, ok := star.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return ident.Name, true
}

// astField is one struct field of a generated type.
type astField struct {
	GoName  string
	GoType  string // rendered expression, e.g. "*string", "[]Result"
	XMLName string // local name from the xml tag
	Options string // xml tag options, e.g. "omitempty"
	Expr    ast.Expr
}

// structFields returns the fields of a generated struct type.
func (p *astPackage) structFields(name string) ([]astField, bool) {
	ts := p.types[name]
	if ts == nil {
		return nil, false
	}
	st, ok := ts.Type.(*ast.StructType)
	if !ok {
		return nil, false
	}
	var out []astField
	for _, f := range st.Fields.List {
		if len(f.Names) == 0 {
			continue // embedded
		}
		if f.Names[0].Name == "XMLName" {
			continue // xml.Name wrapper marker, not a payload field
		}
		xmlName, options := xmlTag(f)
		if xmlName == "XMLName" || xmlName == "-" {
			continue
		}
		expr := f.Type
		out = append(out, astField{
			GoName:  f.Names[0].Name,
			GoType:  p.expr(expr),
			XMLName: xmlName,
			Options: options,
			Expr:    expr,
		})
	}
	return out, true
}

func xmlTag(f *ast.Field) (name, options string) {
	if f.Tag == nil {
		return "", ""
	}
	tag := strings.Trim(f.Tag.Value, "`")
	idx := strings.Index(tag, `xml:"`)
	if idx < 0 {
		return "", ""
	}
	rest := tag[idx+len(`xml:"`):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return "", ""
	}
	value := rest[:end]
	parts := strings.Split(value, ",")
	name = parts[0]
	if len(parts) > 1 {
		options = strings.Join(parts[1:], ",")
	}
	return name, options
}

func (p *astPackage) expr(e ast.Expr) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, p.fset, e); err != nil {
		return "any"
	}
	return buf.String()
}

// kind classifies fields for DTO conversion.
type kind int

const (
	kindScalar kind = iota
	kindEnum
	kindStruct
)

// typeInfo describes the shape of a field type.
type typeInfo struct {
	Kind        kind
	Base        string // base named type (struct or enum), "" for primitives
	Pointer     bool
	Array       bool
	ElemPointer bool
}

func (p *astPackage) classify(expr ast.Expr) typeInfo {
	info := typeInfo{}
	if star, ok := expr.(*ast.StarExpr); ok {
		info.Pointer = true
		expr = star.X
	}
	if arr, ok := expr.(*ast.ArrayType); ok {
		info.Array = true
		expr = arr.Elt
		if star, ok := expr.(*ast.StarExpr); ok {
			info.ElemPointer = true
			expr = star.X
		}
	}
	switch t := expr.(type) {
	case *ast.Ident:
		info.Base = t.Name
		if info.Array && t.Name == "byte" {
			info.Kind = kindScalar
			return info
		}
		if ts := p.types[t.Name]; ts != nil {
			if _, isStruct := ts.Type.(*ast.StructType); isStruct {
				info.Kind = kindStruct
			} else {
				info.Kind = kindEnum
			}
		} else {
			info.Kind = kindScalar
		}
	case *ast.SelectorExpr:
		info.Kind = kindScalar // soap.XSDDateTime, time.Time, ...
	default:
		info.Kind = kindScalar
	}
	return info
}

// methodsSorted returns the client method names, sorted (for error messages).
func (p *astPackage) methodsSorted() []string {
	out := make([]string, 0, len(p.methods))
	for name := range p.methods {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
