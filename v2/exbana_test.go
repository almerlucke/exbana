package exbana

import (
	"fmt"
	"math/rand"
	"testing"
	"unicode"
)

type RuneStream struct {
	values     []rune
	pos        int
	mismatches []*Mismatch[rune, int]
}

func NewRuneStream(str string) *RuneStream {
	return &RuneStream{
		values: []rune(str),
		pos:    0,
	}
}

func (ts *RuneStream) Peek1() (rune, error) {
	if ts.pos < len(ts.values) {
		return ts.values[ts.pos], nil
	}

	return 0, nil
}

func (ts *RuneStream) Peek(n int, buf []rune) (int, error) {
	i := 0
	p := ts.pos
	l := len(ts.values)

	for i < n && p < l {
		buf[i] = ts.values[p]
		p++
		i++
	}

	return i, nil
}

func (ts *RuneStream) Read1() (rune, error) {
	if ts.pos < len(ts.values) {
		v := ts.values[ts.pos]
		ts.pos++
		return v, nil
	}

	return 0, nil
}

func (ts *RuneStream) Read(n int, buf []rune) (int, error) {
	i := 0
	l := len(ts.values)

	for i < n && ts.pos < l {
		buf[i] = ts.values[ts.pos]
		ts.pos++
		i++
	}

	return i, nil
}

func (ts *RuneStream) Skip(n int) int {
	m := len(ts.values) - ts.pos
	if n > m {
		n = m
	}

	ts.pos += n

	return n
}

func (ts *RuneStream) Finished() bool {
	return ts.pos >= len(ts.values)
}

func (ts *RuneStream) Position() int {
	return ts.pos
}

func (ts *RuneStream) SetPosition(pos int) error {
	ts.pos = pos
	return nil
}

func (ts *RuneStream) Log(mismatch *Mismatch[rune, int]) {
	ts.mismatches = append(ts.mismatches, mismatch)
}

func (ts *RuneStream) Range(begin int, end int) []rune {
	return ts.values[begin:end]
}

func (ts *RuneStream) Write(objs ...rune) error {
	for _, obj := range objs {
		ts.values = append(ts.values, obj)
	}

	return nil
}

func (ts *RuneStream) Runes() []rune {
	return ts.values
}

func (ts *RuneStream) Finish() error {
	return nil
}

func runeEntityEqual(o1 rune, o2 rune) bool {
	return o1 == o2
}

func runeMatch(r rune) *UnitPattern[rune, int] {
	return Unit[rune, int](func(obj rune) bool {
		return obj == r
	})
}

func runeFuncMatch(rf func(rune) bool) *UnitPattern[rune, int] {
	return Unit[rune, int](func(obj rune) bool {
		return rf(obj)
	})
}

func runeFuncMatchx(id string, rf func(rune) bool) *UnitPattern[rune, int] {
	return Unitx[rune, int](id, false, func(obj rune) bool {
		return rf(obj)
	})
}

func runeSeries(str string) *SeriesPattern[rune, int] {
	return Series[rune, int](runeEntityEqual, []rune(str)...)
}

// go test -run TestExbanaEntitySeries -v

func randomRuneFunc(str string) func() rune {
	runes := []rune(str)
	return func() rune { return runes[rand.Intn(len(runes))] }
}

func TestPrint(t *testing.T) {
	zero := runeMatch('0')
	zero.PrintOutput = "[0]"
	digit := runeFuncMatchx("digit", unicode.IsDigit)
	digit.PrintOutput = "[0-9]"
	alphabeticCharacter := runeFuncMatchx("alphachar", func(r rune) bool { return unicode.IsUpper(r) && unicode.IsLetter(r) })
	alphabeticCharacter.PrintOutput = "[A-Z]"
	anyAlnum := Any[rune, int](Alt[rune, int](alphabeticCharacter, digit))
	identifier := Concatx[rune, int]("identifier", false, alphabeticCharacter, anyAlnum)
	digitMinusZero := Exceptx[rune, int]("digit_minus_zero", false, digit, zero)

	output, err := PrintRules([]Pattern[rune, int]{
		digit,
		alphabeticCharacter,
		identifier,
		digitMinusZero,
	})

	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	t.Log(output)
}

