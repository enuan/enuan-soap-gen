package emit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/enuan/enuan-soap-gen/internal/config"
	"github.com/enuan/enuan-soap-gen/internal/model"
)

// stateView drives the generation of CheckState methods (error_map.state).
type stateView struct {
	SuccessField string
	CodeField    string
	MessageField string
	Checks       []*stateCheck
	Tests        map[string]*stateTest // response DTO type -> fake failure builder
	checked      map[string]bool
	direct       map[string]*stateCheck // type -> state fields it carries directly
}

// stateCheck is one DTO type that maps the embedded application state.
type stateCheck struct {
	Type    string // DTO type name
	Success *model.Field
	Code    *model.Field
	Message *model.Field
	Nested  []*model.Field // struct fields whose DTO also has a CheckState
}

// stateTest locates the state fields inside a response wrapper so that the
// generated test can build a failing response.
type stateTest struct {
	Intermediates []string // Go field names from the wrapper to the state type
	Types         []string // DTO type of each intermediate
	Pointer       []bool
	Success       *model.Field
	Code          *model.Field
	Message       *model.Field
}

// Expr renders the path expression from root to the state struct.
func (t *stateTest) Expr(root string) string {
	if len(t.Intermediates) == 0 {
		return root
	}
	return root + "." + strings.Join(t.Intermediates, ".")
}

// AllocLines returns the statements allocating the nil pointer intermediates.
func (t *stateTest) AllocLines(root string) []string {
	var lines []string
	expr := root
	for i, name := range t.Intermediates {
		expr += "." + name
		if !t.Pointer[i] {
			continue
		}
		lines = append(lines,
			fmt.Sprintf("if %s == nil {", expr),
			fmt.Sprintf("\t%s = &%s{}", expr, t.Types[i]),
			"}",
		)
	}
	return lines
}

// Assignments returns the statements setting a failing state.
func (t *stateTest) Assignments(root string) []string {
	expr := t.Expr(root)
	var lines []string
	if t.Success.Pointer {
		lines = append(lines, fmt.Sprintf("stateSuccess := false; %s = &stateSuccess", expr+"."+t.Success.GoName))
	} else {
		lines = append(lines, fmt.Sprintf("%s = false", expr+"."+t.Success.GoName))
	}
	if t.Code != nil {
		lines = append(lines, assignLiteral(expr+"."+t.Code.GoName, t.Code, "stateCode"))
	}
	if t.Message != nil {
		lines = append(lines, assignLiteral(expr+"."+t.Message.GoName, t.Message, "stateMessage"))
	}
	return lines
}

// assignLiteral renders an assignment with a deterministic test value.
func assignLiteral(expr string, f *model.Field, varName string) string {
	deref := strings.TrimPrefix(f.GoType, "*")
	literal := `"state error"`
	switch deref {
	case "string":
	case "bool":
		literal = "true"
	default:
		switch {
		case strings.HasPrefix(deref, "int"), strings.HasPrefix(deref, "uint"), strings.HasPrefix(deref, "float"):
			literal = "1"
		default:
			literal = `"state error"`
		}
	}
	if f.Pointer {
		return fmt.Sprintf("%s := %s; %s = &%s", varName, literal, expr, varName)
	}
	return fmt.Sprintf("%s = %s", expr, literal)
}

func (s *stateView) needsCheck(dtoType string) bool { return s.checked[dtoType] }

