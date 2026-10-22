package main

import (
	"bytes"
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
	// promptIdle is how long the command must go quiet partway through a line before we take the line for a prompt.
	promptIdle      = 150 * time.Millisecond
	stderrTailLines = 4
)

type (
	model struct {
		command command
		err     error
		// stdinIsTerminal means someone can answer a prompt, so we look for them.
		stdinIsTerminal bool
		stdout          lineBuffer
		stderr          []byte // everything the command wrote, untouched, for the dump on failure
		stderrShown     int    // how much of stderr a prompt has already shown
		stderrLines     lineBuffer
		stderrTail      []string
		links           linkList
		lastOutput      time.Time
		width           int
		prompting       bool
		exiting         bool
	}
	command struct {
		cmd    *exec.Cmd
		errs   chan error
		stdout chan []byte
		stderr chan []byte
		// activity gets a signal after each read from the command, and exited closes once it has exited.
		activity chan struct{}
		exited   chan struct{}
		cancel   context.CancelFunc
	}
	// prompt is a tea.ExecCommand that shows a command's prompt while Bubbletea is paused, then waits for the command
	// to write again or exit, which means the prompt has been answered.
	prompt struct {
		text    []byte
		command command
		out     io.Writer
	}
	tickMsg       time.Time
	promptDoneMsg struct{}
)

func newModel(c command) model {
	return model{
		command: c,
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

// waitForOutput blocks until the command writes something we haven't read yet, or exits.
func (c command) waitForOutput() {
	// activity may hold a signal for output we've already read, so clear it before checking for unread output.
	select {
	case <-c.activity:
	default:
	}
	if len(c.stdout) > 0 || len(c.stderr) > 0 {
		return
	}
	select {
	case <-c.activity:
	case <-c.exited:
	}
}

func (p *prompt) Run() error {
	if _, err := p.out.Write(p.text); err != nil {
		return err
	}
	p.command.waitForOutput()
	return nil
}

func (p *prompt) SetStdin(io.Reader) {}

func (p *prompt) SetStdout(w io.Writer) {
	p.out = w
}

func (p *prompt) SetStderr(io.Writer) {}

func (m model) Init() tea.Cmd {
	return m.tickAfter(firstPrintDelay)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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
		if len(stdout) > 0 || len(stderr) > 0 {
			m.lastOutput = time.Time(msg)
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
		} else if p, ok := m.takePrompt(time.Time(msg)); ok {
			// The empty view this renders erases the live view before Bubbletea hands over the terminal.
			m.prompting = true
			cmds = append(cmds, tea.Exec(p, func(error) tea.Msg { return promptDoneMsg{} }))
		} else {
			cmds = append(cmds, m.tick())
		}
		return m, tea.Sequence(cmds...)
	case promptDoneMsg:
		m.prompting = false
		return m, m.tick()
	case tea.WindowSizeMsg:
		m.width = msg.Width
	}
	return m, nil
}

// takePrompt returns a prompt if the command has gone quiet partway through a line that reads like a question. A
// prompt on stderr brings the rest of the unshown stderr with it, since the question is likely about that output.
func (m *model) takePrompt(now time.Time) (*prompt, bool) {
	if !m.stdinIsTerminal || now.Sub(m.lastOutput) < promptIdle {
		return nil, false
	}
	var text []byte
	if looksLikePrompt(m.stderrLines.partial) {
		text = append(text, m.stderr[m.stderrShown:]...)
		m.stderrShown = len(m.stderr)
		m.stderrLines.flush()
		m.stderrTail = nil
	}
	if looksLikePrompt(m.stdout.partial) {
		line, _ := m.stdout.flush()
		text = append(text, line...)
	}
	if len(text) == 0 {
		return nil, false
	}
	return &prompt{text: text, command: m.command}, true
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

// View shows the links found in stderr so far, then the last few lines of stderr, dimmed. While a prompt has the
// terminal, or once we're exiting, it renders nothing, so the final frame erases it all; otherwise Bubbletea leaves all
// but the last line on screen.
func (m model) View() tea.View {
	if m.exiting || m.prompting {
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

// startCommand starts the command with our stdin, reading its output into channels. Its stdout goes to directStdout
// instead when that's set.
func startCommand(args []string, directStdout io.Writer) (command, error) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	stdout := make(chan []byte, 1024)
	stderr := make(chan []byte, 1024)
	activity := make(chan struct{}, 1)
	// Closing a pipe ends its reader, which then closes the channel.
	var pipes []*io.PipeWriter
	pipeTo := func(ch chan<- []byte) io.Writer {
		r, w := io.Pipe()
		go readBytesToChan(r, ch, activity)
		pipes = append(pipes, w)
		return w
	}
	closePipes := func() {
		for _, w := range pipes {
			_ = w.Close()
		}
	}
	if directStdout != nil {
		cmd.Stdout = directStdout
		close(stdout)
	} else {
		cmd.Stdout = pipeTo(stdout)
	}
	cmd.Stderr = pipeTo(stderr)
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		closePipes()
		cancel()
		return command{}, err
	}
	errs := make(chan error, 1)
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		defer close(errs)
		if err := cmd.Wait(); err != nil {
			errs <- err
		}
		closePipes()
	}()
	return command{
		cancel:   cancel,
		cmd:      cmd,
		errs:     errs,
		stdout:   stdout,
		stderr:   stderr,
		activity: activity,
		exited:   exited,
	}, nil
}

func readBytesToChan(in *io.PipeReader, out chan<- []byte, activity chan<- struct{}) {
	defer close(out)
	for {
		rb := make([]byte, 1024)
		n, err := in.Read(rb)
		if n > 0 {
			out <- rb[:n]
			select {
			case activity <- struct{}{}:
			default:
			}
		}
		if err != nil {
			return
		}
	}
}

// looksLikePrompt reports whether an unterminated line reads like a question waiting for an answer, such as
// "Continue? [y/n] " or "Password: ". A carriage return means a progress line redrawing itself, not a prompt.
func looksLikePrompt(partial []byte) bool {
	if bytes.IndexByte(partial, '\r') >= 0 {
		return false
	}
	s := strings.TrimRight(ansi.Strip(string(partial)), " \t")
	if s == "" {
		return false
	}
	return strings.ContainsRune("?:]>)", rune(s[len(s)-1]))
}
