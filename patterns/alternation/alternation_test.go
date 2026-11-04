package alternation_test

import (
	"bytes"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/vector"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

type P = ebnf.Pattern[rune, runes.Pos]

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

func ends(p P, in string) []int {
	var out []int
	for m, err := range ebnf.Matches[rune, runes.Pos](p, input(in)) {
		if err != nil {
			panic(err)
		}
		out = append(out, m.End.Index)
	}

	return out
}

func TestOffersTheLongestMatchFirst(t *testing.T) {
	a := alternation.New[rune, runes.Pos](word("a"), word("abc"), word("ab"))

	if got := ends(a, "abcd"); len(got) != 3 || got[0] != 3 || got[1] != 2 || got[2] != 1 {
		t.Errorf("ends = %v, want [3 2 1]", got)
	}

	// equally long matches keep the order of the branches
	a = alternation.New[rune, runes.Pos](word("ab"), entity.New[rune, runes.Pos](func(r rune) bool { return r == 'a' }), word("ab"))
	ok, m, _ := ebnf.Parse[rune, runes.Pos](a, input("ab"))
	if !ok || m.End.Index != 2 {
		t.Fatalf("ok=%v m=%+v", ok, m)
	}
}

func TestMatchIsTheBranchThatMatched(t *testing.T) {
	a := alternation.New[rune, runes.Pos](word("x"), word("y"))
	_, m, _ := ebnf.Parse[rune, runes.Pos](a, input("y"))

	if len(m.Components) != 1 {
		t.Fatalf("components = %d", len(m.Components))
	}
	if string(m.Components[0].Value.([]rune)) != "y" {
		t.Errorf("component = %v", m.Components[0].Value)
	}

	// an alternation without a name is looked through, a named one is not
	if u := m.Unpack(); u.Pattern == ebnf.Pattern[rune, runes.Pos](a) {
		t.Error("Unpack stopped at the unnamed alternation")
	}
	named := alternation.New[rune, runes.Pos](word("x")).Named("choice")
	_, m, _ = ebnf.Parse[rune, runes.Pos](named, input("x"))
	if m.Unpack() != m {
		t.Error("Unpack looked through a named alternation")
	}
}

func TestOrthogonalCommitsToTheFirstBranchThatMatches(t *testing.T) {
	// "ab" and then "c" needs the shorter branch, the orthogonal alternation never offers it
	a := alternation.New[rune, runes.Pos](word("a"), word("ab"))
	if got := ends(a, "ab"); len(got) != 2 {
		t.Fatalf("ends = %v", got)
	}

	o := alternation.New[rune, runes.Pos](word("a"), word("ab")).SetOrthogonal(true)
	if got := ends(o, "ab"); len(got) != 1 || got[0] != 1 {
		t.Errorf("orthogonal ends = %v, want [1]", got)
	}
}

func TestChildren(t *testing.T) {
	a := alternation.New[rune, runes.Pos](word("a"))
	a.SetPatterns(ebnf.Patterns[rune, runes.Pos]{word("a"), word("b")})

	if len(a.Children()) != 2 {
		t.Errorf("children = %d", len(a.Children()))
	}
}

func TestGenerateWeights(t *testing.T) {
	a := alternation.New[rune, runes.Pos](
		entity.New[rune, runes.Pos](func(rune) bool { return true }).SetGenerateFunc(func() rune { return 'a' }),
		entity.New[rune, runes.Pos](func(rune) bool { return true }).SetGenerateFunc(func() rune { return 'b' }),
	).SetWeights(0, 1)

	for seed := range int64(20) {
		var w recorder
		if err := a.Generate(ebnf.NewGenContext(seed), &w); err != nil || string(w.runes) != "b" {
			t.Fatalf("seed %d: %q, %v", seed, string(w.runes), err)
		}
	}

	// a weight for every branch, none negative, one at least above zero
	for _, bad := range [][]float64{{1}, {-1, 2}, {0, 0}} {
		var w recorder
		if err := a.SetWeights(bad...).Generate(ebnf.NewGenContext(1), &w); err == nil {
			t.Errorf("weights %v should be an error", bad)
		}
	}
}

func TestEmptyAlternationCanNotGenerate(t *testing.T) {
	var w recorder
	if err := alternation.New[rune, runes.Pos]().Generate(ebnf.NewGenContext(1), &w); err == nil {
		t.Error("expected an error")
	}
}

func TestPrint(t *testing.T) {
	var buf bytes.Buffer
	if err := alternation.New[rune, runes.Pos](word("a"), word("b")).Print(&buf); err != nil || buf.String() != `("a" | "b")` {
		t.Errorf("print = %q, %v", buf.String(), err)
	}
}

type recorder struct{ runes []rune }

func (r *recorder) Write(rs ...rune) error { r.runes = append(r.runes, rs...); return nil }
func (r *recorder) Finish() error          { return nil }
