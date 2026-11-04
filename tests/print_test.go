package tests

import (
	"bytes"
	"strings"
	"testing"
	"unicode"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

func TestPrint(t *testing.T) {
	// Test Print for various patterns
	a := ebnf.Name(altR(ebnf.Text(vecR("abc"), "abc"), ebnf.Text(vecR("def"), "def")), "Alt")
	c := ebnf.Name(concR(ebnf.Text(runeR('x'), "x"), a), "Conc")
	r := ebnf.Name(repetition.Any[rune, runes.Pos](c), "Rep")
	ex := ebnf.Name(exceptR(ebnf.Text(runeR('a'), "a"), ebnf.Text(runeR('b'), "b")), "Ex")

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
	if buf.String() != "Conc*" {
		t.Errorf("expected Conc*, got %s", buf.String())
	}

	buf.Reset()
	_ = ex.Print(&buf)
	if buf.String() != "a - b" {
		t.Errorf("expected a - b, got %s", buf.String())
	}
}

func TestPrintRepetitionUnbounded(t *testing.T) {
	x := runeR('x')
	ebnf.Text(x, `"x"`)

	var buf bytes.Buffer
	if err := repR(x, 2, 0).Print(&buf); err != nil {
		t.Fatal(err)
	}
	if got, want := buf.String(), `(2 * "x", "x"*)`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}

	// children with an ID are printed by reference
	ebnf.Name(x, "ex")
	buf.Reset()
	if err := repR(x, 1, 3).Print(&buf); err != nil {
		t.Fatal(err)
	}
	if got, want := buf.String(), `(1 * ex, 2 * ex?)`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestNameAndTextHelpers(t *testing.T) {
	d := ebnf.Name(entR(unicode.IsDigit), "digit")
	if d.ID() != "digit" || ebnf.IDOf[rune, runes.Pos](d) != "digit" {
		t.Error("name not set")
	}

	plus := ebnf.Text(runeR('+'), `"+"`)
	var buf bytes.Buffer
	if err := concR(plus, d).Print(&buf); err != nil || buf.String() != `("+", digit)` {
		t.Errorf("print = %q, %v", buf.String(), err)
	}
}

func TestNamedAndPrintedChainOnEveryPattern(t *testing.T) {
	a := runeR('a')

	named := map[string]ebnf.Named{
		"entity":        entR(unicode.IsDigit).Named("n"),
		"vector":        vecR("ab").Named("n"),
		"end":           endR().Named("n"),
		"alternation":   altR(a).Named("n"),
		"concatenation": concR(a).Named("n"),
		"repetition":    repR(a, 0, 0).Named("n"),
		"except":        exceptR(a, a).Named("n"),
	}
	for name, p := range named {
		if p.ID() != "n" {
			t.Errorf("%s: ID = %q", name, p.ID())
		}
	}

	// text is what leaves print as, and chains with the other setters
	var buf bytes.Buffer
	leaf := vecR("ab").Printed("<ab>").Named("ab")
	if err := leaf.Print(&buf); err != nil || buf.String() != "<ab>" {
		t.Errorf("print = %q, %v", buf.String(), err)
	}
	buf.Reset()
	if err := ebnf.PrintChild[rune, runes.Pos](&buf, leaf); err != nil || buf.String() != "ab" {
		t.Errorf("as child = %q, %v", buf.String(), err)
	}
	if got := repR(a, 0, 0).Named("r").SetPossessive(true); !got.Possessive() || got.ID() != "r" {
		t.Error("Named does not chain into the other setters")
	}
	buf.Reset()
	if err := endR().Printed("$").Print(&buf); err != nil || buf.String() != "$" {
		t.Errorf("end = %q, %v", buf.String(), err)
	}
}

func TestRepetitionAccessorsAndPrintForms(t *testing.T) {
	x := ebnf.Name(runeR('x'), "x")

	r := repetition.New[rune, runes.Pos](x, 2, 5)
	if r.Min() != 2 || r.Max() != 5 || r.Possessive() {
		t.Errorf("min=%d max=%d possessive=%v", r.Min(), r.Max(), r.Possessive())
	}
	if !r.SetPossessive(true).Possessive() {
		t.Error("SetPossessive")
	}

	for _, c := range []struct {
		p    ebnf.Pattern[rune, runes.Pos]
		want string
	}{
		{repetition.Any[rune, runes.Pos](x), "x*"},
		{repetition.Optional[rune, runes.Pos](x), "x?"},
		{repetition.OneOrMore[rune, runes.Pos](x), "x+"},
	} {
		var buf bytes.Buffer
		if err := c.p.Print(&buf); err != nil || buf.String() != c.want {
			t.Errorf("print = %q, %v, want %q", buf.String(), err, c.want)
		}
	}

	// the counted forms are some spelling that mentions the count and the pattern
	for _, c := range []struct{ min, max int }{{2, 0}, {2, 5}, {3, 3}} {
		var buf bytes.Buffer
		if err := repetition.New[rune, runes.Pos](x, c.min, c.max).Print(&buf); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), "x") || !strings.Contains(buf.String(), string(rune('0'+c.min))) {
			t.Errorf("min %d max %d printed as %q", c.min, c.max, buf.String())
		}
	}
}
