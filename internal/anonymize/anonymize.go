// Package anonymize turns a real-world WSDL into a public corpus copy:
// customer service/port/binding/portType/operation/message names, namespaces
// and endpoints are replaced with deterministic placeholders, while XSD type
// and field names are preserved (unless they match a deny word) so that
// generated-code assertions stay meaningful.
//
// The transformation is byte-range based (see internal/xmltree) and preserves
// namespace prefixes, attribute styles and formatting of everything it does
// not deliberately rewrite.
package anonymize

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/enuan/enuan-soap-gen/internal/diagnose"
	"github.com/enuan/enuan-soap-gen/internal/xmltree"
)

// DefaultPlaceholder is the host used for scrubbed endpoints.
const DefaultPlaceholder = "example.invalid"

// Options controls anonymization.
type Options struct {
	// Placeholder replaces the host of every absolute URL (default
	// DefaultPlaceholder).
	Placeholder string
	// DenyWords are matched case-insensitively against symbol names. Matching
	// XSD symbols (types, elements, attributes) are renamed wholesale;
	// WSDL-level symbols are always renamed.
	DenyWords []string
}

// Mapping records every rename that was applied.
type Mapping struct {
	Services   map[string]string
	Ports      map[string]string
	Bindings   map[string]string
	PortTypes  map[string]string
	Operations map[string]string
	Messages   map[string]string
	Types      map[string]string // xsd complexType/simpleType
	Elements   map[string]string
	Attributes map[string]string
	Namespaces map[string]string
}

// Result is the anonymized document plus the applied mapping.
type Result struct {
	XML     []byte
	Mapping *Mapping
}

var urlRe = regexp.MustCompile(`(?i)^https?://([a-z0-9.\-]+)(.*)$`)
var urlScanRe = regexp.MustCompile(`(?i)https?://([a-z0-9.\-]+)`)
var qnameRe = regexp.MustCompile(`^([A-Za-z_][\w.\-]*):(.+)$`)

// Anonymize rewrites raw and returns the public corpus copy.
func Anonymize(raw []byte, opts Options) (*Result, error) {
	if opts.Placeholder == "" {
		opts.Placeholder = DefaultPlaceholder
	}
	doc, err := xmltree.Parse(raw)
	if err != nil {
		return nil, err
	}
	a := &anonymizer{doc: doc, opts: opts, mapping: newMapping()}
	a.collect()
	a.buildRenames()

	for _, n := range doc.Nodes() {
		if n.Name.Local == "documentation" {
			n.Drop = true
			continue
		}
		var changed bool
		newTag := n.RewriteStartTag(doc.Raw, func(name, value string) (string, bool) {
			nv, ok := a.rewriteAttr(n, name, value)
			if !ok || nv == value {
				return "", false
			}
			changed = true
			return nv, true
		})
		if changed {
			n.TagReplace = newTag
		}
	}

	out := doc.Render()
	if err := scanInvariants(out, opts); err != nil {
		return nil, err
	}
	return &Result{XML: out, Mapping: a.mapping}, nil
}

// Renames returns all applied renames as sorted "kind: old -> new" strings.
func (m *Mapping) Renames() []string {
	var out []string
	add := func(kind string, mm map[string]string) {
		for _, old := range sortedMapKeys(mm) {
			out = append(out, fmt.Sprintf("%s: %s -> %s", kind, old, mm[old]))
		}
	}
	add("service", m.Services)
	add("port", m.Ports)
	add("binding", m.Bindings)
	add("portType", m.PortTypes)
	add("operation", m.Operations)
	add("message", m.Messages)
	add("type", m.Types)
	add("element", m.Elements)
	add("attribute", m.Attributes)
	add("namespace", m.Namespaces)
	return out
}

type anonymizer struct {
	doc     *xmltree.Document
	opts    Options
	mapping *Mapping

	symbols map[string][]string // kind -> unique names in document order
	seen    map[string]map[string]bool
	nsList  []string
	nsSeen  map[string]bool
}

var kinds = []string{"services", "ports", "bindings", "portTypes", "operations", "messages", "types", "elements", "attributes"}

