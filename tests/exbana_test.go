package tests

import (
	ebnf "github.com/almerlucke/exbana/v2"
	ent "github.com/almerlucke/exbana/v2/patterns/entity"
	rep "github.com/almerlucke/exbana/v2/patterns/repetition"
	vec "github.com/almerlucke/exbana/v2/patterns/vector"
	"github.com/almerlucke/exbana/v2/readers/runes"
	"math/rand"
	"strings"
	"testing"
)

func runeEntityEqual(o1 rune, o2 rune) bool {
	return o1 == o2
}

//func runeMatch(r rune) *UnitPattern[rune, int] {
//	return Unit[rune, int](func(obj rune) bool {
//		return obj == r
//	})
//}
//
//func runeFuncMatch(rf func(rune) bool) *UnitPattern[rune, int] {
//	return Unit[rune, int](func(obj rune) bool {
//		return rf(obj)
//	})
//}
//
//func runeFuncMatchx(id string, rf func(rune) bool) *UnitPattern[rune, int] {
//	return Unitx[rune, int](id, false, func(obj rune) bool {
//		return rf(obj)
//	})
//}

//func runeSeries(str string) *SeriesPattern[rune, int] {
//	return Series[rune, int](runeEntityEqual, []rune(str)...)
//}

// go test -run TestExbanaEntitySeries -v

func randomRuneFunc(str string) func() rune {
	runes := []rune(str)
	return func() rune { return runes[rand.Intn(len(runes))] }
}

//func TestPrint(t *testing.T) {
//	zero := runeMatch('0')
//	zero.PrintOutput = "[0]"
//	digit := runeFuncMatchx("digit", unicode.IsDigit)
//	digit.PrintOutput = "[0-9]"
//	alphabeticCharacter := runeFuncMatchx("alphachar", func(r rune) bool { return unicode.IsUpper(r) && unicode.IsLetter(r) })
//	alphabeticCharacter.PrintOutput = "[A-Z]"
//	anyAlnum := Any[rune, int](Alternation[rune, int](alphabeticCharacter, digit))
//	identifier := Concatx[rune, int]("identifier", false, alphabeticCharacter, anyAlnum)
//	digitMinusZero := Exceptx[rune, int]("digit_minus_zero", false, digit, zero)
//
//	output, err := PrintRules([]Pattern[rune, int]{
//		digit,
//		alphabeticCharacter,
//		identifier,
//		digitMinusZero,
//	})
//
//	if err != nil {
//		t.Error(err)
//		t.FailNow()
//	}
//
//	t.Log(output)
//}

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
// 	anyAlnum := Any(Alternation(alphabeticCharacter, digit))
// 	identifier := Concatx("identifier", false, alphabeticCharacter, anyAlnum)
// 	number := Concatx("number", false, Opt(minus), digit, anyDigit)
// 	assignmentRightSide := Alternation(number, identifier, stringValue)
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

//func TestScan(t *testing.T) {
//	s := NewRuneStream("testing {hallo}hallo this :330ehallo")
//	hallo := Concat[rune, int](runeMatch('{'), runeSeries("hallo"), runeMatch('}'))
//

//

//}

//func evalToString(m *ebnf.Match[rune, int], _ ebnf.Reader[rune, int]) (any, error) {
//	return string(m.Value.([]rune)), nil
//}

func runeEq(o1 rune, o2 rune) bool {
	return o1 == o2
}

func runeMatch(r rune) *ent.Entity[rune, runes.Pos] {
	return ent.New[rune, runes.Pos](func(obj rune) bool {
		return obj == r
	})
}

//
//func runeFuncMatch(rf func(rune) bool) *UnitPattern[rune, int] {
//	return Unit[rune, int](func(obj rune) bool {
//		return rf(obj)
//	})
//}
//
//func runeFuncMatchx(id string, rf func(rune) bool) *UnitPattern[rune, int] {
//	return Unitx[rune, int](id, false, func(obj rune) bool {
//		return rf(obj)
//	})
//}

func runeVector(v []rune) *vec.Vector[rune, runes.Pos] {
	return vec.New[rune, runes.Pos](runeEq, v...)
}

func TestExbana(t *testing.T) {
	r, _ := runes.New(strings.NewReader("test\r\nad1:==333da"))

	// isDigit := entity.New[rune, runes.Pos](unicode.IsDigit)
	//as1 := runeVector([]rune(":="))
	//as2 := runeVector([]rune(":=="))
	//alt := alt.New[rune, runes.Pos](as1, as2).SetID("assign length")
	rpt := rep.New[rune, runes.Pos](runeMatch('3'), 1, 3)

	results, err := ebnf.Scan[rune, runes.Pos](r, rpt)
	if err != nil {
		t.Fatalf("err %v", err)
	}

	for _, result := range results {
		s, _ := r.Range(result.Begin, result.End)
		t.Logf("result %v: %v - pos %d", result.Pattern.ID(), string(s), result.Begin)
	}

	//isA := Unitx[rune, int]("is_a", false, func(obj rune) bool { return obj == 'a' })
	//isB := Unitx[rune, int]("is_b", false, func(obj rune) bool { return obj == 'b' })
	//altAB := Altx[rune, int]("is_a_or_b", false, isA, isB)
	//repAB := Repx[rune, int]("ab_repeat", true, altAB, 3, 4)
	//
	//transformTable := TransformTable[rune, int]{
	//	"is_a_or_b": func(m *Match[rune, int], t TransformTable[rune, int], s Reader[rune, int]) any {
	//		return t.Transform(m.Components[0], s)
	//	},
	//	"ab_repeat": func(m *Match[rune, int], t TransformTable[rune, int], s Reader[rune, int]) any {
	//		results := m.Components
	//
	//		str := ""
	//
	//		for _, r := range results {
	//			str += string(t.Transform(r, s).([]rune))
	//		}
	//
	//		return str
	//	},
	//}
	//
	//matched, result, _ := repAB.Match(s, s)
	//if matched {
	//	t.Logf("%v", result.Transform(transformTable, s))
	//}
	//
	//for _, mismatch := range s.mismatches {
	//	fmt.Printf("mismatch %v %v %v", mismatch.Pattern.ID(), mismatch.Begin, mismatch.End)
	//}
}

