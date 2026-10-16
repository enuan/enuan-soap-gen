package verify_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enuan/enuan-soap-gen/internal/config"
	"github.com/enuan/enuan-soap-gen/internal/emit"
	"github.com/enuan/enuan-soap-gen/internal/model"
	"github.com/enuan/enuan-soap-gen/internal/testutil"
	"github.com/enuan/enuan-soap-gen/internal/verify"
)

const tagliandiYAML = `service: tagliandi
wsdl: %WS
module: example.com/tagliandi-gw
gateway: tagliandi-gateway
auth:
  strategy: login
  login:
    operation: Operation1
    token_field: LoginResult.SessionId
    username_field: Request.CompanyLogin
    password_field: Request.Password
    inject: Context.SessionId
error_map:
  state:
    success_field: Successo
    code_field: CodiceErrore
    message_field: Messaggio
`

// generate emits the tagliandi gateway into a temp dir.
func generate(t *testing.T) (*config.Config, string) {
	t.Helper()
	root := t.TempDir()
	yaml := strings.Replace(tagliandiYAML, "%WS", testutil.CorpusPath(t, "tagliandi"), 1)
	cfgPath := filepath.Join(root, "soapgen.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	m, err := model.Build(cfg, testutil.ClientFixture(t, "tagliandi"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := emit.Run(emit.Options{Root: root, Config: cfg, Model: m}); err != nil {
		t.Fatal(err)
	}
	testutil.CopyDir(t, testutil.ClientFixture(t, "tagliandi"), filepath.Join(root, cfg.ClientDir))
	return cfg, root
}

func run(t *testing.T, cfg *config.Config) *verify.Report {
	t.Helper()
	report, err := verify.Run(verify.Options{Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func hasCheck(t *testing.T, report *verify.Report, name string, status verify.Status) {
	t.Helper()
	for _, c := range report.Checks {
		if c.Name == name {
			if c.Status != status {
				t.Fatalf("check %s = %s (%s), want %s", name, c.Status, c.Detail, status)
			}
			return
		}
	}
	t.Fatalf("check %s not found", name)
}

func TestRunGeneratedProject(t *testing.T) {
	cfg, root := generate(t)
	report := run(t, cfg)
	if report.Failed() {
		t.Fatalf("report failed:\n%s", render(t, report))
	}
	hasCheck(t, report, "client", verify.OK)
	hasCheck(t, report, "client/Operation1", verify.OK)
	hasCheck(t, report, "api/dtos", verify.OK)
	hasCheck(t, report, "api/register/Operation3", verify.OK)
	hasCheck(t, report, "auth/login", verify.OK)
	hasCheck(t, report, "error_map/state", verify.OK)

	// JSON report is machine readable.
	var buf bytes.Buffer
	if err := report.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Service string         `json:"service"`
		Checks  []verify.Check `json:"checks"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON report: %v\n%s", err, buf.String())
	}
	if decoded.Service != "tagliandi" || len(decoded.Checks) != len(report.Checks) {
		t.Errorf("unexpected JSON report: %+v", decoded)
	}
	_ = root
}

func TestRunDetectsMissingSessionWiring(t *testing.T) {
	cfg, root := generate(t)
	if err := os.Remove(filepath.Join(root, cfg.APIDir, "auth_gen.go")); err != nil {
		t.Fatal(err)
	}
	report := run(t, cfg)
	hasCheck(t, report, "auth/login", verify.Fail)
	if !report.Failed() {
		t.Fatal("expected the report to fail")
	}
}

func TestRunDetectsMissingInjection(t *testing.T) {
	cfg, root := generate(t)
	path := filepath.Join(root, cfg.APIDir, "operations_gen.go")
	src := read(t, path)
	src = strings.Replace(src, "func (s *Service) applySessionOperation3(", "func (s *Service) removedSessionOperation3(", 1)
	write(t, path, src)
	report := run(t, cfg)
	hasCheck(t, report, "auth/inject/Operation3", verify.Fail)
}

func TestRunDetectsMissingRegistration(t *testing.T) {
	cfg, root := generate(t)
	path := filepath.Join(root, cfg.APIDir, "operations_gen.go")
	src := read(t, path)
	src = strings.Replace(src, `OperationID: "operation4"`, `OperationID: "renamed4"`, 1)
	write(t, path, src)
	report := run(t, cfg)
	hasCheck(t, report, "api/register/Operation4", verify.Fail)
}

func TestRunDetectsMissingDTO(t *testing.T) {
	cfg, root := generate(t)
	path := filepath.Join(root, cfg.APIDir, "operations_gen.go")
	src := read(t, path)
	src = strings.Replace(src, "type BaseContextDTO struct {", "type renamedContextDTO struct {", 1)
	write(t, path, src)
	report := run(t, cfg)
	hasCheck(t, report, "api/dtos", verify.Fail)
}

func TestRunDetectsDuplicateClientMethod(t *testing.T) {
	cfg, root := generate(t)
	path := filepath.Join(root, cfg.ClientDir, "client.go")
	src := read(t, path)
	// Duplicate Operation2: the F0 binding-dedup regression.
	idx := strings.Index(src, "func (c *Client) Operation2(")
	if idx < 0 {
		t.Fatal("Operation2 method not found")
	}
	end := strings.Index(src[idx:], "\n}\n")
	src = src + "\n" + src[idx:idx+end+3]
	write(t, path, src)
	report := run(t, cfg)
	hasCheck(t, report, "client/Operation2", verify.Fail)
}

func TestRunSkipsUnselectedOperations(t *testing.T) {
	cfg, root := generate(t)
	cfg.Operations.Include = []string{"Operation2"}
	report, err := verify.Run(verify.Options{Config: cfg, Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed() {
		t.Fatalf("report failed:\n%s", render(t, report))
	}
	for _, c := range report.Checks {
		if strings.HasSuffix(c.Name, "Operation1") {
			t.Errorf("unselected operation was checked: %+v", c)
		}
	}
}

func render(t *testing.T, report *verify.Report) string {
	t.Helper()
	var buf bytes.Buffer
	if err := report.WriteText(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
