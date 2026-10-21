package main

import "bytes"

// lineBuffer turns output that arrives in arbitrary chunks into whole lines.
type lineBuffer struct {
	partial []byte
}

// write appends p and returns the lines it completed, without their newlines.
func (b *lineBuffer) write(p []byte) []string {
	b.partial = append(b.partial, p...)
	var lines []string
	for {
		i := bytes.IndexByte(b.partial, '\n')
		if i < 0 {
			break
		}
		lines = append(lines, string(b.partial[:i]))
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
