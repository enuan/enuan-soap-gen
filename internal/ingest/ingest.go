// Package ingest loads WSDL documents from local files or HTTP(S) URLs and
// records a content hash so that generated artifacts can be traced back to the
// exact WSDL revision they came from.
package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/enuan/soap-go/wsdl"
)

// MaxDocumentSize bounds downloaded WSDL documents.
const MaxDocumentSize = 32 << 20 // 32 MiB

// Source is a loaded WSDL document.
type Source struct {
	Ref     string // original reference: file path or URL
	Path    string // local path usable for include/import resolution
	Raw     []byte
	SHA256  string
	FromURL bool

	tmpDir string
}

// Load reads a WSDL from a file path or an HTTP(S) URL.
func Load(ref string) (*Source, error) {
	if isURL(ref) {
		return loadURL(ref)
	}
	raw, err := os.ReadFile(ref)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", ref, err)
	}
	if len(raw) > MaxDocumentSize {
		return nil, fmt.Errorf("read %s: document exceeds %d bytes", ref, MaxDocumentSize)
	}
	return &Source{Ref: ref, Path: ref, Raw: raw, SHA256: hash(raw)}, nil
}

// Close releases the temporary file created for URL sources.
func (s *Source) Close() error {
	if s.tmpDir == "" {
		return nil
	}
	return os.RemoveAll(s.tmpDir)
}

// Parse unmarshals the WSDL, resolving xs:include/xs:import relative to the
// document location.
func (s *Source) Parse() (*wsdl.Definitions, error) {
	defs, err := wsdl.ParseFromFile(s.Path)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.Ref, err)
	}
	return defs, nil
}

func hash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func isURL(ref string) bool {
	u, err := url.Parse(ref)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}

func loadURL(ref string) (*Source, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(ref)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", ref, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %s", ref, resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxDocumentSize+1))
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", ref, err)
	}
	if len(raw) > MaxDocumentSize {
		return nil, fmt.Errorf("GET %s: document exceeds %d bytes", ref, MaxDocumentSize)
	}

	dir, err := os.MkdirTemp("", "soapgen-wsdl-*")
	if err != nil {
		return nil, err
	}
	base := "service.wsdl"
	if u, err := url.Parse(ref); err == nil && path.Base(u.Path) != "." && path.Base(u.Path) != "/" {
		base = path.Base(u.Path)
	}
	local := filepath.Join(dir, base)
	if err := os.WriteFile(local, raw, 0o600); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return &Source{Ref: ref, Path: local, Raw: raw, SHA256: hash(raw), FromURL: true, tmpDir: dir}, nil
}
