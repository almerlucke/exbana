package exbana

import (
	"testing"
	"unicode"
)

type TestStream struct {
	values     []rune
	pos        int
	mismatches []*Mismatch
}

func NewTestStream() *TestStream {
	return &TestStream{
		values: []rune("testing 123"),
		pos:    0,
	}
}

func (ts *TestStream) Peek() (Entity, error) {
	if ts.pos < len(ts.values) {
		return ts.values[ts.pos], nil
	}

	return nil, nil
}

func (ts *TestStream) Read() (Entity, error) {
	if ts.pos < len(ts.values) {
		v := ts.values[ts.pos]
		ts.pos += 1
		return v, nil
	}

	return nil, nil
}

func (ts *TestStream) Finished() bool {
	return ts.pos >= len(ts.values)
}

func (ts *TestStream) Position() Position {
	return ts.pos
}

func (ts *TestStream) SetPosition(pos Position) error {
	ts.pos = pos.(int)
	return nil
}

func (ts *TestStream) Log(mismatch *Mismatch) {
	ts.mismatches = append(ts.mismatches, mismatch)
}

func (ts *TestStream) ValueForRange(begin Position, end Position) Value {
	return string(ts.values[begin.(int):end.(int)])
}

func TestExbana(t *testing.T) {
	s := NewTestStream()
	isDigit := func(entity Entity) bool { return unicode.IsDigit(entity.(rune)) }

	sem := NewEntityMatch("is_digit", false, isDigit)

	cm := NewConcatenationMatch("match 3 digits?", false, []Matcher{sem, sem, sem})

	for !s.Finished() {
		matched, result, _ := sem.Match(s, s)
		if matched {
			t.Logf("%v %v - %v - %v", result.Identifier, result.Begin, result.End, result.Value)
		}
	}

	s.SetPosition(0)

	for !s.Finished() {
		matched, result, _ := cm.Match(s, s)
		if matched {
			t.Logf("%v %v - %v - %v", result.Identifier, result.Begin, result.End, result.Value)
		}
	}
}
