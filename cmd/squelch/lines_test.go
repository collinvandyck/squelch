package main

import (
	"slices"
	"testing"
)

// TestLineBufferWrite verifies that lines split across writes come back whole and in order.
func TestLineBufferWrite(t *testing.T) {
	for _, tc := range []struct {
		name   string
		writes []string
		want   []string
		rest   string
	}{
		{name: "one line per write", writes: []string{"a\n", "b\n"}, want: []string{"a", "b"}},
		{name: "line split across writes", writes: []string{"a", "b", "c\n"}, want: []string{"abc"}},
		{name: "several lines in one write", writes: []string{"a\nb\nc\n"}, want: []string{"a", "b", "c"}},
		{name: "blank lines kept", writes: []string{"\n", "a\n\n"}, want: []string{"", "a", ""}},
		{name: "trailing partial held back", writes: []string{"a\nb", "c"}, want: []string{"a"}, rest: "bc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b lineBuffer
			var got []string
			for _, w := range tc.writes {
				got = append(got, b.write([]byte(w))...)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("lines = %q, want %q", got, tc.want)
			}
			rest, _ := b.flush()
			if rest != tc.rest {
				t.Errorf("flush = %q, want %q", rest, tc.rest)
			}
		})
	}
}

// TestLineBufferFlush verifies that flush reports whether a partial line existed and empties the buffer.
func TestLineBufferFlush(t *testing.T) {
	var b lineBuffer
	if _, ok := b.flush(); ok {
		t.Fatal("flush on empty buffer reported a line")
	}
	b.write([]byte("a\n"))
	if _, ok := b.flush(); ok {
		t.Fatal("flush after a complete line reported a line")
	}
	b.write([]byte("partial"))
	if line, ok := b.flush(); !ok || line != "partial" {
		t.Fatalf("flush = %q, %v, want %q, true", line, ok, "partial")
	}
	if _, ok := b.flush(); ok {
		t.Fatal("second flush reported a line")
	}
}
