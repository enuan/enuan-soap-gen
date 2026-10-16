// Package xmltree provides a minimal XML DOM that keeps the original byte
// offsets of every element.
//
// WSDL transformations (fallback normalization, anonymization) must edit raw
// documents while preserving namespace prefixes, formatting and everything
// they do not touch deliberately. encoding/xml's struct round-trip is lossy
// for WSDL documents (it mangles xmlns declarations and drops prefixes), so
// transformations are applied as byte-range edits over the original document.
package xmltree

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// Node is an element of the parsed document, anchored to the original bytes.
type Node struct {
	Name        xml.Name
	StartOff    int // offset of '<' of the start tag
	StartTagEnd int // offset just past '>' of the start tag
	EndTagStart int // offset of '<' of the end tag (StartTagEnd when self-closing)
	EndOff      int // offset just past '>' of the end tag
	SelfClosing bool
	Parent      *Node
	Children    []*Node

	// Edit directives applied by Render.
	Drop       bool   // remove the element (and its content) entirely
	Replace    []byte // replaces the whole element range [StartOff, EndOff)
	TagReplace []byte // replaces the start tag range [StartOff, StartTagEnd)
}

// Document is a parsed XML document.
type Document struct {
	Raw  []byte
	Root *Node
	all  []*Node
}

// Parse builds the element tree of raw.
func Parse(raw []byte) (*Document, error) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	doc := &Document{Raw: raw}
	var stack []*Node
	for {
		before := int(dec.InputOffset())
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &Node{Name: t.Name, StartOff: findLT(raw, before)}
			n.StartTagEnd = int(dec.InputOffset())
			n.SelfClosing = n.StartTagEnd-2 >= n.StartOff && raw[n.StartTagEnd-2] == '/' && raw[n.StartTagEnd-1] == '>'
			if len(stack) == 0 {
				if doc.Root != nil {
					return nil, fmt.Errorf("parse XML: multiple root elements")
				}
				doc.Root = n
			} else {
				p := stack[len(stack)-1]
				n.Parent = p
				p.Children = append(p.Children, n)
			}
			doc.all = append(doc.all, n)
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("parse XML: unexpected end element </%s>", t.Name.Local)
			}
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if n.Name.Local != t.Name.Local {
				return nil, fmt.Errorf("parse XML: mismatched end element </%s> for <%s>", t.Name.Local, n.Name.Local)
			}
			if n.SelfClosing {
				n.EndTagStart = n.StartTagEnd
				n.EndOff = n.StartTagEnd
				break
			}
			n.EndTagStart = findLT(raw, before)
			n.EndOff = int(dec.InputOffset())
		}
	}
	if len(stack) != 0 {
		return nil, fmt.Errorf("parse XML: unclosed element <%s>", stack[len(stack)-1].Name.Local)
	}
	if doc.Root == nil {
		return nil, fmt.Errorf("parse XML: no root element")
	}
	return doc, nil
}

// Nodes returns all elements in document order (pre-order).
func (d *Document) Nodes() []*Node { return d.all }

// Render applies the edit directives and returns the new document bytes.
func (d *Document) Render() []byte {
	var out bytes.Buffer
	out.Grow(len(d.Raw))
	out.Write(d.Raw[:d.Root.StartOff]) // XML declaration, comments, doctype
	d.render(d.Root, &out)
	out.Write(d.Raw[d.Root.EndOff:]) // trailing whitespace/comments
	return out.Bytes()
}

func (d *Document) render(n *Node, out *bytes.Buffer) {
	if n.Drop {
		return
	}
	if n.Replace != nil {
		out.Write(n.Replace)
		return
	}
	if n.TagReplace != nil {
		out.Write(n.TagReplace)
	} else {
		out.Write(d.Raw[n.StartOff:n.StartTagEnd])
	}
	if n.SelfClosing {
		return
	}
	pos := n.StartTagEnd
	for _, ch := range n.Children {
		out.Write(d.Raw[pos:ch.StartOff])
		d.render(ch, out)
		pos = ch.EndOff
	}
	out.Write(d.Raw[pos:n.EndTagStart])
	out.Write(d.Raw[n.EndTagStart:n.EndOff])
}

