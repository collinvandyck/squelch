package main

import (
	"bytes"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
)

const (
	// drawInterval limits how often stderr alone redraws the view.
	drawInterval    = 30 * time.Millisecond
	stderrTailLines = 4
)

// screen draws the live view, the links found in stderr and the last few lines of it, beneath the command's output on
// the terminal. When the view is on screen, the cursor is parked on its first line, so that the command's next output
// starts there once the view is erased.
type screen struct {
	mu          sync.Mutex
	term        io.Writer
	width       int
	cursor      cursorTracker
	stderr      []byte // everything the command wrote, untouched, for the dump on failure
	stderrLines lineBuffer
	stderrTail  []string
	links       linkList
	drawn       bool
	lastDraw    time.Time
	drawTimer   *time.Timer
	closed      bool
}

func newScreen(term io.Writer, width int) *screen {
	return &screen{term: term, width: width, cursor: newCursorTracker()}
}

// writeStdout passes the command's stdout to the terminal untouched. The view is erased before and redrawn after in a
// single write, so it doesn't flicker.
func (s *screen) writeStdout(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var buf bytes.Buffer
	s.erase(&buf)
	buf.Write(p)
	s.cursor.write(p)
	s.draw(&buf)
	_, _ = s.term.Write(buf.Bytes())
}

func (s *screen) writeStderr(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stderr = append(s.stderr, p...)
	lines := s.stderrLines.write(p)
	s.links.add(lines)
	s.addStderrTail(lines)
	s.redrawSoon()
}

func (s *screen) resize(width int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.width = width
	s.redrawSoon()
}

// close erases the view for good and reports whether the command left the cursor at the start of a line.
func (s *screen) close() (atLineStart bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.drawTimer != nil {
		s.drawTimer.Stop()
	}
	var buf bytes.Buffer
	s.erase(&buf)
	_, _ = s.term.Write(buf.Bytes())
	return s.cursor.atLineStart
}

func (s *screen) redrawSoon() {
	if s.closed || s.drawTimer != nil {
		return
	}
	if wait := drawInterval - time.Since(s.lastDraw); wait > 0 {
		s.drawTimer = time.AfterFunc(wait, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.drawTimer = nil
			s.redraw()
		})
		return
	}
	s.redraw()
}

func (s *screen) redraw() {
	var buf bytes.Buffer
	s.erase(&buf)
	s.draw(&buf)
	if buf.Len() > 0 {
		_, _ = s.term.Write(buf.Bytes())
	}
}

func (s *screen) erase(buf *bytes.Buffer) {
	if s.drawn {
		buf.WriteString(ansi.EraseScreenBelow)
		s.drawn = false
	}
}

// draw writes the view and parks the cursor on its first line. It draws only when the command has left the cursor at
// the start of a line on the main screen. Otherwise the line belongs to the command, which may be waiting for an
// answer to it, or the whole screen does.
func (s *screen) draw(buf *bytes.Buffer) {
	if s.closed || !s.cursor.atLineStart || s.cursor.altScreen {
		return
	}
	lines := s.viewLines()
	if len(lines) == 0 {
		return
	}
	// The command may have put the terminal in raw mode, where a bare newline doesn't return the cursor.
	buf.WriteString(strings.Join(lines, "\r\n"))
	if len(lines) > 1 {
		buf.WriteString(ansi.CursorUp(len(lines) - 1))
	}
	buf.WriteByte('\r')
	s.drawn = true
	s.lastDraw = time.Now()
}

func (s *screen) viewLines() []string {
	var lines []string
	if len(s.links.links) > 0 {
		lines = strings.Split(s.links.view(s.width), "\n")
	}
	dim := ansi.NewStyle().Faint()
	for _, line := range s.tail() {
		// Strip the command's own escapes: its resets would undo the dimming and cursor moves would break the layout.
		line = ansi.Strip(line)
		if s.width > 0 {
			line = ansi.Truncate(line, s.width, "…")
		}
		lines = append(lines, dim.Styled(line))
	}
	return lines
}

// tail returns the last few lines of stderr, ending with the line the command is partway through, if any. A prompt on
// stderr shows up there while it waits for an answer.
func (s *screen) tail() []string {
	current := s.stderrLines.current()
	if isBlank(current) {
		return s.stderrTail
	}
	tail := append(slices.Clone(s.stderrTail), current)
	return tail[max(0, len(tail)-stderrTailLines):]
}

// addStderrTail skips blank lines, which would only use up the tail's few lines. s.stderr still has them.
func (s *screen) addStderrTail(lines []string) {
	for _, line := range lines {
		if !isBlank(line) {
			s.stderrTail = append(s.stderrTail, line)
		}
	}
	if over := len(s.stderrTail) - stderrTailLines; over > 0 {
		s.stderrTail = slices.Clone(s.stderrTail[over:])
	}
}

func isBlank(line string) bool {
	return strings.TrimSpace(ansi.Strip(line)) == ""
}
