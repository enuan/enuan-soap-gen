package xmltree

import (
	"strings"
	"testing"
)

func TestParseRenderRoundTrip(t *testing.T) {
	raw := []byte(`<?xml version="1.0"?><a xmlns:x="urn:x"><b x:attr="1" plain='2'>text<c/></b><d/></a>`)
	doc, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(doc.Render()); got != string(raw) {
		t.Fatalf("round trip mismatch:\n got: %s\nwant: %s", got, raw)
	}
	if doc.Root.Name.Local != "a" {
		t.Fatalf("root = %q", doc.Root.Name.Local)
	}
	b := doc.Root.Child("", "b")
	if b == nil {
		t.Fatal("child b not found")
	}
	if got := string(doc.Inner(b)); got != "text<c/>" {
		t.Fatalf("inner = %q", got)
	}
	attrs := b.RawAttrs(raw)
	if len(attrs) != 2 {
		t.Fatalf("attrs = %d", len(attrs))
	}
	if attrs[0].Name != "x:attr" || attrs[0].Value(raw) != "1" {
		t.Fatalf("attr 0 = %q=%q", attrs[0].Name, attrs[0].Value(raw))
	}
	if attrs[1].Name != "plain" || attrs[1].Value(raw) != "2" {
		t.Fatalf("attr 1 = %q=%q", attrs[1].Name, attrs[1].Value(raw))
	}
	c := b.Child("", "c")
	if c == nil || !c.SelfClosing {
		t.Fatalf("child c self-closing = %v", c != nil)
	}
	d := doc.Root.Child("", "d")
	if d == nil || !d.SelfClosing {
		t.Fatal("child d not self-closing")
	}
}

func TestEdits(t *testing.T) {
	raw := []byte(`<a><b>keep</b><c>drop</c><d>replace</d><e attr="old">tag</e></a>`)
	doc, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	doc.Root.Child("", "b").TagReplace = []byte(`<b class="x">`)
	doc.Root.Child("", "c").Drop = true
	doc.Root.Child("", "d").Replace = []byte("<d>new</d>")
	e := doc.Root.Child("", "e")
	e.TagReplace = e.RewriteStartTag(raw, func(name, value string) (string, bool) {
		if name == "attr" {
			return "new", true
		}
		return "", false
	})
	got := string(doc.Render())
	want := `<a><b class="x">keep</b><d>new</d><e attr="new">tag</e></a>`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestRewriteStartTagKeepsQuotes(t *testing.T) {
	raw := []byte(`<x a='1' b="2"/>`)
	doc, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	n := doc.Root
	got := string(n.RewriteStartTag(raw, func(name, value string) (string, bool) {
		if name == "b" {
			return "nuovo & <valore>", true
		}
		return "", false
	}))
	want := `<x a='1' b="nuovo &amp; &lt;valore&gt;"/>`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestEscapeUnescape(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"a&b", "a&amp;b"},
		{"<x>", "&lt;x&gt;"},
		{`he said "hi"`, "he said &quot;hi&quot;"},
		{"plain", "plain"},
	} {
		if got := EscapeAttr(tc.in); got != tc.want {
			t.Errorf("EscapeAttr(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if got := Unescape(tc.want); got != tc.in {
			t.Errorf("Unescape(%q) = %q, want %q", tc.want, got, tc.in)
		}
	}
	if got := Unescape("a&#65;&#x42;c"); got != "aABc" {
		t.Errorf("Unescape numeric = %q", got)
	}
	if got := Unescape("a&unknown;b"); got != "a&unknown;b" {
		t.Errorf("Unescape unknown = %q", got)
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := Parse([]byte("<a><b></a>")); err == nil {
		t.Fatal("expected mismatched end element error")
	}
	if _, err := Parse([]byte("<a><b>")); err == nil {
		t.Fatal("expected unclosed element error")
	}
	if _, err := Parse([]byte("")); err == nil {
		t.Fatal("expected no root element error")
	}
}

func TestPrefix(t *testing.T) {
	raw := []byte(`<xs:schema xmlns:xs="urn:xs"><complexType/></xs:schema>`)
	doc, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Root.Prefix(raw); got != "xs" {
		t.Fatalf("root prefix = %q", got)
	}
	if got := doc.Root.Child("", "complexType").Prefix(raw); got != "" {
		t.Fatalf("child prefix = %q", got)
	}
	if !strings.HasPrefix(string(doc.Render()), "<xs:schema") {
		t.Fatal("render lost prefix")
	}
}
