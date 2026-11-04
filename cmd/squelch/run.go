package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/creack/pty"
	"github.com/spf13/cobra"
)

// drainTimeout is how long to keep reading the command's output after it exits.
const drainTimeout = 250 * time.Millisecond

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
	switch {
	case term.IsTerminal(os.Stdout.Fd()):
		return runWithView(args, os.Stdout)
	case term.IsTerminal(os.Stderr.Fd()):
		return runWithView(args, os.Stderr)
	default:
		return runPlain(args)
	}
}

// runWithView runs the command with the live view drawn on tty. When tty is our stdout, the command's stdout goes
// through a pty, so that the command sees a terminal, and we pass its bytes on untouched. Otherwise the command gets our
// stdout directly.
func runWithView(args []string, tty *os.File) error {
	width, _, _ := term.GetSize(tty.Fd())
	s := newScreen(tty, width)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	var ptmx, ptySlave *os.File
	if tty == os.Stdout {
		var err error
		if ptmx, ptySlave, err = openPty(tty); err != nil {
			return err
		}
		defer ptmx.Close()
		cmd.Stdout = ptySlave
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		return err
	}
	defer stderrR.Close()
	cmd.Stderr = stderrW
	err = cmd.Start()
	// The command has its own copies now. Ours would keep the readers from seeing EOF once it exits.
	_ = stderrW.Close()
	if ptySlave != nil {
		_ = ptySlave.Close()
	}
	if err != nil {
		return err
	}
	var readers sync.WaitGroup
	readers.Go(func() { copyTo(s.writeStderr, stderrR) })
	if ptmx != nil {
		readers.Go(func() { copyTo(s.writeStdout, ptmx) })
	}
	stopSignals := catchSignals(func(sig os.Signal) { _ = cmd.Process.Signal(sig) })
	defer stopSignals()
	stopResize := followResize(tty, ptmx, s)
	defer stopResize()
	cmdErr := cmd.Wait()
	// What the command wrote before it exited is waiting to be read, but something it left running in the background can
	// hold the pty or pipe open indefinitely, so don't wait long for EOF.
	deadline := time.Now().Add(drainTimeout)
	_ = stderrR.SetReadDeadline(deadline)
	if ptmx != nil {
		_ = ptmx.SetReadDeadline(deadline)
	}
	readers.Wait()
	if !s.close() && cmdErr != nil {
		// Start the stderr dump on a line of its own.
		_, _ = tty.WriteString("\r\n")
	}
	return finish(cmdErr, s.stderr)
}

// openPty opens a pty the size of tty for the command's stdout. Its output processing is off, so the command's bytes
// reach the terminal exactly as written.
func openPty(tty *os.File) (ptmx, slave *os.File, err error) {
	m, slave, err := pty.Open()
	if err != nil {
		return nil, nil, err
	}
	ptmx, err = pollable(m)
	if err == nil {
		err = pty.InheritSize(tty, ptmx)
	}
	if err == nil {
		_, err = term.MakeRaw(slave.Fd())
	}
	if err != nil {
		if ptmx != nil {
			_ = ptmx.Close()
		}
		_ = slave.Close()
		return nil, nil, err
	}
	return ptmx, slave, nil
}

// pollable returns a copy of f that supports read deadlines, and closes f. An os.File supports deadlines only if its
// descriptor was non-blocking when it was wrapped, which pty.Open's isn't.
func pollable(f *os.File) (*os.File, error) {
	syscall.ForkLock.RLock()
	fd, err := syscall.Dup(int(f.Fd()))
	if err == nil {
		syscall.CloseOnExec(fd)
	}
	syscall.ForkLock.RUnlock()
	_ = f.Close()
	if err != nil {
		return nil, err
	}
	if err := syscall.SetNonblock(fd, true); err != nil {
		_ = syscall.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), f.Name()), nil
}

// followResize keeps the view's width, and the pty's size when there is one, in step with tty.
func followResize(tty, ptmx *os.File, s *screen) (stop func()) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGWINCH)
	go func() {
		for range sigs {
			if ptmx != nil {
				_ = pty.InheritSize(tty, ptmx)
			}
			if width, _, err := term.GetSize(tty.Fd()); err == nil {
				s.resize(width)
			}
		}
	}()
	return func() {
		signal.Stop(sigs)
		close(sigs)
	}
}

func copyTo(write func([]byte), r io.Reader) {
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			write(buf[:n])
		}
		if err != nil {
			return
		}
	}
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
