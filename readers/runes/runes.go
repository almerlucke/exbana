package runes

import (
	"bufio"
	"fmt"
	"io"
	"strconv"

	ebnf "github.com/almerlucke/exbana/v3"
)

// Pos is a position in a runes Reader: the line and column are for people (both count from zero), the index is what positions are compared by.
type Pos struct {
	Line  int
	Col   int
	Index int
}

// Compare orders positions by index, negative if p is before op.
func (p Pos) Compare(op Pos) int {
	return p.Index - op.Index
}

// Reader is an ebnf.Reader over runes. Line endings are normalized to \n when it is created.
type Reader struct {
	data []rune
	pos  Pos
}

// New reads all of r into a Reader.
func New(r io.Reader) (*Reader, error) {
	var (
		data    = make([]rune, 0)
		crFound bool
	)

	in := bufio.NewReader(r)
	for {
		if c, _, err := in.ReadRune(); err != nil {
			if err == io.EOF {
				if crFound {
					data = append(data, '\n')
				}
				break
			} else {
				return nil, err
			}
		} else {
			if crFound {
				if c == '\n' {
					data = append(data, c)
				} else {
					data = append(data, '\n', c)
				}
				crFound = false
			} else if c == '\r' {
				crFound = true
			} else {
				data = append(data, c)
			}
		}
	}

	return &Reader{data: data}, nil
}

// Data returns all runes of the reader.
func (r *Reader) Data() []rune {
	return r.data
}

// Peek1 implements ebnf.Reader.
func (r *Reader) Peek1() (rune, error) {
	if r.pos.Index < len(r.data) {
		return r.data[r.pos.Index], nil
	}

	return 0, io.EOF
}

// Read1 implements ebnf.Reader.
func (r *Reader) Read1() (rune, error) {
	if r.pos.Index < len(r.data) {
		c := r.data[r.pos.Index]
		r.pos.Index++
		r.pos.Col++
		if c == '\n' {
			r.pos.Line++
			r.pos.Col = 0
		}

		return c, nil
	}

	return 0, io.EOF
}

// Peek implements ebnf.Reader.
func (r *Reader) Peek(n int, buf []rune) (int, error) {
	var (
		i int
		p = r.pos.Index
		l = len(r.data)
	)

	for i < n && p < l {
		buf[i] = r.data[p]
		p++
		i++
	}

	if i != n {
		return i, io.EOF
	}

	return i, nil
}

func (r *Reader) read(n int, buf []rune) (int, error) {
	i := 0
	l := len(r.data)

	for i < n && r.pos.Index < l {
		c := r.data[r.pos.Index]
		if buf != nil {
			buf[i] = c
		}
		r.pos.Index++
		r.pos.Col++
		i++
		if c == '\n' {
			r.pos.Line++
			r.pos.Col = 0
		}
	}

	if i != n {
		return i, io.EOF
	}

	return i, nil
}

// Read implements ebnf.Reader.
func (r *Reader) Read(n int, buf []rune) (int, error) {
	return r.read(n, buf)
}

// Skip implements ebnf.Reader.
func (r *Reader) Skip(n int) (int, error) {
	return r.read(n, nil)
}

// Finished implements ebnf.Reader.
func (r *Reader) Finished() bool {
	return r.pos.Index >= len(r.data)
}

// Position implements ebnf.Reader.
func (r *Reader) Position() (Pos, error) {
	return r.pos, nil
}

// SetPosition implements ebnf.Reader, it fails for a position outside the data.
func (r *Reader) SetPosition(p Pos) error {
	if p.Index < 0 || p.Index > len(r.data) {
		return fmt.Errorf("position out of bounds: %v", p)
	}
	r.pos = p
	return nil
}

// Range implements ebnf.Reader, the returned slice is part of the data and must not be changed.
func (r *Reader) Range(p1 Pos, p2 Pos) ([]rune, error) {
	if p1.Index < 0 || p1.Index > len(r.data) || p2.Index < 0 || p2.Index > len(r.data) {
		return nil, fmt.Errorf("len(%d) -> position(s) out of bounds: %v - %v", len(r.data), p1, p2)
	}

	return r.data[p1.Index:p2.Index], nil
}

// Length implements ebnf.Reader, the number of runes from p1 to p2.
func (r *Reader) Length(p1 Pos, p2 Pos) int {
	return p2.Index - p1.Index
}

// FormatPos formats a position as line:col, counting from 1
func FormatPos(p Pos) string {
	return fmt.Sprintf("%d:%d", p.Line+1, p.Col+1)
}

// FormatRune formats a rune as a quoted character
func FormatRune(r rune) string {
	return strconv.QuoteRune(r)
}

// NewFarthestLog creates a FarthestLog for a rune reader that formats errors as "syntax error at line:col: unexpected 'x', expected ..."
func NewFarthestLog(r ebnf.Reader[rune, Pos]) *ebnf.FarthestLog[rune, Pos] {
	l := ebnf.NewFarthestLog(r)
	l.FormatPos = FormatPos
	l.FormatElem = FormatRune

	return l
}
