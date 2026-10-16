// Package diagnose inspects a parsed WSDL for constructs that the gateway
// generator supports, silently ignores, or must reject.
//
// With the F0 fixes in github.com/enuan/soap-go the historic WSDL
// sanitization/flattening steps are no longer needed for generation, so this
// stage performs diagnostics only (plus the optional fallback normalization
// escape hatch in normalize.go).
package diagnose

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/enuan/soap-go/wsdl"
	"github.com/enuan/soap-go/xsd"
)

// XML namespaces used by WSDL 1.1, SOAP bindings and XSD.
const (
	NSWSDL             = "http://schemas.xmlsoap.org/wsdl/"
	NSSOAP11           = "http://schemas.xmlsoap.org/wsdl/soap/"
	NSSOAP12           = "http://schemas.xmlsoap.org/wsdl/soap12/"
	NSHTTP             = "http://schemas.xmlsoap.org/wsdl/http/"
	NSMIME             = "http://schemas.xmlsoap.org/wsdl/mime/"
	NSXSD              = "http://www.w3.org/2001/XMLSchema"
	MSSerializationNS  = "http://schemas.microsoft.com/2003/10/Serialization/"
	generatorReference = "enuan-soap-go >= v0.29.2-enuan.1"
)

// Severity classifies a finding. Errors block gateway generation.
type Severity string

const (
	Info    Severity = "info"
	Warning Severity = "warning"
	Error   Severity = "error"
)

var severityRank = map[Severity]int{Error: 0, Warning: 1, Info: 2}

// Finding is one diagnostic result.
type Finding struct {
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	Location string   `json:"location"`
	Message  string   `json:"message"`
}

// Stats summarizes the WSDL structure. Counts are independent of anonymous
// renames, which makes them usable to compare originals and anonymized copies.
type Stats struct {
	Services          int `json:"services"`
	Ports             int `json:"ports"`
	PortTypes         int `json:"portTypes"`
	Operations        int `json:"operations"`
	Messages          int `json:"messages"`
	Bindings          int `json:"bindings"`
	SOAP11Bindings    int `json:"soap11Bindings"`
	SOAP12Bindings    int `json:"soap12Bindings"`
	HTTPBindings      int `json:"httpBindings"`
	Schemas           int `json:"schemas"`
	ComplexTypes      int `json:"complexTypes"`
	SimpleTypes       int `json:"simpleTypes"`
	Elements          int `json:"elements"`      // top-level elements
	LocalElements     int `json:"localElements"` // elements inside type content models
	Attributes        int `json:"attributes"`
	Enumerations      int `json:"enumerations"` // number of enumeration values
	ComplexExtensions int `json:"complexExtensions"`
	Nillable          int `json:"nillable"`
	Unbounded         int `json:"unbounded"`
}

// Report is the complete diagnostic result for one WSDL document.
type Report struct {
	Ref             string    `json:"ref,omitempty"`
	SHA256          string    `json:"sha256,omitempty"`
	Service         string    `json:"service,omitempty"`
	TargetNamespace string    `json:"targetNamespace,omitempty"`
	Stats           Stats     `json:"stats"`
	Findings        []Finding `json:"findings"`
}

// HasErrors reports whether any finding blocks generation.
func (r *Report) HasErrors() bool {
	for _, f := range r.Findings {
		if f.Severity == Error {
			return true
		}
	}
	return false
}

func (r *Report) add(sev Severity, rule, loc, format string, args ...any) {
	r.Findings = append(r.Findings, Finding{
		Rule:     rule,
		Severity: sev,
		Location: loc,
		Message:  fmt.Sprintf(format, args...),
	})
}

func (r *Report) sortFindings() {
	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if severityRank[a.Severity] != severityRank[b.Severity] {
			return severityRank[a.Severity] < severityRank[b.Severity]
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		if a.Location != b.Location {
			return a.Location < b.Location
		}
		return a.Message < b.Message
	})
}

