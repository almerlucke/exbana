package repetition_test

import (
	"bytes"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	"github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
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

func char(c rune) *entity.Entity[rune, runes.Pos] {
	return entity.New[rune, runes.Pos](func(r rune) bool { return r == c }).
		SetGenerateFunc(func() rune { return c }).Printed(string(c))
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

func TestOffersTheMostRepetitionsFirstAndGivesBackOneAtATime(t *testing.T) {
	got := ends(repetition.Any[rune, runes.Pos](char('a')), "aaab")
	want := []int{3, 2, 1, 0}

	if len(got) != len(want) {
		t.Fatalf("ends = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ends = %v, want %v", got, want)
		}
	}
}

func TestMinAndMaxLimitTheCount(t *testing.T) {
	r := repetition.New[rune, runes.Pos](char('a'), 2, 3)

	for in, want := range map[string][]int{
		"a":     nil,
		"aa":    {2},
		"aaa":   {3, 2},
		"aaaaa": {3, 2},
	} {
		got := ends(r, in)
		if len(got) != len(want) {
			t.Errorf("%q: ends = %v, want %v", in, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%q: ends = %v, want %v", in, got, want)
			}
		}
	}
}

func TestShortcuts(t *testing.T) {
	a := char('a')

	if r := repetition.Optional[rune, runes.Pos](a); r.Min() != 0 || r.Max() != 1 {
		t.Errorf("Optional is %d..%d", r.Min(), r.Max())
	}
	if r := repetition.Any[rune, runes.Pos](a); r.Min() != 0 || r.Max() != 0 {
		t.Errorf("Any is %d..%d", r.Min(), r.Max())
	}
	if r := repetition.OneOrMore[rune, runes.Pos](a); r.Min() != 1 || r.Max() != 0 {
		t.Errorf("OneOrMore is %d..%d", r.Min(), r.Max())
	}
}

func TestPossessiveNeverGivesBack(t *testing.T) {
	// a* then "a" can only match if the repetition gives its last "a" back
	build := func(possessive bool) P {
		return concatenation.New[rune, runes.Pos](repetition.Any[rune, runes.Pos](char('a')).SetPossessive(possessive), char('a'))
	}

	if ok, _, _ := ebnf.Parse[rune, runes.Pos](build(false), input("aaa")); !ok {
		t.Error("backtracking repetition should match")
	}
	if ok, _, _ := ebnf.Parse[rune, runes.Pos](build(true), input("aaa")); ok {
		t.Error("possessive repetition should not match")
	}
}

func TestARepetitionThatConsumesNothingStops(t *testing.T) {
	// (a*)* would repeat the empty match forever
	inner := repetition.Any[rune, runes.Pos](char('a'))
	for _, possessive := range []bool{false, true} {
		r := repetition.Any[rune, runes.Pos](inner).SetPossessive(possessive)

		if ok, m, err := ebnf.Parse[rune, runes.Pos](r, input("aab")); err != nil || !ok || m.End.Index != 2 {
			t.Errorf("possessive=%v: ok=%v m=%+v err=%v", possessive, ok, m, err)
		}
	}
}

func TestGenerateStaysWithinBoundsAndFollowsTheSeed(t *testing.T) {
	r := repetition.New[rune, runes.Pos](char('a'), 2, 5)

	lengths := map[int]bool{}
	for seed := range int64(100) {
		var w recorder
		if err := r.Generate(ebnf.NewGenContext(seed), &w); err != nil {
			t.Fatal(err)
		}
		if n := len(w.runes); n < 2 || n > 5 {
			t.Fatalf("seed %d generated %d", seed, n)
		}
		lengths[len(w.runes)] = true
	}

	if len(lengths) != 4 {
		t.Errorf("lengths seen: %v, want 2 to 5", lengths)
	}

	var a, b recorder
	_ = r.Generate(ebnf.NewGenContext(3), &a)
	_ = r.Generate(ebnf.NewGenContext(3), &b)
	if string(a.runes) != string(b.runes) {
		t.Error("same seed, different output")
	}
}

func TestGenerateCountOptions(t *testing.T) {
	var w recorder
	r := repetition.New[rune, runes.Pos](char('a'), 1, 4).SetCountFunc(func(*ebnf.GenContext) int { return 3 })
	if err := r.Generate(ebnf.NewGenContext(1), &w); err != nil || string(w.runes) != "aaa" {
		t.Errorf("out=%q err=%v", string(w.runes), err)
	}

	// a count outside min and max is an error
	w = recorder{}
	if err := r.SetCountFunc(func(*ebnf.GenContext) int { return 9 }).Generate(ebnf.NewGenContext(1), &w); err == nil {
		t.Error("expected an error for a count above max")
	}
}

func TestPrint(t *testing.T) {
	a := char('a')

	for _, c := range []struct {
		r    P
		want string
	}{
		{repetition.Any[rune, runes.Pos](a), "a*"},
		{repetition.Optional[rune, runes.Pos](a), "a?"},
		{repetition.OneOrMore[rune, runes.Pos](a), "a+"},
	} {
		var buf bytes.Buffer
		if err := c.r.Print(&buf); err != nil || buf.String() != c.want {
			t.Errorf("print = %q, %v, want %q", buf.String(), err, c.want)
		}
	}
}

type recorder struct{ runes []rune }

func (r *recorder) Write(rs ...rune) error { r.runes = append(r.runes, rs...); return nil }
func (r *recorder) Finish() error          { return nil }
