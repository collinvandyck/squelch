package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	// tea.Println output written before the renderer's first flush lands at the top of the screen, and the first frame
	// then erases everything below it (bubbletea#1740). Frames flush every ~16ms, so wait a few before printing.
	firstPrintDelay = 50 * time.Millisecond
	stderrTailLines = 4
)

type (
	model struct {
		args    []string
		command command
		err     error
		// directStdout, when set, receives the command's stdout instead of it being printed through Bubbletea.
		directStdout io.Writer
		stdout       lineBuffer
		stderr       []byte // everything the command wrote, untouched, for the dump on failure
		stderrLines  lineBuffer
		stderrTail   []string
		links        linkList
		width        int
		exiting      bool
	}
	command struct {
		cmd    *exec.Cmd
		errs   chan error
		stdout chan []byte
		stderr chan []byte
		cancel context.CancelFunc
	}
	tickMsg   time.Time
	signalMsg struct{ sig os.Signal }
)

func newModel(args []string) model {
	return model{
		args: args,
	}
}

func (c command) tryWait() (err error, done bool) {
	select {
	case err, ok := <-c.errs:
		return err, !ok
	default:
		return nil, false
	}
}

func (c command) tryStdout() (res []byte) {
	return c.tryRead(c.stdout)
}

func (c command) tryStderr() (res []byte) {
	return c.tryRead(c.stderr)
}

// drain blocks until the readers close their channels. Call it once the command has exited, when that happens quickly.
func (c command) drain() (stdout, stderr []byte) {
	for bs := range c.stdout {
		stdout = append(stdout, bs...)
	}
	for bs := range c.stderr {
		stderr = append(stderr, bs...)
	}
	return
}

func (c command) tryRead(ch <-chan []byte) (res []byte) {
	for {
		select {
		case bs, ok := <-ch:
			if !ok {
				return res
			}
			res = append(res, bs...)
		default:
			return
		}
	}
}

func (m model) Init() tea.Cmd {
	return m.startCmd
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case command:
		m.command = msg
		return m, m.tickAfter(firstPrintDelay)
	case signalMsg:
		if m.command.cmd != nil && m.command.cmd.Process != nil {
			_ = m.command.cmd.Process.Signal(msg.sig)
		}
	case tickMsg:
		err, done := m.command.tryWait()
		m.err = err
		finished := err != nil || done
		var stdout, stderr []byte
		if finished {
			stdout, stderr = m.command.drain()
		} else {
			stdout, stderr = m.command.tryStdout(), m.command.tryStderr()
		}
		m.stderr = append(m.stderr, stderr...)
		stderrLines := m.stderrLines.write(stderr)
		m.links.add(stderrLines)
		m.addStderrTail(stderrLines)
		var cmds []tea.Cmd
		lines := m.stdout.write(stdout)
		if finished {
			if line, ok := m.stdout.flush(); ok {
				lines = append(lines, line)
			}
		}
		if len(lines) > 0 {
			out := strings.Join(lines, "\n")
			if out == "" {
				// tea.Println drops an empty string, so a lone blank line would vanish.
				out = " "
			}
			cmds = append(cmds, tea.Println(out))
		}
		if finished {
			m.exiting = true
			cmds = append(cmds, tea.Quit)
		} else {
			cmds = append(cmds, m.tick())
		}
		return m, tea.Sequence(cmds...)
	case tea.WindowSizeMsg:
		m.width = msg.Width
	}
	return m, nil
}

// addStderrTail skips blank lines, which would only use up the tail's few lines. m.stderr still has them.
func (m *model) addStderrTail(lines []string) {
	for _, line := range lines {
		if strings.TrimSpace(ansi.Strip(line)) != "" {
			m.stderrTail = append(m.stderrTail, line)
		}
	}
	if over := len(m.stderrTail) - stderrTailLines; over > 0 {
		m.stderrTail = slices.Clone(m.stderrTail[over:])
	}
}

func (m model) tick() tea.Cmd {
	return m.tickAfter(10 * time.Millisecond)
}

func (m model) tickAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// View shows the links found in stderr so far, then the last few lines of stderr, dimmed. Once we're exiting it renders
// nothing, so the final frame erases it all; otherwise Bubbletea leaves all but the last line on screen.
func (m model) View() tea.View {
	if m.exiting {
		return tea.NewView("")
	}
	var sections []string
	if len(m.links.links) > 0 {
		sections = append(sections, m.links.view(m.width))
	}
	if len(m.stderrTail) > 0 {
		sections = append(sections, m.stderrTailView())
	}
	return tea.NewView(strings.Join(sections, "\n"))
}

func (m model) stderrTailView() string {
	dim := ansi.NewStyle().Faint()
	lines := make([]string, len(m.stderrTail))
	for i, line := range m.stderrTail {
		// Strip the command's own escapes: its resets would undo the dimming and cursor moves would break the layout.
		line = ansi.Strip(line)
		if m.width > 0 {
			line = ansi.Truncate(line, m.width, "…")
		}
		lines[i] = dim.Styled(line)
	}
	return strings.Join(lines, "\n")
}

func (m model) startCmd() tea.Msg {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, m.args[0], m.args[1:]...)
	stdout := make(chan []byte, 1024)
	stderr := make(chan []byte, 1024)
	// Closing a pipe ends its reader, which then closes the channel.
	var pipes []*io.PipeWriter
	pipeTo := func(ch chan<- []byte) io.Writer {
		r, w := io.Pipe()
		go readBytesToChan(r, ch)
		pipes = append(pipes, w)
		return w
	}
	closePipes := func() {
		for _, w := range pipes {
			_ = w.Close()
		}
	}
	if m.directStdout != nil {
		cmd.Stdout = m.directStdout
		close(stdout)
	} else {
		cmd.Stdout = pipeTo(stdout)
	}
	cmd.Stderr = pipeTo(stderr)
	cmd.Stdin = os.Stdin
	errs := make(chan error, 1)
	start := command{
		cancel: cancel,
		cmd:    cmd,
		errs:   errs,
		stdout: stdout,
		stderr: stderr,
	}
	if err := cmd.Start(); err != nil {
		errs <- err
		close(errs)
		closePipes()
		return start
	}
	go func() {
		defer close(errs)
		if err := cmd.Wait(); err != nil {
			errs <- err
		}
		closePipes()
	}()
	return start
}

func readBytesToChan(in *io.PipeReader, out chan<- []byte) {
	defer close(out)
	for {
		rb := make([]byte, 1024)
		n, err := in.Read(rb)
		if n > 0 {
			out <- rb[:n]
		}
		if err != nil {
			return
		}
	}
}