// Check analyzes defs. raw is the original document and is only used for
// constructs that the soap-go structs do not model (currently
// substitutionGroup); pass nil to skip raw-level checks.
func Check(defs *wsdl.Definitions, raw []byte) *Report {
	r := &Report{}
	if defs == nil {
		r.add(Error, "internal/no-definitions", "definitions", "no parsed definitions")
		r.sortFindings()
		return r
	}
	r.Service = defs.Name
	r.TargetNamespace = defs.TargetNamespace
	r.Stats.Services = len(defs.Service)
	for i := range defs.Service {
		r.Stats.Ports += len(defs.Service[i].Ports)
	}
	r.Stats.PortTypes = len(defs.PortType)
	for i := range defs.PortType {
		r.Stats.Operations += len(defs.PortType[i].Operations)
	}
	r.Stats.Messages = len(defs.Messages)

	c := &checker{r: r, typeNames: map[string]bool{}, elementNames: map[string]bool{}}

	for _, imp := range defs.Imports {
		r.add(Error, "unsupported/wsdl-import", "wsdl:import["+imp.Location+"]",
			"external WSDL imports are not supported; inline the imported document")
	}

	c.bindings(defs)
	c.portTypes(defs)
	c.schemas(defs)
	c.messages(defs)
	if raw != nil {
		c.rawScan(raw)
	}

	r.sortFindings()
	return r
}

type checker struct {
	r            *Report
	typeNames    map[string]bool // named complex types, for base/ref resolution
	elementNames map[string]bool // top-level elements, for wsdl:part resolution
}

func (c *checker) bindings(defs *wsdl.Definitions) {
	c.r.Stats.Bindings = len(defs.Binding)
	soap11ByPortType := map[string]int{}
	for i := range defs.Binding {
		b := &defs.Binding[i]
		loc := "binding:" + b.Name
		switch {
		case b.SOAP11Binding != nil:
			c.r.Stats.SOAP11Bindings++
			soap11ByPortType[localName(b.Type)]++
		case b.SOAP12Binding != nil:
			c.r.Stats.SOAP12Bindings++
		case b.HTTPBinding != nil:
			c.r.Stats.HTTPBindings++
			continue
		}
		if b.SOAP11Binding == nil && b.SOAP12Binding == nil {
			continue
		}
		style := ""
		if b.SOAP11Binding != nil {
			style = b.SOAP11Binding.Style
		}
		if b.SOAP12Binding != nil {
			style = b.SOAP12Binding.Style
		}
		for j := range b.BindingOperations {
			op := &b.BindingOperations[j]
			opLoc := loc + "/operation:" + op.Name
			opStyle := style
			if op.SOAP11Operation != nil && op.SOAP11Operation.Style != "" {
				opStyle = op.SOAP11Operation.Style
			}
			if op.SOAP12Operation != nil && op.SOAP12Operation.Style != "" {
				opStyle = op.SOAP12Operation.Style
			}
			if opStyle == "rpc" {
				c.r.add(Error, "unsupported/rpc-style", opLoc,
					"rpc style is not supported (SOAP 1.1 document/literal only)")
			}
			for _, body := range []*wsdl.BindingBody{op.Input, op.Output} {
				if body == nil {
					continue
				}
				if use := bodyUse(body); use == "encoded" {
					c.r.add(Error, "unsupported/encoded-use", opLoc,
						"use=\"encoded\" is not supported (literal only)")
				}
				if body.MIMEContent != nil || body.MIMEMultipartRelated != nil || body.MIMEXML != nil {
					c.r.add(Error, "unsupported/mime-binding", opLoc,
						"MIME/MTOM bindings are not supported")
				}
				if n := len(body.SOAP11Header) + len(body.SOAP12Header); n > 0 {
					c.r.add(Info, "info/soap-headers", opLoc,
						"%d soap:header(s) declared; supported by %s", n, generatorReference)
				}
			}
		}
	}
	switch {
	case c.r.Stats.SOAP11Bindings == 0 && c.r.Stats.SOAP12Bindings > 0:
		c.r.add(Error, "unsupported/soap12-only", "definitions",
			"no SOAP 1.1 binding found (SOAP 1.2 only)")
	case c.r.Stats.SOAP11Bindings == 0 && c.r.Stats.SOAP12Bindings == 0:
		c.r.add(Error, "unsupported/no-soap-binding", "definitions",
			"no SOAP 1.1/1.2 binding found")
	}
	if c.r.Stats.HTTPBindings > 0 {
		c.r.add(Info, "info/http-binding-ignored", "definitions",
			"%d HTTP GET/POST binding(s) ignored; SOAP bindings are used", c.r.Stats.HTTPBindings)
	}
	for _, pt := range sortedKeys(soap11ByPortType) {
		if n := soap11ByPortType[pt]; n > 1 {
			c.r.add(Info, "known-pitfall/duplicate-soap11-bindings", "portType:"+pt,
				"%d SOAP 1.1 bindings reference the same portType; %s emits one client method per operation",
				n, generatorReference)
		}
	}
}