var kindPrefix = map[string]string{
	"services":   "Service",
	"ports":      "Port",
	"bindings":   "Binding",
	"portTypes":  "PortType",
	"operations": "Operation",
	"messages":   "Message",
	"types":      "Type",
	"elements":   "Element",
	"attributes": "Attribute",
}

func newMapping() *Mapping {
	return &Mapping{
		Services:   map[string]string{},
		Ports:      map[string]string{},
		Bindings:   map[string]string{},
		PortTypes:  map[string]string{},
		Operations: map[string]string{},
		Messages:   map[string]string{},
		Types:      map[string]string{},
		Elements:   map[string]string{},
		Attributes: map[string]string{},
		Namespaces: map[string]string{},
	}
}

func (a *anonymizer) collect() {
	a.symbols = map[string][]string{}
	a.seen = map[string]map[string]bool{}
	a.nsSeen = map[string]bool{}
	for _, k := range kinds {
		a.seen[k] = map[string]bool{}
	}
	for _, n := range a.doc.Nodes() {
		if kind := symbolKind(n.Name.Space, n.Name.Local); kind != "" {
			if name, ok := n.Attr(a.doc.Raw, "name"); ok && name != "" {
				a.addSymbol(kind, name)
			}
		}
		for _, attr := range n.RawAttrs(a.doc.Raw) {
			if _, _, ok := xmltree.NamespaceDecl(attr.Name); !ok {
				continue
			}
			uri := attr.Value(a.doc.Raw)
			if uri == "" || isStandardNamespace(uri) || a.nsSeen[uri] {
				continue
			}
			a.nsSeen[uri] = true
			a.nsList = append(a.nsList, uri)
		}
	}
}

func (a *anonymizer) addSymbol(kind, name string) {
	if a.seen[kind][name] {
		return
	}
	a.seen[kind][name] = true
	a.symbols[kind] = append(a.symbols[kind], name)
}

func (a *anonymizer) buildRenames() {
	for _, kind := range kinds {
		force := kind != "types" && kind != "elements" && kind != "attributes"
		for _, name := range a.symbols[kind] {
			if !force && !a.denied(name) {
				continue
			}
			a.mappingFor(kind)[name] = kindPrefix[kind] + strconv.Itoa(len(a.mappingFor(kind))+1)
		}
	}
	for i, uri := range a.nsList {
		a.mapping.Namespaces[uri] = fmt.Sprintf("urn:example:ns%d", i+1)
	}
}

func (a *anonymizer) mappingFor(kind string) map[string]string {
	switch kind {
	case "services":
		return a.mapping.Services
	case "ports":
		return a.mapping.Ports
	case "bindings":
		return a.mapping.Bindings
	case "portTypes":
		return a.mapping.PortTypes
	case "operations":
		return a.mapping.Operations
	case "messages":
		return a.mapping.Messages
	case "types":
		return a.mapping.Types
	case "elements":
		return a.mapping.Elements
	case "attributes":
		return a.mapping.Attributes
	}
	return nil
}

func (a *anonymizer) denied(name string) bool {
	lower := strings.ToLower(name)
	for _, w := range a.opts.DenyWords {
		if w != "" && strings.Contains(lower, strings.ToLower(w)) {
			return true
		}
	}
	return false
}

func (a *anonymizer) rewriteAttr(n *xmltree.Node, name, value string) (string, bool) {
	// Namespace declarations: map the URI, keep the prefix.
	if _, _, ok := xmltree.NamespaceDecl(name); ok {
		if newURI, ok := a.mapping.Namespaces[value]; ok {
			return newURI, true
		}
		return "", false
	}

	// Symbol definitions.
	if name == "name" {
		if kind := symbolKind(n.Name.Space, n.Name.Local); kind != "" {
			if newName, ok := a.mappingFor(kind)[value]; ok {
				return newName, true
			}
		}
		return "", false
	}

	// QName references.
	if kind := qnameAttrKind(n.Name.Space, n.Name.Local, name); kind != "" {
		if m := qnameRe.FindStringSubmatch(value); m != nil {
			if newName, ok := a.mappingFor(kind)[m[2]]; ok {
				return m[1] + ":" + newName, true
			}
			return "", false
		}
		return "", false
	}

	newValue := value
	// Namespace URI occurrences inside values (soapAction, targetNamespace, ...).
	for oldURI, newURI := range a.mapping.Namespaces {
		newValue = strings.ReplaceAll(newValue, oldURI, newURI)
	}
	// WSDL-level symbol occurrences inside id-like values.
	if substringAttr(name) {
		for _, kind := range []string{"bindings", "portTypes", "operations", "services", "ports", "messages"} {
			newValue = replaceAllSymbols(newValue, a.mappingFor(kind))
		}
	}
	// Absolute URLs pointing at customer infrastructure.
	if m := urlRe.FindStringSubmatch(newValue); m != nil && !allowedHost(m[1]) {
		newValue = "https://" + a.opts.Placeholder + "/service"
	}
	if newValue != value {
		return newValue, true
	}
	return "", false
}

