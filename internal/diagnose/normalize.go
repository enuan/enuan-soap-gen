package diagnose

import (
	"bytes"
	"fmt"

	"github.com/enuan/enuan-soap-gen/internal/xmltree"
)

// NormalizeOptions selects the fallback WSDL transformations.
//
// These transformations are NOT part of the default generation path: with the
// enuan/soap-go fixes (v0.29.2-enuan.1) the code generator handles the
// Microsoft serialization schema, duplicate SOAP 1.1 bindings and
// complexContent inheritance directly. They remain as an escape hatch for
// WSDLs that hit a codegen bug that is not fixed yet.
type NormalizeOptions struct {
	RemoveSerializationSchema bool
	DedupeBindings            bool
	FlattenInheritance        bool
}

// DefaultNormalizeOptions enables every fallback transformation.
func DefaultNormalizeOptions() NormalizeOptions {
	return NormalizeOptions{
		RemoveSerializationSchema: true,
		DedupeBindings:            true,
		FlattenInheritance:        true,
	}
}

// NormalizeStats lists what the normalization changed.
type NormalizeStats struct {
	RemovedSchemas   []string `json:"removedSchemas,omitempty"`
	RemovedBindings  []string `json:"removedBindings,omitempty"`
	RemovedPorts     []string `json:"removedPorts,omitempty"`
	FlattenedTypes   []string `json:"flattenedTypes,omitempty"`
	UnflattenedTypes []string `json:"unflattenedTypes,omitempty"`
}

// Empty reports whether normalization changed nothing.
func (s *NormalizeStats) Empty() bool {
	return len(s.RemovedSchemas) == 0 && len(s.RemovedBindings) == 0 &&
		len(s.RemovedPorts) == 0 && len(s.FlattenedTypes) == 0
}

// Normalize applies the fallback transformations to a raw WSDL and returns the
// new document bytes. The input is never modified.
func Normalize(raw []byte, opts NormalizeOptions) ([]byte, *NormalizeStats, error) {
	doc, err := xmltree.Parse(raw)
	if err != nil {
		return nil, nil, err
	}
	stats := &NormalizeStats{}

	if opts.RemoveSerializationSchema {
		removeSerializationSchemas(doc, stats)
	}
	if opts.DedupeBindings {
		dedupeBindings(doc, stats)
	}
	if opts.FlattenInheritance {
		flattenInheritance(doc, stats)
	}

	return doc.Render(), stats, nil
}

func removeSerializationSchemas(doc *xmltree.Document, stats *NormalizeStats) {
	for _, n := range doc.Nodes() {
		if n.Name.Local != "schema" {
			continue
		}
		tns, ok := n.Attr(doc.Raw, "targetNamespace")
		if !ok || tns != MSSerializationNS {
			continue
		}
		n.Drop = true
		stats.RemovedSchemas = append(stats.RemovedSchemas, tns)
	}
}

// dedupeBindings keeps the first SOAP 1.1 binding for each portType and drops
// the following ones together with the service ports that reference them.
func dedupeBindings(doc *xmltree.Document, stats *NormalizeStats) {
	seenPortType := map[string]bool{}
	removed := map[string]bool{}
	for _, n := range doc.Nodes() {
		if n.Name.Local != "binding" || (n.Name.Space != "" && n.Name.Space != NSWSDL) {
			continue
		}
		if n.Child(NSSOAP11, "binding") == nil {
			continue
		}
		portType := ""
		if t, ok := n.Attr(doc.Raw, "type"); ok {
			portType = localName(t)
		}
		if portType != "" && seenPortType[portType] {
			name, _ := n.Attr(doc.Raw, "name")
			n.Drop = true
			removed[name] = true
			stats.RemovedBindings = append(stats.RemovedBindings, name)
			continue
		}
		seenPortType[portType] = true
	}
	if len(removed) == 0 {
		return
	}
	for _, n := range doc.Nodes() {
		if n.Name.Local != "port" || (n.Name.Space != "" && n.Name.Space != NSWSDL) {
			continue
		}
		binding, ok := n.Attr(doc.Raw, "binding")
		if !ok || !removed[localName(binding)] {
			continue
		}
		name, _ := n.Attr(doc.Raw, "name")
		n.Drop = true
		stats.RemovedPorts = append(stats.RemovedPorts, name)
	}
}