func bodyUse(body *wsdl.BindingBody) string {
	if body.SOAP11Body != nil {
		return body.SOAP11Body.Use
	}
	if body.SOAP12Body != nil {
		return body.SOAP12Body.Use
	}
	return ""
}

func (c *checker) portTypes(defs *wsdl.Definitions) {
	for i := range defs.PortType {
		pt := &defs.PortType[i]
		for j := range pt.Operations {
			op := &pt.Operations[j]
			if op.Input == nil || op.Output == nil {
				c.r.add(Warning, "warning/one-way-operation",
					"portType:"+pt.Name+"/operation:"+op.Name,
					"operation without both input and output is not mapped by the gateway")
			}
		}
	}
}

func (c *checker) schemas(defs *wsdl.Definitions) {
	if defs.Types == nil {
		return
	}
	for i := range defs.Types.Schemas {
		s := &defs.Types.Schemas[i]
		for j := range s.ComplexTypes {
			if s.ComplexTypes[j].Name != "" {
				c.typeNames[s.ComplexTypes[j].Name] = true
			}
		}
		for j := range s.Elements {
			if s.Elements[j].Name != "" {
				c.elementNames[s.Elements[j].Name] = true
			}
		}
	}
	for i := range defs.Types.Schemas {
		c.schema(&defs.Types.Schemas[i])
	}
}

func (c *checker) schema(s *xsd.Schema) {
	c.r.Stats.Schemas++
	loc := "schema:" + s.TargetNamespace
	if s.TargetNamespace == MSSerializationNS {
		c.r.add(Info, "known-pitfall/ms-serialization-schema", loc,
			"Microsoft serialization schema detected; handled by %s (self-referencing simple types no longer overflow)",
			generatorReference)
	}
	if len(s.Imports) > 0 {
		c.r.add(Info, "info/xsd-imports", loc,
			"%d xs:import directive(s) resolved while parsing", len(s.Imports))
	}
	if len(s.Includes) > 0 {
		c.r.add(Info, "info/xsd-includes", loc,
			"%d xs:include directive(s) resolved while parsing", len(s.Includes))
	}
	extensions := 0
	for i := range s.Attributes {
		c.r.Stats.Attributes++
		c.nameHints(loc+"/attribute:"+s.Attributes[i].Name, s.Attributes[i].Name)
	}
	for i := range s.SimpleTypes {
		c.simpleType(loc+"/simpleType:"+s.SimpleTypes[i].Name, &s.SimpleTypes[i])
	}
	for i := range s.ComplexTypes {
		ct := &s.ComplexTypes[i]
		c.r.Stats.ComplexTypes++
		if ct.ComplexContent != nil && ct.ComplexContent.Extension != nil {
			extensions++
		}
		c.complexType(loc+"/complexType:"+ct.Name, ct)
	}
	for i := range s.Elements {
		e := &s.Elements[i]
		c.r.Stats.Elements++
		c.element(loc+"/element:"+e.Name, e)
	}
	if extensions > 0 {
		c.r.add(Info, "info/complex-content-extension", loc,
			"%d complex type(s) use complexContent/extension; base fields are flattened by %s",
			extensions, generatorReference)
	}
}

