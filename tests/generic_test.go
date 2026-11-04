package tests

import (
	"slices"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	"github.com/almerlucke/exbana/v3/patterns/end"
	ent "github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/except"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/patterns/vector"
	"github.com/almerlucke/exbana/v3/readers/slice"
)

// The library is generic in the element type and the position type. These tests use ints as elements and a plain
// int as position, through the slice reader, which has nothing in common with the runes reader (no lines, no columns).

func newInts(v ...int) *slice.Reader[int] { return slice.New(v) }

type intWriter struct {
	out      []int
	finished bool
}

func (w *intWriter) Write(v ...int) error { w.out = append(w.out, v...); return nil }
func (w *intWriter) Finish() error        { w.finished = true; return nil }

type (
	ip = ebnf.Pattern[int, int]
)

func isOdd(x int) bool    { return x%2 != 0 }
func isEven(x int) bool   { return x%2 == 0 }
func eqInt(a, b int) bool { return a == b }

func oddP() *ent.Entity[int, int]  { return ebnf.Name(ent.New[int, int](isOdd), "odd") }
func evenP() *ent.Entity[int, int] { return ebnf.Name(ent.New[int, int](isEven), "even") }
func anyIntP() *ent.Entity[int, int] {
	return ebnf.Name(ent.New[int, int](func(int) bool { return true }), "int")
}
func intsVec(v ...int) *vector.Vector[int, int] { return vector.New[int, int](eqInt, v...) }

func TestGenericReaderBasicPatterns(t *testing.T) {
	// odd, even*, end
	g := concatenation.New[int, int](oddP(), repetition.Any[int, int](evenP()), end.New[int, int]())

	for _, c := range []struct {
		in   []int
		want bool
	}{
		{[]int{1}, true},
		{[]int{1, 2, 4, 6}, true},
		{[]int{1, 2, 3}, false},
		{[]int{2}, false},
		{nil, false},
	} {
		ok, m, err := ebnf.Parse[int, int](g, newInts(c.in...))
		if err != nil || ok != c.want {
			t.Errorf("%v: got %v, %v want %v", c.in, ok, err, c.want)
		}
		if ok && m.End != len(c.in) {
			t.Errorf("%v: ends at %d", c.in, m.End)
		}
	}
}

func TestGenericReaderVectorAlternationAndRange(t *testing.T) {
	// longest first: the three element vector wins over its prefix
	g := alternation.New[int, int](intsVec(1, 2), intsVec(1, 2, 3))
	rd := newInts(1, 2, 3, 4)

	ok, m, err := ebnf.Parse[int, int](g, rd)
	if err != nil || !ok || m.End != 3 {
		t.Fatalf("ok=%v m=%+v err=%v", ok, m, err)
	}

	vals, _ := rd.Range(m.Begin, m.End)
	if !slices.Equal(vals, []int{1, 2, 3}) {
		t.Errorf("range = %v", vals)
	}
	if pos, _ := rd.Position(); pos != 3 {
		t.Errorf("position after match = %d", pos)
	}

	// a vector that runs out of input does not match
	if ok, _, _ := ebnf.Parse[int, int](intsVec(1, 2, 3), newInts(1, 2)); ok {
		t.Error("vector matched past the end")
	}
}

func TestGenericReaderBacktrackingAndMemo(t *testing.T) {
	// any*, 9: the repetition has to give back what the vector needs
	build := func() ip {
		return concatenation.New[int, int](repetition.Any[int, int](anyIntP()), intsVec(9))
	}

	for _, memo := range []bool{false, true} {
		var rd ebnf.Reader[int, int] = newInts(4, 5, 9)
		if memo {
			rd = ebnf.WithMemo[int, int](rd, slice.Index)
		}

		ok, m, err := ebnf.Parse[int, int](build(), rd)
		if err != nil || !ok || m.End != 3 {
			t.Errorf("memo=%v: ok=%v m=%+v err=%v", memo, ok, m, err)
		}
	}

	if ok, _, _ := ebnf.Parse[int, int](build(), newInts(4, 5)); ok {
		t.Error("matched without the 9")
	}
}

func TestGenericReaderExcept(t *testing.T) {
	g := except.New[int, int](anyIntP(), intsVec(7))

	if ok, _, _ := ebnf.Parse[int, int](g, newInts(7)); ok {
		t.Error("matched the exception")
	}
	if ok, _, _ := ebnf.Parse[int, int](g, newInts(8)); !ok {
		t.Error("did not match something the exception allows")
	}
}

func TestGenericReaderRuleEval(t *testing.T) {
	// sum = (odd | even)+, evaluated by adding the elements
	elem := alternation.New[int, int](oddP(), evenP())
	sum := rule.New[int, int]("sum", repetition.OneOrMore[int, int](elem)).
		SetEval(func(m *ebnf.Match[int, int], rd ebnf.Reader[int, int]) (any, error) {
			vals, err := rd.Range(m.Begin, m.End)
			total := 0
			for _, v := range vals {
				total += v
			}
			return total, err
		})

	rd := newInts(1, 2, 3, 4)
	ok, m, err := ebnf.Parse[int, int](sum, rd)
	if err != nil || !ok || m.ID() != "sum" {
		t.Fatalf("ok=%v m=%+v err=%v", ok, m, err)
	}
	if v, err := m.Eval(rd); err != nil || v != 10 {
		t.Errorf("eval = %v, %v", v, err)
	}
}

func TestGenericReaderSyntaxError(t *testing.T) {
	g := concatenation.New[int, int](oddP(), evenP(), evenP())
	rd := newInts(1, 2, 3)
	log := ebnf.NewFarthestLog[int, int](rd)

	if ok, _, _ := ebnf.Parse[int, int](g, ebnf.NewParseContext[int, int](rd).WithLogger(log)); ok {
		t.Fatal("should not match")
	}

	e := log.Last()
	if e == nil || e.Pos != 2 || e.AtEnd || e.Found != 3 || !slices.Equal(e.Expected, []string{"even"}) {
		t.Fatalf("got %+v", e)
	}
	if got := e.Error(); got != "syntax error at 2: unexpected 3, expected even" {
		t.Errorf("message = %q", got)
	}

	// end of input
	rd = newInts(1, 2)
	log = ebnf.NewFarthestLog[int, int](rd)
	ebnf.Parse[int, int](g, ebnf.NewParseContext[int, int](rd).WithLogger(log))
	if e := log.Last(); e == nil || e.Pos != 2 || !e.AtEnd {
		t.Errorf("got %+v", e)
	}
}

func TestGenericReaderScan(t *testing.T) {
	rd := newInts(1, 2, 0, 1, 2)
	results, err := ebnf.Scan[int, int](rd, intsVec(1, 2))
	if err != nil || len(results) != 2 || results[0].Begin != 0 || results[1].Begin != 3 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
}

func TestGenericGenerate(t *testing.T) {
	seven := ent.New[int, int](isOdd).SetGenerateFunc(func() int { return 7 })
	g := concatenation.New[int, int](intsVec(1, 2), repetition.New[int, int](seven, 3, 3), end.New[int, int]())

	w := &intWriter{}
	if err := g.Generate(ebnf.NewGenContext(1), w); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.out, []int{1, 2, 7, 7, 7}) || !w.finished {
		t.Errorf("out=%v finished=%v", w.out, w.finished)
	}

	// what is generated is matched by the same grammar
	if ok, _, _ := ebnf.Parse[int, int](g, newInts(w.out...)); !ok {
		t.Error("generated output is not matched by its grammar")
	}
}