// Inner returns the raw bytes between the start and end tags.
func (d *Document) Inner(n *Node) []byte { return d.Raw[n.StartTagEnd:n.EndTagStart] }

// RenderInner renders the content between the start and end tags, applying
// the edit directives of descendants.
func (d *Document) RenderInner(n *Node) []byte {
	var out bytes.Buffer
	d.renderChildren(n, &out)
	return out.Bytes()
}

// RenderNode renders a single element (including its tags), applying the edit
// directives of the node itself and its descendants.
func (d *Document) RenderNode(n *Node) []byte {
	var out bytes.Buffer
	d.render(n, &out)
	return out.Bytes()
}

func (d *Document) renderChildren(n *Node, out *bytes.Buffer) {
	pos := n.StartTagEnd
	for _, ch := range n.Children {
		out.Write(d.Raw[pos:ch.StartOff])
		d.render(ch, out)
		pos = ch.EndOff
	}
	out.Write(d.Raw[pos:n.EndTagStart])
}

// Child returns the first child element with the given namespace and local
// name (space "" matches any namespace).
func (n *Node) Child(space, local string) *Node {
	for _, c := range n.Children {
		if c.Name.Local == local && (space == "" || c.Name.Space == space) {
			return c
		}
	}
	return nil
}

// ChildrenNamed returns all child elements with the given namespace and local
// name (space "" matches any namespace).
func (n *Node) ChildrenNamed(space, local string) []*Node {
	var out []*Node
	for _, c := range n.Children {
		if c.Name.Local == local && (space == "" || c.Name.Space == space) {
			out = append(out, c)
		}
	}
	return out
}

// RawAttr is one attribute of a start tag, with the byte offsets of its name
// and value inside the original document.
type RawAttr struct {
	Name       string // raw name including prefix, e.g. "type", "xmlns:tns", "wsu:Id"
	NameStart  int
	NameEnd    int
	ValueStart int // offset of the opening quote
	ValueEnd   int // offset just past the closing quote
}

// Value returns the unescaped attribute value.
func (a RawAttr) Value(raw []byte) string {
	return Unescape(string(raw[a.ValueStart+1 : a.ValueEnd-1]))
}

// RawAttrs parses the start tag of n.
func (n *Node) RawAttrs(raw []byte) []RawAttr {
	return ScanStartTag(raw, n.StartOff, n.StartTagEnd)
}

// Attr returns the value of the first attribute whose raw name matches.
func (n *Node) Attr(raw []byte, name string) (string, bool) {
	for _, a := range n.RawAttrs(raw) {
		if a.Name == name {
			return a.Value(raw), true
		}
	}
	return "", false
}

// NamespaceDecl reports the namespace URI declared by an xmlns attribute, if
// the raw attribute name is a namespace declaration.
func NamespaceDecl(name string) (prefix, uri string, ok bool) {
	if name == "xmlns" {
		return "", "", true
	}
	if rest, found := strings.CutPrefix(name, "xmlns:"); found {
		return rest, "", true
	}
	return "", "", false
}

// Prefix returns the namespace prefix used in the element's start tag ("" if
// the element name is unqualified).
func (n *Node) Prefix(raw []byte) string {
	i := n.StartOff + 1
	j := i
	for j < n.StartTagEnd && raw[j] != ':' && raw[j] != '>' && raw[j] != '/' && !isSpace(raw[j]) {
		j++
	}
	if j < n.StartTagEnd && raw[j] == ':' {
		return string(raw[i:j])
	}
	return ""
}

