package main

import (
	"bytes"
	"strings"
)

// lineBuffer turns output that arrives in arbitrary chunks into whole lines.
type lineBuffer struct {
	partial []byte
}

// write appends p and returns the lines it completed, without their newlines. A line keeps only what follows its last
// carriage return, which is what a terminal would have left showing.
func (b *lineBuffer) write(p []byte) []string {
	b.partial = append(b.partial, p...)
	var lines []string
	for {
		i := bytes.IndexByte(b.partial, '\n')
		if i < 0 {
			break
		}
		lines = append(lines, visible(string(b.partial[:i])))
		b.partial = b.partial[i+1:]
	}
	if len(b.partial) == 0 {
		b.partial = nil
	}
	return lines
}

// current returns what the unterminated last line would show on a terminal so far.
func (b *lineBuffer) current() string {
	return visible(string(b.partial))
}

// flush returns the unterminated last line, if there is one, and empties the buffer.
func (b *lineBuffer) flush() (string, bool) {
	if len(b.partial) == 0 {
		return "", false
	}
	line := string(b.partial)
	b.partial = nil
	return line, true
}

// visible returns what follows the last carriage return in line, ignoring one at the very end, as with \r\n endings.
func visible(line string) string {
	line = strings.TrimSuffix(line, "\r")
	if i := strings.LastIndexByte(line, '\r'); i >= 0 {
		line = line[i+1:]
	}
	return line
}
