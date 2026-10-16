// Package config loads soapgen.yaml, the single place where per-service
// semantics live. Everything else in the generator is mechanical.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// SoapGoModule is the soap-go module path used by generated code. The enuan
// stack is hard-wired to the enuan fork; there is no soapgen.yaml knob.
const SoapGoModule = "github.com/enuan/soap-go"

// Config is the soapgen.yaml document.
type Config struct {
	Service        string     `yaml:"service"`
	WSDL           string     `yaml:"wsdl"`
	Module         string     `yaml:"module"`
	BasePath       string     `yaml:"base_path"`
	SoapBin        string     `yaml:"soap_bin"`
	ClientDir      string     `yaml:"client_dir"`
	APIDir         string     `yaml:"api_dir"`
	CmdDir         string     `yaml:"cmd_dir"`
	Gateway        string     `yaml:"gateway"`
	Title          string     `yaml:"title"`
	ConfigPath     string     `yaml:"config_path"`
	TimeoutSeconds int        `yaml:"timeout_seconds"`
	DefaultRetries int        `yaml:"default_retries"`
	Operations     Operations `yaml:"operations"`
	Auth           Auth       `yaml:"auth"`
	ErrorMap       ErrorMap   `yaml:"error_map"`
	Naming         Naming     `yaml:"naming"`

	// Dir is the directory containing soapgen.yaml (not serialized).
	Dir string `yaml:"-"`
}

// Operations selects and routes the exposed SOAP operations.
type Operations struct {
	Include []string            `yaml:"include"`
	Exclude []string            `yaml:"exclude"`
	HTTP    map[string]HTTPRule `yaml:"http"`
}

// HTTPRule maps one operation to a REST method/path. The "default" key applies
// to operations without a specific rule; {operation} in the path is replaced
// by the SOAP operation name.
type HTTPRule struct {
	Method string `yaml:"method"`
	Path   string `yaml:"path"`
}

// Auth describes how the session token is obtained and injected.
type Auth struct {
	Strategy string      `yaml:"strategy"` // none | login | header
	Login    LoginConfig `yaml:"login"`
	Header   string      `yaml:"header"` // HTTP header injected by strategy=header
}

// LoginConfig configures the login operation used by auth.strategy=login.
// All field names are dotted Go paths relative to the request/response DTO of
// the login operation (e.g. "Request.CompanyLogin", "LoginResult.SessionId").
type LoginConfig struct {
	Operation     string   `yaml:"operation"`      // login SOAP operation
	TokenField    string   `yaml:"token_field"`    // token path in the response DTO
	UsernameField string   `yaml:"username_field"` // optional credentials paths
	PasswordField string   `yaml:"password_field"`
	Inject        string   `yaml:"inject"`      // token path in every other request DTO
	SkipInject    []string `yaml:"skip_inject"` // operations that must not receive the token
}

// ErrorMap describes application-level error state inside HTTP 200 responses.
type ErrorMap struct {
	State StateConfig `yaml:"state"`
}

// StateConfig configures the success/error fields of the application state.
// The fields are looked up by Go name on every DTO reachable from a response;
// a single configuration therefore covers differently nested result types.
type StateConfig struct {
	SuccessField string `yaml:"success_field"` // bool (or *bool)
	CodeField    string `yaml:"code_field"`    // optional
	MessageField string `yaml:"message_field"` // optional
	ToHTTP       *bool  `yaml:"to_http"`       // default true when success_field is set
}

// Enabled reports whether the state mapping must be generated and applied.
func (s StateConfig) Enabled() bool {
	if s.SuccessField == "" {
		return false
	}
	return s.ToHTTP == nil || *s.ToHTTP
}

// Naming controls the generated JSON field names.
type Naming struct {
	JSON string `yaml:"json"` // camel (default) | preserve
}

var serviceNameRe = regexp.MustCompile(`[^a-z0-9_]+`)

// Load reads and validates a soapgen.yaml file, applying defaults.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.Dir = filepath.Dir(path)
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.BasePath == "" {
		c.BasePath = "/api"
	}
	if !strings.HasPrefix(c.BasePath, "/") {
		c.BasePath = "/" + c.BasePath
	}
	c.BasePath = strings.TrimSuffix(c.BasePath, "/")
	if c.Gateway == "" {
		c.Gateway = c.installName() + "-gateway"
	}
	if c.Title == "" {
		c.Title = c.installName() + " SOAP gateway"
	}
	if c.ConfigPath == "" {
		c.ConfigPath = c.installName()
	}
	if c.ClientDir == "" {
		c.ClientDir = filepath.Join("internal", "soap", c.PackageName())
	}
	if c.APIDir == "" {
		c.APIDir = filepath.Join("internal", "api")
	}
	if c.CmdDir == "" {
		c.CmdDir = filepath.Join("cmd", c.gatewayName())
	}
	if c.TimeoutSeconds == 0 {
		c.TimeoutSeconds = 30
	}
	if c.DefaultRetries == 0 {
		c.DefaultRetries = 1 // non-idempotent SOAP calls: no retry by default
	}
	if c.Naming.JSON == "" {
		c.Naming.JSON = "camel"
	}
	if c.Auth.Strategy == "" {
		c.Auth.Strategy = "none"
	}
	if c.Operations.HTTP == nil {
		c.Operations.HTTP = map[string]HTTPRule{}
	}
	if _, ok := c.Operations.HTTP["default"]; !ok {
		c.Operations.HTTP["default"] = HTTPRule{Method: "POST", Path: "/{operation}"}
	}
}

