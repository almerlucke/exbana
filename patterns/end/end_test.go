package end_test

import (
	"bytes"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/end"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

func input(s string) *runes.Reader {
	rd, err := runes.New(strings.NewReader(s))
	if err != nil {
		panic(err)
	}

	return rd
}

func TestMatchesOnlyAtTheEndAndConsumesNothing(t *testing.T) {
	e := end.New[rune, runes.Pos]()

	rd := input("")
	ok, m, err := ebnf.Parse[rune, runes.Pos](e, rd)
	if err != nil || !ok || m.Begin != m.End {
		t.Fatalf("ok=%v m=%+v err=%v", ok, m, err)
	}

	if ok, _, _ := ebnf.Parse[rune, runes.Pos](e, input("x")); ok {
		t.Error("matched before the end")
	}

	// after reading everything
	rd = input("ab")
	_, _ = rd.Skip(2)
	if ok, _, _ := ebnf.Parse[rune, runes.Pos](e, rd); !ok {
		t.Error("did not match after the last element")
	}
}

func TestGenerateFinishesTheWriter(t *testing.T) {
	w := &writer{}
	if err := end.New[rune, runes.Pos]().Generate(ebnf.NewGenContext(1), w); err != nil || !w.finished || w.written != 0 {
		t.Errorf("finished=%v written=%d err=%v", w.finished, w.written, err)
	}
}

func TestPrintedText(t *testing.T) {
	var buf bytes.Buffer
	if err := end.New[rune, runes.Pos]().Printed("end of input").Print(&buf); err != nil || buf.String() != "end of input" {
		t.Errorf("print = %q, %v", buf.String(), err)
	}
}

type writer struct {
	written  int
	finished bool
}

func (w *writer) Write(rs ...rune) error { w.written += len(rs); return nil }
func (w *writer) Finish() error          { w.finished = true; return nil }
