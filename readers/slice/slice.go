// Package slice provides a reader over a slice of any element type, with the index into the slice as position. It is
// how to parse something other than text: a stream of tokens from a lexer, bytes, events, records.
//
//	tokens := []Token{...}
//	ok, match, err := ebnf.Parse(grammar, slice.New(tokens))
package slice

import (
	"fmt"
	"io"

	ebnf "github.com/almerlucke/exbana/v3"
)

// Reader is an ebnf.Reader over a slice. The position is the index of the next element, so positions are plain ints
// and can be used directly as the memo index (see Index).
type Reader[T any] struct {
	data []T
	pos  int
}

// New creates a Reader over data. The data is not copied and must not be changed while it is read.
func New[T any](data []T) *Reader[T] {
	return &Reader[T]{data: data}
}

// Data returns the slice the reader reads.
func (r *Reader[T]) Data() []T {
	return r.data
}

// Index returns the position as the unique integer ParseContext.WithMemo asks for: the position itself.
func Index(p int) int {
	return p
}

// Peek1 implements ebnf.Reader.
func (r *Reader[T]) Peek1() (T, error) {
	if r.pos < len(r.data) {
		return r.data[r.pos], nil
	}

	var zero T

	return zero, io.EOF
}

// Read1 implements ebnf.Reader.
func (r *Reader[T]) Read1() (T, error) {
	v, err := r.Peek1()
	if err == nil {
		r.pos++
	}

	return v, err
}

// Peek implements ebnf.Reader.
func (r *Reader[T]) Peek(n int, buf []T) (int, error) {
	i := copy(buf[:n], r.data[r.pos:])
	if i != n {
		return i, io.EOF
	}

	return i, nil
}

// Read implements ebnf.Reader.
func (r *Reader[T]) Read(n int, buf []T) (int, error) {
	i, err := r.Peek(n, buf)
	r.pos += i

	return i, err
}

// Skip implements ebnf.Reader.
func (r *Reader[T]) Skip(n int) (int, error) {
	i := min(n, len(r.data)-r.pos)
	r.pos += i

	if i != n {
		return i, io.EOF
	}

	return i, nil
}

// Finished implements ebnf.Reader.
func (r *Reader[T]) Finished() bool {
	return r.pos >= len(r.data)
}

// Position implements ebnf.Reader.
func (r *Reader[T]) Position() (int, error) {
	return r.pos, nil
}

// SetPosition implements ebnf.Reader, it fails for a position outside the data.
func (r *Reader[T]) SetPosition(p int) error {
	if p < 0 || p > len(r.data) {
		return fmt.Errorf("position out of bounds: %d", p)
	}

	r.pos = p

	return nil
}

// Range implements ebnf.Reader, the returned slice is part of the data and must not be changed.
func (r *Reader[T]) Range(p1, p2 int) ([]T, error) {
	if p1 < 0 || p1 > len(r.data) || p2 < 0 || p2 > len(r.data) || p1 > p2 {
		return nil, fmt.Errorf("len(%d) -> position(s) out of bounds: %d - %d", len(r.data), p1, p2)
	}

	return r.data[p1:p2], nil
}

// Length implements ebnf.Reader, the number of elements from p1 to p2.
func (r *Reader[T]) Length(p1, p2 int) int {
	return p2 - p1
}

// NewFarthestLog creates a FarthestLog for a slice reader. The unexpected element is formatted with format, or with %v
// if format is nil. Positions are indexes into the slice, counted from zero.
func NewFarthestLog[T any](r ebnf.Reader[T, int], format func(T) string) *ebnf.FarthestLog[T, int] {
	l := ebnf.NewFarthestLog(r)
	l.FormatPos = func(p int) string { return fmt.Sprintf("element %d", p) }
	l.FormatElem = format

	return l
}