func (c *Config) installName() string {
	return sanitizeName(c.Service)
}

// gatewayName is the command directory name (and therefore the binary name):
// the configured gateway, with only path-hostile characters replaced. Hyphens
// are preserved, so "uvetgbt-soap-gateway" really produces
// cmd/uvetgbt-soap-gateway and a binary with that name.
func (c *Config) gatewayName() string {
	return sanitizePathSegment(c.Gateway)
}

// PackageName is the Go package name of the generated SOAP client.
func (c *Config) PackageName() string {
	return sanitizeName(c.Service)
}

// WSDLPath resolves the WSDL path relative to the config file.
func (c *Config) WSDLPath() string {
	if filepath.IsAbs(c.WSDL) {
		return c.WSDL
	}
	return filepath.Join(c.Dir, c.WSDL)
}

// RuleFor returns the HTTP rule for an operation.
func (c *Config) RuleFor(operation string) HTTPRule {
	if r, ok := c.Operations.HTTP[operation]; ok {
		return r.withDefaults()
	}
	return c.Operations.HTTP["default"].withDefaults()
}

func (r HTTPRule) withDefaults() HTTPRule {
	if r.Method == "" {
		r.Method = "POST"
	}
	if r.Path == "" {
		r.Path = "/{operation}"
	}
	return r
}

// PathFor returns the full REST path of an operation; {operation} is replaced
// by the camelCase operation name (e.g. /api/echo).
func (c *Config) PathFor(operation string) string {
	return c.BasePath + strings.ReplaceAll(c.RuleFor(operation).Path, "{operation}", lowerFirst(operation))
}

// lowerFirst lowercases the first rune of s.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'A' && r[0] <= 'Z' {
		r[0] += 'a' - 'A'
	}
	return string(r)
}

// Included reports whether an operation is selected for generation.
func (c *Config) Included(operation string) bool {
	for _, e := range c.Operations.Exclude {
		if e == operation {
			return false
		}
	}
	if len(c.Operations.Include) == 0 {
		return true
	}
	for _, i := range c.Operations.Include {
		if i == operation {
			return true
		}
	}
	return false
}

// Validate checks required fields and enumerated values.
func (c *Config) Validate() error {
	if c.Service == "" {
		return fmt.Errorf("service is required")
	}
	if c.WSDL == "" {
		return fmt.Errorf("wsdl is required")
	}
	if c.Module == "" {
		return fmt.Errorf("module is required")
	}
	switch c.Naming.JSON {
	case "camel", "preserve":
	default:
		return fmt.Errorf("naming.json %q is not supported (camel|preserve)", c.Naming.JSON)
	}
	switch c.Auth.Strategy {
	case "none", "login", "header":
	default:
		return fmt.Errorf("auth.strategy %q is not supported (none|login|header)", c.Auth.Strategy)
	}
	if c.Auth.Strategy == "login" {
		if c.Auth.Login.Operation == "" {
			return fmt.Errorf("auth.login.operation is required when auth.strategy=login")
		}
		if c.Auth.Login.TokenField == "" {
			return fmt.Errorf("auth.login.token_field is required when auth.strategy=login")
		}
		if (c.Auth.Login.UsernameField == "") != (c.Auth.Login.PasswordField == "") {
			return fmt.Errorf("auth.login.username_field and password_field must be set together")
		}
	}
	if c.Auth.Strategy == "header" && c.Auth.Header == "" {
		return fmt.Errorf("auth.header is required when auth.strategy=header")
	}
	if c.ErrorMap.State.SuccessField == "" && (c.ErrorMap.State.CodeField != "" || c.ErrorMap.State.MessageField != "") {
		return fmt.Errorf("error_map.state.success_field is required when code_field or message_field are set")
	}
	if _, err := os.Stat(c.WSDLPath()); err != nil {
		return fmt.Errorf("wsdl %s: %w", c.WSDL, err)
	}
	return nil
}

// sanitizePathSegment keeps a configured name usable as a directory and
// binary name: characters that are unsafe in a path are replaced with '-' and
// leading/trailing separators are trimmed. Hyphens survive on purpose.
func sanitizePathSegment(name string) string {
	s := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			return r
		}
		return '-'
	}, strings.TrimSpace(name))
	s = strings.Trim(s, "-._")
	if s == "" {
		return "gateway"
	}
	return s
}

func sanitizeName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	s = serviceNameRe.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if s == "" {
		s = "soap"
	}
	if s[0] >= '0' && s[0] <= '9' {
		s = "s_" + s
	}
	return s
}
