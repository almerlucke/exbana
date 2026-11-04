package main

import (
	"bytes"
	"math"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/examples/calcgen/grammar"
	"github.com/almerlucke/exbana/v3/examples/calcgen/parser"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

func TestGeneratedCodeIsUpToDate(t *testing.T) {
	want, err := grammar.Source()
	if err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile("parser/parser.go")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, want) {
		t.Fatal("parser/parser.go is not what the grammar generates: run go generate in examples/calcgen")
	}
}

func TestEval(t *testing.T) {
	for in, want := range map[string]float64{
		"7":                  7,
		"1 + 2":              3,
		"1 + 2 * 3":          7,
		"(1 + 2) * 3":        9,
		"10 - 3 - 2":         5, // left associative: (10-3)-2
		"100 / 10 / 5":       2, // (100/10)/5
		"2 * 3 + 4 * 5":      26,
		"1 - 2 - 3 - 4":      -8,
		"-3":                 -3,
		"--3":                3,
		"2 * -3":             -6,
		"- (1 + 2)":          -3,
		"10 % 4 * 2":         4,
		"1.5 + 2.25":         3.75,
		"  ( ( ( 1 ) ) )  ":  1,
		"10 - 3 - 2 * (4+1)": -3,
		"8 / 4 / 2 * 3 - 1":  2,
	} {
		got, err := Eval(in)
		if err != nil || math.Abs(got-want) > 1e-9 {
			t.Errorf("%q = %v, %v, want %v", in, got, err, want)
		}
	}

	for in, want := range map[string]string{
		"":        "syntax error at 1:1: unexpected end of input",
		"1 +":     "syntax error at 1:4: unexpected end of input",
		"1 + * 2": "syntax error at 1:5: unexpected '*'",
		"(1 + 2":  "syntax error at 1:7: unexpected end of input",
		"1 2":     "syntax error at 1:3: unexpected '2'",
		"1 / 0":   "division by zero",
	} {
		_, err := Eval(in)
		if err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Errorf("%q: %v, want %q", in, err, want)
		}
	}
}

func TestLongChainsAreLoopsNotRecursion(t *testing.T) {
	// a left recursive rule is a loop in the generated parser: ten thousand terms are one node with ten thousand children
	in := "1" + strings.Repeat(" + 1", 10000)

	if v, err := Eval(in); err != nil || v != 10001 {
		t.Errorf("got %v, %v", v, err)
	}
}

// the grammar as it was written: default backtracking and left recursion that grows a seed, the meaning the generated parser has to have
var original = grammar.Calc()

// agree parses with the generated parser and with the grammar as it was written, and fails if they differ
func agree(t testing.TB, p *parser.CalcParser, input string) {
	t.Helper()

	// the runes reader makes line endings uniform, the generated parser reads what it is given
	if strings.ContainsRune(input, '\r') {
		t.Skip()
	}

	rd, err := runes.New(strings.NewReader(input))
	if err != nil {
		t.Skip()
	}

	log := runes.NewFarthestLog(rd)

	ok, m, perr := ebnf.Parse[rune, runes.Pos](original, ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(log))
	if perr != nil {
		t.Fatal(perr)
	}

	tree, err := p.Parse(input)

	if (err == nil) != ok {
		t.Fatalf("%.60q: the generated parser matched=%v (%v), the grammar %v", input, err == nil, err, ok)
	}

	if ok {
		if got := utf8.RuneCountInString(input[:tree.End]); got != m.End.Index {
			t.Fatalf("%.60q: ends at %d, the grammar at %d", input, got, m.End.Index)
		}

		return
	}

	if e := log.Err(); e != nil && e.Error() != err.Error() {
		t.Fatalf("%.60q:\n  generated %q\n  grammar   %q", input, err.Error(), e.Error())
	}
}

func TestAgreesWithTheGrammarAsItWasWritten(t *testing.T) {
	p := parser.NewCalcParser()

	for _, in := range []string{
		"1", "1+2", "1 + 2 * 3 - 4", "(1)", "((1+2)*3)", "-1", "--1", "1.5", "1 - -2", "10 / 2 / 5", "  1  ",
		"", "+", "1 +", "1 + * 2", "(1 + 2", "1 2", "1.", ".5", "(", ")", "1 + (2 * (3 - ", "1 +\n2", "é", "1\xff", "1 + 2)",
	} {
		agree(t, p, in)
	}
}

// FuzzGeneratedParserAgainstGrammar: on any input the parser generated from left recursive rules and the grammar they were
// written in (which grows a seed and offers every match) have to give the same answer and the same syntax error.
func FuzzGeneratedParserAgainstGrammar(f *testing.F) {
	for _, seed := range []string{"1+2*3", "(1-(2-3))-4", "-(1)", "1 + 2 +", "((", "1.5.2", "9 % 4 / 2", "1 -", "- - 1", "2*(3", "1\n+\n2"} {
		f.Add(seed)
	}

	p := parser.NewCalcParser()

	f.Fuzz(func(t *testing.T, input string) {
		agree(t, p, input)
	})
}
