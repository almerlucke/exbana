package exbana

import (
	"fmt"
	"testing"
	"unicode"
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

// go test -run TestExbanaEntitySeries -v

func TestExbana(t *testing.T) {
	s := NewTestStream("abaaa")
	isA := NewEntityMatch("is_a", false, func(entity Entity) bool { return entity.(rune) == 'a' })
	isB := NewEntityMatch("is_b", false, func(entity Entity) bool { return entity.(rune) == 'b' })
	altAB := NewAlternationMatch("is_a_or_b", false, []Matcher{isA, isB})
	repAB := NewRepetitionMatch("ab_repeat", true, altAB, 3, 4)

	transformTable := TransformTable{
		"is_a_or_b": func(m *MatchResult, t TransformTable) Value {
			return t.Transform(m.Value.(*MatchResult))
		},
		"ab_repeat": func(m *MatchResult, t TransformTable) Value {
			results := m.Value.([]*MatchResult)

			str := ""

			for _, r := range results {
				str += t.Transform(r).(string)
			}

			return str
		},
	}

	matched, result, _ := repAB.Match(s, s)
	if matched {
		t.Logf("%v", transformTable.Transform(result))
	}

	for _, mismatch := range s.mismatches {
		fmt.Printf("mismatch %v %v %v %v", mismatch.Identifier, mismatch.Begin, mismatch.End, mismatch.Error)
	}

	// s.SetPosition(0)

	// for !s.Finished() {
	// 	matched, result, _ := cm.Match(s, s)
	// 	if matched {
	// 		t.Logf("%v %v - %v - %v", result.Identifier, result.Begin, result.End, result.Value)
	// 	}
	// }
}

func runeEntityEqual(e1 Entity, e2 Entity) bool {
	return e1.(rune) == e2.(rune)
}

func stringToEntitySeries(str string) []Entity {
	entities := []Entity{}

	for _, r := range str {
		entities = append(entities, r)
	}

	return entities
}

// go test -run TestExbanaEntitySeries -v

func TestExbanaEntitySeries(t *testing.T) {
	s := NewTestStream("hallr")
	isHallo := NewEntitySeriesMatch("hallo", true, stringToEntitySeries("hallo"), runeEntityEqual)

	transformTable := TransformTable{}

	matched, result, _ := isHallo.Match(s, s)
	if matched {
		t.Logf("%v", transformTable.Transform(result))
	}

	for _, mismatch := range s.mismatches {
		fmt.Printf("mismatch %v %v %v %v\n", mismatch.Identifier, mismatch.Begin, mismatch.End, mismatch.Error)
	}
}

func TestExbanaException(t *testing.T) {
	s := NewTestStream("123457")
	isDigit := NewEntityMatch("isLetter", false, func(entity Entity) bool { return unicode.IsDigit(entity.(rune)) })
	isSix := NewEntityMatch("isSix", false, func(entity Entity) bool { return entity.(rune) == '6' })
	isDigitExceptSix := NewExceptionMatch("isDigitExceptSix", false, isDigit, isSix)
	allDigitsExceptSix := NewRepetitionMatch("allDigitsExceptSix", false, isDigitExceptSix, 1, 0)
	endOfStream := NewEndOfStreamMatch("endOfStream", false)
	allDigitsExceptSixTillTheEnd := NewConcatenationMatch("allDigitsExceptSixTillTheEnd", true, []Matcher{allDigitsExceptSix, endOfStream})

	transformTable := TransformTable{
		"allDigitsExceptSix": func(result *MatchResult, table TransformTable) Value {
			str := ""
			for _, r := range result.Value.([]*MatchResult) {
				str += r.Value.(string)
			}
			return str
		},
		"allDigitsExceptSixTillTheEnd": func(result *MatchResult, table TransformTable) Value {
			return table.Transform(result.Value.([]*MatchResult)[0])
		},
	}

	matched, result, _ := allDigitsExceptSixTillTheEnd.Match(s, s)
	if matched {
		t.Logf("%v", transformTable.Transform(result))
	}

	for _, mismatch := range s.mismatches {
		t.Logf("mismatch %v %v %v %v\n", mismatch.Identifier, mismatch.Begin, mismatch.End, mismatch.Error)
	}
}
