package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "version flag",
			args: []string{"-version"},
			want: "leasegrid 0.1.0",
		},
		{ // added
			name: "double-dash version flag",
			args: []string{"--version"},
			want: "leasegrid 0.1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("go", append([]string{"run", "."}, tt.args...)...)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("run command: %v\noutput: %s", err, output)
			}

			got := strings.TrimSpace(string(output))
			if got != tt.want {
				t.Fatalf("version output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHelp(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "-h")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run help command: %v\noutput: %s", err, output)
	}

	if !strings.Contains(string(output), "LeaseGrid manages compute leases") {
		t.Fatalf("help output does not contain product description:\n%s", output)
	}
}
