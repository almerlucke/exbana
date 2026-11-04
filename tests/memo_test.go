package tests

import (
	"slices"
	"strings"
	"testing"
	"time"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

func TestMemoGivesSameResult(t *testing.T) {
	build := func() ebnf.Pattern[rune, runes.Pos] {
		return concR(anyR(altR(vecR("a"), vecR("ab"), vecR("b"))), vecR("c"), endR())
	}
	for _, in := range []string{"abababc", "aabbc", "abx", "", "c"} {
		m1, r1, _ := ebnf.Parse(build(), newRunes(in))
		m2, r2, _ := ebnf.Parse(build(), ebnf.WithMemo[rune, runes.Pos](newRunes(in), func(p runes.Pos) int { return p.Index }))
		if m1 != m2 || (m1 && r1.End.Index != r2.End.Index) {
			t.Errorf("%q: plain %v, memo %v", in, m1, m2)
		}
	}
}

// (a*)* is the classic catastrophic backtracking case, it can only finish quickly if failures are remembered
func TestMemoAvoidsExponentialBlowup(t *testing.T) {
	in := strings.Repeat("a", 28) + "b"
	build := func() ebnf.Pattern[rune, runes.Pos] {
		return concR(anyR(anyR(runeR('a'))), runeR('c'))
	}

	rd := ebnf.WithMemo[rune, runes.Pos](newRunes(in), func(p runes.Pos) int { return p.Index })
	done := make(chan bool)
	go func() {
		matched, _, _ := ebnf.Parse(build(), rd)
		done <- matched
	}()
	select {
	case matched := <-done:
		if matched {
			t.Error("unexpected match")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("memoization did not prevent exponential backtracking")
	}
}

func TestMemoMakesNestedAlternationsPolynomial(t *testing.T) {
	// 3^30 steps without the memo, which would never finish
	g := unfactoredGrammar(30)

	done := make(chan *ebnf.Match[rune, runes.Pos], 1)
	go func() {
		ok, m, _ := ebnf.Parse(g, ebnf.WithMemo[rune, runes.Pos](newRunes("a-a"), func(p runes.Pos) int { return p.Index }))
		if !ok {
			m = nil
		}
		done <- m
	}()

	select {
	case m := <-done:
		if m == nil || m.End.Index != 3 {
			t.Fatalf("got %+v", m)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("did not finish, the first match of a pattern is not memoized")
	}
}

func TestMemoizedFirstMatchGivesSameResults(t *testing.T) {
	g := unfactoredGrammar(6)
	for _, in := range []string{"a", "a+a", "a-a", "a+", "b", ""} {
		ok1, m1, _ := ebnf.Parse(g, newRunes(in))
		ok2, m2, _ := ebnf.Parse(g, ebnf.WithMemo[rune, runes.Pos](newRunes(in), func(p runes.Pos) int { return p.Index }))
		if ok1 != ok2 || (ok1 && m1.End.Index != m2.End.Index) {
			t.Errorf("%q: plain %v, memo %v", in, ok1, ok2)
		}
	}
}

func TestMemoWithManyEndPositions(t *testing.T) {
	// a* can end in 41 ways, more than the memo scans through before it switches to a set
	idx := func(p runes.Pos) int { return p.Index }

	for _, n := range []int{3, 8, 9, 40} {
		in := strings.Repeat("a", n)

		ends := func(r ebnf.Reader[rune, runes.Pos]) []int {
			var out []int
			// both branches end in the same places, so every end is offered twice and only the memo leaves out the repeats
			for m, err := range ebnf.Matches[rune, runes.Pos](altR(repR(runeR('a'), 0, 0), repR(runeR('a'), 0, 0)), r) {
				if err != nil {
					t.Fatal(err)
				}
				out = append(out, m.End.Index)
			}
			return out
		}

		plain, memo := ends(newRunes(in)), ends(ebnf.WithMemo[rune, runes.Pos](newRunes(in), idx))
		if len(plain) != 2*(n+1) {
			t.Errorf("n=%d: plain offers %d matches, want %d", n, len(plain), 2*(n+1))
		}
		want := make([]int, n+1)
		for i := range want {
			want[i] = n - i
		}
		if !slices.Equal(memo, want) {
			t.Errorf("n=%d: memo %v, want each end once: %v", n, memo, want)
		}

		// and backtracking through all of them works: a* then "ab" needs the last 'a' back
		g := concR(repR(runeR('a'), 0, 0), vecR("ab"), endR())
		for _, suffix := range []string{"b", "c"} {
			r1, r2 := newRunes(in+suffix), ebnf.WithMemo[rune, runes.Pos](newRunes(in+suffix), idx)
			ok1, _, _ := ebnf.Parse[rune, runes.Pos](g, r1)
			ok2, _, _ := ebnf.Parse[rune, runes.Pos](g, r2)
			if ok1 != ok2 || ok1 != (suffix == "b" && n > 0) {
				t.Errorf("n=%d suffix %q: plain %v, memo %v", n, suffix, ok1, ok2)
			}
		}
	}
}

func TestMemoizedRepetitionDoesNotRecursePerItem(t *testing.T) {
	if testing.Short() {
		t.Skip("a million items take a few seconds")
	}

	// hundreds of thousands of items: a call stack with a few nested calls per item would not fit in the stack limit
	const n = 1_000_000

	in := strings.Repeat("a", n)
	g := concR(repR(runeR('a'), 0, 0), runeR('b'))

	// a* then "b" has to give all the way back for the wrong last character, and reports no match
	rd := ebnf.WithMemo[rune, runes.Pos](newRunes(in+"c"), func(p runes.Pos) int { return p.Index })
	if ok, _, err := ebnf.Parse[rune, runes.Pos](g, rd); ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}

	rd = ebnf.WithMemo[rune, runes.Pos](newRunes(in+"b"), func(p runes.Pos) int { return p.Index })
	ok, m, err := ebnf.Parse[rune, runes.Pos](g, rd)
	if err != nil || !ok || m.End.Index != n+1 || len(m.Components[0].Components) != n {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}