// buildState resolves error_map.state against the DTO graph. For every type
// that carries the success field it generates a CheckState method; wrapper
// types delegate to the nested DTOs, so a single configuration covers
// differently nested result types.
func buildState(cfg *config.Config, m *model.Model, ix dtoIndex) (*stateView, error) {
	st := cfg.ErrorMap.State
	if !st.Enabled() {
		return nil, nil
	}
	if st.SuccessField == "" {
		return nil, fmt.Errorf("error_map.state.success_field is required")
	}
	s := &stateView{
		SuccessField: st.SuccessField,
		CodeField:    st.CodeField,
		MessageField: st.MessageField,
		Tests:        map[string]*stateTest{},
		checked:      map[string]bool{},
		direct:       map[string]*stateCheck{},
	}

	types := make([]string, 0, len(ix))
	for typ := range ix {
		types = append(types, typ)
	}
	sort.Strings(types)

	byType := map[string]*stateCheck{}
	for _, typ := range types {
		success, ok := ix.field(typ, st.SuccessField)
		if !ok {
			continue
		}
		if success.Array || success.Kind != model.FieldScalar || strings.TrimPrefix(success.GoType, "*") != "bool" {
			return nil, fmt.Errorf("error_map.state.success_field: %s.%s is %s, a bool (or *bool) is required", typ, success.GoName, success.GoType)
		}
		check := &stateCheck{Type: typ, Success: success}
		if st.CodeField != "" {
			f, ok := ix.field(typ, st.CodeField)
			if !ok {
				return nil, fmt.Errorf("error_map.state.code_field: %s has no field %s (available: %s)", typ, st.CodeField, ix.names(typ))
			}
			if f.Array || f.Kind == model.FieldStruct {
				return nil, fmt.Errorf("error_map.state.code_field: %s.%s must be a scalar", typ, f.GoName)
			}
			check.Code = f
		}
		if st.MessageField != "" {
			f, ok := ix.field(typ, st.MessageField)
			if !ok {
				return nil, fmt.Errorf("error_map.state.message_field: %s has no field %s (available: %s)", typ, st.MessageField, ix.names(typ))
			}
			if f.Array || f.Kind == model.FieldStruct {
				return nil, fmt.Errorf("error_map.state.message_field: %s.%s must be a scalar", typ, f.GoName)
			}
			check.Message = f
		}
		byType[typ] = check
		s.direct[typ] = check
		s.checked[typ] = true
	}
	if len(byType) == 0 {
		return nil, fmt.Errorf("error_map.state.success_field: no generated type has a %s field", st.SuccessField)
	}

	// Propagate: a type needs a check when a nested non-array DTO needs one.
	for changed := true; changed; {
		changed = false
		for _, typ := range types {
			if s.checked[typ] {
				continue
			}
			for _, f := range ix[typ] {
				if f.Kind == model.FieldStruct && !f.Array && s.checked[f.StructType+"DTO"] {
					s.checked[typ] = true
					changed = true
					break
				}
			}
		}
	}

	for _, typ := range types {
		if !s.checked[typ] {
			continue
		}
		check := byType[typ]
		if check == nil {
			check = &stateCheck{Type: typ}
		}
		for _, f := range ix[typ] {
			if f.Kind == model.FieldStruct && !f.Array && s.checked[f.StructType+"DTO"] {
				check.Nested = append(check.Nested, f)
			}
		}
		s.Checks = append(s.Checks, check)
	}

	for _, op := range m.Operations {
		root := op.Name + "Response"
		if !s.checked[root] {
			continue
		}
		if t := s.findStateTest(root, ix, map[string]bool{}); t != nil {
			s.Tests[root] = t
		}
	}
	return s, nil
}

// findStateTest walks from a response wrapper to the first checked type.
func (s *stateView) findStateTest(typ string, ix dtoIndex, visited map[string]bool) *stateTest {
	if visited[typ] {
		return nil
	}
	visited[typ] = true
	for _, f := range ix[typ] {
		if f.Kind != model.FieldStruct || f.Array {
			continue
		}
		dto := f.StructType + "DTO"
		if !s.checked[dto] {
			continue
		}
		if c, ok := s.direct[dto]; ok {
			return &stateTest{
				Intermediates: []string{f.GoName},
				Types:         []string{dto},
				Pointer:       []bool{f.Pointer},
				Success:       c.Success,
				Code:          c.Code,
				Message:       c.Message,
			}
		}
		if nested := s.findStateTest(dto, ix, visited); nested != nil {
			nested.Intermediates = append([]string{f.GoName}, nested.Intermediates...)
			nested.Types = append([]string{dto}, nested.Types...)
			nested.Pointer = append([]bool{f.Pointer}, nested.Pointer...)
			return nested
		}
	}
	return nil
}
