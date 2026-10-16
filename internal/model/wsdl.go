package model

import (
	"strconv"
	"strings"

	"github.com/enuan/soap-go/wsdl"
	"github.com/enuan/soap-go/xsd"
)

// wsdlOperation is one portType operation enriched with its binding action.
type wsdlOperation struct {
	Name       string
	Action     string
	InputElem  string
	OutputElem string
	Doc        string
}

// wsdlIndex indexes the inline schemas for name resolution.
type wsdlIndex struct {
	elements     map[string]*xsd.Element
	complexTypes map[string]*xsd.ComplexType
	simpleTypes  map[string]*xsd.SimpleType
}

func newWSDLIndex(defs *wsdl.Definitions) *wsdlIndex {
	ix := &wsdlIndex{
		elements:     map[string]*xsd.Element{},
		complexTypes: map[string]*xsd.ComplexType{},
		simpleTypes:  map[string]*xsd.SimpleType{},
	}
	if defs.Types == nil {
		return ix
	}
	for i := range defs.Types.Schemas {
		s := &defs.Types.Schemas[i]
		for j := range s.Elements {
			if name := s.Elements[j].Name; name != "" {
				if _, exists := ix.elements[name]; !exists {
					ix.elements[name] = &s.Elements[j]
				}
			}
		}
		for j := range s.ComplexTypes {
			if name := s.ComplexTypes[j].Name; name != "" {
				if _, exists := ix.complexTypes[name]; !exists {
					ix.complexTypes[name] = &s.ComplexTypes[j]
				}
			}
		}
		for j := range s.SimpleTypes {
			if name := s.SimpleTypes[j].Name; name != "" {
				if _, exists := ix.simpleTypes[name]; !exists {
					ix.simpleTypes[name] = &s.SimpleTypes[j]
				}
			}
		}
	}
	return ix
}

// extractOperations returns the portType operations (SOAP 1.1 actions) and the
// preferred service endpoint (HTTPS when available).
func extractOperations(defs *wsdl.Definitions) ([]wsdlOperation, string) {
	actions := map[string]string{}
	for i := range defs.Binding {
		b := &defs.Binding[i]
		if b.SOAP11Binding == nil {
			continue
		}
		for j := range b.BindingOperations {
			op := &b.BindingOperations[j]
			if op.SOAP11Operation != nil && op.SOAP11Operation.SOAPAction != "" {
				if _, exists := actions[op.Name]; !exists {
					actions[op.Name] = op.SOAP11Operation.SOAPAction
				}
			}
		}
	}
	messages := map[string]*wsdl.Message{}
	for i := range defs.Messages {
		messages[defs.Messages[i].Name] = &defs.Messages[i]
	}

	var ops []wsdlOperation
	for i := range defs.PortType {
		for j := range defs.PortType[i].Operations {
			op := &defs.PortType[i].Operations[j]
			entry := wsdlOperation{Name: op.Name, Action: actions[op.Name], Doc: strings.TrimSpace(op.Documentation)}
			if op.Input != nil {
				entry.InputElem = messageElement(messages, op.Input.Message)
			}
			if op.Output != nil {
				entry.OutputElem = messageElement(messages, op.Output.Message)
			}
			ops = append(ops, entry)
		}
	}

	endpoint := ""
	for i := range defs.Service {
		for j := range defs.Service[i].Ports {
			port := &defs.Service[i].Ports[j]
			for _, addr := range []string{addressOf(port, false), addressOf(port, true)} {
				if addr == "" {
					continue
				}
				if endpoint == "" {
					endpoint = addr
				}
				if strings.HasPrefix(addr, "https://") {
					return ops, addr
				}
			}
		}
	}
	return ops, endpoint
}

func addressOf(port *wsdl.Port, prefer12 bool) string {
	if prefer12 && port.SOAP12Address != nil {
		return port.SOAP12Address.Location
	}
	if !prefer12 && port.SOAP11Address != nil {
		return port.SOAP11Address.Location
	}
	return ""
}

func messageElement(messages map[string]*wsdl.Message, message string) string {
	m := messages[localName(message)]
	if m == nil || len(m.Parts) == 0 {
		return ""
	}
	return localName(m.Parts[0].Element)
}

// fieldSpec is the WSDL-side description of one field.
type fieldSpec struct {
	XMLName   string
	MinOccurs string
	MaxOccurs string
	Nillable  bool
	MinLength int
	MaxLength int
	Pattern   string
	Minimum   string
	Maximum   string
	Default   string
	Enum      []string
	Doc       string
}

// elementFields returns the top-level fields of an element wrapper.
func (ix *wsdlIndex) elementFields(name string) ([]*fieldSpec, bool) {
	e := ix.elements[name]
	if e == nil {
		return nil, false
	}
	if e.Type != "" {
		if ct := ix.complexTypes[localName(e.Type)]; ct != nil {
			return ix.complexFields(ct, map[string]bool{}), true
		}
	}
	if e.ComplexType != nil {
		return ix.complexFields(e.ComplexType, map[string]bool{}), true
	}
	return nil, true
}

// goTypeFields returns the fields of a named complex type identified by the
// Go type name generated for it (accepts the "Wrapper" suffix used for
// element wrappers).
func (ix *wsdlIndex) goTypeFields(goName string) ([]*fieldSpec, bool) {
	if ct := ix.complexTypes[goName]; ct != nil {
		return ix.complexFields(ct, map[string]bool{}), true
	}
	if base, ok := strings.CutSuffix(goName, "Wrapper"); ok {
		return ix.elementFields(base)
	}
	return nil, false
}

