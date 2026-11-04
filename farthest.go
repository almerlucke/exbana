package exbana

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

// SyntaxError describes the farthest point a match got to before it failed.
type SyntaxError[T, P any] struct {
	Pos      P        // position of the failure
	Expected []string // descriptions of what would have matched at Pos
	Found    T        // the element at Pos, only valid if AtEnd is false
	AtEnd    bool     // the failure is at the end of the stream

	formatPos  func(P) string
	formatElem func(T) string
}

// Error implements the error interface, for example: syntax error at 3:14: unexpected 'x', expected digit or ")"
func (e *SyntaxError[T, P]) Error() string {
	var sb strings.Builder

	sb.WriteString("syntax error")

	if e.formatPos != nil {
		sb.WriteString(" at " + e.formatPos(e.Pos))
	} else {
		sb.WriteString(fmt.Sprintf(" at %v", e.Pos))
	}

	switch {
	case e.AtEnd:
		sb.WriteString(": unexpected end of input")
	case e.formatElem != nil:
		sb.WriteString(": unexpected " + e.formatElem(e.Found))
	default:
		sb.WriteString(fmt.Sprintf(": unexpected %v", e.Found))
	}

	switch len(e.Expected) {
	case 0:
	case 1:
		sb.WriteString(", expected " + e.Expected[0])
	default:
		last := len(e.Expected) - 1
		sb.WriteString(", expected " + strings.Join(e.Expected[:last], ", ") + " or " + e.Expected[last])
	}

	return sb.String()
}

// FarthestLog is a Logger that remembers only the mismatches that got the furthest into the stream, which is the
// best guess for where the syntax error is. Give it to the parse through a ParseContext (WithLogger), parse, and
// when the match fails call Err to get an error with the position and what was expected there.
//
// Mismatches of composite patterns (concatenation, exception) are ignored: they are always caused by a leaf pattern that
// failed deeper, and that one is logged itself. The same goes for composites without an ID, like an unnamed
// repetition or alternation. Composites with an ID are reported by name, so name only the rules you want to see in messages. Mismatches are compared by their begin position, so a vector that
// fails halfway is reported at its start.
//
// To get useful messages give the leaf patterns of the grammar an ID or print output, they are used as the
// description of what was expected. Patterns without any are left out of the expected list.
type FarthestLog[T, P any] struct {
	// FormatPos formats a position for the error message (optional)
	FormatPos func(P) string
	// FormatElem formats the unexpected element for the error message (optional)
	FormatElem func(T) string

	reader   Reader[T, P]
	has      bool
	pos      P
	expected []Pattern[T, P]
	seen     map[Pattern[T, P]]struct{}
}

// NewFarthestLog creates a FarthestLog. The reader is used to compare positions and to look up the unexpected
// element, it should be the reader that is matched against (or the one it wraps).
func NewFarthestLog[T, P any](r Reader[T, P]) *FarthestLog[T, P] {
	return &FarthestLog[T, P]{reader: r}
}

// LogMismatch implements the Logger interface
func (l *FarthestLog[T, P]) LogMismatch(m *Mismatch[T, P]) {
	if m.Unmatched != nil {
		return
	}

	// an unnamed composite (like digit+) would only print as a grammar fragment, and the leaf patterns it is
	// built from fail at the same position or further, and are logged themselves
	if _, composite := m.Pattern.(Parent[T, P]); composite && IDOf(m.Pattern) == NoID {
		return
	}

	if l.has {
		diff := l.reader.Length(l.pos, m.Begin)
		if diff < 0 {
			return
		}

		if diff > 0 {
			l.has = false
		}
	}

	if !l.has {
		l.has = true
		l.pos = m.Begin
		l.expected = l.expected[:0]
		l.seen = make(map[Pattern[T, P]]struct{})
	}

	if _, ok := l.seen[m.Pattern]; !ok {
		l.seen[m.Pattern] = struct{}{}
		l.expected = append(l.expected, m.Pattern)
	}
}

// Reset forgets everything that was logged
func (l *FarthestLog[T, P]) Reset() {
	l.has = false
	l.expected = nil
	l.seen = nil
}

// Last returns the farthest failure as a SyntaxError, or nil if nothing was logged.
func (l *FarthestLog[T, P]) Last() *SyntaxError[T, P] {
	if !l.has {
		return nil
	}

	e := &SyntaxError[T, P]{Pos: l.pos, formatPos: l.FormatPos, formatElem: l.FormatElem}

	seen := make(map[string]struct{})
	for _, p := range l.expected {
		d := Describe(p)
		if d == "" {
			continue
		}
		if _, ok := seen[d]; !ok {
			seen[d] = struct{}{}
			e.Expected = append(e.Expected, d)
		}
	}

	// look at the element at the failure position, and put the reader back where it was
	if cur, err := l.reader.Position(); !IsStreamError(err) {
		if l.reader.SetPosition(l.pos) == nil {
			found, err := l.reader.Peek1()
			if err == io.EOF {
				e.AtEnd = true
			} else if err == nil {
				e.Found = found
			}
		}
		_ = l.reader.SetPosition(cur)
	}

	return e
}

// Err returns the farthest failure as an error, or nil if nothing was logged.
func (l *FarthestLog[T, P]) Err() error {
	if e := l.Last(); e != nil {
		return e
	}

	return nil
}

// Describe returns the ID of a pattern, or else its EBNF print output, or an empty string if it has neither.
func Describe[T, P any](p Pattern[T, P]) string {
	if id := IDOf(p); id != NoID {
		return id
	}

	var buf bytes.Buffer
	if err := p.Print(&buf); err != nil {
		return ""
	}

	return buf.String()
}

// Parent is implemented by patterns that are built from other patterns.
type Parent[T, P any] interface {
	// Children returns the patterns this pattern is built from. Patterns that are only used for lookahead
	// (the exception of an except pattern) are not included.
	Children() Patterns[T, P]
}
