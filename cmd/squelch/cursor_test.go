package main

import "testing"

// TestCursorTracker checks that the tracker knows when the cursor is at the start of a line and when the command is
// on the alternate screen, including when escape sequences are split across writes.
func TestCursorTracker(t *testing.T) {
	for _, tc := range []struct {
		name            string
		writes          []string
		wantAtLineStart bool
		wantAltScreen   bool
	}{
		{name: "nothing written", wantAtLineStart: true},
		{name: "partial line", writes: []string{"Name: "}},
		{name: "whole line", writes: []string{"hello\n"}, wantAtLineStart: true},
		{name: "carriage return", writes: []string{"50%\r"}, wantAtLineStart: true},
		{name: "progress redraw", writes: []string{"\r\x1b[K60%"}},
		{name: "color reset after newline", writes: []string{"\x1b[31mred\n\x1b[0m"}, wantAtLineStart: true},
		{name: "color before text", writes: []string{"\n\x1b[1m", "bold"}},
		{name: "osc title", writes: []string{"\n\x1b]0;my title\x07"}, wantAtLineStart: true},
		{name: "osc hyperlink with ST", writes: []string{"\n\x1b]8;;https://x.io\x1b\\"}, wantAtLineStart: true},
		{name: "charset designation", writes: []string{"\n\x1b(B"}, wantAtLineStart: true},
		{name: "bell", writes: []string{"\n\a"}, wantAtLineStart: true},
		{name: "enter alt screen", writes: []string{"\x1b[?1049h"}, wantAtLineStart: true, wantAltScreen: true},
		{name: "enter alt screen split", writes: []string{"\x1b", "[?10", "49h"}, wantAtLineStart: true, wantAltScreen: true},
		{name: "alt screen among other modes", writes: []string{"\x1b[?25;1049h"}, wantAtLineStart: true, wantAltScreen: true},
		{name: "old alt screen mode", writes: []string{"\x1b[?47h"}, wantAtLineStart: true, wantAltScreen: true},
		{name: "other private mode", writes: []string{"\x1b[?25l"}, wantAtLineStart: true},
		{name: "non-private mode 1049", writes: []string{"\x1b[1049h"}, wantAtLineStart: true},
		{
			name:            "leaving alt screen restores line start",
			writes:          []string{"Name: ", "\x1b[?1049h", "drawing\n", "\x1b[?1049l"},
			wantAtLineStart: false,
		},
		{
			name:            "leaving alt screen at line start",
			writes:          []string{"done\n\x1b[?1049h", "drawing", "\x1b[?1049l"},
			wantAtLineStart: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCursorTracker()
			for _, w := range tc.writes {
				c.write([]byte(w))
			}
			if c.atLineStart != tc.wantAtLineStart {
				t.Errorf("atLineStart = %v, want %v", c.atLineStart, tc.wantAtLineStart)
			}
			if c.altScreen != tc.wantAltScreen {
				t.Errorf("altScreen = %v, want %v", c.altScreen, tc.wantAltScreen)
			}
		})
	}
}
