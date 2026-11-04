package main

import (
	"slices"
	"strings"
)

const (
	parseText parseState = iota
	parseEsc
	parseEscIntermediate
	parseCSI
	parseString    // OSC, DCS, and the other sequences that run until a string terminator
	parseStringEsc // an ESC inside a string, which starts the ESC \ terminator
)

type (
	// cursorTracker follows what the command writes to the terminal, closely enough to know two things: whether the
	// cursor is at the start of a line, and whether the command has switched to the alternate screen, as full-screen
	// programs do. Sequences can arrive split across writes, so it keeps its parse state between them.
	cursorTracker struct {
		atLineStart bool
		altScreen   bool
		// lineStartBeforeAlt is restored on leaving the alternate screen, which puts the cursor back where it was.
		lineStartBeforeAlt bool
		state              parseState
		params             []byte
	}
	parseState int
)

// altScreenModes are the private modes that switch to the alternate screen.
var altScreenModes = []string{"47", "1047", "1049"}

func newCursorTracker() cursorTracker {
	return cursorTracker{atLineStart: true}
}

func (c *cursorTracker) write(p []byte) {
	for _, b := range p {
		c.writeByte(b)
	}
}

func (c *cursorTracker) writeByte(b byte) {
	switch c.state {
	case parseText:
		switch {
		case b == 0x1b:
			c.state = parseEsc
		case b == '\n' || b == '\r':
			c.atLineStart = true
		case b == '\t' || b >= 0x20 && b != 0x7f:
			c.atLineStart = false
		default:
			// Other control characters, such as the bell, don't move the cursor off the start of a line.
		}
	case parseEsc:
		switch {
		case b == '[':
			c.state = parseCSI
			c.params = c.params[:0]
		case b == ']' || b == 'P' || b == 'X' || b == '^' || b == '_':
			c.state = parseString
		case b >= 0x20 && b <= 0x2f:
			c.state = parseEscIntermediate
		default:
			c.state = parseText
		}
	case parseEscIntermediate:
		if b < 0x20 || b > 0x2f {
			c.state = parseText
		}
	case parseCSI:
		switch {
		case b >= 0x20 && b <= 0x3f:
			c.params = append(c.params, b)
		case b >= 0x40 && b <= 0x7e:
			c.csi(string(c.params), b)
			c.state = parseText
		default:
			c.state = parseText
		}
	case parseString:
		switch b {
		case 0x07:
			c.state = parseText
		case 0x1b:
			c.state = parseStringEsc
		default:
		}
	case parseStringEsc:
		c.state = parseText
	default:
		c.state = parseText
	}
}

func (c *cursorTracker) csi(params string, final byte) {
	if final != 'h' && final != 'l' {
		return
	}
	modes, ok := strings.CutPrefix(params, "?")
	if !ok || !slices.ContainsFunc(strings.Split(modes, ";"), func(m string) bool {
		return slices.Contains(altScreenModes, m)
	}) {
		return
	}
	switch {
	case final == 'h' && !c.altScreen:
		c.altScreen = true
		c.lineStartBeforeAlt = c.atLineStart
	case final == 'l' && c.altScreen:
		c.altScreen = false
		c.atLineStart = c.lineStartBeforeAlt
	default:
	}
}
