// Package cli wires the subcommands together. All state a command touches
// (clock, prober, output streams) is injected through App so the full CLI
// can be exercised in-process by the tests.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/JaydenCJ/okpage/internal/config"
	"github.com/JaydenCJ/okpage/internal/probe"
	"github.com/JaydenCJ/okpage/internal/version"
)

// Exit codes, documented in the README and asserted by scripts/smoke.sh.
const (
	ExitOK      = 0
	ExitDown    = 1 // `check`: at least one service failed its probe
	ExitUsage   = 2
	ExitRuntime = 3
)

const usage = `okpage %s — probes your services and renders a static status page

Usage:
  okpage init [dir]        scaffold okpage.toml and an incidents/ directory
  okpage check [flags]     probe every service, record results, exit 1 on failures
  okpage build [flags]     render the static site from history + incidents
  okpage version           print the version

Flags (check and build):
  -c, --config path        config file (default "okpage.toml")

Flags (check only):
  --build                  also render the site after probing
  --quiet                  suppress per-service output

Exit codes: 0 ok, 1 service down, 2 usage error, 3 runtime error.
`

// App carries the injectable dependencies of the CLI.
type App struct {
	Stdout io.Writer
	Stderr io.Writer
	Prober *probe.Prober
	Now    func() time.Time
}

// Run executes the CLI with real dependencies. This is the entry point used
// by cmd/okpage.
func Run(args []string, stdout, stderr io.Writer) int {
	app := &App{Stdout: stdout, Stderr: stderr, Prober: probe.New(), Now: time.Now}
	return app.Run(context.Background(), args)
}

// Run dispatches to the requested subcommand and maps errors to exit codes.
func (a *App) Run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(a.Stderr, usage, version.Version)
		return ExitUsage
	}

	switch args[0] {
	case "version", "--version", "-v":
		fmt.Fprintf(a.Stdout, "okpage %s\n", version.Version)
		return ExitOK
	case "help", "--help", "-h":
		fmt.Fprintf(a.Stdout, usage, version.Version)
		return ExitOK
	case "init":
		return a.exit(a.cmdInit(args[1:]))
	case "check":
		return a.exit(a.cmdCheck(ctx, args[1:]))
	case "build":
		return a.exit(a.cmdBuild(args[1:]))
	default:
		fmt.Fprintf(a.Stderr, "okpage: unknown command %q\n\n", args[0])
		fmt.Fprintf(a.Stderr, usage, version.Version)
		return ExitUsage
	}
}

// cliError pairs an error with the exit code it should produce.
type cliError struct {
	code int
	err  error
}

func (e *cliError) Error() string { return e.err.Error() }

func usageErr(format string, args ...any) error {
	return &cliError{code: ExitUsage, err: fmt.Errorf(format, args...)}
}

func downErr(format string, args ...any) error {
	return &cliError{code: ExitDown, err: fmt.Errorf(format, args...)}
}

// exit prints err (if any) and converts it to a process exit code.
// Undecorated errors are runtime errors.
func (a *App) exit(err error) int {
	if err == nil {
		return ExitOK
	}
	fmt.Fprintf(a.Stderr, "okpage: %v\n", err)
	if ce, ok := err.(*cliError); ok {
		return ce.code
	}
	return ExitRuntime
}

// newFlagSet builds a silent FlagSet; parse errors surface as usage errors.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// configFlag registers -c/--config on fs.
func configFlag(fs *flag.FlagSet) *string {
	path := fs.String("config", "okpage.toml", "")
	fs.StringVar(path, "c", "okpage.toml", "")
	return path
}

// loadConfig loads the config file and resolves the paths inside it
// relative to the config file's own directory, so `okpage check -c
// /srv/status/okpage.toml` works identically from cron and from a shell.
func loadConfig(path string) (*config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	base := filepath.Dir(path)
	cfg.Output = resolve(base, cfg.Output)
	cfg.History = resolve(base, cfg.History)
	cfg.Incidents = resolve(base, cfg.Incidents)
	return cfg, nil
}

func resolve(base, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(base, path)
}
