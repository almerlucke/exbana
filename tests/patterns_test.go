package tests

import (
	"strings"
	"testing"
	"time"
	"unicode"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

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

func TestEntityAndVectorAtEOF(t *testing.T) {
	notQuote := entR(func(r rune) bool { return r != '"' })
	matched, _, err := notQuote.Match(newRunes(""))
	if err != nil || matched {
		t.Errorf("entity at EOF: expected clean mismatch, got %v, %v", matched, err)
	}

	matched, _, err = vecR("ab").Match(newRunes("a"))
	if err != nil || matched {
		t.Errorf("vector past EOF: expected clean mismatch, got %v, %v", matched, err)
	}

	rd := newRunes("a")
	rd.Skip(1)
	matched, _, err = vecR("").Match(rd)
	if err != nil || !matched {
		t.Errorf("empty vector at EOF: expected match, got %v, %v", matched, err)
	}
}

func TestRepetitionEmptyMatchTerminates(t *testing.T) {
	// Any(Optional(a)) can match without consuming input
	p := repetition.Any[rune, runes.Pos](repetition.Optional[rune, runes.Pos](runeR('a')))
	done := make(chan struct{})
	go func() {
		defer close(done)
		matched, _, err := p.Match(newRunes("b"))
		if err != nil || !matched {
			t.Errorf("expected (empty) match, got %v, %v", matched, err)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("repetition did not terminate on empty sub-match")
	}
}

func TestScanEmptyMatchTerminates(t *testing.T) {
	p := repetition.Any[rune, runes.Pos](runeR('a'))
	done := make(chan struct{})
	go func() {
		defer close(done)
		res, err := ebnf.Scan[rune, runes.Pos](newRunes("baab"), p)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(res) == 0 {
			t.Errorf("expected results")
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Scan did not terminate on empty match")
	}
}

func TestPositionAfterMatch(t *testing.T) {
	rd := newRunes("aaab")
	p := concR(anyR(runeR('a')), runeR('a'))
	matched, result, err := p.Match(rd)
	if err != nil || !matched {
		t.Fatal(matched, err)
	}
	if pos, _ := rd.Position(); pos.Index != result.End.Index || pos.Index != 3 {
		t.Errorf("reader at %d, match ends at %d, want 3", pos.Index, result.End.Index)
	}
}

func TestChildren(t *testing.T) {
	a, b := runeR('a'), runeR('b')

	parents := map[string]ebnf.Parent[rune, runes.Pos]{
		"concatenation": concR(a, b),
		"alternation":   altR(a, b),
		"repetition":    repR(a, 0, 0),
		"except":        exceptR(a, b),
		"rule":          rule.New[rune, runes.Pos]("r", a),
	}
	want := map[string]int{"concatenation": 2, "alternation": 2, "repetition": 1, "except": 1, "rule": 1}

	for name, p := range parents {
		if got := len(p.Children()); got != want[name] {
			t.Errorf("%s: %d children, want %d", name, got, want[name])
		}
	}

	// the exception is lookahead and is not a child
	if c := exceptR(a, b).Children(); c[0] != ebnf.Pattern[rune, runes.Pos](a) {
		t.Error("the child of an except should be its must pattern")
	}

	if n := len(rule.Declare[rune, runes.Pos]("undefined").Children()); n != 0 {
		t.Errorf("an undefined rule has %d children", n)
	}

	// SetPatterns replaces what a composite is built from
	c := concR(a)
	c.SetPatterns(ebnf.Patterns[rune, runes.Pos]{a, b, a})
	if len(c.Children()) != 3 {
		t.Error("SetPatterns on concatenation")
	}
	al := altR(a)
	al.SetPatterns(ebnf.Patterns[rune, runes.Pos]{a, b})
	if len(al.Children()) != 2 {
		t.Error("SetPatterns on alternation")
	}
}

func TestMatchValuesAndOptional(t *testing.T) {
	ok, m, err := ebnf.Parse[rune, runes.Pos](concR(runeR('a'), runeR('b')), newRunes("ab"))
	if err != nil || !ok {
		t.Fatal(ok, err)
	}

	vals := m.Values()
	if len(vals) != 2 || string(vals[0].([]rune)) != "a" || string(vals[1].([]rune)) != "b" {
		t.Errorf("values = %v", vals)
	}

	opt := repetition.Optional[rune, runes.Pos](runeR('a'))
	_, m, _ = ebnf.Parse[rune, runes.Pos](opt, newRunes("a"))
	if c, ok := m.Optional(); !ok || string(c.Value.([]rune)) != "a" {
		t.Errorf("present: %v, %v", c, ok)
	}
	_, m, _ = ebnf.Parse[rune, runes.Pos](opt, newRunes("x"))
	if c, ok := m.Optional(); ok || c != nil {
		t.Errorf("absent: %v, %v", c, ok)
	}
}
