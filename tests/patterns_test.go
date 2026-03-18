package tests

import (
	"strings"
	"testing"
	"unicode"

	ebnf "github.com/almerlucke/exbana/v2"
	"github.com/almerlucke/exbana/v2/patterns/alternation"
	"github.com/almerlucke/exbana/v2/patterns/concatenation"
	"github.com/almerlucke/exbana/v2/patterns/end"
	ent "github.com/almerlucke/exbana/v2/patterns/entity"
	"github.com/almerlucke/exbana/v2/patterns/except"
	"github.com/almerlucke/exbana/v2/patterns/repetition"
	"github.com/almerlucke/exbana/v2/patterns/vector"
	"github.com/almerlucke/exbana/v2/readers/runes"
)

// Helper functions for patterns with rune type
func altR(patterns ...ebnf.Pattern[rune, runes.Pos]) *alternation.Alternation[rune, runes.Pos] {
	return alternation.New[rune, runes.Pos](patterns...)
}

func concR(patterns ...ebnf.Pattern[rune, runes.Pos]) *concatenation.Concatenation[rune, runes.Pos] {
	return concatenation.New[rune, runes.Pos](patterns...)
}

func endR() *end.End[rune, runes.Pos] {
	return end.New[rune, runes.Pos]()
}

func entR(matchFunc func(rune) bool) *ent.Entity[rune, runes.Pos] {
	return ent.New[rune, runes.Pos](matchFunc)
}

func runeR(r rune) *ent.Entity[rune, runes.Pos] {
	e := entR(func(obj rune) bool {
		return obj == r
	})
	e.SetGenerateFunc(func() rune { return r })
	return e
}

func exceptR(must ebnf.Pattern[rune, runes.Pos], exception ebnf.Pattern[rune, runes.Pos]) *except.Except[rune, runes.Pos] {
	return except.New[rune, runes.Pos](must, exception)
}

func repR(pattern ebnf.Pattern[rune, runes.Pos], min int, max int) *repetition.Repetition[rune, runes.Pos] {
	return repetition.New[rune, runes.Pos](pattern, min, max)
}

func vecR(s string) *vector.Vector[rune, runes.Pos] {
	return vector.New[rune, runes.Pos](func(r1, r2 rune) bool {
		return r1 == r2
	}, []rune(s)...)
}

func TestEntity(t *testing.T) {
	isDigit := entR(unicode.IsDigit)
	rd, _ := runes.New(strings.NewReader("1a"))

	matched, _, err := isDigit.Match(rd)
	if err != nil || !matched {
		t.Errorf("expected match for '1', got %v, %v", matched, err)
	}

	matched, _, err = isDigit.Match(rd)
	if err != nil || matched {
		t.Errorf("expected mismatch for 'a', got %v, %v", matched, err)
	}
}

func TestVector(t *testing.T) {
	vec := vecR("hello")
	rd, _ := runes.New(strings.NewReader("hello world"))

	matched, _, err := vec.Match(rd)
	if err != nil || !matched {
		t.Errorf("expected match for 'hello', got %v, %v", matched, err)
	}

	rd, _ = runes.New(strings.NewReader("hellp"))
	matched, _, err = vec.Match(rd)
	if err != nil || matched {
		t.Errorf("expected mismatch for 'hellp', got %v, %v", matched, err)
	}
}

func TestConcatenation(t *testing.T) {
	c := concR(runeR('a'), runeR('b'), runeR('c'))
	rd, _ := runes.New(strings.NewReader("abc"))

	matched, _, err := c.Match(rd)
	if err != nil || !matched {
		t.Errorf("expected match for 'abc', got %v, %v", matched, err)
	}

	rd, _ = runes.New(strings.NewReader("abd"))
	matched, _, err = c.Match(rd)
	if err != nil || matched {
		t.Errorf("expected mismatch for 'abd', got %v, %v", matched, err)
	}
}

func TestAlternation(t *testing.T) {
	// Test basic alternation
	a := altR(vecR("abc"), vecR("abcd"), vecR("ab"))
	rd, _ := runes.New(strings.NewReader("abcd"))

	matched, result, err := a.Match(rd)
	if err != nil || !matched {
		t.Errorf("expected match for 'abcd', got %v, %v", matched, err)
	}

	// Should match the longest one: "abcd"
	s, _ := rd.Range(result.Begin, result.End)
	if string(s) != "abcd" {
		t.Errorf("expected longest match 'abcd', got %s", string(s))
	}

	// Test orthogonal alternation
	a2 := altR(vecR("abc"), vecR("abcd")).SetOrthogonal(true)
	rd2, _ := runes.New(strings.NewReader("abcd"))
	matched, result, err = a2.Match(rd2)
	if err != nil || !matched {
		t.Errorf("expected match for 'abcd', got %v, %v", matched, err)
	}

	// Should match the first one in orthogonal mode: "abc"
	s2, _ := rd2.Range(result.Begin, result.End)
	if string(s2) != "abc" {
		t.Errorf("expected first match 'abc' in orthogonal mode, got %s", string(s2))
	}
}

