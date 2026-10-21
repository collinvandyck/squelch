package main

import (
	"slices"
	"testing"
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