// func TestGenerate(t *testing.T) {
// 	rand.Seed(time.Now().UnixNano())

// 	wr := NewTestStream("")

// 	minus := runeMatch('-')
// 	minus.(*UnitPattern).GenerateFunc = func() Object { return '-' }
// 	doubleQuote := runeMatch('"')
// 	doubleQuote.(*UnitPattern).GenerateFunc = func() Object { return '"' }
// 	assignSymbol := runeSeries(":=")
// 	semiColon := runeMatch(';')
// 	semiColon.(*UnitPattern).GenerateFunc = func() Object { return ';' }
// 	allCharacters := runeFuncMatch(unicode.IsGraphic)
// 	allCharacters.(*UnitPattern).GenerateFunc = randomRuneFunc("456$#@agsg")
// 	allButDoubleQuote := Except(allCharacters, doubleQuote)
// 	stringValue := Concatx("string", true, doubleQuote, Any(allButDoubleQuote), doubleQuote)
// 	whiteSpace := runeFuncMatch(unicode.IsSpace)
// 	whiteSpace.(*UnitPattern).GenerateFunc = randomRuneFunc(" ")
// 	atLeastOneWhiteSpace := Rep(whiteSpace, 1, 0)
// 	atLeastOneWhiteSpace.MaxGen = 0
// 	digit := runeFuncMatch(unicode.IsDigit)
// 	digit.(*UnitPattern).GenerateFunc = randomRuneFunc("1234567890")
// 	anyDigit := Any(digit)
// 	alphabeticCharacter := runeFuncMatch(func(r rune) bool { return unicode.IsUpper(r) && unicode.IsLetter(r) })
// 	alphabeticCharacter.(*UnitPattern).GenerateFunc = randomRuneFunc("ABCDEFGHIJKLMNOPQRSTUVWXYZ")
// 	anyAlnum := Any(Alt(alphabeticCharacter, digit))
// 	identifier := Concatx("identifier", false, alphabeticCharacter, anyAlnum)
// 	number := Concatx("number", false, Opt(minus), digit, anyDigit)
// 	assignmentRightSide := Alt(number, identifier, stringValue)
// 	assignment := Concatx("assignment", false, identifier, assignSymbol, assignmentRightSide)
// 	programTerminal := runeSeries("PROGRAM")
// 	beginTerminal := runeSeries("BEGIN")
// 	endTerminal := runeSeries("END")
// 	assignmentsInternal := Concat(assignment, semiColon, atLeastOneWhiteSpace)
// 	assignments := Any(assignmentsInternal)
// 	program := Concatx("program", true,
// 		programTerminal, atLeastOneWhiteSpace, identifier, atLeastOneWhiteSpace, beginTerminal, atLeastOneWhiteSpace, assignments, endTerminal,
// 	)

// 	program.Generate(wr)

// 	t.Logf("%v", string(wr.Runes()))
// }

func TestScan(t *testing.T) {
	s := NewRuneStream("testing {hallo}hallo this :330ehallo")
	hallo := Concat[rune, int](runeMatch('{'), runeSeries("hallo"), runeMatch('}'))

	results, err := Scan[rune, int](s, hallo)
	if err != nil {
		t.Errorf("err %v", err)
		t.FailNow()
	}

	for _, result := range results {
		t.Logf("result %v", string(result.Components[1].Value.([]rune)))
	}
}

func TestExbana(t *testing.T) {
	s := NewRuneStream("abaaa")
	isA := Unitx[rune, int]("is_a", false, func(obj rune) bool { return obj == 'a' })
	isB := Unitx[rune, int]("is_b", false, func(obj rune) bool { return obj == 'b' })
	altAB := Altx[rune, int]("is_a_or_b", false, isA, isB)
	repAB := Repx[rune, int]("ab_repeat", true, altAB, 3, 4)

	transformTable := TransformTable[rune, int]{
		"is_a_or_b": func(m *Result[rune, int], t TransformTable[rune, int], s ObjectReader[rune, int]) any {
			return t.Transform(m.Components[0], s)
		},
		"ab_repeat": func(m *Result[rune, int], t TransformTable[rune, int], s ObjectReader[rune, int]) any {
			results := m.Components

			str := ""

			for _, r := range results {
				str += string(t.Transform(r, s).([]rune))
			}

			return str
		},
	}

	matched, result, _ := repAB.Match(s, s)
	if matched {
		t.Logf("%v", result.Transform(transformTable, s))
	}

	for _, mismatch := range s.mismatches {
		fmt.Printf("mismatch %v %v %v", mismatch.Pattern.ID(), mismatch.Begin, mismatch.End)
	}
}

