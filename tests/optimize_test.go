package tests

import (
	"bytes"
	"math/rand"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/optimize"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

// The claim of the optimizer is that the grammar it changes matches exactly what the original did. This tests it on
// random grammars, which are built twice from the same seed so that one can be optimized and the other left alone. They are
// compared alone, and followed by other random grammars: an open grammar has to keep its meaning in every context.

type parsed struct {
	ok   bool
	end  int
	tree string
}

func parseWith(t testing.TB, p ebnf.Pattern[rune, runes.Pos], input []rune, memo bool) parsed {
	t.Helper()

	var rd ebnf.Reader[rune, runes.Pos] = newRunes(string(input))
	if memo {
		rd = ebnf.WithMemo[rune, runes.Pos](rd, func(p runes.Pos) int { return p.Index })
	}

	ok, m, err := ebnf.Parse(p, rd)
	if err != nil {
		t.Fatalf("input %q: %v", string(input), err)
	}

	if !ok {
		return parsed{}
	}

	return parsed{true, m.End.Index, fingerprint(m)}
}

// checkOptimized builds the grammar of the seed twice, optimizes one, and compares them on the input. It returns the
// number of patterns that were changed.
func checkOptimized(t testing.TB, seed int64, opts optimize.Options, input []rune, tails []gnode) int {
	t.Helper()

	build := func() gnode { return randomNode(rand.New(rand.NewSource(seed)), 3) }
	original, optimized := build(), build()

	res, err := optimize.Optimize[rune, runes.Pos](optimized.p, opts)
	if err != nil {
		t.Fatalf("seed %d: %v", seed, err)
	}

	var desc bytes.Buffer
	_ = original.p.Print(&desc)

	fail := func(what string, a, b parsed) {
		t.Helper()
		t.Fatalf("seed %d, grammar %s, input %q, %s:\noriginal  %+v\noptimized %+v\n%v", seed, desc.String(), string(input), what, a, b, res)
	}

	same := func(a, b parsed) bool { return a.ok == b.ok && a.end == b.end && a.tree == b.tree }

	for _, memo := range []bool{false, true} {
		if a, b := parseWith(t, original.p, input, memo), parseWith(t, optimized.p, input, memo); !same(a, b) {
			fail("alone", a, b)
		}
	}

	if a, b := parseWith(t, concR(original.p, endR()), input, false), parseWith(t, concR(optimized.p, endR()), input, false); !same(a, b) {
		fail("followed by the end of the input", a, b)
	}

	if !opts.Closed {
		// open: the same in front of anything
		for _, tail := range tails {
			if a, b := parseWith(t, concR(original.p, tail.p), input, false), parseWith(t, concR(optimized.p, tail.p), input, false); a.ok != b.ok || a.end != b.end {
				fail("followed by "+printed(tail.p), a, b)
			}
		}
	}

	changed := 0
	for _, d := range res.Safe() {
		_ = d
		changed++
	}

	return changed
}

func printed(p ebnf.Pattern[rune, runes.Pos]) string {
	var b bytes.Buffer
	_ = p.Print(&b)

	return b.String()
}

func TestOptimizedGrammarsMatchLikeTheOriginal(t *testing.T) {
	rng := rand.New(rand.NewSource(31337))

	for _, opts := range []optimize.Options{{}, {Closed: true}} {
		safe, cases := 0, 0

		for range 700 {
			seed := rng.Int63()

			tails := make([]gnode, 4)
			for i := range tails {
				tails[i] = randomNode(rng, 2)
			}

			for range 10 {
				input := make([]rune, rng.Intn(8))
				for i := range input {
					input[i] = rune('a' + rng.Intn(3))
				}

				safe += checkOptimized(t, seed, opts, input, tails)
				cases++
			}
		}

		// a test that never optimizes anything proves nothing
		if safe < cases/3 {
			t.Errorf("closed=%v: only %d safe decisions in %d cases", opts.Closed, safe, cases)
		}

		t.Logf("closed=%v: %d cases, %d safe decisions in all", opts.Closed, cases, safe)
	}
}

// FuzzOptimizedGrammarsMatchLikeTheOriginal lets the fuzzer choose the grammar, what follows it, and the input.
func FuzzOptimizedGrammarsMatchLikeTheOriginal(f *testing.F) {
	f.Add(int64(1), int64(2), "abcab", false)
	f.Add(int64(31337), int64(7), "", true)
	f.Add(int64(99), int64(3), "aaaa", false)

	f.Fuzz(func(t *testing.T, seed, tailSeed int64, raw string, closed bool) {
		trng := rand.New(rand.NewSource(tailSeed))
		tails := []gnode{randomNode(trng, 2), randomNode(trng, 2)}

		checkOptimized(t, seed, optimize.Options{Closed: closed}, abcInput(raw, 9), tails)
	})
}
