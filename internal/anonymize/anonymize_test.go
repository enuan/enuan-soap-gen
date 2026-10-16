package anonymize_test

import (
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enuan/enuan-soap-gen/internal/anonymize"
	"github.com/enuan/enuan-soap-gen/internal/diagnose"
	"github.com/enuan/enuan-soap-gen/internal/testutil"
	"github.com/enuan/soap-go/wsdl"
)

const sensitiveWSDL = `<?xml version="1.0" encoding="utf-8"?>
<definitions name="AcmeChat" targetNamespace="http://acme-corp.example/internal"
    xmlns="http://schemas.xmlsoap.org/wsdl/"
    xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
    xmlns:xs="http://www.w3.org/2001/XMLSchema"
    xmlns:tns="http://acme-corp.example/internal">
  <types>
    <xs:schema targetNamespace="http://acme-corp.example/internal" elementFormDefault="qualified">
      <xs:complexType name="AcmeSessionContext">
        <xs:sequence><xs:element name="SessionId" type="xs:string"/></xs:sequence>
      </xs:complexType>
      <xs:complexType name="AcmeSubmitRequest">
        <xs:complexContent><xs:extension base="tns:AcmeSessionContext">
          <xs:sequence><xs:element name="Payload" type="xs:string"/></xs:sequence>
        </xs:extension></xs:complexContent>
      </xs:complexType>
      <xs:element name="Submit"><xs:complexType><xs:sequence>
        <xs:element name="request" type="tns:AcmeSubmitRequest"/>
      </xs:sequence></xs:complexType></xs:element>
      <xs:element name="SubmitResponse"><xs:complexType><xs:sequence>
        <xs:element name="ok" type="xs:boolean"/>
      </xs:sequence></xs:complexType></xs:element>
    </xs:schema>
  </types>
  <message name="AcmeSubmitIn"><part name="parameters" element="tns:Submit"/></message>
  <message name="AcmeSubmitOut"><part name="parameters" element="tns:SubmitResponse"/></message>
  <portType name="AcmeChatPortType">
    <operation name="SubmitChat">
      <input message="tns:AcmeSubmitIn"/><output message="tns:AcmeSubmitOut"/>
    </operation>
  </portType>
  <binding name="AcmeChatBinding" type="tns:AcmeChatPortType">
    <soap:binding transport="http://schemas.xmlsoap.org/soap/http"/>
    <operation name="SubmitChat">
      <soap:operation soapAction="http://acme-corp.example/internal/SubmitChat" style="document"/>
      <input><soap:body use="literal"/></input>
      <output><soap:body use="literal"/></output>
    </operation>
  </binding>
  <service name="AcmeChatService">
    <port name="AcmeChatPort" binding="tns:AcmeChatBinding">
      <soap:address location="https://chat.internal.acme-corp.example/CHAT/ChatService.svc"/>
    </port>
  </service>
</definitions>`

func TestAnonymize(t *testing.T) {
	res, err := anonymize.Anonymize([]byte(sensitiveWSDL), anonymize.Options{
		DenyWords: []string{"acme", "chat"},
	})
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(res.XML))
	for _, leak := range []string{"acme", "chat", "internal.acme", "acme-corp.example"} {
		if strings.Contains(lower, leak) {
			t.Errorf("anonymized output still contains %q", leak)
		}
	}

	// Structural stats must be preserved.
	orig := parseWSDL(t, []byte(sensitiveWSDL))
	anon := parseWSDL(t, res.XML)
	origStats := diagnose.Check(orig, []byte(sensitiveWSDL)).Stats
	anonStats := diagnose.Check(anon, res.XML).Stats
	if origStats != anonStats {
		t.Errorf("stats changed:\norig %s\nanon %s", origStats, anonStats)
	}

	// WSDL-level symbols are always renamed; deny-matched XSD symbols too.
	if _, ok := res.Mapping.Operations["SubmitChat"]; !ok {
		t.Error("operation SubmitChat not renamed")
	}
	if _, ok := res.Mapping.Types["AcmeSubmitRequest"]; !ok {
		t.Error("deny-matched type AcmeSubmitRequest not renamed")
	}
	if _, ok := res.Mapping.Types["AcmeSessionContext"]; !ok {
		t.Error("deny-matched type AcmeSessionContext not renamed")
	}
	// Fields that do not match a deny word must survive.
	if !strings.Contains(string(res.XML), "SessionId") {
		t.Error("field SessionId should be preserved")
	}
	if !strings.Contains(string(res.XML), "Payload") {
		t.Error("field Payload should be preserved")
	}
	// The namespace must be replaced by a placeholder.
	if !strings.Contains(string(res.XML), "urn:example:ns1") {
		t.Error("namespace placeholder missing")
	}
	// QName references must stay consistent.
	if !strings.Contains(string(res.XML), `base="tns:Type1"`) {
		t.Errorf("QName reference to renamed base type not rewritten:\n%s", excerpt(res.XML, "complexContent"))
	}
}

func TestAnonymizeRefusesLeftoverDenyWord(t *testing.T) {
	raw := []byte(sensitiveWSDL)
	// "secret" only appears in an appinfo text node, which is preserved.
	raw = bytes.Replace(raw, []byte(`<xs:sequence><xs:element name="SessionId"`),
		[]byte(`<xs:annotation><xs:appinfo>secret</xs:appinfo></xs:annotation><xs:sequence><xs:element name="SessionId"`), 1)
	if _, err := anonymize.Anonymize(raw, anonymize.Options{DenyWords: []string{"secret"}}); err == nil {
		t.Fatal("expected the leak scan to reject the output")
	}
}

func TestCorpusIsLeakFree(t *testing.T) {
	root := testutil.ModuleRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "testdata", "corpus"))
	if err != nil {
		t.Fatal(err)
	}
	deny := []string{"biztravel", "alterdev", "uvet", "infocar", "tagliandi", "maintenance", "rtwebservices"}
	checked := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".wsdl") {
			continue
		}
		checked++
		raw, err := os.ReadFile(filepath.Join(root, "testdata", "corpus", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(string(raw))
		for _, w := range deny {
			if strings.Contains(lower, w) {
				t.Errorf("%s contains deny word %q", e.Name(), w)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no corpus files found")
	}
}

func parseWSDL(t *testing.T, raw []byte) *wsdl.Definitions {
	t.Helper()
	var defs wsdl.Definitions
	if err := xml.Unmarshal(raw, &defs); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return &defs
}

func excerpt(raw []byte, around string) string {
	s := string(raw)
	i := strings.Index(s, around)
	if i < 0 {
		return "(not found)"
	}
	end := min(i+200, len(s))
	return s[i:end]
}