func TestRepetition(t *testing.T) {
	// Test 1 or more
	r := repetition.OneOrMore[rune, runes.Pos](runeR('a'))
	rd, _ := runes.New(strings.NewReader("aaaab"))

	matched, result, err := r.Match(rd)
	if err != nil || !matched {
		t.Errorf("expected match for 'aaaa', got %v, %v", matched, err)
	}

	if len(result.Components) != 4 {
		t.Errorf("expected 4 components, got %d", len(result.Components))
	}

	// Test min/max
	r2 := repR(runeR('a'), 2, 3)
	rd2, _ := runes.New(strings.NewReader("aaaaa"))
	matched, result, err = r2.Match(rd2)
	if err != nil || !matched {
		t.Errorf("expected match for 'aaa', got %v, %v", matched, err)
	}
	if len(result.Components) != 3 {
		t.Errorf("expected 3 components, got %d", len(result.Components))
	}
}

func TestExcept(t *testing.T) {
	// Match any digit except '0'
	isDigit := entR(unicode.IsDigit)
	isZero := runeR('0')
	ex := exceptR(isDigit, isZero)

	rd, _ := runes.New(strings.NewReader("1"))
	matched, _, err := ex.Match(rd)
	if err != nil || !matched {
		t.Errorf("expected match for '1', got %v, %v", matched, err)
	}

	rd, _ = runes.New(strings.NewReader("0"))
	matched, _, err = ex.Match(rd)
	if err != nil || matched {
		t.Errorf("expected mismatch for '0', got %v, %v", matched, err)
	}
}

func TestEnd(t *testing.T) {
	e := endR()
	rd, _ := runes.New(strings.NewReader("a"))

	// Should not match before end
	matched, _, err := e.Match(rd)
	if err != nil || matched {
		t.Errorf("expected mismatch before end, got %v, %v", matched, err)
	}

	// Consume 'a'
	rd.Read1()

	// Should match at end
	matched, _, err = e.Match(rd)
	if err != nil || !matched {
		t.Errorf("expected match at end, got %v, %v", matched, err)
	}
}

func TestBacktracking(t *testing.T) {
	// (a)* , a
	// Greedily (a)* will take all 'a's, but then 'a' will fail.
	// Backtracking should make (a)* give back one 'a'.

	aStar := repetition.Any[rune, runes.Pos](runeR('a'))
	c := concR(aStar, runeR('a'))

	rd, _ := runes.New(strings.NewReader("aaa"))
	matched, result, err := c.Match(rd)
	if err != nil || !matched {
		t.Fatalf("expected match with backtracking, got %v, %v", matched, err)
	}

	if len(result.Components) != 2 {
		t.Errorf("expected 2 main components, got %d", len(result.Components))
	}

	starMatch := result.Components[0]
	if len(starMatch.Components) != 2 {
		t.Errorf("expected (a)* to match 2 'a's after backtracking, got %d", len(starMatch.Components))
	}
}

func TestPrint(t *testing.T) {
	// Test Print for various patterns
	a := altR(vecR("abc").SetPrintOutput("abc"), vecR("def").SetPrintOutput("def")).SetID("Alt")
	c := concR(runeR('x').SetPrintOutput("x"), a).SetID("Conc")
	r := repetition.Any[rune, runes.Pos](c).SetID("Rep")
	ex := exceptR(runeR('a').SetPrintOutput("a"), runeR('b').SetPrintOutput("b")).SetID("Ex")

	var buf strings.Builder
	_ = a.Print(&buf)
	if buf.String() != "(abc | def)" {
		t.Errorf("expected (abc | def), got %s", buf.String())
	}

	buf.Reset()
	_ = c.Print(&buf)
	if buf.String() != "(x, Alt)" {
		t.Errorf("expected (x, Alt), got %s", buf.String())
	}

	buf.Reset()
	_ = r.Print(&buf)
	if buf.String() != "(x, Alt)*" {
		t.Errorf("expected (x, Alt)*, got %s", buf.String())
	}

	buf.Reset()
	_ = ex.Print(&buf)
	if buf.String() != "a - b" {
		t.Errorf("expected a - b, got %s", buf.String())
	}
}
