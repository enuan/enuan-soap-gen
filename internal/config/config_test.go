package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enuan/enuan-soap-gen/internal/config"
)

func load(t *testing.T, extra string) *config.Config {
	t.Helper()
	cfg, err := loadErr(t, extra)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func loadErr(t *testing.T, extra string) (*config.Config, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "wsdl"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wsdl", "ChatService.wsdl"), []byte("<definitions/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	yaml := "service: chat\nwsdl: wsdl/ChatService.wsdl\nmodule: example.com/chat-gw\n" + extra
	path := filepath.Join(dir, "soapgen.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// TestRemovedSoapGoKeys: soap_go and soap_go_version no longer exist; strict
// decoding rejects them instead of silently ignoring them.
func TestRemovedSoapGoKeys(t *testing.T) {
	for _, key := range []string{
		"soap_go: github.com/example/soap-go\n",
		"soap_go_version: v0.29.2-enuan.1\n",
	} {
		if _, err := loadErr(t, key); err == nil {
			t.Errorf("soapgen.yaml with %q was accepted, want an unknown-field error", strings.TrimSpace(key))
		}
	}
}

// TestCmdDirKeepsGatewayName: the command directory (and therefore the binary
// built from it) keeps the configured gateway name, hyphens included.
func TestCmdDirKeepsGatewayName(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"hyphenated gateway", "gateway: uvetgbt-soap-gateway\n", filepath.Join("cmd", "uvetgbt-soap-gateway")},
		{"default gateway", "", filepath.Join("cmd", "chat-gateway")},
		{"unsafe characters", "gateway: \"../evil name\"\n", filepath.Join("cmd", "evil-name")},
		{"explicit cmd_dir wins", "gateway: uvetgbt-soap-gateway\ncmd_dir: cmd/custom\n", filepath.Join("cmd", "custom")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := load(t, tc.yaml).CmdDir; got != tc.want {
				t.Errorf("CmdDir = %q, want %q", got, tc.want)
			}
		})
	}
}