func (ix *wsdlIndex) complexFields(ct *xsd.ComplexType, seen map[string]bool) []*fieldSpec {
	if ct == nil {
		return nil
	}
	var out []*fieldSpec
	if cc := ct.ComplexContent; cc != nil && cc.Extension != nil {
		base := localName(cc.Extension.Base)
		if base != "" && !seen[base] {
			seen[base] = true
			out = append(out, ix.complexFields(ix.complexTypes[base], seen)...)
		}
		out = append(out, ix.modelFields(cc.Extension.Sequence, cc.Extension.Choice, cc.Extension.All)...)
	} else if sc := ct.SimpleContent; sc != nil && sc.Extension != nil {
		for i := range sc.Extension.Attributes {
			out = append(out, ix.specForAttribute(&sc.Extension.Attributes[i]))
		}
	} else {
		out = append(out, ix.modelFields(ct.Sequence, ct.Choice, ct.All)...)
	}
	for i := range ct.Attributes {
		out = append(out, ix.specForAttribute(&ct.Attributes[i]))
	}
	return out
}

func (ix *wsdlIndex) modelFields(seq *xsd.Sequence, ch *xsd.Choice, all *xsd.All) []*fieldSpec {
	var out []*fieldSpec
	add := func(elements []xsd.Element) {
		for i := range elements {
			out = append(out, ix.specForElement(&elements[i]))
		}
	}
	switch {
	case seq != nil:
		add(seq.Elements)
		for i := range seq.Choices {
			add(seq.Choices[i].Elements)
		}
	case ch != nil:
		add(ch.Elements)
	case all != nil:
		add(all.Elements)
	}
	return out
}

func (ix *wsdlIndex) specForElement(e *xsd.Element) *fieldSpec {
	spec := &fieldSpec{
		XMLName:   localName(e.Name),
		MinOccurs: e.MinOccurs,
		MaxOccurs: e.MaxOccurs,
		Nillable:  e.Nillable,
		Default:   e.Default,
		Doc:       documentation(e.Annotation),
	}
	target := e
	if e.Ref != "" {
		spec.XMLName = localName(e.Ref)
		if ref := ix.elements[localName(e.Ref)]; ref != nil {
			target = ref
		}
	}
	switch {
	case target.SimpleType != nil:
		ix.applySimpleType(spec, target.SimpleType)
	case target.Type != "":
		if st := ix.simpleTypes[localName(target.Type)]; st != nil {
			ix.applySimpleType(spec, st)
		}
	}
	return spec
}

func (ix *wsdlIndex) specForAttribute(a *xsd.Attribute) *fieldSpec {
	spec := &fieldSpec{XMLName: localName(a.Name), Doc: documentation(a.Annotation), MinOccurs: "0"}
	if a.Use == "required" {
		spec.MinOccurs = "1"
	}
	if a.SimpleType != nil {
		ix.applySimpleType(spec, a.SimpleType)
	} else if a.Type != "" {
		if st := ix.simpleTypes[localName(a.Type)]; st != nil {
			ix.applySimpleType(spec, st)
		}
	}
	return spec
}

func (ix *wsdlIndex) applySimpleType(spec *fieldSpec, st *xsd.SimpleType) {
	if st == nil || st.Restriction == nil {
		return
	}
	r := st.Restriction
	for _, e := range r.Enumerations {
		spec.Enum = append(spec.Enum, e.Value)
	}
	if r.Length != nil {
		if n, err := strconv.Atoi(r.Length.Value); err == nil {
			spec.MinLength, spec.MaxLength = n, n
		}
	}
	if r.MinLength != nil {
		if n, err := strconv.Atoi(r.MinLength.Value); err == nil {
			spec.MinLength = n
		}
	}
	if r.MaxLength != nil {
		if n, err := strconv.Atoi(r.MaxLength.Value); err == nil {
			spec.MaxLength = n
		}
	}
	spec.Pattern = joinPatterns(r.Patterns)
	if r.MinInclusive != nil {
		spec.Minimum = numericFacet(r.MinInclusive.Value)
	}
	if r.MaxInclusive != nil {
		spec.Maximum = numericFacet(r.MaxInclusive.Value)
	}
	if spec.Doc == "" {
		spec.Doc = documentation(st.Annotation)
	}
	if spec.Doc == "" {
		spec.Doc = documentation(r.Annotation)
	}
}

// joinPatterns merges the xs:pattern facets; JSON Schema allows a single
// pattern, so multiple facets are combined with non-capturing groups.
func joinPatterns(patterns []xsd.Pattern) string {
	var parts []string
	for i := range patterns {
		if v := strings.TrimSpace(patterns[i].Value); v != "" {
			parts = append(parts, v)
		}
	}
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	for i := range parts {
		parts[i] = "(?:" + parts[i] + ")"
	}
	return strings.Join(parts, "|")
}

// numericFacet returns the facet value only when it is usable as a JSON Schema
// number (XSD also allows date/duration bounds, which Huma cannot apply).
func numericFacet(value string) string {
	if value == "" {
		return ""
	}
	if _, err := strconv.ParseFloat(value, 64); err != nil {
		return ""
	}
	return value
}

func documentation(a *xsd.Annotation) string {
	if a == nil {
		return ""
	}
	var parts []string
	for _, d := range a.Documentation {
		if text := strings.TrimSpace(d.Content); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

func localName(qname string) string {
	if i := strings.LastIndexByte(qname, ':'); i >= 0 {
		return qname[i+1:]
	}
	return qname
}

// targetNamespace returns the first schema namespace (service contract ns).
func targetNamespace(defs *wsdl.Definitions) string {
	if defs.TargetNamespace != "" {
		return defs.TargetNamespace
	}
	if defs.Types != nil {
		for i := range defs.Types.Schemas {
			if ns := defs.Types.Schemas[i].TargetNamespace; ns != "" {
				return ns
			}
		}
	}
	return ""
}
