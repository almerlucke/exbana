package tests

import (
	"bytes"
	"errors"
	"math/rand"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

func TestGeneratedOutputIsAlwaysMatchedByItsGrammar(t *testing.T) {
	// whatever a grammar generates, the same grammar has to match, checked with the oracle that knows nothing about
	// matching strategies and with the library itself
	rng := rand.New(rand.NewSource(99))

	generated := 0

	for range 400 {
		g := randomNodeFor(rng, 3, true)

		var desc bytes.Buffer
		_ = g.p.Print(&desc)

		for seed := range int64(8) {
			out, err := gen(t, g.p, ebnf.NewGenContext(seed))
			if err != nil {
				t.Fatalf("grammar %s, seed %d: %v", desc.String(), seed, err)
			}

			in := []rune(out)
			if !g.ends(in, 0)[len(in)] {
				t.Fatalf("grammar %s generated %q, which the oracle says it does not match", desc.String(), out)
			}

			ok, _, perr := ebnf.Parse[rune, runes.Pos](concR(g.p, endR()), newRunes(out))
			if perr != nil || !ok {
				t.Fatalf("grammar %s generated %q, which it does not match (err %v)", desc.String(), out, perr)
			}

			generated++
		}
	}

	t.Logf("%d generated strings matched their grammars", generated)
}

func TestGenerationIsDeterministicPerSeedForRandomGrammars(t *testing.T) {
	rng := rand.New(rand.NewSource(5))

	for range 100 {
		g := randomNodeFor(rng, 3, true)

		for seed := range int64(4) {
			a, errA := gen(t, g.p, ebnf.NewGenContext(seed))
			b, errB := gen(t, g.p, ebnf.NewGenContext(seed))
			if a != b || (errA == nil) != (errB == nil) {
				t.Fatalf("seed %d: %q vs %q", seed, a, b)
			}
		}
	}
}

// list = "[" [ item { "," item } ] "]"   item = "x" | list, steered by depth so that it ends
func listGrammar() rr {
	list := rule.Declare[rune, runes.Pos]("list")
	item := altR(runeR('x'), list).SetWeightFunc(func(ctx *ebnf.GenContext) []float64 {
		// deeper means more likely to stop
		if ctx.Depth() > 24 {
			return []float64{1, 0}
		}
		return []float64{1, 1}
	})

	more := repR(concR(runeR(','), item), 0, 3)
	list.Define(concR(runeR('['), repetition.Optional[rune, runes.Pos](concR(item, more)), runeR(']')))

	return list
}

func TestRecursiveGenerationEndsAndRoundTrips(t *testing.T) {
	list := listGrammar()
	full := concR(list, endR())

	longest := 0

	for seed := range int64(200) {
		out, err := gen(t, list, ebnf.NewGenContext(seed))
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}

		if ok, _, perr := ebnf.Parse[rune, runes.Pos](full, newRunes(out)); perr != nil || !ok {
			t.Fatalf("seed %d generated %q, which does not parse (err %v)", seed, out, perr)
		}

		if strings.Count(out, "[") != strings.Count(out, "]") {
			t.Fatalf("seed %d: unbalanced %q", seed, out)
		}

		longest = max(longest, len(out))
	}

	if longest < 20 {
		t.Errorf("the longest of 200 lists was %d runes, the grammar is not recursing", longest)
	}
}

func TestLeftRecursiveRuleCanNotGenerateWithoutAWayOut(t *testing.T) {
	// expr = expr "-" "x" | "x", steered into the recursion: generating can only stop at the depth limit
	expr := rule.Declare[rune, runes.Pos]("expr").SetLeftRecursive(true)
	body := altR(concR(expr, runeR('-'), runeR('x')), runeR('x')).SetWeights(1, 0)
	expr.Define(body)

	ctx := ebnf.NewGenContext(1)
	ctx.MaxDepth = 100

	if _, err := gen(t, expr, ctx); !errors.Is(err, ebnf.ErrMaxDepth) {
		t.Errorf("err = %v, want ErrMaxDepth", err)
	}
	if ctx.Depth() != 0 {
		t.Errorf("depth not restored: %d", ctx.Depth())
	}

	// with a way out it generates, and what it generates parses with the left recursive rule
	body.SetWeights(1, 1)
	ok := 0
	for seed := range int64(100) {
		ctx := ebnf.NewGenContext(seed)
		ctx.MaxDepth = 60

		out, err := gen(t, expr, ctx)
		if errors.Is(err, ebnf.ErrMaxDepth) {
			continue // a long run of recursion, the depth limit did its job
		}
		if err != nil {
			t.Fatal(err)
		}

		if matched, _, perr := ebnf.Parse[rune, runes.Pos](concR(expr, endR()), newRunes(out)); perr != nil || !matched {
			t.Fatalf("seed %d generated %q, which does not parse (err %v)", seed, out, perr)
		}
		ok++
	}

	if ok < 50 {
		t.Errorf("only %d of 100 seeds generated", ok)
	}
}

func BenchmarkGenerateWords(b *testing.B) {
	// 3 to 6 syllables of a consonant and a vowel, joined by dashes, repeated
	vowel := altR(runeR('a'), runeR('e'), runeR('i'), runeR('o'), runeR('u'))
	consonant := altR(runeR('k'), runeR('m'), runeR('n'), runeR('s'), runeR('t'))
	word := repR(concR(consonant, vowel), 3, 6)
	words := repR(concR(word, runeR('-')), 20, 20)

	var sink sliceWriter

	b.ReportAllocs()

	for b.Loop() {
		sink.out = sink.out[:0]
		if err := words.Generate(ebnf.NewGenContext(1), &sink); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGenerateRecursiveLists(b *testing.B) {
	list := listGrammar()

	var sink sliceWriter

	b.ReportAllocs()

	for b.Loop() {
		sink.out = sink.out[:0]
		if err := list.Generate(ebnf.NewGenContext(7), &sink); err != nil {
			b.Fatal(err)
		}
	}
}