// // go test -run TestExbanaEntitySeries -v

func TestExbanaEntitySeries(t *testing.T) {
	s := NewRuneStream("hallo")
	isHallo := Seriesx[rune, int]("hallo", true, runeEntityEqual, []rune("hallo")...)

	transformTable := TransformTable[rune, int]{}

	matched, result, _ := isHallo.Match(s, s)
	if matched {
		t.Logf("%v", string(transformTable.Transform(result, s).([]rune)))
	}

	for _, mismatch := range s.mismatches {
		fmt.Printf("mismatch %v %v %v\n", mismatch.Pattern.ID(), mismatch.Begin, mismatch.End)
	}
}

// func TestExbanaException(t *testing.T) {
// 	s := NewTestStream("123457")
// 	isDigit := Unitx("isLetter", false, func(obj Object) bool { return unicode.IsDigit(obj.(rune)) })
// 	isSix := Unitx("isSix", false, func(obj Object) bool { return obj.(rune) == '6' })
// 	isDigitExceptSix := Exceptx("isDigitExceptSix", false, isDigit, isSix)
// 	allDigitsExceptSix := Repx("allDigitsExceptSix", false, isDigitExceptSix, 1, 0)
// 	endOfStream := Endx("endOfStream", false)
// 	allDigitsExceptSixTillTheEnd := Concatx("allDigitsExceptSixTillTheEnd", true, allDigitsExceptSix, endOfStream)

// 	transformTable := TransformTable{
// 		"allDigitsExceptSix": func(result *Result, table TransformTable, s ObjectReader) Value {
// 			str := ""
// 			for _, r := range result.Value.([]*Result) {
// 				str += r.Value.(string)
// 			}
// 			return str
// 		},
// 		"allDigitsExceptSixTillTheEnd": func(result *Result, table TransformTable, s ObjectReader) Value {
// 			return table.Transform(result.Value.([]*Result)[0], s)
// 		},
// 	}

// 	matched, result, _ := allDigitsExceptSixTillTheEnd.Match(s, s)
// 	if matched {
// 		t.Logf("%v", transformTable.Transform(result, s))
// 	}

// 	for _, mismatch := range s.mismatches {
// 		t.Logf("mismatch %v %v %v %v\n", mismatch.ID, mismatch.Begin, mismatch.End, mismatch.Error)
// 	}
// }

// type ProgramValueType int

// const (
// 	ProgramValueTypeString ProgramValueType = iota
// 	ProgramValueTypeNumber
// 	ProgramValueTypeIdentifier
// )

// type ProgramValue struct {
// 	Content string
// 	Type    ProgramValueType
// }

// type ProgramAssignment struct {
// 	LeftSide  *ProgramValue
// 	RightSide *ProgramValue
// }

// type Program struct {
// 	Name        *ProgramValue
// 	Assignments []*ProgramAssignment
// }

// func TestExbanaProgram(t *testing.T) {
// 	s := NewTestStream(`PROGRAM DEMO1
// 	BEGIN
// 		A:=3;
// 		B:=45;
// 		H:=-100023;
// 		C:=A;
// 		D123:=B34A;
// 		BABOON:=GIRAFFE;
// 		TEXT:="Hello world!";
// 	END`)

