// Command leasegrid manages compute leases across DePIN providers.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/namangourh/leasegrid/internal/app"
	"github.com/namangourh/leasegrid/internal/config"
	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/output"
	"github.com/namangourh/leasegrid/internal/providers/akash"
	"github.com/namangourh/leasegrid/internal/providers/demo"
)

// version is a var, not a const, so release builds can override it with
// go build -ldflags "-X main.version=v1.0.0".
var version = "0.1.0"

// usage is the top-level help text shown above the flag list.
const usage = `LeaseGrid manages compute leases across DePIN providers.

Usage:
  leasegrid [flags] <command> [command flags]

Flags go before the command, for example:
  leasegrid --config my-config.yaml list

Commands:
  list    List leases from the configured providers

Flags:
`

// listUsage is the help text for the list command.
const listUsage = `List leases from the configured providers, soonest expiry first.

Usage:
  leasegrid list [flags]

Examples:
  leasegrid list --expiring-within 24h
  leasegrid list --provider demo-a --status active --format json

Flags:
`

// errUsage marks errors caused by invalid command-line input. The details
// have already been printed to stderr, so main only sets the exit code.
var errUsage = errors.New("invalid usage")

// deps is everything run needs from outside: where to write output, the
// clock, and how to build the application service. main supplies real
// values; tests supply buffers, a fixed clock and fake providers.
type deps struct {
	stdout io.Writer
	stderr io.Writer
	now    func() time.Time

	// newService builds the service from the config file at configPath (""
	// means the default location), with now as the providers' clock. Only
	// commands that need providers call it.
	newService func(configPath string, now func() time.Time) (*app.Service, error)
}

func main() {
	// Ctrl+C cancels ctx, which makes in-flight provider calls return.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, os.Args[1:], deps{stdout: os.Stdout, stderr: os.Stderr, now: time.Now, newService: loadService})
	stop() // os.Exit skips deferred calls, so release the signal handler explicitly

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
	case errors.Is(err, context.Canceled):
		// interrupted, e.g. by Ctrl+C: 128 + SIGINT (2), by convention
		return 130
	default:
		return 1
	}
}

// loadService loads the configuration and builds the service from it.
func loadService(configPath string, now func() time.Time) (*app.Service, error) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, err
	}
	return buildService(cfg, now)
}

// loadConfig applies the file-location rules: an explicit path must exist,
// while a file at the default location may be missing.
func loadConfig(configPath string) (config.Config, error) {
	if configPath != "" {
		return config.Load(configPath)
	}
	path, err := config.DefaultPath()
	if err != nil {
		return config.Config{}, err
	}
	return config.LoadOptional(path)
}

// buildService turns configuration into an application service, giving
// providers now as their clock. With newProvider, it is the only code that
// knows concrete provider implementations: the program's composition root.
func buildService(cfg config.Config, now func() time.Time) (*app.Service, error) {
	var ps []app.Provider
	for _, pc := range cfg.EnabledProviders() {
		p, err := newProvider(pc, now)
		if err != nil {
			return nil, err
		}
		ps = append(ps, p)
	}
	return app.New(ps...), nil
}

// newProvider builds the provider that a config entry names. Building one
// makes no network calls. A setting the provider doesn't use is an error, so
// an owner put on the wrong entry fails loudly instead of being ignored.
func newProvider(pc config.ProviderConfig, now func() time.Time) (app.Provider, error) {
	if pc.Name == akash.Name {
		p, err := akash.New(akash.Config{BaseURL: pc.Endpoint, Owner: pc.Owner, Now: now})
		if err != nil {
			return nil, err
		}
		return p, nil
	}

	p, ok := demo.New(pc.Name, now)
	if !ok {
		return nil, fmt.Errorf("unknown provider %q (available: %s, %s, %s)", pc.Name, akash.Name, demo.NameA, demo.NameB)
	}
	if pc.Owner != "" {
		return nil, fmt.Errorf("provider %q does not take an owner", pc.Name)
	}
	if pc.Endpoint != "" {
		return nil, fmt.Errorf("provider %q does not take an endpoint", pc.Name)
	}
	return p, nil
}

// run executes the CLI. It never calls os.Exit, so tests can call it
// directly with their own arguments, buffers and fake providers.
func run(ctx context.Context, args []string, d deps) error {
	fs := flag.NewFlagSet("leasegrid", flag.ContinueOnError)
	fs.SetOutput(d.stderr)
	showVersion := fs.Bool("version", false, "print the LeaseGrid version")
	configPath := fs.String("config", "", "path to the config file (default: leasegrid/config.yaml in your user config directory)")
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
		return runList(ctx, rest, *configPath, d)
	default:
		_, _ = fmt.Fprintf(d.stderr, "unknown command %q\n\n", cmd)
		fs.Usage()
		return errUsage
	}
}

// runList implements "leasegrid list": it parses list's flags, asks the
// application service (built from the config at configPath) for leases, and
// writes them in the chosen format. Every flag is validated before any
// provider is called.
func runList(ctx context.Context, args []string, configPath string, d deps) error {
	// Read the clock once, so the providers, the --expiring-within cutoff and
	// the table's relative times all agree on what "now" is. UTC to the second
	// keeps times in JSON short and the same in every time zone.
	now := d.now().UTC().Truncate(time.Second)

	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(d.stderr)

	format := output.FormatTable
	fs.Func("format", "output `format`: table, json or yaml (default table)", func(s string) error {
		f, err := output.ParseFormat(s)
		if err != nil {
			return err
		}
		format = f
		return nil
	})
	provider := fs.String("provider", "", "list leases from the provider with this `name` only")
	var status lease.Status
	fs.Func("status", "list only leases with this `status`: active, closed or insufficient_funds", func(s string) error {
		st, err := lease.ParseStatus(s)
		if err != nil {
			return err
		}
		status = st
		return nil
	})
	sortBy := app.DefaultSort
	fs.Func("sort-by", "sort by this `key`: id, provider, status or expires (default expires)", func(s string) error {
		k, err := lease.ParseSortKey(s)
		if err != nil {
			return err
		}
		sortBy = k
		return nil
	})
	var expiringWithin time.Duration
	fs.Func("expiring-within", "list only leases that expire within this `duration`, e.g. 90m or 24h (a week is 168h)", func(s string) error {
		within, err := time.ParseDuration(s)
		if err != nil {
			return errors.New("not a duration such as 90m or 24h")
		}
		if within <= 0 {
			return errors.New("must be greater than zero")
		}
		expiringWithin = within
		return nil
	})

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

	criteria := lease.Criteria{Provider: *provider, Status: status}
	if expiringWithin > 0 {
		criteria.ExpiresBefore = now.Add(expiringWithin)
	}

	svc, err := d.newService(configPath, func() time.Time { return now })
	if err != nil {
		return err
	}
	leases, err := svc.ListLeases(ctx, app.ListOptions{Criteria: criteria, SortBy: sortBy})
	if errors.Is(err, app.ErrUnknownProvider) {
		// A mistyped --provider is a usage error. main doesn't print usage
		// errors, so print this one here.
		_, _ = fmt.Fprintf(d.stderr, "leasegrid: %v\n", err)
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	if err != nil {
		return err
	}

	if len(leases) == 0 && format == output.FormatTable {
		// Keep stdout for data: an empty table is reported on stderr.
		_, err := fmt.Fprintln(d.stderr, "No leases found.")
		return err
	}
	return output.Write(d.stdout, format, now, leases)
}
