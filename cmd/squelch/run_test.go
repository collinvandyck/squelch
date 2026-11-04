package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
)

var squelchBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "squelch-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	squelchBin = filepath.Join(dir, "squelch")
	if out, err := exec.Command("go", "build", "-o", squelchBin, ".").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building squelch: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// TestRunShowsStdoutPrompt checks that a prompt on stdout without a trailing newline shows up while the command waits
// for an answer.
func TestRunShowsStdoutPrompt(t *testing.T) {
	term := startInPty(t, `printf 'Continue? [y/n] '; read x; echo "answer: $x"`)
	term.waitFor(t, "Continue? [y/n]")
	term.write(t, "y\n")
	term.waitFor(t, "answer: y")
	term.wait(t)
}

// TestRunShowsStderrPrompt checks that a prompt on stderr without a trailing newline shows up in the live tail while
// the command waits for an answer.
func TestRunShowsStderrPrompt(t *testing.T) {
	term := startInPty(t, `echo "plan 1" >&2; printf 'Proceed? ' >&2; read x; echo "answer: $x"`)
	term.waitFor(t, "Proceed?")
	term.write(t, "y\n")
	term.waitFor(t, "answer: y")
	term.wait(t)
}

// TestRunGivesStdoutATerminal checks that the command's stdout is a terminal, as it would be without squelch, so that
// colors, prompts, and full-screen programs work.
func TestRunGivesStdoutATerminal(t *testing.T) {
	term := startInPty(t, `if [ -t 1 ]; then echo "stdout is a terminal"; else echo "stdout is not a terminal"; fi`)
	term.wait(t)
	term.waitFor(t, "stdout is a terminal")
}

// TestRunKeepsViewOffUnfinishedLine checks that the live view doesn't draw over a line of stdout the command hasn't
// finished, such as a prompt waiting for an answer.
func TestRunKeepsViewOffUnfinishedLine(t *testing.T) {
	term := startInPty(t, `printf 'Name: '; echo noise >&2; read x; echo "hi $x"`)
	term.waitFor(t, "Name: ")
	time.Sleep(300 * time.Millisecond)
	if strings.Contains(term.text(), "noise") {
		t.Fatalf("view drawn over the prompt:\n%q", term.text())
	}
	term.write(t, "bob\n")
	term.waitFor(t, "hi bob")
	term.wait(t)
}

// TestRunKeepsViewOffAltScreen checks that the live view isn't drawn while the command is on the alternate screen.
func TestRunKeepsViewOffAltScreen(t *testing.T) {
	term := startInPty(t, `printf '\033[?1049h'; echo noise >&2; sleep 0.3; printf '\033[?1049l'`)
	term.wait(t)
	out := term.raw()
	leave := strings.Index(out, "\x1b[?1049l")
	if leave < 0 {
		t.Fatalf("command's output didn't reach the terminal:\n%q", out)
	}
	if noise := strings.Index(out, "noise"); noise >= 0 && noise < leave {
		t.Errorf("view drawn on the alternate screen:\n%q", out)
	}
}

// TestRunDumpsStderrOnFailure checks that a failing command's stderr is printed in full and its exit code is passed on.
func TestRunDumpsStderrOnFailure(t *testing.T) {
	term := startInPty(t, `for i in 1 2 3 4 5 6; do echo "oops $i" >&2; done; exit 3`)
	err := term.cmd.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("squelch exited with %v, want exit code 3", err)
	}
	term.waitFor(t, "oops 1")
}

// TestRunDoesNotWaitForBackgroundProcesses checks that squelch exits with the command, even when something the command
// started in the background still holds its stdout and stderr open.
func TestRunDoesNotWaitForBackgroundProcesses(t *testing.T) {
	term := startInPty(t, `sleep 10 & echo started`)
	done := make(chan error, 1)
	go func() { done <- term.cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("squelch failed: %v\noutput:\n%q", err, term.text())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("squelch still running 5s after the command exited")
	}
	term.waitFor(t, "started")
}

// TestRunCtrlCAtPrompt checks that Ctrl+C while a prompt waits ends the command, and squelch dies from SIGINT with it.
func TestRunCtrlCAtPrompt(t *testing.T) {
	term := startInPty(t, `printf 'Continue? [y/n] '; read x`)
	term.waitFor(t, "Continue? [y/n]")
	term.write(t, "\x03")
	err := term.cmd.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("squelch exited with %v, want death by SIGINT", err)
	}
	if ws := exitErr.Sys().(syscall.WaitStatus); !ws.Signaled() || ws.Signal() != syscall.SIGINT {
		t.Errorf("squelch exited with %v, want death by SIGINT", err)
	}
}

// TestRunTermAtPrompt checks that SIGTERM to squelch while a prompt waits reaches the command.
func TestRunTermAtPrompt(t *testing.T) {
	term := startInPty(t, `printf 'Continue? [y/n] '; read x`)
	term.waitFor(t, "Continue? [y/n]")
	if err := term.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- term.cmd.Wait() }()
	select {
	case err := <-done:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("squelch exited with %v, want death by SIGTERM", err)
		}
		if ws := exitErr.Sys().(syscall.WaitStatus); !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
			t.Errorf("squelch exited with %v, want death by SIGTERM", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("squelch still running 5s after SIGTERM")
	}
}

type ptyTerm struct {
	cmd  *exec.Cmd
	ptmx *os.File
	mu   sync.Mutex
	out  bytes.Buffer
	done chan struct{}
}

// startInPty runs script under squelch with a pty for stdin, stdout, and stderr.
func startInPty(t *testing.T, script string) *ptyTerm {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), squelchBin, "run", "sh", "-c", script)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 100})
	if err != nil {
		t.Fatal(err)
	}
	term := &ptyTerm{cmd: cmd, ptmx: ptmx, done: make(chan struct{})}
	go func() {
		defer close(term.done)
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			term.mu.Lock()
			term.out.Write(buf[:n])
			term.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = ptmx.Close()
		<-term.done
	})
	return term
}

func (p *ptyTerm) text() string {
	return ansi.Strip(p.raw())
}

func (p *ptyTerm) raw() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.out.String()
}

func (p *ptyTerm) waitFor(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(p.text(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in output:\n%q", want, p.text())
}

func (p *ptyTerm) write(t *testing.T, s string) {
	t.Helper()
	if _, err := io.WriteString(p.ptmx, s); err != nil {
		t.Fatal(err)
	}
}

// wait waits for squelch to exit successfully.
func (p *ptyTerm) wait(t *testing.T) {
	t.Helper()
	if err := p.cmd.Wait(); err != nil {
		t.Fatalf("squelch failed: %v\noutput:\n%q", err, p.text())
	}
}