// ScanStartTag parses raw[start:end], which must be a start tag, and returns
// its attributes in document order.
func ScanStartTag(raw []byte, start, end int) []RawAttr {
	i := start + 1 // skip '<'
	for i < end && !isSpace(raw[i]) && raw[i] != '/' && raw[i] != '>' {
		i++ // element name
	}
	var attrs []RawAttr
	for i < end {
		for i < end && isSpace(raw[i]) {
			i++
		}
		if i >= end || raw[i] == '/' || raw[i] == '>' {
			break
		}
		nameStart := i
		for i < end && !isSpace(raw[i]) && raw[i] != '=' {
			i++
		}
		nameEnd := i
		for i < end && isSpace(raw[i]) {
			i++
		}
		if i >= end || raw[i] != '=' {
			break // malformed; stop scanning
		}
		i++ // '='
		for i < end && isSpace(raw[i]) {
			i++
		}
		if i >= end || (raw[i] != '"' && raw[i] != '\'') {
			break
		}
		quote := raw[i]
		valueStart := i
		i++
		for i < end && raw[i] != quote {
			i++
		}
		if i >= end {
			break // unterminated value
		}
		i++ // closing quote
		attrs = append(attrs, RawAttr{
			Name:       string(raw[nameStart:nameEnd]),
			NameStart:  nameStart,
			NameEnd:    nameEnd,
			ValueStart: valueStart,
			ValueEnd:   i,
		})
	}
	return attrs
}

// RewriteStartTag returns the start tag of n with attribute values substituted
// by rewrite. Everything not returned by rewrite is copied verbatim (including
// single/double quote style). rewrite receives the raw attribute name and the
// unescaped value.
func (n *Node) RewriteStartTag(raw []byte, rewrite func(name, value string) (string, bool)) []byte {
	attrs := n.RawAttrs(raw)
	var out bytes.Buffer
	out.Grow(n.StartTagEnd - n.StartOff)
	pos := n.StartOff
	for _, a := range attrs {
		out.Write(raw[pos : a.ValueStart+1]) // up to and including the opening quote
		if nv, ok := rewrite(a.Name, a.Value(raw)); ok {
			out.WriteString(EscapeAttr(nv))
		} else {
			out.Write(raw[a.ValueStart+1 : a.ValueEnd-1])
		}
		pos = a.ValueEnd - 1 // closing quote
	}
	out.Write(raw[pos:n.StartTagEnd])
	return out.Bytes()
}

// Unescape expands the standard XML entities. WSDL attribute values rarely
// need more than the predefined entities; numeric references are supported.
func Unescape(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	var out strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '&' {
			out.WriteByte(s[i])
			i++
			continue
		}
		j := strings.IndexByte(s[i:], ';')
		if j < 0 {
			out.WriteByte(s[i])
			i++
			continue
		}
		entity := s[i+1 : i+j]
		switch entity {
		case "lt":
			out.WriteByte('<')
		case "gt":
			out.WriteByte('>')
		case "amp":
			out.WriteByte('&')
		case "quot":
			out.WriteByte('"')
		case "apos":
			out.WriteByte('\'')
		default:
			if r, ok := decodeNumericEntity(entity); ok {
				out.WriteRune(r)
			} else {
				out.WriteString(s[i : i+j+1])
			}
		}
		i += j + 1
	}
	return out.String()
}

func decodeNumericEntity(entity string) (rune, bool) {
	if rest, ok := strings.CutPrefix(entity, "#x"); ok {
		var r rune
		if _, err := fmt.Sscanf(rest, "%x", &r); err != nil {
			return 0, false
		}
		return r, true
	}
	if rest, ok := strings.CutPrefix(entity, "#"); ok {
		var r rune
		if _, err := fmt.Sscanf(rest, "%d", &r); err != nil {
			return 0, false
		}
		return r, true
	}
	return 0, false
}

// EscapeAttr escapes a value for use inside a double-quoted attribute.
func EscapeAttr(s string) string {
	if !strings.ContainsAny(s, "&<>\"") {
		return s
	}
	var out strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			out.WriteString("&amp;")
		case '<':
			out.WriteString("&lt;")
		case '>':
			out.WriteString("&gt;")
		case '"':
			out.WriteString("&quot;")
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

func findLT(raw []byte, from int) int {
	if i := bytes.IndexByte(raw[from:], '<'); i >= 0 {
		return from + i
	}
	return from
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }
