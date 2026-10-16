// Package testutil provides helpers shared by tests in this module.
package testutil

import (
	"os"
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