func (c *checker) simpleType(path string, st *xsd.SimpleType) {
	c.r.Stats.SimpleTypes++
	if st.Restriction != nil {
		c.r.Stats.Enumerations += len(st.Restriction.Enumerations)
		if base := localName(st.Restriction.Base); base != "" && base == st.Name {
			c.r.add(Info, "known-pitfall/simple-type-shadowing", path,
				"simple type restricted from %q shadows the XSD primitive with the same name; handled by %s (recursion guard)",
				st.Restriction.Base, generatorReference)
		}
	}
	if st.List != nil || st.Union != nil {
		c.r.add(Warning, "warning/xsd-list-union", path,
			"xs:list/xs:union simple types are not fully mapped by the gateway")
	}
}

func (c *checker) complexType(path string, ct *xsd.ComplexType) {
	if ct.Abstract {
		c.r.add(Error, "unsupported/abstract-type", path,
			"abstract complex types cannot be mapped to JSON objects")
	}
	if ct.Mixed {
		c.r.add(Warning, "warning/mixed-content", path,
			"mixed content is not mapped by the gateway")
	}
	if ct.SimpleContent != nil {
		c.r.add(Info, "info/simple-content", path,
			"simpleContent is mapped to a scalar Go type")
	}
	if cc := ct.ComplexContent; cc != nil {
		if ext := cc.Extension; ext != nil {
			c.r.Stats.ComplexExtensions++
			if base := localName(ext.Base); base != "" && !c.typeNames[base] {
				c.r.add(Warning, "warning/unresolved-base-type", path,
					"complexContent extension base %q is not declared in the inline schemas", ext.Base)
			}
			if ext.Sequence != nil {
				c.sequence(path+"/extension", ext.Sequence)
			}
			if ext.Choice != nil {
				c.r.add(Error, "unsupported/xsd-choice", path+"/extension",
					"xs:choice cannot be mapped to a typed JSON object")
				c.choice(path+"/extension", ext.Choice)
			}
			if ext.All != nil {
				c.all(path+"/extension", ext.All)
			}
		}
		if cc.Restriction != nil {
			c.r.add(Warning, "warning/complex-content-restriction", path,
				"complexContent restriction is not mapped by the gateway")
		}
	}
	if ct.Sequence != nil {
		c.sequence(path, ct.Sequence)
	}
	if ct.Choice != nil {
		c.r.add(Error, "unsupported/xsd-choice", path,
			"xs:choice cannot be mapped to a typed JSON object")
		c.choice(path, ct.Choice)
	}
	if ct.All != nil {
		c.all(path, ct.All)
	}
	if ct.Group != nil {
		c.r.add(Warning, "warning/xsd-group-ref", path,
			"xs:group references are not resolved by the generator")
	}
	if n := len(ct.Attributes) + len(ct.AttributeGroups); n > 0 || ct.AnyAttribute != nil {
		c.r.Stats.Attributes += n
		c.r.add(Info, "info/attributes", path,
			"%d attribute(s) declared; attributes are not part of the JSON body contract", n)
		for i := range ct.Attributes {
			c.nameHints(path+"/attribute:"+ct.Attributes[i].Name, ct.Attributes[i].Name)
		}
	}
}

func (c *checker) sequence(path string, seq *xsd.Sequence) {
	for i := range seq.Elements {
		c.r.Stats.LocalElements++
		c.element(path+"/sequence", &seq.Elements[i])
	}
	for i := range seq.Choices {
		c.r.add(Error, "unsupported/xsd-choice", path,
			"xs:choice cannot be mapped to a typed JSON object")
		c.choice(path, &seq.Choices[i])
	}
	for i := range seq.Sequences {
		c.sequence(path, &seq.Sequences[i])
	}
	for range seq.Any {
		c.r.add(Error, "unsupported/xsd-any", path,
			"xs:any cannot be mapped to a typed JSON object")
	}
	for i := range seq.Groups {
		c.r.add(Warning, "warning/xsd-group-ref", path,
			"xs:group references are not resolved by the generator (ref %q)", seq.Groups[i].Ref)
	}
}

