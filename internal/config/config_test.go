package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/enuan/enuan-soap-gen/internal/config"
)

func load(t *testing.T, extra string) *config.Config {
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
		t.Fatal(err)
	}
	return cfg
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
