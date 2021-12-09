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
	s := NewTestStream()
	sem := NewSingleEntityMatch("match even ints", func(entity interface{}) bool { return entity.(int)%2 == 0 })
	cm := NewConcatenationMatch("match 5, 6", []PatternMatcher{
		NewSingleEntityMatch("5", func(entity interface{}) bool { return entity.(int) == 5 }),
		NewSingleEntityMatch("6", func(entity interface{}) bool { return entity.(int) == 6 }),
	})
	var m PatternMatcher = sem

	t.Logf("%v", m.Identifier())

	for !s.Finished() {
		match, result := m.Match(s)
		if match {
			t.Logf("match %v - %v", result.StartPosition, result.EndPosition)
		}
	}

	m = cm

	s.SetPosition(0)

	t.Logf("%v", m.Identifier())

	for !s.Finished() {
		match, result := m.Match(s)
		if match {
			t.Logf("match %v - %v", result.StartPosition, result.EndPosition)
		}
	}

	t.Log("check")
}
