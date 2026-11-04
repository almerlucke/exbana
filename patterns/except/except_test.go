package except_test

import (
	"bytes"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	"github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/except"
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

func anyRune() *entity.Entity[rune, runes.Pos] {
	return entity.New[rune, runes.Pos](func(rune) bool { return true }).
		SetGenerateFuncCtx(func(ctx *ebnf.GenContext) rune { return rune('a' + ctx.Intn(3)) }).Printed("any")
}

func word(s string) *vector.Vector[rune, runes.Pos] {
	return vector.New[rune, runes.Pos](func(a, b rune) bool { return a == b }, []rune(s)...).Printed(`"` + s + `"`)
}

func TestMatchesWhatTheExceptionDoesNot(t *testing.T) {
	// any character except a quote
	e := except.New[rune, runes.Pos](anyRune(), word(`"`))

	if ok, m, _ := ebnf.Parse[rune, runes.Pos](e, input("a")); !ok || m.End.Index != 1 {
		t.Errorf("ok=%v m=%+v", ok, m)
	}
	if ok, _, _ := ebnf.Parse[rune, runes.Pos](e, input(`"`)); ok {
		t.Error("matched the exception")
	}
	if ok, _, _ := ebnf.Parse[rune, runes.Pos](e, input("")); ok {
		t.Error("matched the empty input")
	}
}

func TestTheExceptionIsLookaheadAndMayBeLongerThanTheMatch(t *testing.T) {
	// one character, unless a comment starts here: "//" is longer than what the pattern matches
	e := except.New[rune, runes.Pos](anyRune(), word("//"))

	for in, want := range map[string]bool{"/x": true, "//": false, "x//": true, "/": true} {
		if ok, _, _ := ebnf.Parse[rune, runes.Pos](e, input(in)); ok != want {
			t.Errorf("%q: ok=%v, want %v", in, ok, want)
		}
	}
}

func TestChildrenLeavesOutTheException(t *testing.T) {
	must, exception := anyRune(), word("x")
	e := except.New[rune, runes.Pos](must, exception)

	c := e.Children()
	if len(c) != 1 || c[0] != ebnf.Pattern[rune, runes.Pos](must) {
		t.Errorf("children = %v", c)
	}
}

func TestGenerateAvoidsWhatTheExceptionMatches(t *testing.T) {
	// a, b or c, but not a
	e := except.New[rune, runes.Pos](anyRune(), word("a"))

	for seed := range int64(50) {
		var w recorder
		if err := e.Generate(ebnf.NewGenContext(seed), &w); err != nil {
			t.Fatal(err)
		}
		if string(w.runes) == "a" {
			t.Fatalf("seed %d generated what the exception matches", seed)
		}
	}
}

func TestGenerateNeedsAWayToCheckTheException(t *testing.T) {
	// a concatenation can not tell if it would match generated values, so there is no trusting the result
	exception := concatenation.New[rune, runes.Pos](word("a"), word("b"))
	e := except.New[rune, runes.Pos](anyRune(), exception)

	var w recorder
	if err := e.Generate(ebnf.NewGenContext(1), &w); err == nil {
		t.Error("expected an error")
	}

	// with an accept function the check is yours
	e.SetAcceptFunc(func(values []rune) bool { return string(values) != "b" })
	for seed := range int64(30) {
		w = recorder{}
		if err := e.Generate(ebnf.NewGenContext(seed), &w); err != nil || string(w.runes) == "b" {
			t.Fatalf("seed %d: %q, %v", seed, string(w.runes), err)
		}
	}
}

func TestGenerateGivesUpAfterTheAttempts(t *testing.T) {
	e := except.New[rune, runes.Pos](
		entity.New[rune, runes.Pos](func(rune) bool { return true }).SetGenerateFunc(func() rune { return 'a' }),
		word("a"),
	).SetGenerateAttempts(4)

	var w recorder
	err := e.Generate(ebnf.NewGenContext(1), &w)
	if err == nil || !strings.Contains(err.Error(), "4 attempts") {
		t.Errorf("err = %v", err)
	}
}

func TestExceptionOfAnAlternationIsChecked(t *testing.T) {
	// an alternation of vectors can recognize generated values
	e := except.New[rune, runes.Pos](anyRune(), alternation.New[rune, runes.Pos](word("a"), word("b")))

	for seed := range int64(30) {
		var w recorder
		if err := e.Generate(ebnf.NewGenContext(seed), &w); err != nil || string(w.runes) != "c" {
			t.Fatalf("seed %d: %q, %v", seed, string(w.runes), err)
		}
	}
}

func TestPrint(t *testing.T) {
	var buf bytes.Buffer
	if err := except.New[rune, runes.Pos](anyRune(), word(`"`)).Print(&buf); err != nil || buf.String() != `any - """` {
		t.Errorf("print = %q, %v", buf.String(), err)
	}
}

type recorder struct{ runes []rune }

func (r *recorder) Write(rs ...rune) error { r.runes = append(r.runes, rs...); return nil }
func (r *recorder) Finish() error          { return nil }
