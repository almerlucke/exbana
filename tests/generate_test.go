package tests

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"unicode"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/end"
	"github.com/almerlucke/exbana/v3/patterns/except"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

type sliceWriter struct{ out []rune }

func (w *sliceWriter) Write(rs ...rune) error { w.out = append(w.out, rs...); return nil }
func (w *sliceWriter) Finish() error          { return nil }

func gen(t *testing.T, p ebnf.Pattern[rune, runes.Pos], ctx *ebnf.GenContext) (string, error) {
	t.Helper()
	w := &sliceWriter{}
	err := p.Generate(ctx, w)
	return string(w.out), err
}

// a random grammar: (a | b | c){0,6}
func randomGrammar() ebnf.Pattern[rune, runes.Pos] {
	return repR(altR(runeR('a'), runeR('b'), runeR('c')), 3, 6)
}

func TestGenerateSeedIsReproducible(t *testing.T) {
	g := randomGrammar()
	run := func(seed int64) string {
		var sb strings.Builder
		ctx := ebnf.NewGenContext(seed)
		for range 20 {
			s, err := gen(t, g, ctx)
			if err != nil {
				t.Fatal(err)
			}
			sb.WriteString(s + ",")
		}
		return sb.String()
	}

	if run(42) != run(42) {
		t.Error("same seed gave different output")
	}
	if run(42) == run(43) {
		t.Error("different seeds gave the same output")
	}
}

func TestGenerateWeights(t *testing.T) {
	count := func(weights ...float64) (a, b int) {
		alt := altR(runeR('a'), runeR('b')).SetWeights(weights...)
		ctx := ebnf.NewGenContext(7)
		for range 4000 {
			s, err := gen(t, alt, ctx)
			if err != nil {
				t.Fatal(err)
			}
			if s == "a" {
				a++
			} else {
				b++
			}
		}
		return
	}

	if a, b := count(1, 0); a != 4000 || b != 0 {
		t.Errorf("weight 0 chosen: %d %d", a, b)
	}
	if a, b := count(0, 5); a != 0 || b != 4000 {
		t.Errorf("weight 0 chosen: %d %d", a, b)
	}
	// 3:1 should give about 3000 : 1000
	if a, b := count(3, 1); a < 2850 || a > 3150 {
		t.Errorf("3:1 gave %d : %d", a, b)
	}
	if a, b := count(1, 1); a < 1850 || a > 2150 {
		t.Errorf("1:1 gave %d : %d", a, b)
	}
}

func TestGenerateWeightFuncUsesState(t *testing.T) {
	alt := altR(runeR('a'), runeR('b')).SetWeightFunc(func(ctx *ebnf.GenContext) []float64 {
		if ctx.State.(string) == "first" {
			return []float64{1, 0}
		}
		return []float64{0, 1}
	})

	ctx := ebnf.NewGenContext(1)
	ctx.State = "first"
	if s, _ := gen(t, alt, ctx); s != "a" {
		t.Errorf("got %q", s)
	}
	ctx.State = "second"
	if s, _ := gen(t, alt, ctx); s != "b" {
		t.Errorf("got %q", s)
	}
}

func TestGenerateWeightErrors(t *testing.T) {
	ctx := ebnf.NewGenContext(1)

	if _, err := gen(t, altR(runeR('a'), runeR('b')).SetWeights(1), ctx); err == nil {
		t.Error("expected error for wrong number of weights")
	}
	if _, err := gen(t, altR(runeR('a'), runeR('b')).SetWeights(0, 0), ctx); err == nil {
		t.Error("expected error for all zero weights")
	}
	if _, err := gen(t, altR(runeR('a'), runeR('b')).SetWeights(-1, 2), ctx); err == nil {
		t.Error("expected error for negative weight")
	}
	if _, err := gen(t, altR(), ctx); err == nil {
		t.Error("expected error for empty alternation, not a panic")
	}
}

func TestGenerateMaxDepth(t *testing.T) {
	// always recurse: must stop with ErrMaxDepth instead of overflowing the stack
	deep := concR()
	deep.SetPatterns(ebnf.Patterns[rune, runes.Pos]{runeR('('), altR(deep).SetWeights(1), runeR(')')})
	ctx := ebnf.NewGenContext(1)
	ctx.MaxDepth = 50
	if _, err := gen(t, deep, ctx); !errors.Is(err, ebnf.ErrMaxDepth) {
		t.Errorf("expected ErrMaxDepth, got %v", err)
	}
	if ctx.Depth() != 0 {
		t.Errorf("depth not restored after error: %d", ctx.Depth())
	}

	// the depth is available to weight functions, so a grammar can steer itself to its end
	steered := concR()
	alt := altR(steered, runeR('x')).SetWeightFunc(func(ctx *ebnf.GenContext) []float64 {
		if ctx.Depth() >= 10 {
			return []float64{0, 1}
		}
		return []float64{1, 0}
	})
	steered.SetPatterns(ebnf.Patterns[rune, runes.Pos]{runeR('('), alt, runeR(')')})
	ctx = ebnf.NewGenContext(1)
	s, err := gen(t, steered, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "x") || strings.Count(s, "(") != strings.Count(s, ")") {
		t.Errorf("unexpected output %q", s)
	}
	if ctx.Depth() != 0 {
		t.Errorf("depth not restored: %d", ctx.Depth())
	}
}

