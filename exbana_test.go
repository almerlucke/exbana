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

func (ts *TestStream) Peek() (Object, error) {
	if ts.pos < len(ts.values) {
		return ts.values[ts.pos], nil
	}

	return nil, nil
}

func (ts *TestStream) Read() (Object, error) {
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

func runeEntityEqual(o1 Object, o2 Object) bool {
	return (o1 != nil) && (o2 != nil) && (o1.(rune) == o2.(rune))
}

// go test -run TestExbanaEntitySeries -v

func TestExbana(t *testing.T) {
	s := NewTestStream("abaaa")
	isA := Unitx("is_a", false, func(obj Object) bool { return obj.(rune) == 'a' })
	isB := Unitx("is_b", false, func(obj Object) bool { return obj.(rune) == 'b' })
	altAB := Altx("is_a_or_b", false, isA, isB)
	repAB := Repx("ab_repeat", true, altAB, 3, 4)

	transformTable := TransformTable{
		"is_a_or_b": func(m *Result, t TransformTable, s ObjectStreamer) Value {
			return t.Transform(m.Value.(*Result), s)
		},
		"ab_repeat": func(m *Result, t TransformTable, s ObjectStreamer) Value {
			results := m.Value.([]*Result)

			str := ""

			for _, r := range results {
				str += t.Transform(r, s).(string)
			}

			return str
		},
	}

	matched, result, _ := repAB.Match(s, s)
	if matched {
		t.Logf("%v", transformTable.Transform(result, s))
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

func stringToSeries(str string) []Object {
	entities := []Object{}

	for _, r := range str {
		entities = append(entities, r)
	}

	return entities
}

func TestExbanaEntitySeries(t *testing.T) {
	s := NewTestStream("hallr")
	isHallo := Seriesx("hallo", true, runeEntityEqual, stringToSeries("hallo")...)

	transformTable := TransformTable{}

	matched, result, _ := isHallo.Match(s, s)
	if matched {
		t.Logf("%v", transformTable.Transform(result, s))
	}

	for _, mismatch := range s.mismatches {
		fmt.Printf("mismatch %v %v %v %v\n", mismatch.ID, mismatch.Begin, mismatch.End, mismatch.Error)
	}
}

func TestExbanaException(t *testing.T) {
	s := NewTestStream("123457")
	isDigit := Unitx("isLetter", false, func(obj Object) bool { return unicode.IsDigit(obj.(rune)) })
	isSix := Unitx("isSix", false, func(obj Object) bool { return obj.(rune) == '6' })
	isDigitExceptSix := Exceptx("isDigitExceptSix", false, isDigit, isSix)
	allDigitsExceptSix := Repx("allDigitsExceptSix", false, isDigitExceptSix, 1, 0)
	endOfStream := Endx("endOfStream", false)
	allDigitsExceptSixTillTheEnd := Concatx("allDigitsExceptSixTillTheEnd", true, allDigitsExceptSix, endOfStream)

	transformTable := TransformTable{
		"allDigitsExceptSix": func(result *Result, table TransformTable, s ObjectStreamer) Value {
			str := ""
			for _, r := range result.Value.([]*Result) {
				str += r.Value.(string)
			}
			return str
		},
		"allDigitsExceptSixTillTheEnd": func(result *Result, table TransformTable, s ObjectStreamer) Value {
			return table.Transform(result.Value.([]*Result)[0], s)
		},
	}

	matched, result, _ := allDigitsExceptSixTillTheEnd.Match(s, s)
	if matched {
		t.Logf("%v", transformTable.Transform(result, s))
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

	runeMatch := func(r rune) Pattern {
		return Unit(func(obj Object) bool {
			return obj != nil && obj.(rune) == r
		})
	}

	runeFuncMatch := func(rf func(rune) bool) Pattern {
		return Unit(func(obj Object) bool {
			return obj != nil && rf(obj.(rune))
		})
	}

	runeSeries := func(str string) Pattern {
		return Series(runeEntityEqual, stringToSeries(str)...)
	}

	minus := runeMatch('-')
	doubleQuote := runeMatch('"')
	assignSymbol := runeSeries(":=")
	semiColon := runeMatch(';')
	allCharacters := runeFuncMatch(unicode.IsGraphic)
	allButDoubleQuote := Except(allCharacters, doubleQuote)
	stringValue := Concatx("string", true, doubleQuote, Any(allButDoubleQuote), doubleQuote)
	whiteSpace := runeFuncMatch(unicode.IsSpace)
	atLeastOneWhiteSpace := Rep(whiteSpace, 1, 0)
	digit := runeFuncMatch(unicode.IsDigit)
	anyDigit := Any(digit)
	alphabeticCharacter := runeFuncMatch(func(r rune) bool { return unicode.IsUpper(r) && unicode.IsLetter(r) })
	anyAlnum := Any(Alt(alphabeticCharacter, digit))
	identifier := Concatx("identifier", false, alphabeticCharacter, anyAlnum)
	number := Concatx("number", false, Opt(minus), digit, anyDigit)
	assignmentRightSide := Alt(number, identifier, stringValue)
	assignment := Concatx("assignment", false, identifier, assignSymbol, assignmentRightSide)
	programTerminal := runeSeries("PROGRAM")
	beginTerminal := runeSeries("BEGIN")
	endTerminal := runeSeries("END")
	assignmentsInternal := Concat(assignment, semiColon, atLeastOneWhiteSpace)
	assignments := Any(assignmentsInternal)
	program := Concatx("program", true,
		programTerminal, atLeastOneWhiteSpace, identifier, atLeastOneWhiteSpace, beginTerminal, atLeastOneWhiteSpace, assignments, endTerminal,
	)

	transformTable := TransformTable{
		"assignment": func(result *Result, table TransformTable, stream ObjectStreamer) Value {
			elements := result.Value.([]*Result)

			leftSide := table.Transform(elements[0], stream).(*ProgramValue)
			rightSide := table.Transform(elements[2].Value.(*Result), stream).(*ProgramValue)

			return &ProgramAssignment{LeftSide: leftSide, RightSide: rightSide}
		},
		"number": func(result *Result, table TransformTable, stream ObjectStreamer) Value {
			elements := result.Value.([]*Result)
			numContent := ""

			if len(elements[0].Value.([]*Result)) > 0 {
				numContent += "-"
			}

			numContent += elements[1].Value.(string)

			for _, numChr := range elements[2].Value.([]*Result) {
				numContent += numChr.Value.(string)
			}

			return &ProgramValue{Content: numContent, Type: ProgramValueTypeNumber}
		},
		"string": func(result *Result, table TransformTable, stream ObjectStreamer) Value {
			elements := result.Value.([]*Result)
			stringContent := ""

			for _, strChr := range elements[1].Value.([]*Result) {
				stringContent += strChr.Value.(string)
			}

			return &ProgramValue{Content: stringContent, Type: ProgramValueTypeString}
		},
		"identifier": func(result *Result, table TransformTable, stream ObjectStreamer) Value {
			elements := result.Value.([]*Result)
			// First character
			idContent := elements[0].Value.(string)
			// Rest of characters
			for _, alnum := range elements[1].Value.([]*Result) {
				idContent += alnum.Value.(*Result).Value.(string)
			}

			return &ProgramValue{Content: idContent, Type: ProgramValueTypeIdentifier}
		},
		"program": func(result *Result, table TransformTable, stream ObjectStreamer) Value {
			elements := result.Value.([]*Result)
			name := table.Transform(elements[2], stream).(*ProgramValue)

			assignments := []*ProgramAssignment{}

			rawAssignments := elements[6].Value.([]*Result)

			for _, rawAssignment := range rawAssignments {
				assignment := table.Transform(rawAssignment.Value.([]*Result)[0], stream).(*ProgramAssignment)
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
		program := transformTable.Transform(result, s).(*Program)
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
