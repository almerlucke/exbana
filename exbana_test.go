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

func (ts *TestStream) Peek() (Obj, error) {
	if ts.pos < len(ts.values) {
		return ts.values[ts.pos], nil
	}

	return nil, nil
}

func (ts *TestStream) Read() (Obj, error) {
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

func (ts *TestStream) Pos() Pos {
	return ts.pos
}

func (ts *TestStream) SetPos(pos Pos) error {
	ts.pos = pos.(int)
	return nil
}

func (ts *TestStream) Log(mismatch *Mismatch) {
	ts.mismatches = append(ts.mismatches, mismatch)
}

func (ts *TestStream) ValForRange(begin Pos, end Pos) Val {
	return string(ts.values[begin.(int):end.(int)])
}

func runeEntityEqual(e1 Obj, e2 Obj) bool {
	return (e1 != nil) && (e2 != nil) && (e1.(rune) == e2.(rune))
}

func stringToSeries(str string) []Obj {
	entities := []Obj{}

	for _, r := range str {
		entities = append(entities, r)
	}

	return entities
}

// go test -run TestExbanaEntitySeries -v

func TestExbana(t *testing.T) {
	s := NewTestStream("abaaa")
	isA := NewSingleF("is_a", false, func(obj Obj) bool { return obj.(rune) == 'a' })
	isB := NewSingleF("is_b", false, func(obj Obj) bool { return obj.(rune) == 'b' })
	altAB := NewOrF("is_a_or_b", false, []Matcher{isA, isB})
	repAB := NewRepF("ab_repeat", true, altAB, 3, 4)

	transformTable := TransTable{
		"is_a_or_b": func(m *Result, t TransTable) Val {
			return t.Transform(m.Val.(*Result))
		},
		"ab_repeat": func(m *Result, t TransTable) Val {
			results := m.Val.([]*Result)

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
		fmt.Printf("mismatch %v %v %v %v", mismatch.ID, mismatch.Begin, mismatch.End, mismatch.Error)
	}

	// s.SetPosition(0)

	// for !s.Finished() {
	// 	matched, result, _ := cm.Match(s, s)
	// 	if matched {
	// 		t.Logf("%v %v - %v - %v", result.Identifier, result.Begin, result.End, result.Value)
	// 	}
	// }
}

// go test -run TestExbanaEntitySeries -v

func TestExbanaEntitySeries(t *testing.T) {
	s := NewTestStream("hallr")
	isHallo := NewSeriesF("hallo", true, stringToSeries("hallo"), runeEntityEqual)

	transformTable := TransTable{}

	matched, result, _ := isHallo.Match(s, s)
	if matched {
		t.Logf("%v", transformTable.Transform(result))
	}

	for _, mismatch := range s.mismatches {
		fmt.Printf("mismatch %v %v %v %v\n", mismatch.ID, mismatch.Begin, mismatch.End, mismatch.Error)
	}
}

func TestExbanaException(t *testing.T) {
	s := NewTestStream("123457")
	isDigit := NewSingleF("isLetter", false, func(obj Obj) bool { return unicode.IsDigit(obj.(rune)) })
	isSix := NewSingleF("isSix", false, func(obj Obj) bool { return obj.(rune) == '6' })
	isDigitExceptSix := NewExceptF("isDigitExceptSix", false, isDigit, isSix)
	allDigitsExceptSix := NewRepF("allDigitsExceptSix", false, isDigitExceptSix, 1, 0)
	endOfStream := NewEndF("endOfStream", false)
	allDigitsExceptSixTillTheEnd := NewAndF("allDigitsExceptSixTillTheEnd", true, []Matcher{allDigitsExceptSix, endOfStream})

	transformTable := TransTable{
		"allDigitsExceptSix": func(result *Result, table TransTable) Val {
			str := ""
			for _, r := range result.Val.([]*Result) {
				str += r.Val.(string)
			}
			return str
		},
		"allDigitsExceptSixTillTheEnd": func(result *Result, table TransTable) Val {
			return table.Transform(result.Val.([]*Result)[0])
		},
	}

	matched, result, _ := allDigitsExceptSixTillTheEnd.Match(s, s)
	if matched {
		t.Logf("%v", transformTable.Transform(result))
	}

	for _, mismatch := range s.mismatches {
		t.Logf("mismatch %v %v %v %v\n", mismatch.ID, mismatch.Begin, mismatch.End, mismatch.Error)
	}
}

type ProgramValueType int

const (
	ProgramValueTypeString ProgramValueType = iota
	ProgramValueTypeNumber
	ProgramValueTypeIdentifier
)

type ProgramValue struct {
	Content string
	Type    ProgramValueType
}

type ProgramAssignment struct {
	LeftSide  *ProgramValue
	RightSide *ProgramValue
}

type Program struct {
	Name        *ProgramValue
	Assignments []*ProgramAssignment
}

func TestExbanaProgram(t *testing.T) {
	s := NewTestStream(`PROGRAM DEMO1
	BEGIN
		A:=3;
		B:=45;
		H:=-100023;
		C:=A;
		D123:=B34A;
		BABOON:=GIRAFFE;
		TEXT:="Hello world!";
	END`)

	runeMatch := func(r rune) SingleFunc {
		return func(obj Obj) bool {
			return obj != nil && obj.(rune) == r
		}
	}

	runeFuncMatch := func(rf func(rune) bool) SingleFunc {
		return func(obj Obj) bool {
			return obj != nil && rf(obj.(rune))
		}
	}

	minus := NewSingle(runeMatch('-'))
	doubleQuote := NewSingle(runeMatch('"'))
	assignSymbol := NewSeries(stringToSeries(":="), runeEntityEqual)
	semiColon := NewSingle(runeMatch(';'))
	allCharacters := NewSingle(runeFuncMatch(unicode.IsGraphic))
	allButDoubleQuote := NewExcept(allCharacters, doubleQuote)
	stringValue := NewAndF("string", true, []Matcher{doubleQuote, NewAny(allButDoubleQuote), doubleQuote})
	whiteSpace := NewSingle(runeFuncMatch(unicode.IsSpace))
	atLeastOneWhiteSpace := NewRep(whiteSpace, 1, 0)
	digit := NewSingle(runeFuncMatch(unicode.IsDigit))
	anyDigit := NewAny(digit)
	alphabeticCharacter := NewSingle(runeFuncMatch(func(r rune) bool { return unicode.IsUpper(r) && unicode.IsLetter(r) }))
	anyAlnum := NewAny(NewOr([]Matcher{alphabeticCharacter, digit}))
	identifier := NewAndF("identifier", false, []Matcher{alphabeticCharacter, anyAlnum})
	number := NewAndF("number", false, []Matcher{NewOpt(minus), digit, anyDigit})
	assignmentRightSide := NewOr([]Matcher{number, identifier, stringValue})
	assignment := NewAndF("assignment", false, []Matcher{identifier, assignSymbol, assignmentRightSide})
	programTerminal := NewSeries(stringToSeries("PROGRAM"), runeEntityEqual)
	beginTerminal := NewSeries(stringToSeries("BEGIN"), runeEntityEqual)
	endTerminal := NewSeries(stringToSeries("END"), runeEntityEqual)
	assignmentsInternal := NewAnd([]Matcher{assignment, semiColon, atLeastOneWhiteSpace})
	assignments := NewAny(assignmentsInternal)
	program := NewAndF("program", true, []Matcher{
		programTerminal, atLeastOneWhiteSpace, identifier, atLeastOneWhiteSpace, beginTerminal, atLeastOneWhiteSpace, assignments, endTerminal,
	})

	transformTable := TransTable{
		"assignment": func(result *Result, table TransTable) Val {
			elements := result.Val.([]*Result)

			leftSide := table.Transform(elements[0]).(*ProgramValue)
			rightSide := table.Transform(elements[2].Val.(*Result)).(*ProgramValue)

			return &ProgramAssignment{LeftSide: leftSide, RightSide: rightSide}
		},
		"number": func(result *Result, table TransTable) Val {
			elements := result.Val.([]*Result)
			numContent := ""

			if len(elements[0].Val.([]*Result)) > 0 {
				numContent += "-"
			}

			numContent += elements[1].Val.(string)

			for _, numChr := range elements[2].Val.([]*Result) {
				numContent += numChr.Val.(string)
			}

			return &ProgramValue{Content: numContent, Type: ProgramValueTypeNumber}
		},
		"string": func(result *Result, table TransTable) Val {
			elements := result.Val.([]*Result)
			stringContent := ""

			for _, strChr := range elements[1].Val.([]*Result) {
				stringContent += strChr.Val.(string)
			}

			return &ProgramValue{Content: stringContent, Type: ProgramValueTypeString}
		},
		"identifier": func(result *Result, table TransTable) Val {
			elements := result.Val.([]*Result)
			// First character
			idContent := elements[0].Val.(string)
			// Rest of characters
			for _, alnum := range elements[1].Val.([]*Result) {
				idContent += alnum.Val.(*Result).Val.(string)
			}

			return &ProgramValue{Content: idContent, Type: ProgramValueTypeIdentifier}
		},
		"program": func(result *Result, table TransTable) Val {
			elements := result.Val.([]*Result)
			name := table.Transform(elements[2]).(*ProgramValue)

			assignments := []*ProgramAssignment{}

			rawAssignments := elements[6].Val.([]*Result)

			for _, rawAssignment := range rawAssignments {
				assignment := table.Transform(rawAssignment.Val.([]*Result)[0]).(*ProgramAssignment)
				assignments = append(assignments, assignment)
			}

			return &Program{
				Name:        name,
				Assignments: assignments,
			}
		},
	}

	matched, result, _ := program.Match(s, s)
	if matched {
		program := transformTable.Transform(result).(*Program)
		t.Logf("Program %v", program.Name.Content)
		for _, assignment := range program.Assignments {
			t.Logf("Assignment: %v = %v", assignment.LeftSide.Content, assignment.RightSide.Content)
		}
	} else {
		for _, mismatch := range s.mismatches {
			t.Logf("mismatch %v %v %v %v\n", mismatch.ID, mismatch.Begin, mismatch.End, mismatch.Error)
		}
	}

}