// flattenInheritance inlines complexContent/extension base fields into derived
// types, producing plain sequences. The transformation works on the original
// bytes so namespace prefixes and formatting survive.
func flattenInheritance(doc *xmltree.Document, stats *NormalizeStats) {
	f := &flattener{doc: doc, named: map[string]*xmltree.Node{}, memo: map[*xmltree.Node]flatPart{}}
	for _, n := range doc.Nodes() {
		if n.Name.Local != "complexType" {
			continue
		}
		if name, ok := n.Attr(doc.Raw, "name"); ok && name != "" {
			if _, exists := f.named[name]; !exists {
				f.named[name] = n
			}
		}
	}
	for _, n := range doc.Nodes() {
		if n.Name.Local != "complexType" {
			continue
		}
		cc := n.Child(NSXSD, "complexContent")
		if cc == nil || cc.Child(NSXSD, "extension") == nil {
			continue
		}
		part, ok := f.content(n)
		if !ok {
			name, _ := n.Attr(doc.Raw, "name")
			stats.UnflattenedTypes = append(stats.UnflattenedTypes, displayName(name))
			continue
		}
		prefix := cc.Prefix(doc.Raw)
		var replacement bytes.Buffer
		if len(bytes.TrimSpace(part.seq)) > 0 {
			fmt.Fprintf(&replacement, "<%ssequence>", qualify(prefix))
			replacement.Write(part.seq)
			fmt.Fprintf(&replacement, "</%ssequence>", qualify(prefix))
		}
		replacement.Write(part.attrs)
		cc.Replace = replacement.Bytes()

		name, _ := n.Attr(doc.Raw, "name")
		stats.FlattenedTypes = append(stats.FlattenedTypes, displayName(name))
	}
}

type flatPart struct {
	seq   []byte // content model children (elements)
	attrs []byte // attribute declarations
}

type flattener struct {
	doc   *xmltree.Document
	named map[string]*xmltree.Node
	memo  map[*xmltree.Node]flatPart
	busy  map[*xmltree.Node]bool
}

// content returns the flattened content of a complex type: the element
// declarations of its content model (including inherited ones) and its
// attribute declarations. ok is false when the type cannot be flattened
// faithfully (choice or group content models, inheritance cycles).
func (f *flattener) content(ct *xmltree.Node) (flatPart, bool) {
	if v, ok := f.memo[ct]; ok {
		return v, true
	}
	if f.busy[ct] {
		return flatPart{}, false // inheritance cycle: leave the document alone
	}
	if f.busy == nil {
		f.busy = map[*xmltree.Node]bool{}
	}
	f.busy[ct] = true
	defer delete(f.busy, ct)

	var part flatPart
	var model *xmltree.Node
	if cc := ct.Child(NSXSD, "complexContent"); cc != nil {
		ext := cc.Child(NSXSD, "extension")
		if ext == nil {
			return flatPart{}, false // restriction: not flattened
		}
		if base, exists := ext.Attr(f.doc.Raw, "base"); exists {
			if baseNode := f.named[localName(base)]; baseNode != nil && baseNode != ct {
				basePart, ok := f.content(baseNode)
				if !ok {
					return flatPart{}, false
				}
				part.seq = append(part.seq, basePart.seq...)
				part.attrs = append(part.attrs, basePart.attrs...)
			}
		}
		model = ext
	} else {
		model = ct
	}

	seq, ok := f.modelChildren(model)
	if !ok {
		return flatPart{}, false
	}
	part.seq = append(part.seq, seq...)
	part.attrs = append(part.attrs, f.attributeChildren(model)...)

	f.memo[ct] = part
	return part, true
}

// modelChildren returns the element declarations of the content model.
func (f *flattener) modelChildren(n *xmltree.Node) ([]byte, bool) {
	var out []byte
	for _, ch := range n.Children {
		switch ch.Name.Local {
		case "sequence", "all":
			out = append(out, f.doc.RenderInner(ch)...)
		case "choice", "group":
			return nil, false
		}
	}
	return out, true
}

func (f *flattener) attributeChildren(n *xmltree.Node) []byte {
	var out []byte
	for _, ch := range n.Children {
		switch ch.Name.Local {
		case "attribute", "attributeGroup":
			out = append(out, f.doc.RenderNode(ch)...)
		}
	}
	return out
}

func displayName(name string) string {
	if name == "" {
		return "#inline"
	}
	return name
}

func qualify(prefix string) string {
	if prefix == "" {
		return ""
	}
	return prefix + ":"
}
