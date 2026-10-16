package ingest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const minimalWSDL = `<?xml version="1.0"?>
<definitions xmlns="http://schemas.xmlsoap.org/wsdl/" xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
    xmlns:tns="urn:test" targetNamespace="urn:test" name="Svc">
  <message name="PingIn"><part name="parameters" element="tns:Ping"/></message>
  <message name="PingOut"><part name="parameters" element="tns:PingResponse"/></message>
  <portType name="PingPortType">
    <operation name="Ping"><input message="tns:PingIn"/><output message="tns:PingOut"/></operation>
  </portType>
  <binding name="PingBinding" type="tns:PingPortType">
    <soap:binding transport="http://schemas.xmlsoap.org/soap/http"/>
    <operation name="Ping"><soap:operation soapAction="urn:test/Ping" style="document"/></operation>
  </binding>
  <service name="PingService"><port name="PingPort" binding="tns:PingBinding">
    <soap:address location="http://example.invalid/ping"/>
  </port></service>
  <types><schema targetNamespace="urn:test" xmlns="http://www.w3.org/2001/XMLSchema">
    <element name="Ping"><complexType><sequence><element name="message" type="string"/></sequence></complexType></element>
    <element name="PingResponse"><complexType><sequence><element name="result" type="string"/></sequence></complexType></element>
  </schema></types>
</definitions>`

func TestLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.wsdl")
	if err := os.WriteFile(path, []byte(minimalWSDL), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	if src.FromURL {
		t.Fatal("file source marked as URL")
	}
	if src.SHA256 == "" || len(src.SHA256) != 64 {
		t.Fatalf("bad hash %q", src.SHA256)
	}
	if _, err := src.Parse(); err != nil {
		t.Fatalf("parse: %v", err)
	}
}

func TestLoadURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wsdl/service.asmx" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, minimalWSDL)
	}))
	defer srv.Close()

	src, err := Load(srv.URL + "/wsdl/service.asmx?WSDL")
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	if !src.FromURL {
		t.Fatal("URL source not marked as URL")
	}
	if filepath.Base(src.Path) != "service.asmx" {
		t.Fatalf("temp file name = %q", src.Path)
	}
	if _, err := src.Parse(); err != nil {
		t.Fatalf("parse: %v", err)
	}
}

func TestLoadURLErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := Load(srv.URL); err == nil {
		t.Fatal("expected status error")
	}
}
