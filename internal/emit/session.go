package emit

import (
	"fmt"
	"strings"

	"github.com/enuan/enuan-soap-gen/internal/config"
	"github.com/enuan/enuan-soap-gen/internal/model"
)

// sessionView drives the generated auth_gen.go and the token injection in the
// operation handlers (auth.strategy=login).
type sessionView struct {
	Operation         string // generated client method of the login operation
	RequestRoot       string // DTO type of the login request (Operation1Request)
	ResponseRoot      string // DTO type of the login response
	RequestConverter  string
	ResponseConverter string
	Token             *accessor
	Username          *accessor // nil when no credentials are configured
	Password          *accessor
	LoginName         string // SOAP operation name, for docs
	LoginResponseXML  string // canned login response for the generated test
	TokenValue        string // token value used by the tests
}

// buildSession resolves the auth.login configuration against the model.
func buildSession(cfg *config.Config, m *model.Model, ix dtoIndex) (*sessionView, error) {
	if cfg.Auth.Strategy != "login" {
		return nil, nil
	}
	login := cfg.Auth.Login
	var op *model.Operation
	for _, o := range m.Operations {
		if o.Name == login.Operation {
			op = o
			break
		}
	}
	if op == nil {
		names := make([]string, 0, len(m.Operations))
		for _, o := range m.Operations {
			names = append(names, o.Name)
		}
		return nil, fmt.Errorf("auth.login.operation %q is not among the generated operations (%s)", login.Operation, strings.Join(names, ", "))
	}

	s := &sessionView{
		Operation:         op.Method,
		LoginName:         op.Name,
		RequestRoot:       op.Name + "Request",
		ResponseRoot:      op.Name + "Response",
		RequestConverter:  lowerCamel(op.Input.GoType) + "ToSOAP",
		ResponseConverter: lowerCamel(op.Output.GoType) + "FromSOAP",
	}
	var err error
	if s.Token, err = ix.resolvePath(s.ResponseRoot, login.TokenField); err != nil {
		return nil, fmt.Errorf("auth.login.token_field: %w", err)
	}
	if login.UsernameField != "" {
		if s.Username, err = ix.resolvePath(s.RequestRoot, login.UsernameField); err != nil {
			return nil, fmt.Errorf("auth.login.username_field: %w", err)
		}
		if s.Password, err = ix.resolvePath(s.RequestRoot, login.PasswordField); err != nil {
			return nil, fmt.Errorf("auth.login.password_field: %w", err)
		}
	}
	s.TokenValue = testToken
	s.LoginResponseXML = fmt.Sprintf("<%s xmlns=%q>%s</%s>",
		op.Output.Element, m.Namespace, s.Token.XMLSnippet(testToken), op.Output.Element)
	return s, nil
}

// testToken is the token the generated session tests log in with.
const testToken = "sess-1"