func TestGenerateFuncCtxUsesSeed(t *testing.T) {
	digit := entR(unicode.IsDigit).SetGenerateFuncCtx(func(ctx *ebnf.GenContext) rune { return rune('0' + ctx.Intn(10)) })
	g := repR(digit, 8, 8)
	a, _ := gen(t, g, ebnf.NewGenContext(5))
	b, _ := gen(t, g, ebnf.NewGenContext(5))
	c, _ := gen(t, g, ebnf.NewGenContext(6))
	if a != b || a == c || len(a) != 8 {
		t.Errorf("%q %q %q", a, b, c)
	}
}

func TestGenerateRepetitionCountWeights(t *testing.T) {
	// counts 2, 3, 4 with odds 1:0:3
	counts := map[int]int{}
	g := repR(runeR('a'), 2, 0).SetCountWeights(1, 0, 3)
	ctx := ebnf.NewGenContext(3)
	for range 4000 {
		s, err := gen(t, g, ctx)
		if err != nil {
			t.Fatal(err)
		}
		counts[len(s)]++
	}
	if counts[3] != 0 || counts[2]+counts[4] != 4000 || counts[4] < 2850 || counts[4] > 3150 {
		t.Errorf("unexpected counts %v", counts)
	}

	// weights that reach past max give an error when such a count is picked (3 of 4 equal weights are past max)
	over := repR(runeR('a'), 0, 1).SetCountWeights(1, 1, 1, 1)
	errs := 0
	for range 100 {
		if _, err := gen(t, over, ctx); err != nil {
			errs++
		}
	}
	if errs < 50 || errs == 100 {
		t.Errorf("expected about 75 errors out of 100, got %d", errs)
	}
}

func TestGenerateRepetitionCountFunc(t *testing.T) {
	g := repR(runeR('a'), 1, 5).SetCountFunc(func(ctx *ebnf.GenContext) int { return ctx.State.(int) })
	ctx := ebnf.NewGenContext(1)
	for _, n := range []int{1, 3, 5} {
		ctx.State = n
		if s, err := gen(t, g, ctx); err != nil || len(s) != n {
			t.Errorf("count %d: got %q, %v", n, s, err)
		}
	}
	for _, n := range []int{0, 6} {
		ctx.State = n
		if _, err := gen(t, g, ctx); err == nil {
			t.Errorf("count %d: expected error", n)
		}
	}
}

func TestGenerateEntityWithoutGenerator(t *testing.T) {
	_, err := gen(t, ebnf.Name(entR(unicode.IsDigit), "digit"), ebnf.NewGenContext(1))
	if !errors.Is(err, ebnf.ErrNoGenerator) || !strings.Contains(err.Error(), "digit") {
		t.Errorf("got %v", err)
	}
}

func TestGenerateExceptHonoursException(t *testing.T) {
	// any lowercase letter except a, b or c
	letter := entR(func(r rune) bool { return r >= 'a' && r <= 'z' }).SetGenerateFuncCtx(func(ctx *ebnf.GenContext) rune {
		return rune('a' + ctx.Intn(26))
	})
	g := exceptR(letter, altR(runeR('a'), runeR('b'), runeR('c')))

	ctx := ebnf.NewGenContext(1)
	for range 500 {
		s, err := gen(t, g, ctx)
		if err != nil {
			t.Fatal(err)
		}
		if s == "a" || s == "b" || s == "c" || len(s) != 1 {
			t.Fatalf("generated %q which the exception forbids", s)
		}
	}

	// a vector as exception is recognized as a prefix
	two := repR(entR(func(r rune) bool { return r == 'a' || r == 'b' }).SetGenerateFuncCtx(func(ctx *ebnf.GenContext) rune { return rune('a' + ctx.Intn(2)) }), 2, 2)
	g2 := exceptR(two, vecR("ab"))
	for range 200 {
		s, err := gen(t, g2, ctx)
		if err != nil {
			t.Fatal(err)
		}
		if s == "ab" {
			t.Fatal("generated the excepted vector")
		}
	}
}

func TestGenerateExceptCannotCheck(t *testing.T) {
	letter := entR(unicode.IsLetter).SetGenerateFunc(func() rune { return 'q' })

	// an exception that is not recognizable (a concatenation) is an error without an accept function
	g := exceptR(letter, concR(runeR('a'), runeR('b')))
	if _, err := gen(t, g, ebnf.NewGenContext(1)); err == nil {
		t.Error("expected an error for an exception that can't be checked")
	}

	// ... but fine with one
	g = exceptR(letter, concR(runeR('a'), runeR('b'))).SetAcceptFunc(func(v []rune) bool { return string(v) != "ab" })
	if s, err := gen(t, g, ebnf.NewGenContext(1)); err != nil || s != "q" {
		t.Errorf("got %q, %v", s, err)
	}

	// an exception that always matches leaves nothing to generate
	always := exceptR(letter, entR(unicode.IsLetter))
	if _, err := gen(t, always, ebnf.NewGenContext(1)); err == nil {
		t.Error("expected an error when every attempt is excepted")
	}
}

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
