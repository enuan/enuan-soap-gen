// Command enuan-soap-gen is the build-time generator that turns a SOAP/WSDL
// service into a REST/JSON gateway in Go.
//
// The F1 milestone exposes the diagnostic and preparation commands; code
// generation (clientgen/model/emit) lands in later milestones.
package main

import (
	"encoding/xml"
	"fmt"
	"os"

	"github.com/enuan/enuan-soap-gen/internal/anonymize"
	"github.com/enuan/enuan-soap-gen/internal/diagnose"
	"github.com/enuan/enuan-soap-gen/internal/ingest"
	"github.com/enuan/soap-go/wsdl"
	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "enuan-soap-gen",
		Short:         "Generate REST/JSON gateways from SOAP WSDLs",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newDiagnoseCommand(), newNormalizeCommand(), newAnonymizeCommand(), newVersionCommand())
	return root
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "enuan-soap-gen "+version)
			return nil
		},
	}
}

func newDiagnoseCommand() *cobra.Command {
	var (
		input  string
		asJSON bool
		failOn string
	)
	cmd := &cobra.Command{
		Use:   "diagnose",
		Short: "Check a WSDL for unsupported or historically problematic constructs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			src, err := ingest.Load(input)
			if err != nil {
				return err
			}
			defer src.Close()

			defs, err := src.Parse()
			if err != nil {
				return err
			}
			report := diagnose.Check(defs, src.Raw)
			report.Ref = src.Ref
			report.SHA256 = src.SHA256
			if asJSON {
				if err := report.WriteJSON(cmd.OutOrStdout()); err != nil {
					return err
				}
			} else if err := report.WriteText(cmd.OutOrStdout()); err != nil {
				return err
			}
			switch failOn {
			case "never":
				return nil
			case "warning":
				for _, f := range report.Findings {
					if f.Severity == diagnose.Warning || f.Severity == diagnose.Error {
						return fmt.Errorf("diagnose: findings at or above %s severity", failOn)
					}
				}
			default: // error
				if report.HasErrors() {
					return fmt.Errorf("diagnose: unsupported constructs found")
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&input, "input", "i", "", "WSDL file path or URL (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the report as JSON")
	cmd.Flags().StringVar(&failOn, "fail-on", "error", "exit non-zero on findings: error|warning|never")
	_ = cmd.MarkFlagRequired("input")
	return cmd
}

func newNormalizeCommand() *cobra.Command {
	var (
		input     string
		output    string
		noRemove  bool
		noDedupe  bool
		noFlatten bool
		showStats bool
	)
	cmd := &cobra.Command{
		Use:   "normalize",
		Short: "Fallback transformations: remove MS serialization schema, dedupe bindings, flatten inheritance",
		Long: "Fallback WSDL transformations.\n\n" +
			"These are NOT needed for generation with enuan/soap-go >= v0.29.2-enuan.1;\n" +
			"use them only as a temporary mitigation for a WSDL that hits an unfixed\n" +
			"codegen bug.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			src, err := ingest.Load(input)
			if err != nil {
				return err
			}
			defer src.Close()

			opts := diagnose.NormalizeOptions{
				RemoveSerializationSchema: !noRemove,
				DedupeBindings:            !noDedupe,
				FlattenInheritance:        !noFlatten,
			}
			out, stats, err := diagnose.Normalize(src.Raw, opts)
			if err != nil {
				return err
			}
			// Verify that the transformed document still parses before writing.
			var check wsdl.Definitions
			if err := xml.Unmarshal(out, &check); err != nil {
				return fmt.Errorf("normalized document does not parse: %w", err)
			}
			if err := os.WriteFile(output, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s (removed schemas: %d, bindings: %d, ports: %d, flattened types: %d, unflattened: %d)\n",
				output, len(stats.RemovedSchemas), len(stats.RemovedBindings), len(stats.RemovedPorts),
				len(stats.FlattenedTypes), len(stats.UnflattenedTypes))
			if showStats {
				for _, s := range stats.RemovedSchemas {
					fmt.Fprintf(cmd.OutOrStdout(), "removed schema %s\n", s)
				}
				for _, b := range stats.RemovedBindings {
					fmt.Fprintf(cmd.OutOrStdout(), "removed binding %s\n", b)
				}
				for _, p := range stats.RemovedPorts {
					fmt.Fprintf(cmd.OutOrStdout(), "removed port %s\n", p)
				}
				for _, t := range stats.FlattenedTypes {
					fmt.Fprintf(cmd.OutOrStdout(), "flattened type %s\n", t)
				}
				for _, t := range stats.UnflattenedTypes {
					fmt.Fprintf(cmd.OutOrStdout(), "unflattened type %s\n", t)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&input, "input", "i", "", "WSDL file path or URL (required)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "output WSDL path (required)")
	cmd.Flags().BoolVar(&noRemove, "no-remove-ms-schema", false, "keep the Microsoft serialization schema")
	cmd.Flags().BoolVar(&noDedupe, "no-dedupe-bindings", false, "keep duplicate SOAP 1.1 bindings")
	cmd.Flags().BoolVar(&noFlatten, "no-flatten-inheritance", false, "keep complexContent/extension inheritance")
	cmd.Flags().BoolVar(&showStats, "stats", false, "print every modification")
	_ = cmd.MarkFlagRequired("input")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}

func newAnonymizeCommand() *cobra.Command {
	var (
		input      string
		output     string
		denyWords  []string
		allowEmpty bool
		verbose    bool
	)
	cmd := &cobra.Command{
		Use:   "anonymize",
		Short: "Produce a public corpus copy of a WSDL (renamed symbols, scrubbed endpoints)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if len(denyWords) == 0 && !allowEmpty {
				return anonymize.ErrNoDenyWords
			}
			src, err := ingest.Load(input)
			if err != nil {
				return err
			}
			defer src.Close()

			res, err := anonymize.Anonymize(src.Raw, anonymize.Options{DenyWords: denyWords})
			if err != nil {
				return err
			}
			if err := os.WriteFile(output, res.XML, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s (%d bytes)\n", output, len(res.XML))
			if verbose {
				for _, line := range res.Mapping.Renames() {
					fmt.Fprintln(cmd.OutOrStdout(), line)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&input, "input", "i", "", "WSDL file path or URL (required)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "output WSDL path (required)")
	cmd.Flags().StringSliceVar(&denyWords, "deny", nil, "extra sensitive tokens to redact (repeatable)")
	cmd.Flags().BoolVar(&allowEmpty, "allow-no-deny", false, "skip the leak scan (not recommended)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print the rename mapping")
	_ = cmd.MarkFlagRequired("input")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}
