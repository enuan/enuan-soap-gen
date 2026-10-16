// Package clientgen wraps the enuan/soap-go `soap gen` CLI to produce the
// typed SOAP client used by the generated gateway.
package clientgen

import (
	"context"
	"fmt"
	"os/exec"
	"runtime/debug"
	"strings"

	"github.com/enuan/enuan-soap-gen/internal/config"
)

// Options configures one client generation run.
type Options struct {
	WSDLPath string
	Package  string // Go package name of the generated client
	OutDir   string
	// SoapBin is an explicit path to the `soap` binary. When empty the CLI is
	// run from the Go module cache with
	// `go run github.com/enuan/soap-go/cmd/soap@<generator's soap-go version>`.
	SoapBin string
}

// Run generates the client. It returns the combined CLI output on failure.
func Run(ctx context.Context, opts Options) error {
	name, args := Command(opts)
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, out)
	}
	return nil
}

// Command returns the executable and arguments for the generation run.
func Command(opts Options) (string, []string) {
	genArgs := []string{"gen", "--client", "-p", opts.Package, "-i", opts.WSDLPath, "-d", opts.OutDir}
	if opts.SoapBin != "" {
		return opts.SoapBin, genArgs
	}
	soapGo := config.SoapGoModule
	target := soapGo + "/cmd/soap"
	if v := resolveVersion(); v != "" {
		target += "@" + v
	}
	return "go", append([]string{"run", target}, genArgs...)
}

// resolveVersion picks the soap-go version to run: the version this generator
// was built with, then "latest".
func resolveVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == config.SoapGoModule && dep.Version != "" && dep.Version != "(devel)" {
				return dep.Version
			}
		}
	}
	return "latest"
}
