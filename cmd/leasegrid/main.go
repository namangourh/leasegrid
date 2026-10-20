// Command leasegrid manages compute leases across DePIN providers.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// version is a var, not a const, so release builds can override it with
// go build -ldflags "-X main.version=v1.0.0".
var version = "0.1.0"

// usage is the help text shown above the flag list.
const usage = `LeaseGrid manages compute leases across DePIN providers.

Usage:
  leasegrid [flags]

`

// errUsage marks errors caused by invalid command-line input. The flag
// package has already printed the details, so main only sets the exit code.
var errUsage = errors.New("invalid usage")

func main() {
	err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)
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
// directly with their own arguments and output buffers.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("leasegrid", flag.ContinueOnError)
	fs.SetOutput(stderr)
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
		_, err := fmt.Fprintf(stdout, "leasegrid %s\n", version)
		return err
	}
	return nil
}
