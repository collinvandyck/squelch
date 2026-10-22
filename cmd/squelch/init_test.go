package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestInitZshNoSquelch checks that a wrapped command runs through squelch, which hides stderr on success, unless
// NOSQUELCH is non-empty. It also checks that the wrapper replaces an alias defined before init.
func TestInitZshNoSquelch(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not installed")
	}
	dir := t.TempDir()
	if err := os.Symlink(squelchBin, filepath.Join(dir, "squelch")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fakecmd"), []byte("#!/bin/sh\necho out\necho err >&2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		noSquelch string
		want      string
	}{
		{name: "unset", want: "out\n"},
		{name: "empty", noSquelch: "NOSQUELCH=", want: "out\n"},
		{name: "set", noSquelch: "NOSQUELCH=1", want: "out\nerr\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := `
autoload -Uz compinit && compinit -u -D
alias fakecmd='echo stale alias'
eval "$(squelch init zsh --cmd fakecmd)"
` + tc.noSquelch + ` fakecmd`
			cmd := exec.CommandContext(t.Context(), zsh, "-fc", script)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "HOME="+dir, "PATH="+dir+":"+os.Getenv("PATH"))
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("zsh failed: %v\n%s", err, out)
			}
			if string(out) != tc.want {
				t.Errorf("output = %q, want %q", out, tc.want)
			}
		})
	}
}
