package runes

import (
	"bufio"
	"fmt"
	"io"
)

type Pos struct {
	Line  int
	Col   int
	Index int
}

type Reader struct {
	data []rune
	pos  Pos
}

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

func (r *Reader) Data() []rune {
	return r.data
}

func (r *Reader) Peek1() (rune, error) {
	if r.pos.Index < len(r.data) {
		return r.data[r.pos.Index], nil
	}

	return 0, io.EOF
}

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

func (r *Reader) Read(n int, buf []rune) (int, error) {
	return r.read(n, buf)
}

func (r *Reader) Skip(n int) (int, error) {
	return r.read(n, nil)
}

func (r *Reader) Finished() bool {
	return r.pos.Index >= len(r.data)
}

func (r *Reader) Position() (Pos, error) {
	return r.pos, nil
}

func (r *Reader) SetPosition(p Pos) error {
	if p.Index < 0 || p.Index > len(r.data) {
		return fmt.Errorf("position out of bounds: %v", p)
	}
	r.pos = p
	return nil
}

func (r *Reader) Range(p1 Pos, p2 Pos) ([]rune, error) {
	if p1.Index < 0 || p1.Index >= len(r.data) || p2.Index < 0 || p2.Index > len(r.data) {
		return nil, fmt.Errorf("len(%d) -> position(s) out of bounds: %v - %v", len(r.data), p1, p2)
	}

	return r.data[p1.Index:p2.Index], nil
}

func (r *Reader) Length(p1 Pos, p2 Pos) int {
	return p2.Index - p1.Index
}
