package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// binPath is the leasegrid binary that TestMain builds once for smoke tests.
var binPath string

// TestMain runs once per package, before any test. It builds the real binary
// so smoke tests exercise exactly what users run, without recompiling per case.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "leasegrid-test")
	if err != nil {
		log.Fatalf("create temp dir: %v", err)
	}
	binPath = filepath.Join(dir, "leasegrid")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", binPath, ".").CombinedOutput(); err != nil {
		log.Fatalf("build leasegrid: %v\n%s", err, out)
	}

	code := m.Run()
	_ = os.RemoveAll(dir) // best-effort cleanup of the temp binary
	os.Exit(code)
}

// TestRun calls run in-process: fast, counted in coverage, no subprocess.
func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantErr    error  // nil means success
		wantStdout string // exact match
		wantStderr string // substring; empty means stderr must be empty
	}{
		{name: "version flag", args: []string{"-version"}, wantStdout: "leasegrid 0.1.0\n"},
		{name: "double-dash version flag", args: []string{"--version"}, wantStdout: "leasegrid 0.1.0\n"},
		{name: "help flag", args: []string{"--help"}, wantStderr: "LeaseGrid manages compute leases"},
		{name: "unknown flag", args: []string{"--bogus"}, wantErr: errUsage, wantStderr: "flag provided but not defined: -bogus"},
		{name: "no arguments", args: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(t.Context(), tt.args, &stdout, &stderr)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("run() error = %v, want %v", err, tt.wantErr)
			}
			if got := stdout.String(); got != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", got, tt.wantStdout)
			}
			if tt.wantStderr == "" {
				if stderr.Len() != 0 {
					t.Errorf("stderr = %q, want empty", stderr.String())
				}
			} else if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

// TestExitCode checks the single place where errors become exit statuses.
func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "success", err: nil, want: 0},
		{name: "usage error", err: fmt.Errorf("%w: bad flag", errUsage), want: 2},
		{name: "other error", err: errors.New("provider unavailable"), want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exitCode(tt.err); got != tt.want {
				t.Errorf("exitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

// TestBinarySmoke runs the built binary as a separate process, checking real
// exit codes and output: the CLI-boundary check that in-process tests can't do.
func TestBinarySmoke(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string // substring of combined stdout+stderr
	}{
		{name: "version", args: []string{"--version"}, wantCode: 0, wantOut: "leasegrid 0.1.0"},
		{name: "help", args: []string{"--help"}, wantCode: 0, wantOut: "LeaseGrid manages compute leases"},
		{name: "unknown flag", args: []string{"--bogus"}, wantCode: 2, wantOut: "flag provided but not defined"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := exec.Command(binPath, tt.args...).CombinedOutput()

			code := 0
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				code = exitErr.ExitCode()
			} else if err != nil {
				t.Fatalf("start binary: %v", err)
			}

			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d\noutput: %s", code, tt.wantCode, out)
			}
			if !strings.Contains(string(out), tt.wantOut) {
				t.Errorf("output does not contain %q:\n%s", tt.wantOut, out)
			}
		})
	}
}
