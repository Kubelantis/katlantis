package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/runatlantis/atlantis/server/core/migrate"
	"github.com/runatlantis/atlantis/server/core/typesafe"
)

// MigrateWorkflowsCmd converts custom workflows, workflow hooks and custom
// policy checks into native inputs.
type MigrateWorkflowsCmd struct {
	Stdout, Stderr io.Writer

	reposYAML, atlantisYAML, reportPath string
	write, strict                       bool
	typesafeKey, typesafeURL, model     string
}

// errNeedsAttention makes --strict exit non-zero.
var errNeedsAttention = errors.New("some behaviour was removed or needs review; see the report")

// Init returns the cobra command.
func (m *MigrateWorkflowsCmd) Init() *cobra.Command {
	c := &cobra.Command{
		Use:   "migrate-workflows",
		Short: "Convert custom workflows, hooks and custom policy checks into native inputs",
		Long: `Converts a server-side repos.yaml and/or a repo-level atlantis.yaml.

Built-in steps and their arguments, fixed and templated env values, and known
no-op commands are converted by rules. Other custom commands, multienv steps,
workflow hooks and custom policy checks are removed and listed in the report.
With a TypeSafe API key, Jev labels each removed command (wrapper tool, policy
scanner, cost estimation, ...) so the report says what replaces it; the label
never changes the converted files.

Without --write the migrated files are printed to stdout.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return m.run(cmd.Context())
		},
	}
	f := c.Flags()
	f.StringVar(&m.reposYAML, "repos-yaml", "", "Server-side repo config to migrate.")
	f.StringVar(&m.atlantisYAML, "atlantis-yaml", "", "Repo-level atlantis.yaml to migrate. Server workflows it references are resolved from --repos-yaml.")
	f.BoolVar(&m.write, "write", false, "Overwrite the files in place (the originals are kept with a .orig suffix).")
	f.StringVar(&m.reportPath, "report", "", "Write the Markdown report to this file instead of stderr.")
	f.BoolVar(&m.strict, "strict", false, "Exit non-zero when behaviour was removed or needs review.")
	f.StringVar(&m.typesafeKey, "typesafe-api-key", cmpOr(os.Getenv("ATLANTIS_TYPESAFE_API_KEY"), os.Getenv("TYPESAFE_API_KEY")), "TypeSafe API key for labelling removed commands. Defaults to $ATLANTIS_TYPESAFE_API_KEY, then $TYPESAFE_API_KEY.")
	f.StringVar(&m.typesafeURL, "typesafe-api-url", "https://api.typesafe.ai", "TypeSafe API base URL.")
	f.StringVar(&m.model, "typesafe-model", "jev-1.13.0", "TypeSafe model.")
	return c
}

func cmpOr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func (m *MigrateWorkflowsCmd) run(ctx context.Context) error {
	if m.reposYAML == "" && m.atlantisYAML == "" {
		return errors.New("pass --repos-yaml, --atlantis-yaml, or both")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var results []*migrate.Result
	var server map[string]migrate.Inputs
	if m.reposYAML != "" {
		src, err := os.ReadFile(m.reposYAML)
		if err != nil {
			return err
		}
		r, err := migrate.MigrateServerConfig(m.reposYAML, src)
		if err != nil {
			return err
		}
		server = r.Workflows
		results = append(results, r)
	}
	if m.atlantisYAML != "" {
		src, err := os.ReadFile(m.atlantisYAML)
		if err != nil {
			return err
		}
		r, err := migrate.MigrateRepoConfig(m.atlantisYAML, src, server)
		if err != nil {
			return err
		}
		results = append(results, r)
	}

	var notes []migrate.Note
	for _, r := range results {
		notes = append(notes, r.Notes...)
	}
	if m.typesafeKey != "" {
		lctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		labeler := &migrate.JevLabeler{
			Evaluator:     &typesafe.Client{BaseURL: m.typesafeURL, APIKey: m.typesafeKey, Model: m.model},
			MinConfidence: 0.8,
		}
		if err := labeler.Label(lctx, notes); err != nil {
			fmt.Fprintf(m.Stderr, "warning: %s\n", err)
		}
	}

	for _, r := range results {
		if m.write {
			if err := os.Rename(r.File, r.File+".orig"); err != nil {
				return err
			}
			if err := os.WriteFile(r.File, r.Output, 0o644); err != nil { // #nosec G306 -- config files are not secret
				return err
			}
			fmt.Fprintf(m.Stderr, "migrated %s (original kept as %s.orig)\n", r.File, r.File)
		} else {
			fmt.Fprintf(m.Stdout, "# --- %s (migrated)\n%s", r.File, r.Output)
		}
	}
	report := migrate.Report(notes)
	if m.reportPath != "" {
		if err := os.WriteFile(m.reportPath, []byte(report), 0o644); err != nil { // #nosec G306 -- report is not secret
			return err
		}
	} else {
		fmt.Fprint(m.Stderr, report)
	}
	if m.strict && migrate.NeedsAttention(notes) {
		return errNeedsAttention
	}
	return nil
}
