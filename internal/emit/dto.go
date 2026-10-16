package emit

import (
	"fmt"
	"strings"

	"github.com/enuan/enuan-soap-gen/internal/model"
)

// dtoIndex maps DTO type names to their fields. It covers both the operation
// wrappers (Operation1Request/Operation1Response) and the nested types
// (BaseContextDTO, ...).
type dtoIndex map[string][]*model.Field

func buildDTOIndex(m *model.Model) dtoIndex {
	ix := dtoIndex{}
	for _, t := range m.Types {
		ix[t.Name+"DTO"] = t.Fields
	}
	for _, op := range m.Operations {
		ix[op.Name+"Request"] = op.Input.Fields
		ix[op.Name+"Response"] = op.Output.Fields
	}
	return ix
}

func (ix dtoIndex) field(typ, goName string) (*model.Field, bool) {
	for _, f := range ix[typ] {
		if f.GoName == goName {
			return f, true
		}
	}
	return nil, false
}

func (ix dtoIndex) names(typ string) string {
	fields := ix[typ]
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		names = append(names, f.GoName)
	}
	return strings.Join(names, ", ")
}

// accessor is a validated dotted path inside a DTO, e.g. Context.SessionId in
// Operation2Request. Intermediates are struct fields (pointer or value), the
// leaf is a scalar.
type accessor struct {
	Path    []string // Go field names
	Types   []string // DTO type name of each intermediate segment
	Pointer []bool   // whether each intermediate segment is a pointer
	Fields  []*model.Field
	Leaf    *model.Field
}

// resolvePath walks a dotted Go path starting from the DTO type root.
func (ix dtoIndex) resolvePath(root, path string) (*accessor, error) {
	if root == "" || path == "" {
		return nil, fmt.Errorf("empty accessor (root=%q path=%q)", root, path)
	}
	if _, ok := ix[root]; !ok {
		return nil, fmt.Errorf("unknown DTO type %s", root)
	}
	parts := strings.Split(path, ".")
	a := &accessor{}
	typ := root
	for i, name := range parts {
		f, ok := ix.field(typ, name)
		if !ok {
			return nil, fmt.Errorf("%s has no field %s (available: %s)", typ, name, ix.names(typ))
		}
		last := i == len(parts)-1
		if !last {
			if f.Kind != model.FieldStruct {
				return nil, fmt.Errorf("%s.%s is not a struct, cannot traverse it", typ, name)
			}
			if f.Array {
				return nil, fmt.Errorf("%s.%s is a slice, cannot traverse it", typ, name)
			}
			a.Path = append(a.Path, name)
			a.Types = append(a.Types, f.StructType+"DTO")
			a.Pointer = append(a.Pointer, f.Pointer)
			a.Fields = append(a.Fields, f)
			typ = f.StructType + "DTO"
			continue
		}
		if f.Kind == model.FieldStruct {
			return nil, fmt.Errorf("%s.%s is a struct: a scalar (string) is required", typ, name)
		}
		if f.Array {
			return nil, fmt.Errorf("%s.%s is a slice: a scalar (string) is required", typ, name)
		}
		if !isStringField(f) {
			return nil, fmt.Errorf("%s.%s is %s: a string is required", typ, name, f.GoType)
		}
		a.Path = append(a.Path, name)
		a.Fields = append(a.Fields, f)
		a.Leaf = f
	}
	return a, nil
}

// XMLSnippet renders nested XML elements ending with the literal value, e.g.
// <LoginResult><SessionId>sess-1</SessionId></LoginResult>.
func (a *accessor) XMLSnippet(value string) string {
	var b strings.Builder
	for _, f := range a.Fields {
		b.WriteString("<" + xmlName(f) + ">")
	}
	b.WriteString(value)
	for i := len(a.Fields) - 1; i >= 0; i-- {
		b.WriteString("</" + xmlName(a.Fields[i]) + ">")
	}
	return b.String()
}

func xmlName(f *model.Field) string {
	if f.XMLName != "" {
		return f.XMLName
	}
	return f.GoName
}

func isStringField(f *model.Field) bool {
	return f.Kind == model.FieldScalar && strings.TrimPrefix(f.GoType, "*") == "string"
}

// Expr renders the full access expression, e.g. req.Context.SessionId.
func (a *accessor) Expr(root string) string {
	return root + "." + strings.Join(a.Path, ".")
}

// AllocLines returns the statements allocating the nil pointer intermediates,
// e.g. "if req.Context == nil { req.Context = &BaseContextDTO{} }".
func (a *accessor) AllocLines(root string) []string {
	var lines []string
	expr := root
	for i := range a.Path[:len(a.Path)-1] {
		expr += "." + a.Path[i]
		if !a.Pointer[i] {
			continue
		}
		lines = append(lines,
			fmt.Sprintf("if %s == nil {", expr),
			fmt.Sprintf("\t%s = &%s{}", expr, a.Types[i]),
			"}",
		)
	}
	return lines
}

// WriteString returns the statements assigning value to the leaf.
// The caller must have emitted AllocLines first.
func (a *accessor) WriteString(root, value string) []string {
	expr := a.Expr(root)
	if a.Leaf.Pointer {
		return []string{fmt.Sprintf("%s = &%s", expr, value)}
	}
	return []string{fmt.Sprintf("%s = %s", expr, value)}
}

// allocLines returns the deduplicated allocation statements of several accessors.
func allocLines(root string, accs ...*accessor) []string {
	seen := map[string]bool{}
	var lines []string
	for _, a := range accs {
		if a == nil {
			continue
		}
		for _, l := range a.AllocLines(root) {
			if seen[l] {
				continue
			}
			seen[l] = true
			lines = append(lines, l)
		}
	}
	return lines
}

// ReadString returns the statements reading the leaf into dst.
func (a *accessor) ReadString(dst, root string) []string {
	expr := a.Expr(root)
	lines := []string{fmt.Sprintf("%s := \"\"", dst)}
	var conds []string
	for i := range a.Path[:len(a.Path)-1] {
		if a.Pointer[i] {
			conds = append(conds, fmt.Sprintf("%s != nil", root+"."+strings.Join(a.Path[:i+1], ".")))
		}
	}
	if a.Leaf.Pointer {
		conds = append(conds, expr+" != nil")
	}
	if len(conds) == 0 {
		return append(lines, fmt.Sprintf("%s = %s", dst, expr))
	}
	lines = append(lines, "if "+strings.Join(conds, " && ")+" {")
	if a.Leaf.Pointer {
		lines = append(lines, fmt.Sprintf("\t%s = *%s", dst, expr))
	} else {
		lines = append(lines, fmt.Sprintf("\t%s = %s", dst, expr))
	}
	return append(lines, "}")
}
