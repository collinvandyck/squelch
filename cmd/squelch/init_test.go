package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// TestInitZshSkipArg checks that a wrapped command skips squelch when one of its arguments exactly matches a --skip-arg.
func TestInitZshSkipArg(t *testing.T) {
	dir := setUpZsh(t)
	for _, tc := range []struct {
		name string
		args string
		want string
	}{
		{name: "no skip arg", args: "sleuth url", want: "out\n"},
		{name: "first skip arg", args: "sleuth --tui url", want: "out\nerr\n"},
		{name: "second skip arg", args: "--interactive", want: "out\nerr\n"},
		{name: "partial match", args: "--tui=false", want: "out\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runZsh(t, dir, `
eval "$(squelch init zsh --cmd fakecmd --skip-arg --tui --skip-arg --interactive)"
fakecmd `+tc.args)
			if got != tc.want {
				t.Errorf("output = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestInitRejectsUnsafeSkipArg checks that a --skip-arg that would need shell quoting is refused.
func TestInitRejectsUnsafeSkipArg(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"init", "zsh", "--cmd", "ct", "--skip-arg", "--tui]}; rm -rf ~"})
	cmd.SetOut(io.Discard)
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "invalid skip argument") {
		t.Errorf("err = %v, want an invalid skip argument error", err)
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