// 	minus := runeMatch('-')
// 	doubleQuote := runeMatch('"')
// 	assignSymbol := runeSeries(":=")
// 	semiColon := runeMatch(';')
// 	allCharacters := runeFuncMatch(unicode.IsGraphic)
// 	allButDoubleQuote := Except(allCharacters, doubleQuote)
// 	stringValue := Concatx("string", true, doubleQuote, Any(allButDoubleQuote), doubleQuote)
// 	whiteSpace := runeFuncMatch(unicode.IsSpace)
// 	atLeastOneWhiteSpace := Rep(whiteSpace, 1, 0)
// 	digit := runeFuncMatch(unicode.IsDigit)
// 	anyDigit := Any(digit)
// 	alphabeticCharacter := runeFuncMatch(func(r rune) bool { return unicode.IsUpper(r) && unicode.IsLetter(r) })
// 	anyAlnum := Any(Alt(alphabeticCharacter, digit))
// 	identifier := Concatx("identifier", false, alphabeticCharacter, anyAlnum)
// 	number := Concatx("number", false, Opt(minus), digit, anyDigit)
// 	assignmentRightSide := Alt(number, identifier, stringValue)
// 	assignment := Concatx("assignment", false, identifier, assignSymbol, assignmentRightSide)
// 	programTerminal := runeSeries("PROGRAM")
// 	beginTerminal := runeSeries("BEGIN")
// 	endTerminal := runeSeries("END")
// 	assignmentsInternal := Concat(assignment, semiColon, atLeastOneWhiteSpace)
// 	assignments := Any(assignmentsInternal)
// 	program := Concatx("program", true,
// 		programTerminal, atLeastOneWhiteSpace, identifier, atLeastOneWhiteSpace, beginTerminal, atLeastOneWhiteSpace, assignments, endTerminal,
// 	)

// 	transformTable := TransformTable{
// 		"assignment": func(result *Result, table TransformTable, stream ObjectReader) Value {
// 			elements := result.Value.([]*Result)

// 			leftSide := table.Transform(elements[0], stream).(*ProgramValue)
// 			rightSide := table.Transform(elements[2].Value.(*Result), stream).(*ProgramValue)

// 			return &ProgramAssignment{LeftSide: leftSide, RightSide: rightSide}
// 		},
// 		"number": func(result *Result, table TransformTable, stream ObjectReader) Value {
// 			elements := result.Value.([]*Result)
// 			numContent := ""

// 			if len(elements[0].Value.([]*Result)) > 0 {
// 				numContent += "-"
// 			}

// 			numContent += elements[1].Value.(string)

// 			for _, numChr := range elements[2].Value.([]*Result) {
// 				numContent += numChr.Value.(string)
// 			}

// 			return &ProgramValue{Content: numContent, Type: ProgramValueTypeNumber}
// 		},
// 		"string": func(result *Result, table TransformTable, stream ObjectReader) Value {
// 			elements := result.Value.([]*Result)
// 			stringContent := ""

// 			for _, strChr := range elements[1].Value.([]*Result) {
// 				stringContent += strChr.Value.(string)
// 			}

// 			return &ProgramValue{Content: stringContent, Type: ProgramValueTypeString}
// 		},
// 		"identifier": func(result *Result, table TransformTable, stream ObjectReader) Value {
// 			elements := result.Value.([]*Result)
// 			// First character
// 			idContent := elements[0].Value.(string)
// 			// Rest of characters
// 			for _, alnum := range elements[1].Value.([]*Result) {
// 				idContent += alnum.Value.(*Result).Value.(string)
// 			}

// 			return &ProgramValue{Content: idContent, Type: ProgramValueTypeIdentifier}
// 		},
// 		"program": func(result *Result, table TransformTable, stream ObjectReader) Value {
// 			elements := result.Value.([]*Result)
// 			name := table.Transform(elements[2], stream).(*ProgramValue)

// 			assignments := []*ProgramAssignment{}

// 			rawAssignments := elements[6].Value.([]*Result)

// 			for _, rawAssignment := range rawAssignments {
// 				assignment := table.Transform(rawAssignment.Value.([]*Result)[0], stream).(*ProgramAssignment)
// 				assignments = append(assignments, assignment)
// 			}

// 			return &Program{
// 				Name:        name,
// 				Assignments: assignments,
// 			}
// 		},
// 	}

// 	matched, result, _ := program.Match(s, s)
// 	if matched {
// 		program := transformTable.Transform(result, s).(*Program)
// 		t.Logf("Program %v", program.Name.Content)
// 		for _, assignment := range program.Assignments {
// 			t.Logf("Assignment: %v = %v", assignment.LeftSide.Content, assignment.RightSide.Content)
// 		}
// 	} else {
// 		for _, mismatch := range s.mismatches {
// 			t.Logf("mismatch %v %v %v %v\n", mismatch.ID, mismatch.Begin, mismatch.End, mismatch.Error)
// 		}
// 	}

// }