func (c *checker) choice(path string, ch *xsd.Choice) {
	for i := range ch.Elements {
		c.r.Stats.LocalElements++
		c.element(path+"/choice", &ch.Elements[i])
	}
	for i := range ch.Choices {
		c.choice(path, &ch.Choices[i])
	}
	for i := range ch.Sequences {
		c.sequence(path, &ch.Sequences[i])
	}
	for range ch.Any {
		c.r.add(Error, "unsupported/xsd-any", path,
			"xs:any cannot be mapped to a typed JSON object")
	}
	for i := range ch.Groups {
		c.r.add(Warning, "warning/xsd-group-ref", path,
			"xs:group references are not resolved by the generator (ref %q)", ch.Groups[i].Ref)
	}
}

func (c *checker) all(path string, all *xsd.All) {
	for i := range all.Elements {
		c.r.Stats.LocalElements++
		c.element(path+"/all", &all.Elements[i])
	}
}

func (c *checker) element(path string, e *xsd.Element) {
	eloc := path + "/element:" + e.Name
	if e.Name == "" && e.Ref != "" {
		eloc = path + "/element[ref:" + localName(e.Ref) + "]"
	}
	if e.Nillable {
		c.r.Stats.Nillable++
	}
	if e.MaxOccurs == "unbounded" {
		c.r.Stats.Unbounded++
	}
	if e.Type == "" && e.ComplexType == nil && e.SimpleType == nil && e.Ref == "" {
		c.r.add(Warning, "warning/untyped-element", eloc,
			"element without type, inline type or ref maps to raw XML")
	}
	c.nameHints(eloc, e.Name)
	if e.ComplexType != nil {
		c.complexType(eloc+"/inline", e.ComplexType)
	}
	if e.SimpleType != nil {
		c.simpleType(eloc+"/inline", e.SimpleType)
	}
}

func (c *checker) messages(defs *wsdl.Definitions) {
	for i := range defs.Messages {
		m := &defs.Messages[i]
		if len(m.Parts) == 0 {
			c.r.add(Warning, "warning/empty-message", "message:"+m.Name,
				"message has no parts")
		}
		for j := range m.Parts {
			p := &m.Parts[j]
			if p.Element == "" && p.Type == "" {
				c.r.add(Warning, "warning/empty-message-part", "message:"+m.Name,
					"part %q has neither element nor type", p.Name)
			}
			if p.Element != "" && !c.elementNames[localName(p.Element)] {
				c.r.add(Warning, "warning/unresolved-message-part", "message:"+m.Name,
					"part %q references element %q which is not declared in any inline schema",
					p.Name, p.Element)
			}
		}
	}
}

// nameHints emits configuration hints for fields that usually require manual
// semantics in soapgen.yaml (session login, application error state).
func (c *checker) nameHints(path, name string) {
	if name == "" {
		return
	}
	lower := strings.ToLower(name)
	if containsAny(lower, "session", "sessione", "token") {
		c.r.add(Info, "config-hint/session-field", path,
			"field %q looks like a session/authentication field; configure auth.strategy in soapgen.yaml", name)
	}
	if containsAny(lower, "state", "successo", "codiceerrore", "errorcode", "errormessage", "esito") {
		c.r.add(Info, "config-hint/state-field", path,
			"field %q looks like an application state/error field; configure error_map in soapgen.yaml", name)
	}
}

// rawScan checks constructs that soap-go's structs do not model.
func (c *checker) rawScan(raw []byte) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			c.r.add(Warning, "warning/raw-scan", "document",
				"raw XML scan stopped early: %v", err)
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "element" {
			continue
		}
		var name string
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "name":
				name = a.Value
			case "substitutionGroup":
				c.r.add(Error, "unsupported/substitution-group", "xs:element:"+name,
					"substitution groups are not supported")
			}
		}
	}
}

func localName(qname string) string {
	if i := strings.LastIndexByte(qname, ':'); i >= 0 {
		return qname[i+1:]
	}
	return qname
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
