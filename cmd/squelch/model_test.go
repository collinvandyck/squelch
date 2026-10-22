package main

import (
	"slices"
	"testing"
	"time"
)

// TestAddStderrTailSkipsBlankLines checks that empty, whitespace-only, and escape-only lines stay out of the tail, and
// that the tail keeps only the last few lines.
func TestAddStderrTailSkipsBlankLines(t *testing.T) {
	var m model
	m.addStderrTail([]string{"one", "", "two", "  \t", "\x1b[0m", "three"})
	m.addStderrTail([]string{"", "four", " five ", ""})
	want := []string{"two", "three", "four", " five "}
	if !slices.Equal(m.stderrTail, want) {
		t.Errorf("stderrTail = %q, want %q", m.stderrTail, want)
	}
}

// TestLooksLikePrompt checks which unterminated lines count as prompts.
func TestLooksLikePrompt(t *testing.T) {
	for _, tc := range []struct {
		partial string
		want    bool
	}{
		{"Please confirm above rollout plan [y/n] ", true},
		{"Do you want to continue (Y/n)?  ", true},
		{"Password:", true},
		{"\x1b[1mName:\x1b[0m ", true},
		{"> ", true},
		{"Waiting for operation to complete...", false},
		{"\rDownloading [=====>    ]", false},
		{"Continue? [y/n]\r", false},
		{"   ", false},
		{"", false},
	} {
		if got := looksLikePrompt([]byte(tc.partial)); got != tc.want {
			t.Errorf("looksLikePrompt(%q) = %v, want %v", tc.partial, got, tc.want)
		}
	}
}

// TestTakePromptNeedsTerminalAndQuiet checks that a prompt is taken only when stdin is a terminal and the command has
// been quiet long enough.
func TestTakePromptNeedsTerminalAndQuiet(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name     string
		terminal bool
		quietFor time.Duration
		want     bool
	}{
		{name: "terminal and quiet", terminal: true, quietFor: promptIdle, want: true},
		{name: "not quiet long enough", terminal: true, quietFor: promptIdle / 2},
		{name: "stdin not a terminal", quietFor: promptIdle},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := model{stdinIsTerminal: tc.terminal, lastOutput: now.Add(-tc.quietFor)}
			m.stdout.write([]byte("Continue? [y/n] "))
			if _, ok := m.takePrompt(now); ok != tc.want {
				t.Errorf("takePrompt ok = %v, want %v", ok, tc.want)
			}
		})
	}
}

// TestTakePromptShowsUnshownStderr checks that a stderr prompt carries all stderr since the last prompt, and that the
// shown stderr is left out of later prompts and the tail.
func TestTakePromptShowsUnshownStderr(t *testing.T) {
	m := model{stdinIsTerminal: true}
	write := func(s string) {
		m.stderr = append(m.stderr, s...)
		m.addStderrTail(m.stderrLines.write([]byte(s)))
	}
	write("plan 1\nplan 2\nProceed? ")
	p, ok := m.takePrompt(time.Now())
	if !ok || string(p.text) != "plan 1\nplan 2\nProceed? " {
		t.Fatalf("first prompt = %q, %v", p.text, ok)
	}
	if len(m.stderrTail) != 0 {
		t.Errorf("stderrTail = %q, want empty", m.stderrTail)
	}
	write("\nplan 3\nReally? ")
	p, ok = m.takePrompt(time.Now())
	if !ok || string(p.text) != "\nplan 3\nReally? " {
		t.Fatalf("second prompt = %q, %v", p.text, ok)
	}
}
