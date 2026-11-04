package entity_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"unicode"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

func input(s string) *runes.Reader {
	rd, err := runes.New(strings.NewReader(s))
	if err != nil {
		panic(err)
	}

	return rd
}

func TestMatchesOneElementThatPassesTheTest(t *testing.T) {
	digit := entity.New[rune, runes.Pos](unicode.IsDigit)

	rd := input("7a")
	ok, m, err := ebnf.Parse[rune, runes.Pos](digit, rd)
	if err != nil || !ok || m.End.Index != 1 || string(m.Value.([]rune)) != "7" {
		t.Fatalf("ok=%v m=%+v err=%v", ok, m, err)
	}

	// what is left is untouched
	if pos, _ := rd.Position(); pos.Index != 1 {
		t.Errorf("position = %d", pos.Index)
	}

	if ok, _, _ := ebnf.Parse[rune, runes.Pos](digit, input("a7")); ok {
		t.Error("matched a letter")
	}
}

func TestAtTheEndOfTheStreamTheTestIsNeverCalled(t *testing.T) {
	calls := 0
	e := entity.New[rune, runes.Pos](func(rune) bool { calls++; return true })

	if ok, _, err := ebnf.Parse[rune, runes.Pos](e, input("")); ok || err != nil {
		t.Errorf("ok=%v err=%v", ok, err)
	}
	if calls != 0 {
		t.Errorf("the test ran %d times on an empty stream, with a zero value", calls)
	}
}

func TestOffersExactlyOneMatch(t *testing.T) {
	e := entity.New[rune, runes.Pos](func(rune) bool { return true })

	n := 0
	for range ebnf.Matches[rune, runes.Pos](e, input("abc")) {
		n++
	}
	if n != 1 {
		t.Errorf("%d matches", n)
	}
}

func TestGenerate(t *testing.T) {
	// without a generate function the entity can not generate, and says which one
	e := entity.New[rune, runes.Pos](unicode.IsDigit).Named("digit")

	var out recorder
	if err := e.Generate(ebnf.NewGenContext(1), &out); !errors.Is(err, ebnf.ErrNoGenerator) || !strings.Contains(err.Error(), "digit") {
		t.Errorf("err = %v", err)
	}

	e.SetGenerateFunc(func() rune { return '5' })
	if err := e.Generate(ebnf.NewGenContext(1), &out); err != nil || string(out.runes) != "5" {
		t.Errorf("out=%q err=%v", string(out.runes), err)
	}

	// the context version draws from the seeded source
	e.SetGenerateFuncCtx(func(ctx *ebnf.GenContext) rune { return rune('0' + ctx.Intn(10)) })
	a, b := recorder{}, recorder{}
	_ = e.Generate(ebnf.NewGenContext(9), &a)
	_ = e.Generate(ebnf.NewGenContext(9), &b)
	if string(a.runes) != string(b.runes) {
		t.Errorf("same seed, %q and %q", string(a.runes), string(b.runes))
	}
}

func TestRecognizeLooksAtTheFirstValue(t *testing.T) {
	e := entity.New[rune, runes.Pos](unicode.IsDigit)

	for _, c := range []struct {
		in   string
		want bool
	}{{"7", true}, {"7x", true}, {"x7", false}, {"", false}} {
		matched, known := e.Recognize([]rune(c.in))
		if matched != c.want || !known {
			t.Errorf("Recognize(%q) = %v, %v", c.in, matched, known)
		}
	}
}

func TestNameAndPrintedText(t *testing.T) {
	e := entity.New[rune, runes.Pos](unicode.IsDigit).Named("digit").Printed("[0-9]")

	var buf bytes.Buffer
	if err := e.Print(&buf); err != nil || buf.String() != "[0-9]" {
		t.Errorf("print = %q, %v", buf.String(), err)
	}
	if e.ID() != "digit" || ebnf.IDOf[rune, runes.Pos](e) != "digit" {
		t.Errorf("id = %q", e.ID())
	}
}

type recorder struct{ runes []rune }

func (r *recorder) Write(rs ...rune) error { r.runes = append(r.runes, rs...); return nil }
func (r *recorder) Finish() error          { return nil }
