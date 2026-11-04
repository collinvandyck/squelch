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
	dir := setUpZsh(t)
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
			got := runZsh(t, dir, `
alias fakecmd='echo stale alias'
eval "$(squelch init zsh --cmd fakecmd)"
`+tc.noSquelch+` fakecmd`)
			if got != tc.want {
				t.Errorf("output = %q, want %q", got, tc.want)
			}
		})
	}
}

// setUpZsh returns a directory holding squelch and fakecmd, a command that writes "out" to stdout and "err" to stderr.
func setUpZsh(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh not installed")
	}
	dir := t.TempDir()
	if err := os.Symlink(squelchBin, filepath.Join(dir, "squelch")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fakecmd"), []byte("#!/bin/sh\necho out\necho err >&2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// runZsh runs script in a bare zsh with completion loaded and dir first on PATH, and returns its combined output.
func runZsh(t *testing.T, dir, script string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "zsh", "-fc", "autoload -Uz compinit && compinit -u -D\n"+script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+dir, "PATH="+dir+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("zsh failed: %v\n%s", err, out)
	}
	return string(out)
}
