package tests

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/end"
	"github.com/almerlucke/exbana/v3/patterns/except"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

type recWriter struct {
	out      []rune
	finished bool
}

func (w *recWriter) Write(rs ...rune) error { w.out = append(w.out, rs...); return nil }
func (w *recWriter) Finish() error          { w.finished = true; return nil }

func TestEndGenerateFinishesTheWriter(t *testing.T) {
	w := &recWriter{}
	if err := endR().Generate(ebnf.NewGenContext(1), w); err != nil {
		t.Fatal(err)
	}
	if !w.finished || len(w.out) != 0 {
		t.Errorf("finished=%v out=%q", w.finished, string(w.out))
	}
}

func TestVectorGenerateWritesItsValues(t *testing.T) {
	got, err := gen(t, vecR("abc"), ebnf.NewGenContext(1))
	if err != nil || got != "abc" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestRuleGenerate(t *testing.T) {
	r := rule.New[rune, runes.Pos]("ab", concR(runeR('a'), runeR('b')))
	if got, err := gen(t, r, ebnf.NewGenContext(1)); err != nil || got != "ab" {
		t.Errorf("got %q, %v", got, err)
	}

	// a rule that is never defined can not generate
	if _, err := gen(t, rule.Declare[rune, runes.Pos]("later"), ebnf.NewGenContext(1)); err == nil || !strings.Contains(err.Error(), "later") {
		t.Errorf("err = %v", err)
	}

	// rules count towards the depth limit, so a recursive rule stops with ErrMaxDepth
	nest := rule.Declare[rune, runes.Pos]("nest")
	nest.Define(concR(runeR('('), nest, runeR(')')))
	ctx := ebnf.NewGenContext(1)
	ctx.MaxDepth = 10
	if _, err := gen(t, nest, ctx); !errors.Is(err, ebnf.ErrMaxDepth) {
		t.Errorf("expected ErrMaxDepth, got %v", err)
	}
	if ctx.Depth() != 0 {
		t.Errorf("depth not restored: %d", ctx.Depth())
	}
}

func TestDepthLimitIsExact(t *testing.T) {
	// three nested composites need depth 3 exactly
	p := concR(concR(concR(runeR('x'))))
	for limit, wantErr := range map[int]bool{2: true, 3: false, 4: false} {
		ctx := ebnf.NewGenContext(1)
		ctx.MaxDepth = limit
		_, err := gen(t, p, ctx)
		if (err != nil) != wantErr {
			t.Errorf("MaxDepth %d: err = %v, want error %v", limit, err, wantErr)
		}
	}
}

func TestRuleEvalFallsBackToTheWrappedPattern(t *testing.T) {
	inner := rule.New[rune, runes.Pos]("inner", runeR('a')).
		WithEval(func(m *ebnf.Match[rune, runes.Pos], rd ebnf.Reader[rune, runes.Pos]) (any, error) {
			return "evaluated", nil
		})
	outer := rule.New[rune, runes.Pos]("outer", inner)

	rd := newRunes("a")
	ok, m, err := ebnf.Parse[rune, runes.Pos](outer, rd)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if v, err := m.Eval(rd); err != nil || v != "evaluated" {
		t.Errorf("eval = %v, %v", v, err)
	}

	// nothing to evaluate with: the value of the match
	plain := rule.New[rune, runes.Pos]("plain", runeR('a'))
	rd = newRunes("a")
	_, m, _ = ebnf.Parse[rune, runes.Pos](plain, rd)
	if v, err := m.Eval(rd); err != nil || string(v.([]rune)) != "a" {
		t.Errorf("eval = %v, %v", v, err)
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

func TestSetMaxGenBoundsUnboundedRepetitions(t *testing.T) {
	counts := func(rep *repetition.Repetition[rune, runes.Pos]) (lo, hi int) {
		lo, hi = 1<<30, -1
		for seed := range int64(200) {
			s, err := gen(t, rep, ebnf.NewGenContext(seed))
			if err != nil {
				t.Fatal(err)
			}
			lo, hi = min(lo, len(s)), max(hi, len(s))
		}
		return
	}

	// zero to three
	if lo, hi := counts(repetition.Any[rune, runes.Pos](runeR('a')).SetMaxGen(3)); lo != 0 || hi != 3 {
		t.Errorf("Any with SetMaxGen(3) generated %d..%d, want 0..3", lo, hi)
	}
	// SetMaxGen counts on top of min: two to three
	if lo, hi := counts(repetition.New[rune, runes.Pos](runeR('a'), 2, 0).SetMaxGen(1)); lo != 2 || hi != 3 {
		t.Errorf("min 2 with SetMaxGen(1) generated %d..%d, want 2..3", lo, hi)
	}
}

func TestSetGenerateAttemptsGivesUp(t *testing.T) {
	// the exception rejects the only thing the must pattern can generate
	g := exceptR(runeR('a'), runeR('a')).SetGenerateAttempts(3)
	_, err := gen(t, g, ebnf.NewGenContext(1))
	if err == nil || !strings.Contains(err.Error(), "3 attempts") {
		t.Errorf("err = %v", err)
	}

	// a must pattern the exception never rejects needs just one attempt
	ok := exceptR(runeR('a'), runeR('b')).SetGenerateAttempts(1)
	if got, err := gen(t, ok, ebnf.NewGenContext(1)); err != nil || got != "a" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestExceptPassesFinishToTheWriter(t *testing.T) {
	// the must pattern finishes the writer (end of stream) and the except forwards that after its check
	g := except.New[rune, runes.Pos](concR(runeR('a'), end.New[rune, runes.Pos]()), runeR('b'))
	w := &recWriter{}
	if err := g.Generate(ebnf.NewGenContext(1), w); err != nil {
		t.Fatal(err)
	}
	if string(w.out) != "a" || !w.finished {
		t.Errorf("out=%q finished=%v", string(w.out), w.finished)
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

func TestGenContextFloat64(t *testing.T) {
	a, b := ebnf.NewGenContext(7), ebnf.NewGenContext(7)
	var seen []float64
	for range 100 {
		x, y := a.Float64(), b.Float64()
		if x != y {
			t.Fatal("same seed, different floats")
		}
		if x < 0 || x >= 1 {
			t.Fatalf("%v is outside [0, 1)", x)
		}
		seen = append(seen, x)
	}
	if len(slices.Compact(slices.Sorted(slices.Values(seen)))) < 90 {
		t.Error("floats do not look random")
	}
}
