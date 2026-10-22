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
		line := strings.TrimSuffix(string(b.partial[:i]), "\r")
		if j := strings.LastIndexByte(line, '\r'); j >= 0 {
			line = line[j+1:]
		}
		lines = append(lines, line)
		b.partial = b.partial[i+1:]
	}
	if len(b.partial) == 0 {
		b.partial = nil
	}
	return lines
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
