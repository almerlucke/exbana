package concatenation_test

import (
	"bytes"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/patterns/vector"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

func input(s string) *runes.Reader {
	rd, err := runes.New(strings.NewReader(s))
	if err != nil {
		panic(err)
	}

	return rd
}

func word(s string) *vector.Vector[rune, runes.Pos] {
	return vector.New[rune, runes.Pos](func(a, b rune) bool { return a == b }, []rune(s)...).Printed(`"` + s + `"`)
}

func TestMatchesInOrderAndKeepsEveryComponent(t *testing.T) {
	c := concatenation.New[rune, runes.Pos](word("ab"), word("cd"), word("e"))

	rd := input("abcdef")
	ok, m, err := ebnf.Parse[rune, runes.Pos](c, rd)
	if err != nil || !ok || m.End.Index != 5 || len(m.Components) != 3 {
		t.Fatalf("ok=%v m=%+v err=%v", ok, m, err)
	}

	for i, want := range []string{"ab", "cd", "e"} {
		if got := string(m.Components[i].Value.([]rune)); got != want {
			t.Errorf("component %d = %q, want %q", i, got, want)
		}
	}

	for _, in := range []string{"abce", "abcd", "xbcde", ""} {
		if ok, _, _ := ebnf.Parse[rune, runes.Pos](c, input(in)); ok {
			t.Errorf("%q matched", in)
		}
	}
}

func TestEmptyConcatenationMatchesTheEmptyInput(t *testing.T) {
	ok, m, err := ebnf.Parse[rune, runes.Pos](concatenation.New[rune, runes.Pos](), input("abc"))
	if err != nil || !ok || m.End.Index != 0 || len(m.Components) != 0 {
		t.Errorf("ok=%v m=%+v err=%v", ok, m, err)
	}
}

func TestEarlierPatternsGiveBackWhenALaterOneFails(t *testing.T) {
	a := word("a")

	// a* then "ab": the repetition has to give back its last "a"
	c := concatenation.New[rune, runes.Pos](repetition.Any[rune, runes.Pos](a), word("ab"))
	if ok, m, err := ebnf.Parse[rune, runes.Pos](c, input("aaab")); err != nil || !ok || m.End.Index != 4 {
		t.Fatalf("ok=%v m=%+v err=%v", ok, m, err)
	}

	// (ab | a) then "bc": the alternation offers "ab" first, the continuation refuses it, and "a" is tried
	c = concatenation.New[rune, runes.Pos](alternation.New[rune, runes.Pos](word("ab"), word("a")), word("bc"))
	if ok, m, _ := ebnf.Parse[rune, runes.Pos](c, input("abc")); !ok || m.End.Index != 3 {
		t.Errorf("ok=%v m=%+v", ok, m)
	}
}

func TestChildrenAndSetPatterns(t *testing.T) {
	c := concatenation.New[rune, runes.Pos](word("a"))
	c.SetPatterns(ebnf.Patterns[rune, runes.Pos]{word("a"), word("b"), word("c")})

	if len(c.Children()) != 3 {
		t.Errorf("children = %d", len(c.Children()))
	}
}

func TestGenerateInOrder(t *testing.T) {
	var w recorder
	c := concatenation.New[rune, runes.Pos](word("ab"), word("cd"), word("e"))

	if err := c.Generate(ebnf.NewGenContext(1), &w); err != nil || string(w.runes) != "abcde" {
		t.Errorf("out=%q err=%v", string(w.runes), err)
	}
}

func TestPrint(t *testing.T) {
	inner := concatenation.New[rune, runes.Pos](word("b"), word("c")).Named("bc")
	c := concatenation.New[rune, runes.Pos](word("a"), inner)

	var buf bytes.Buffer
	if err := c.Print(&buf); err != nil || buf.String() != `("a", bc)` {
		t.Errorf("print = %q, %v", buf.String(), err)
	}

	buf.Reset()
	if err := inner.Print(&buf); err != nil || buf.String() != `("b", "c")` {
		t.Errorf("print = %q, %v", buf.String(), err)
	}
}

type recorder struct{ runes []rune }

func (r *recorder) Write(rs ...rune) error { r.runes = append(r.runes, rs...); return nil }
func (r *recorder) Finish() error          { return nil }
