// Package testutil provides helpers shared by tests in this module.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// ModuleRoot returns the absolute path of the module root (the directory that
// contains go.mod), so tests can reference the shared testdata tree
// independently of their own package location.
func ModuleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("module root (go.mod) not found")
		}
		dir = parent
	}
}

// CorpusPath returns the path of an anonymized WSDL in testdata/corpus.
func CorpusPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(ModuleRoot(t), "testdata", "corpus", name+".wsdl")
}

// ClientFixture returns the directory of the committed soap-go client for a
// corpus WSDL (testdata/clients/<name>). The fixtures let the model and emit
// tests run without the soap-go CLI; refresh them with "make testdata-regen".
func ClientFixture(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(ModuleRoot(t), "testdata", "clients", name)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("client fixture %s: %v (run make testdata-regen)", dir, err)
	}
	return dir
}

// SoapBinary returns the soap-go CLI binary to use for end-to-end tests:
// SOAP_BIN when set, otherwise "soap" from PATH. It skips the test when no
// binary is available.
func SoapBinary(t *testing.T) string {
	t.Helper()
	if bin := os.Getenv("SOAP_BIN"); bin != "" {
		return bin
	}
	bin, err := exec.LookPath("soap")
	if err != nil {
		t.Skip("soap CLI not found: set SOAP_BIN or install github.com/enuan/soap-go/cmd/soap")
	}
	return bin
}

// E2E reports whether end-to-end tests (which need the soap-go CLI) should run.
func E2E() bool {
	return os.Getenv("SOAPGEN_E2E") == "1"
}

// UpdateFixtures reports whether committed fixtures should be rewritten.
func UpdateFixtures() bool {
	return os.Getenv("UPDATE_FIXTURES") == "1"
}

// CopyDir copies a directory tree into dst and returns dst.
func CopyDir(t *testing.T, src, dst string) string {
	t.Helper()
	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copy %s -> %s: %v", src, dst, err)
	}
	return dst
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
