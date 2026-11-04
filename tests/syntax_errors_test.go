package tests

import (
	"strings"
	"testing"
	"unicode"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

// call = "(", digit+, ")"
func callGrammar() ebnf.Pattern[rune, runes.Pos] {
	digit := ebnf.Name(entR(unicode.IsDigit), "digit")
	return concR(lit('('), repetition.OneOrMore[rune, runes.Pos](digit), lit(')'))
}

func parseWithLog(g ebnf.Pattern[rune, runes.Pos], input string) (bool, *ebnf.FarthestLog[rune, runes.Pos]) {
	rd := newRunes(input)
	log := runes.NewFarthestLog(rd)
	matched, _, _ := ebnf.Parse(g, ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(log))
	return matched, log
}

func TestFarthestLogSyntaxError(t *testing.T) {
	matched, log := parseWithLog(callGrammar(), "(12x)")
	if matched {
		t.Fatal("expected mismatch")
	}

	e := log.Last()
	if e == nil {
		t.Fatal("expected a syntax error")
	}
	if e.Pos.Index != 3 || e.AtEnd || e.Found != 'x' {
		t.Errorf("wrong location: pos %v found %q atEnd %v", e.Pos, e.Found, e.AtEnd)
	}
	if got, want := strings.Join(e.Expected, ","), `digit,")"`; got != want {
		t.Errorf("expected %s, got %s", want, got)
	}
	if got, want := log.Err().Error(), `syntax error at 1:4: unexpected 'x', expected digit or ")"`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// the reader is left where it was
	rd := newRunes("(12x)")
	l := runes.NewFarthestLog(rd)
	g := callGrammar()
	ebnf.Parse(g, ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(l))
	before, _ := rd.Position()
	l.Last()
	if after, _ := rd.Position(); after != before {
		t.Errorf("Last moved the reader from %v to %v", before, after)
	}
}

func TestFarthestLogEndOfInput(t *testing.T) {
	_, log := parseWithLog(callGrammar(), "(12")
	if got, want := log.Err().Error(), `syntax error at 1:4: unexpected end of input, expected digit or ")"`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	_, log = parseWithLog(callGrammar(), "")
	if got, want := log.Err().Error(), `syntax error at 1:1: unexpected end of input, expected "("`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFarthestLogMultiLinePosition(t *testing.T) {
	g := concR(lit('a'), lit('\n'), lit('b'))
	_, log := parseWithLog(g, "a\nc")
	if got, want := log.Err().Error(), `syntax error at 2:1: unexpected 'c', expected "b"`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFarthestLogNothingLogged(t *testing.T) {
	matched, log := parseWithLog(callGrammar(), "(1)")
	if !matched {
		t.Fatal("expected match")
	}
	// the repetition fails its last iteration at ')', that is logged, but it is not an error: Err is only
	// meaningful after a failed match. After Reset there is nothing.
	log.Reset()
	if log.Err() != nil {
		t.Error("expected no error after reset")
	}
}

func TestFarthestLogKeepsFarthestAcrossAlternatives(t *testing.T) {
	// ("ab" , "c") | ("a" , "x")  on "abd": first branch fails at 2, second at 1
	g := altR(concR(lit('a'), lit('b'), lit('c')), concR(lit('a'), lit('x')))
	_, log := parseWithLog(g, "abd")
	e := log.Last()
	if e.Pos.Index != 2 || strings.Join(e.Expected, ",") != `"c"` {
		t.Errorf("got pos %d expected %v", e.Pos.Index, e.Expected)
	}
}

func TestFarthestLogIgnoresException(t *testing.T) {
	// string = '"', (char - '"')*, '"' : the exception '"' fails on every normal character and must not show up
	quote := lit('"')
	char := exceptR(ebnf.Name(entR(func(r rune) bool { return true }), "char"), quote)
	g := concR(quote, anyR(char), quote)
	matched, log := parseWithLog(g, `"abc`)
	if matched {
		t.Fatal("expected mismatch")
	}
	e := log.Last()
	if !e.AtEnd || e.Pos.Index != 4 {
		t.Errorf("expected failure at end of input, got %+v", e)
	}
	for _, x := range e.Expected {
		if x == "char" {
			continue
		}
		if x != `"\""` && x != `"""` {
			t.Errorf("unexpected entry %s", x)
		}
	}
}

func TestContextLoggerRecursiveGrammar(t *testing.T) {
	// list = "[" , (list | "x")* , "]"
	list := concR()
	inner := anyR(altR(list, lit('x')))
	list.SetPatterns(ebnf.Patterns[rune, runes.Pos]{lit('['), inner, lit(']')})
	ebnf.Name(list, "list")

	matched, log := parseWithLog(list, "[x[xy]]")
	if matched {
		t.Fatal("expected mismatch")
	}
	if e := log.Last(); e == nil || e.Pos.Index != 4 || e.Found != 'y' {
		t.Errorf("got %+v", e)
	}
}
