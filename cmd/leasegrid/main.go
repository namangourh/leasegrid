// Command leasegrid manages compute leases across DePIN providers.
package main

import (
	"flag"
	"fmt"
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

func main() {
	fs := flag.NewFlagSet("leasegrid", flag.ExitOnError)
	showVersion := fs.Bool("version", false, "print the LeaseGrid version")

	fs.Usage = func() {
		_, _ = fmt.Fprint(fs.Output(), usage)
		fs.PrintDefaults()
	}

	// With ExitOnError, Parse exits the program itself on a bad flag or -h,
	// so the error it returns is always nil here. Discarding it explicitly
	// keeps the errcheck linter happy.
	_ = fs.Parse(os.Args[1:])

	if *showVersion {
		fmt.Printf("leasegrid %s\n", version)
		return
	}
}