// // go test -run TestExbanaEntitySeries -v

//func TestExbanaEntitySeries(t *testing.T) {
//	s := NewRuneStream("hallo")
//	isHallo := Seriesx[rune, int]("hallo", true, runeEntityEqual, []rune("hallo")...)
//
//	transformTable := TransformTable[rune, int]{}
//
//	matched, result, _ := isHallo.Match(s, s)
//	if matched {
//		t.Logf("%v", string(transformTable.Transform(result, s).([]rune)))
//	}
//
//	for _, mismatch := range s.mismatches {
//		fmt.Printf("mismatch %v %v %v\n", mismatch.Pattern.ID(), mismatch.Begin, mismatch.End)
//	}
//}

// func TestExbanaException(t *testing.T) {
// 	s := NewTestStream("123457")
// 	isDigit := Unitx("isLetter", false, func(obj Object) bool { return unicode.IsDigit(obj.(rune)) })
// 	isSix := Unitx("isSix", false, func(obj Object) bool { return obj.(rune) == '6' })
// 	isDigitExceptSix := Exceptx("isDigitExceptSix", false, isDigit, isSix)
// 	allDigitsExceptSix := Repx("allDigitsExceptSix", false, isDigitExceptSix, 1, 0)
// 	endOfStream := Endx("endOfStream", false)
// 	allDigitsExceptSixTillTheEnd := Concatx("allDigitsExceptSixTillTheEnd", true, allDigitsExceptSix, endOfStream)

// 	transformTable := TransformTable{
// 		"allDigitsExceptSix": func(result *Match, table TransformTable, s Reader) Value {
// 			str := ""
// 			for _, r := range result.Value.([]*Match) {
// 				str += r.Value.(string)
// 			}
// 			return str
// 		},
// 		"allDigitsExceptSixTillTheEnd": func(result *Match, table TransformTable, s Reader) Value {
// 			return table.Transform(result.Value.([]*Match)[0], s)
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
// 	anyAlnum := Any(Alternation(alphabeticCharacter, digit))
// 	identifier := Concatx("identifier", false, alphabeticCharacter, anyAlnum)
// 	number := Concatx("number", false, Opt(minus), digit, anyDigit)
// 	assignmentRightSide := Alternation(number, identifier, stringValue)
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
// 		"assignment": func(result *Match, table TransformTable, stream Reader) Value {
// 			elements := result.Value.([]*Match)

// 			leftSide := table.Transform(elements[0], stream).(*ProgramValue)
// 			rightSide := table.Transform(elements[2].Value.(*Match), stream).(*ProgramValue)

// 			return &ProgramAssignment{LeftSide: leftSide, RightSide: rightSide}
// 		},
// 		"number": func(result *Match, table TransformTable, stream Reader) Value {
// 			elements := result.Value.([]*Match)
// 			numContent := ""

// 			if len(elements[0].Value.([]*Match)) > 0 {
// 				numContent += "-"
// 			}

// 			numContent += elements[1].Value.(string)

// 			for _, numChr := range elements[2].Value.([]*Match) {
// 				numContent += numChr.Value.(string)
// 			}

// 			return &ProgramValue{Content: numContent, Type: ProgramValueTypeNumber}
// 		},
// 		"string": func(result *Match, table TransformTable, stream Reader) Value {
// 			elements := result.Value.([]*Match)
// 			stringContent := ""

// 			for _, strChr := range elements[1].Value.([]*Match) {
// 				stringContent += strChr.Value.(string)
// 			}

// 			return &ProgramValue{Content: stringContent, Type: ProgramValueTypeString}
// 		},
// 		"identifier": func(result *Match, table TransformTable, stream Reader) Value {
// 			elements := result.Value.([]*Match)
// 			// First character
// 			idContent := elements[0].Value.(string)
// 			// Rest of characters
// 			for _, alnum := range elements[1].Value.([]*Match) {
// 				idContent += alnum.Value.(*Match).Value.(string)
// 			}

// 			return &ProgramValue{Content: idContent, Type: ProgramValueTypeIdentifier}
// 		},
// 		"program": func(result *Match, table TransformTable, stream Reader) Value {
// 			elements := result.Value.([]*Match)
// 			name := table.Transform(elements[2], stream).(*ProgramValue)

// 			assignments := []*ProgramAssignment{}

// 			rawAssignments := elements[6].Value.([]*Match)

// 			for _, rawAssignment := range rawAssignments {
// 				assignment := table.Transform(rawAssignment.Value.([]*Match)[0], stream).(*ProgramAssignment)
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
