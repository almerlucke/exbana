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

func runeEntityEqual(e1 Entity, e2 Entity) bool {
	return (e1 != nil) && (e2 != nil) && (e1.(rune) == e2.(rune))
}

func stringToEntitySeries(str string) []Entity {
	entities := []Entity{}

	for _, r := range str {
		entities = append(entities, r)
	}

	return entities
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

	runeMatch := func(r rune) EntityMatchFunction {
		return func(e Entity) bool {
			return e != nil && e.(rune) == r
		}
	}

	runeFuncMatch := func(rf func(rune) bool) EntityMatchFunction {
		return func(e Entity) bool {
			return e != nil && rf(e.(rune))
		}
	}

	minus := NewEntityMatch("minus", false, runeMatch('-'))
	doubleQuote := NewEntityMatch("doubleQuote", false, runeMatch('"'))
	assignSymbol := NewEntitySeriesMatch("assignSymbol", true, stringToEntitySeries(":="), runeEntityEqual)
	semiColon := NewEntityMatch("semiColon", false, runeMatch(';'))
	allCharacters := NewEntityMatch("allCharacters", false, runeFuncMatch(unicode.IsGraphic))
	allButDoubleQuote := NewExceptionMatch("allButDoubleQuote", false, allCharacters, doubleQuote)
	stringValue := NewConcatenationMatch("string", true, []Matcher{doubleQuote, NewAnyMatch("stringContent", false, allButDoubleQuote), doubleQuote})
	whiteSpace := NewEntityMatch("whiteSpace", false, runeFuncMatch(unicode.IsSpace))
	atLeastOneWhiteSpace := NewRepetitionMatch("atLeastOneWhiteSpace", false, whiteSpace, 1, 0)
	digit := NewEntityMatch("digit", false, runeFuncMatch(unicode.IsDigit))
	anyDigit := NewAnyMatch("anyDigit", false, digit)
	alphabeticCharacter := NewEntityMatch("alphabeticCharacter", false, runeFuncMatch(func(r rune) bool { return unicode.IsUpper(r) && unicode.IsLetter(r) }))
	anyAlnum := NewAnyMatch("anyAlnum", false, NewAlternationMatch("aa1", false, []Matcher{alphabeticCharacter, digit}))
	identifier := NewConcatenationMatch("identifier", false, []Matcher{alphabeticCharacter, anyAlnum})
	number := NewConcatenationMatch("number", false, []Matcher{NewOptionalMatch("optMinus", false, minus), digit, anyDigit})
	assignmentRightSide := NewAlternationMatch("assignmentRightSide", false, []Matcher{number, identifier, stringValue})
	assignment := NewConcatenationMatch("assignment", false, []Matcher{identifier, assignSymbol, assignmentRightSide})
	programTerminal := NewEntitySeriesMatch("programTerminal", false, stringToEntitySeries("PROGRAM"), runeEntityEqual)
	beginTerminal := NewEntitySeriesMatch("beginTerminal", false, stringToEntitySeries("BEGIN"), runeEntityEqual)
	endTerminal := NewEntitySeriesMatch("endTerminal", false, stringToEntitySeries("END"), runeEntityEqual)
	assignmentsInternal := NewConcatenationMatch("assignmentsInternal", false, []Matcher{assignment, semiColon, atLeastOneWhiteSpace})
	assignments := NewAnyMatch("assignments", false, assignmentsInternal)
	program := NewConcatenationMatch("program", true, []Matcher{
		programTerminal, atLeastOneWhiteSpace, identifier, atLeastOneWhiteSpace, beginTerminal, atLeastOneWhiteSpace, assignments, endTerminal,
	})

	transformTable := TransformTable{
		"assignment": func(result *MatchResult, table TransformTable) Value {
			elements := result.Value.([]*MatchResult)

			leftSide := table.Transform(elements[0]).(*ProgramValue)
			rightSide := table.Transform(elements[2].Value.(*MatchResult)).(*ProgramValue)

			return &ProgramAssignment{LeftSide: leftSide, RightSide: rightSide}
		},
		"number": func(result *MatchResult, table TransformTable) Value {
			elements := result.Value.([]*MatchResult)
			numContent := ""

			if len(elements[0].Value.([]*MatchResult)) > 0 {
				numContent += "-"
			}

			numContent += elements[1].Value.(string)

			for _, numChr := range elements[2].Value.([]*MatchResult) {
				numContent += numChr.Value.(string)
			}

			return &ProgramValue{Content: numContent, Type: ProgramValueTypeNumber}
		},
		"string": func(result *MatchResult, table TransformTable) Value {
			elements := result.Value.([]*MatchResult)
			stringContent := ""

			for _, strChr := range elements[1].Value.([]*MatchResult) {
				stringContent += strChr.Value.(string)
			}

			return &ProgramValue{Content: stringContent, Type: ProgramValueTypeString}
		},
		"identifier": func(result *MatchResult, table TransformTable) Value {
			elements := result.Value.([]*MatchResult)
			// First character
			idContent := elements[0].Value.(string)
			// Rest of characters
			for _, alnum := range elements[1].Value.([]*MatchResult) {
				idContent += alnum.Value.(*MatchResult).Value.(string)
			}

			return &ProgramValue{Content: idContent, Type: ProgramValueTypeIdentifier}
		},
		"program": func(result *MatchResult, table TransformTable) Value {
			elements := result.Value.([]*MatchResult)
			name := table.Transform(elements[2]).(*ProgramValue)

			assignments := []*ProgramAssignment{}

			rawAssignments := elements[6].Value.([]*MatchResult)

			for _, rawAssignment := range rawAssignments {
				assignment := table.Transform(rawAssignment.Value.([]*MatchResult)[0]).(*ProgramAssignment)
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
			t.Logf("mismatch %v %v %v %v\n", mismatch.Identifier, mismatch.Begin, mismatch.End, mismatch.Error)
		}
	}

}
