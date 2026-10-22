package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [flags] command [args...]",
		Short: "Run a command, showing a dimmed stderr tail and printing stderr only if it fails",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return run(args)
		},
	}
	// Stop parsing flags at the command name so that everything after it goes to the command.
	cmd.Flags().SetInterspersed(false)
	return cmd
}

func run(args []string) error {
	opts := []tea.ProgramOption{
		// The command gets our stdin, so Bubbletea must not read it. Without input, Bubbletea also skips its terminal
		// queries, whose replies would otherwise leak into the shell after we exit (bubbletea#1590).
		tea.WithInput(nil),
		// Without raw mode, Ctrl+C is a real SIGINT, and Bubbletea would quit on it before the command exits.
		tea.WithoutSignalHandler(),
	}
	var directStdout io.Writer
	switch {
	case term.IsTerminal(os.Stdout.Fd()):
	case term.IsTerminal(os.Stderr.Fd()):
		// Drawing on stdout would put frames and terminal queries in the command's output, so draw on stderr and give
		// the command our stdout directly.
		opts = append(opts, tea.WithOutput(os.Stderr))
		directStdout = os.Stdout
	default:
		return runPlain(args)
	}
	c, err := startCommand(args, directStdout)
	if err != nil {
		return err
	}
	defer c.cancel()
	// Forward signals here rather than through Bubbletea, whose event loop blocks while a prompt waits for an answer.
	stop := catchSignals(func(sig os.Signal) { _ = c.cmd.Process.Signal(sig) })
	defer stop()
	m := newModel(c)
	m.stdinIsTerminal = term.IsTerminal(os.Stdin.Fd())
	ret, err := tea.NewProgram(m, opts...).Run()
	if err != nil {
		return err
	}
	m = ret.(model)
	return finish(m.err, m.stderr[m.stderrShown:])
}

// runPlain passes stdin and stdout straight through and holds stderr back until we know whether the command failed.
func runPlain(args []string) error {
	var stderr bytes.Buffer
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	stop := catchSignals(func(sig os.Signal) { _ = cmd.Process.Signal(sig) })
	defer stop()
	return finish(cmd.Wait(), stderr.Bytes())
}

// catchSignals keeps SIGINT and SIGTERM from killing us before the command exits, so that we can still report how it
// exited. Ctrl+C already reaches the command, which shares our process group, so only SIGTERM goes to forward.
func catchSignals(forward func(os.Signal)) (stop func()) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		for sig := range sigs {
			if sig != os.Interrupt {
				forward(sig)
			}
		}
	}()
	return func() {
		signal.Stop(sigs)
		close(sigs)
	}
}

// finish writes stderr and exits the way the command did if it failed.
func finish(cmdErr error, stderr []byte) error {
	if cmdErr == nil {
		return nil
	}
	_, _ = os.Stderr.Write(stderr)
	var exitErr *exec.ExitError
	if !errors.As(cmdErr, &exitErr) {
		return cmdErr
	}
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		dieFromSignal(ws.Signal())
	}
	os.Exit(exitErr.ExitCode())
	return nil
}

// dieFromSignal kills us with sig, so that our caller sees the command's fate rather than an exit code. That matters
// to shells: bash stops a loop on Ctrl+C only if the child died from SIGINT. If sig doesn't kill us, because it's
// ignored or Go doesn't treat it as fatal, exit with the conventional 128+sig instead.
func dieFromSignal(sig syscall.Signal) {
	signal.Reset(sig)
	_ = syscall.Kill(os.Getpid(), sig)
	// The signal can land on another thread, so give it a moment to arrive.
	time.Sleep(100 * time.Millisecond)
	os.Exit(128 + int(sig))
}
