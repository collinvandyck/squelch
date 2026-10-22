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

// TestRunShowsStderrPromptWithContext checks that a prompt on stderr shows up along with the stderr before it, including
// lines that had scrolled out of the live tail.
func TestRunShowsStderrPromptWithContext(t *testing.T) {
	term := startInPty(t, `for i in 1 2 3 4 5 6; do echo "plan $i" >&2; done; printf 'Proceed? ' >&2; read x; echo "answer: $x"`)
	term.waitFor(t, "Proceed?")
	term.waitFor(t, "plan 1")
	term.write(t, "y\n")
	term.waitFor(t, "answer: y")
	term.wait(t)
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
	p.mu.Lock()
	defer p.mu.Unlock()
	return ansi.Strip(p.out.String())
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