// replaceAllSymbols replaces old symbol names by new ones, longest first, so
// that "Binding1" is not renamed inside "Binding10".
func replaceAllSymbols(value string, m map[string]string) string {
	keys := sortedMapKeysByLen(m)
	for _, old := range keys {
		if old != "" && strings.Contains(value, old) {
			value = strings.ReplaceAll(value, old, m[old])
		}
	}
	return value
}

func symbolKind(space, local string) string {
	if space == diagnose.NSWSDL || space == "" {
		switch local {
		case "definitions", "service":
			return "services"
		case "port":
			return "ports"
		case "binding":
			return "bindings"
		case "portType":
			return "portTypes"
		case "operation":
			return "operations"
		case "message":
			return "messages"
		}
	}
	if space == diagnose.NSXSD {
		switch local {
		case "complexType", "simpleType":
			return "types"
		case "element":
			return "elements"
		case "attribute":
			return "attributes"
		}
	}
	return ""
}

func qnameAttrKind(space, local, attr string) string {
	switch attr {
	case "message":
		return "messages"
	case "binding":
		if space == diagnose.NSWSDL && local == "port" {
			return "bindings"
		}
	case "element":
		if space == diagnose.NSWSDL && local == "part" {
			return "elements"
		}
	case "type":
		if space == diagnose.NSWSDL && local == "binding" {
			return "portTypes" // wsdl:binding/@type references a portType
		}
		return "types"
	case "base", "itemType", "memberTypes":
		return "types"
	case "ref":
		return "elements"
	}
	return ""
}

func substringAttr(name string) bool {
	switch name {
	case "wsu:Id", "URI", "soapAction", "soap12Action", "wsaw:Action", "wsam:Action", "Action":
		return true
	}
	return false
}

func isStandardNamespace(uri string) bool {
	for _, prefix := range []string{
		"http://schemas.xmlsoap.org/",
		"http://www.w3.org/",
		"http://schemas.microsoft.com/",
		"http://docs.oasis-open.org/",
		"http://example.invalid/",
		"urn:example:",
	} {
		if strings.HasPrefix(uri, prefix) {
			return true
		}
	}
	return false
}

func allowedHost(host string) bool {
	host = strings.ToLower(host)
	for _, suffix := range []string{
		"schemas.xmlsoap.org",
		"www.w3.org",
		"schemas.microsoft.com",
		"docs.oasis-open.org",
		DefaultPlaceholder,
	} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

// scanInvariants verifies that nothing sensitive survived anonymization.
func scanInvariants(out []byte, opts Options) error {
	lower := bytes.ToLower(out)
	var leftover []string
	for _, w := range opts.DenyWords {
		if w != "" && bytes.Contains(lower, []byte(strings.ToLower(w))) {
			leftover = append(leftover, w)
		}
	}
	if len(leftover) > 0 {
		return fmt.Errorf("anonymized output still contains deny word(s): %s", strings.Join(leftover, ", "))
	}
	for _, m := range urlScanRe.FindAllSubmatch(out, -1) {
		host := strings.ToLower(string(m[1]))
		if !allowedHost(host) {
			return fmt.Errorf("anonymized output still contains non-placeholder host %q", host)
		}
	}
	return nil
}

func sortedMapKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedMapKeysByLen(m map[string]string) []string {
	out := sortedMapKeys(m)
	sort.SliceStable(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// ErrNoDenyWords is returned by callers that require a deny list.
var ErrNoDenyWords = errors.New("no deny words configured; refusing to anonymize without a leak check")
