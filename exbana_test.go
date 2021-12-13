package exbana

import (
	"fmt"
	"testing"
)

type TestStream struct {
	values     []rune
	pos        int
	mismatches []*Mismatch
}

func NewTestStream(str string) *TestStream {
	return &TestStream{
		values: []rune(str),
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
	s := NewTestStream("aba")
	isA := NewEntityMatch("is_a", false, func(entity Entity) bool { return entity.(rune) == 'a' })
	isB := NewEntityMatch("is_b", false, func(entity Entity) bool { return entity.(rune) == 'b' })
	altAB := NewAlternationMatch("is_a_or_b", false, []Matcher{isA, isB})
	con3AB := NewConcatenationMatch("ab_3", true, []Matcher{altAB, altAB, altAB})

	transformTable := TransformTable{
		"is_a_or_b": func(m *MatchResult, t TransformTable) Value {
			return t.Transform(m.Value.(*MatchResult))
		},
		"ab_3": func(m *MatchResult, t TransformTable) Value {
			results := m.Value.([]*MatchResult)

			return t.Transform(results[0]).(string) + t.Transform(results[1]).(string) + t.Transform(results[2]).(string)
		},
	}

	matched, result, _ := con3AB.Match(s, s)
	if matched {
		t.Logf("%v", transformTable.Transform(result))
	}

	for _, mismatch := range s.mismatches {
		fmt.Printf("mismatch %v %v %v", mismatch.Identifier, mismatch.Begin, mismatch.End)
	}

	// s.SetPosition(0)

	// for !s.Finished() {
	// 	matched, result, _ := cm.Match(s, s)
	// 	if matched {
	// 		t.Logf("%v %v - %v - %v", result.Identifier, result.Begin, result.End, result.Value)
	// 	}
	// }
}
