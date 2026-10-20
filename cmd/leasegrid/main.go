// Command leasegrid manages compute leases across DePIN providers.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/namangourh/leasegrid/internal/app"
	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/output"
)

// version is a var, not a const, so release builds can override it with
// go build -ldflags "-X main.version=v1.0.0".
var version = "0.1.0"

// usage is the top-level help text shown above the flag list.
const usage = `LeaseGrid manages compute leases across DePIN providers.

Usage:
  leasegrid [flags] <command> [command flags]

Commands:
  list    List leases from the configured providers

Flags:
`

// listUsage is the help text for the list command.
const listUsage = `List leases from the configured providers.

Usage:
  leasegrid list [flags]

Flags:
`

// errUsage marks errors caused by invalid command-line input. The details
// have already been printed to stderr, so main only sets the exit code.
var errUsage = errors.New("invalid usage")

// deps is everything run needs from outside: where to write output and how
// to build the application service. main supplies real values; tests
// supply buffers and fake providers.
type deps struct {
	stdout     io.Writer
	stderr     io.Writer
	newService func() (*app.Service, error) // called only by commands that need providers
}

func main() {
	d := deps{
		stdout: os.Stdout,
		stderr: os.Stderr,
		// No providers yet. Config loading replaces this in step 8 (D-030).
		newService: func() (*app.Service, error) { return app.New(), nil },
	}
	err := run(context.Background(), os.Args[1:], d)
	if err != nil && !errors.Is(err, errUsage) {
		// Last-resort report: if stderr itself fails, nothing is left to do.
		_, _ = fmt.Fprintf(os.Stderr, "leasegrid: %v\n", err)
	}
	os.Exit(exitCode(err))
}

// exitCode maps an error returned by run to a process exit status.
func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errUsage):
		return 2
	default:
		return 1
	}
}

// run executes the CLI. It never calls os.Exit, so tests can call it
// directly with their own arguments, buffers and fake providers.
func run(ctx context.Context, args []string, d deps) error {
	fs := flag.NewFlagSet("leasegrid", flag.ContinueOnError)
	fs.SetOutput(d.stderr)
	showVersion := fs.Bool("version", false, "print the LeaseGrid version")
	fs.Usage = func() {
		// Nothing useful can be done if writing help text fails.
		_, _ = fmt.Fprint(fs.Output(), usage)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil // -h or --help: usage was printed, which is success.
		}
		return fmt.Errorf("%w: %w", errUsage, err)
	}

	if *showVersion {
		_, err := fmt.Fprintf(d.stdout, "leasegrid %s\n", version)
		return err
	}

	// flag stops parsing at the first non-flag argument: the command name.
	if fs.NArg() == 0 {
		fs.Usage()
		return nil
	}

	switch cmd, rest := fs.Arg(0), fs.Args()[1:]; cmd {
	case "list":
		return runList(ctx, rest, d)
	default:
		_, _ = fmt.Fprintf(d.stderr, "unknown command %q\n\n", cmd)
		fs.Usage()
		return errUsage
	}
}

// runList implements "leasegrid list": it parses list's flags, asks the
// application service for leases, and writes them in the chosen format.
func runList(ctx context.Context, args []string, d deps) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(d.stderr)

	format := output.FormatJSON
	fs.Func("format", "output format: json (default json)", func(s string) error {
		f, err := output.ParseFormat(s)
		if err != nil {
			return err
		}
		format = f
		return nil
	})
	provider := fs.String("provider", "", "list leases from this provider only")

	fs.Usage = func() {
		// Nothing useful can be done if writing help text fails.
		_, _ = fmt.Fprint(fs.Output(), listUsage)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("%w: %w", errUsage, err)
	}

	svc, err := d.newService()
	if err != nil {
		return err
	}
	leases, err := svc.ListLeases(ctx, lease.Criteria{Provider: *provider})
	if err != nil {
		return err
	}
	return output.Write(d.stdout, format, leases)
}
