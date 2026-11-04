package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// TestAddStderrTailSkipsBlankLines checks that empty, whitespace-only, and escape-only lines stay out of the tail, and
// that the tail keeps only the last few lines.
func TestAddStderrTailSkipsBlankLines(t *testing.T) {
	var s screen
	s.addStderrTail([]string{"one", "", "two", "  \t", "\x1b[0m", "three"})
	s.addStderrTail([]string{"", "four", " five ", ""})
	want := []string{"two", "three", "four", " five "}
	if !slices.Equal(s.stderrTail, want) {
		t.Errorf("stderrTail = %q, want %q", s.stderrTail, want)
	}
}

// TestScreenTailEndsWithCurrentLine checks that the tail includes the stderr line the command is partway through, as it
// would show on a terminal, and still holds only the last few lines.
func TestScreenTailEndsWithCurrentLine(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stderr string
		want   []string
	}{
		{name: "prompt", stderr: "plan 1\nplan 2\nProceed? ", want: []string{"plan 1", "plan 2", "Proceed? "}},
		{name: "progress", stderr: "start\n10%\r50%", want: []string{"start", "50%"}},
		{name: "blank current line", stderr: "start\n  ", want: []string{"start"}},
		{name: "limit", stderr: "1\n2\n3\n4\nfive", want: []string{"2", "3", "4", "five"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newScreen(&bytes.Buffer{}, 80)
			s.writeStderr([]byte(tc.stderr))
			if got := s.tail(); !slices.Equal(got, tc.want) {
				t.Errorf("tail = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestScreenStdoutErasesAndRedrawsView checks that stdout reaches the terminal untouched, after the view is erased,
// and that the view is drawn again after it.
func TestScreenStdoutErasesAndRedrawsView(t *testing.T) {
	var out bytes.Buffer
	s := newScreen(&out, 80)
	s.writeStderr([]byte("noise\n"))
	if !strings.Contains(ansi.Strip(out.String()), "noise") {
		t.Fatalf("view not drawn: %q", out.String())
	}
	out.Reset()
	s.writeStdout([]byte("\x1b[1mhello\x1b[0m\n"))
	got := out.String()
	want := ansi.EraseScreenBelow + "\x1b[1mhello\x1b[0m\n"
	if !strings.HasPrefix(got, want) {
		t.Fatalf("output = %q, want it to start with %q", got, want)
	}
	if !strings.Contains(ansi.Strip(got[len(want):]), "noise") {
		t.Errorf("view not redrawn after stdout: %q", got)
	}
}

// TestScreenLeavesLineToCommand checks that the view stays off the screen while the command is partway through a line
// of stdout, and comes back once it finishes the line.
func TestScreenLeavesLineToCommand(t *testing.T) {
	var out bytes.Buffer
	s := newScreen(&out, 80)
	s.writeStdout([]byte("Name: "))
	out.Reset()
	s.writeStderr([]byte("noise\n"))
	time.Sleep(2 * drawInterval)
	if out.Len() > 0 {
		t.Fatalf("drew over the command's line: %q", out.String())
	}
	s.writeStdout([]byte("\n"))
	if !strings.Contains(ansi.Strip(out.String()), "noise") {
		t.Errorf("view not drawn after the line ended: %q", out.String())
	}
}

// TestScreenLeavesAltScreenToCommand checks that the view stays off the screen while the command uses the alternate
// screen, and comes back once it leaves.
func TestScreenLeavesAltScreenToCommand(t *testing.T) {
	var out bytes.Buffer
	s := newScreen(&out, 80)
	s.writeStdout([]byte("\x1b[?1049h"))
	out.Reset()
	s.writeStderr([]byte("noise\n"))
	time.Sleep(2 * drawInterval)
	if out.Len() > 0 {
		t.Fatalf("drew on the alternate screen: %q", out.String())
	}
	s.writeStdout([]byte("\x1b[?1049l"))
	if !strings.Contains(ansi.Strip(out.String()), "noise") {
		t.Errorf("view not drawn after leaving the alternate screen: %q", out.String())
	}
}

// TestScreenCloseErasesViewForGood checks that close erases the view, and that nothing draws it again afterward.
func TestScreenCloseErasesViewForGood(t *testing.T) {
	var out bytes.Buffer
	s := newScreen(&out, 80)
	s.writeStderr([]byte("noise\n"))
	out.Reset()
	s.close()
	if out.String() != ansi.EraseScreenBelow {
		t.Fatalf("close wrote %q, want %q", out.String(), ansi.EraseScreenBelow)
	}
	out.Reset()
	s.writeStderr([]byte("more\n"))
	time.Sleep(2 * drawInterval)
	if out.Len() > 0 {
		t.Errorf("drew after close: %q", out.String())
	}
}
