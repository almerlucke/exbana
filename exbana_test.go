package exbana

import (
	"testing"
)

type TestStream struct {
	values []int
	pos    int
}

func NewTestStream() *TestStream {
	return &TestStream{
		values: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		pos:    0,
	}
}

func (ts *TestStream) Peek() interface{} {
	if ts.pos < len(ts.values) {
		return ts.values[ts.pos]
	}

	return nil
}

func (ts *TestStream) Read() interface{} {
	if ts.pos < len(ts.values) {
		v := ts.values[ts.pos]
		ts.pos += 1
		return v
	}

	return nil
}

func (ts *TestStream) Finished() bool {
	return ts.pos >= len(ts.values)
}

func (ts *TestStream) Position() int {
	return ts.pos
}

func (ts *TestStream) SetPosition(pos int) {
	ts.pos = pos
}

func TestExbana(t *testing.T) {
	l := NewPatternMismatchLog()
	s := NewTestStream()

	sem := NewSingleEntityMatch("match even ints", func(entity interface{}) bool { return entity.(int)%2 == 0 })

	cm := NewConcatenationMatch("match 5&6", []PatternMatcher{
		NewSingleEntityMatch("5", func(entity interface{}) bool { return entity.(int) == 5 }),
		NewSingleEntityMatch("6", func(entity interface{}) bool { return entity.(int) == 6 }),
	})

	for !s.Finished() {
		match, result := sem.Match(s, l)
		if match {
			t.Logf("%v %v - %v", result.Identifier, result.Range.Start, result.Range.End)
		}
	}

	s.SetPosition(0)

	for !s.Finished() {
		match, result := cm.Match(s, l)
		if match {
			t.Logf("%v %v - %v", result.Identifier, result.Range.Start, result.Range.End)
		}
	}

	t.Log("check")
}
