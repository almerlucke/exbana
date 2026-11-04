package tests

import (
	"errors"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

type rr = *rule.Rule[rune, runes.Pos]

func evalNum(m *ebnf.Match[rune, runes.Pos], rd ebnf.Reader[rune, runes.Pos]) (any, error) {
	rs, err := rd.Range(m.Begin, m.End)
	if err != nil {
		return nil, err
	}
	return strconv.Atoi(string(rs))
}

func digitsRule() rr {
	digit := ebnf.Name(entR(func(r rune) bool { return r >= '0' && r <= '9' }), "digit")
	return rule.New[rune, runes.Pos]("num", repetition.OneOrMore[rune, runes.Pos](digit)).WithEval(evalNum)
}

func evalFirst(m *ebnf.Match[rune, runes.Pos], rd ebnf.Reader[rune, runes.Pos]) (any, error) {
	return m.Components[0].Eval(rd)
}

// expr = expr "-" num | num, left recursive
func subtractionGrammar(leftRecursive bool) (expr rr, full ebnf.Pattern[rune, runes.Pos]) {
	num := digitsRule()
	expr = rule.Declare[rune, runes.Pos]("expr").WithEval(evalFirst).SetLeftRecursive(leftRecursive)
	sub := rule.New[rune, runes.Pos]("sub", concR(expr, runeR('-'), num)).
		WithEval(func(m *ebnf.Match[rune, runes.Pos], rd ebnf.Reader[rune, runes.Pos]) (any, error) {
			l, err := m.Components[0].Eval(rd)
			if err != nil {
				return nil, err
			}
			r, err := m.Components[2].Eval(rd)
			if err != nil {
				return nil, err
			}
			return l.(int) - r.(int), nil
		})
	expr.Define(altR(sub, num))
	return expr, concR(expr, endR())
}

func TestLeftRecursionIsLeftAssociative(t *testing.T) {
	_, g := subtractionGrammar(true)

	for in, want := range map[string]int{"7": 7, "10-3": 7, "10-3-2": 5, "100-1-2-3-4": 90} {
		rd := newRunes(in)
		ok, m, err := ebnf.Parse[rune, runes.Pos](g, rd)
		if err != nil || !ok {
			t.Errorf("%q: ok=%v err=%v", in, ok, err)
			continue
		}
		// the first component of the grammar is the expression
		v, err := m.Components[0].Eval(rd)
		if err != nil || v != want {
			t.Errorf("%q = %v (%v), want %d, the subtractions must group to the left", in, v, err, want)
		}
	}

	for _, in := range []string{"", "-", "1-", "-1", "1--2", "1-2-"} {
		if ok, _, err := ebnf.Parse[rune, runes.Pos](g, newRunes(in)); ok || err != nil {
			t.Errorf("%q: ok=%v err=%v, expected a plain mismatch", in, ok, err)
		}
	}
}

func TestLeftRecursionPartialMatch(t *testing.T) {
	expr, _ := subtractionGrammar(true)

	// the longest match comes first, the shorter ones are there to backtrack to
	var ends []int
	for m, err := range ebnf.Matches[rune, runes.Pos](expr, newRunes("1-2-3x")) {
		if err != nil {
			t.Fatal(err)
		}
		ends = append(ends, m.End.Index)
	}
	if len(ends) != 3 || ends[0] != 5 || ends[1] != 3 || ends[2] != 1 {
		t.Errorf("ends = %v, want [5 3 1]", ends)
	}
}

func TestLeftRecursionWithoutMarkIsAnErrorNotAStackOverflow(t *testing.T) {
	_, g := subtractionGrammar(false)

	_, _, err := ebnf.Parse[rune, runes.Pos](g, newRunes("1-2"))
	if !errors.Is(err, rule.ErrLeftRecursion) || !strings.Contains(err.Error(), `"expr"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestRepeatedAndEmptyMatchesAreNotLeftRecursion(t *testing.T) {
	// an unmarked rule can match the empty input and be used again at the same position, that is not recursion.
	// A ParseContext makes the rules share their bookkeeping, as they do in every real parse that logs or memoizes.
	maybe := rule.New[rune, runes.Pos]("maybe", repetition.Optional[rune, runes.Pos](runeR('a')))
	g := concR(maybe, maybe, repR(maybe, 0, 3), endR())

	readers := map[string]func(string) ebnf.Reader[rune, runes.Pos]{
		"plain": func(in string) ebnf.Reader[rune, runes.Pos] { return newRunes(in) },
		"context": func(in string) ebnf.Reader[rune, runes.Pos] {
			return ebnf.NewParseContext[rune, runes.Pos](newRunes(in))
		},
		"memo": func(in string) ebnf.Reader[rune, runes.Pos] {
			return ebnf.WithMemo[rune, runes.Pos](newRunes(in), func(p runes.Pos) int { return p.Index })
		},
	}

	for name, newReader := range readers {
		for _, in := range []string{"", "a", "aa", "aaa", "aaaaa", "aaaaaa"} {
			ok, _, err := ebnf.Parse[rune, runes.Pos](g, newReader(in))
			if err != nil {
				t.Fatalf("%s %q: %v", name, in, err)
			}
			if want := len(in) <= 5; ok != want {
				t.Errorf("%s %q: ok=%v want %v", name, in, ok, want)
			}
		}

		// the same rule twice in a row, and as both branches of an alternation
		for _, p := range []ebnf.Pattern[rune, runes.Pos]{concR(maybe, maybe), altR(maybe, maybe)} {
			if _, _, err := ebnf.Parse(p, newReader("a")); err != nil {
				t.Errorf("%s: %v", name, err)
			}
		}
	}
}

func TestLeftRecursionEndsForGrammarsThatAreNotMonotone(t *testing.T) {
	// l = "a" - l: once l has matched, the exception stops it from matching. There is no meaningful answer, but
	// the rounds must end, and with the seed only growing the answer is the first round's.
	l := rule.Declare[rune, runes.Pos]("l").SetLeftRecursive(true)
	l.Define(exceptR(vecR("a"), l))

	done := make(chan bool, 1)
	go func() {
		ok, _, _ := ebnf.Parse[rune, runes.Pos](l, newRunes("a"))
		done <- ok
	}()

	select {
	case ok := <-done:
		if !ok {
			t.Error("the matches of an earlier round were lost")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("did not end")
	}
}

func TestIndirectLeftRecursion(t *testing.T) {
	// a = b "a" | "b"   b = a "c"   so a = "b" ("ca")*, recursion through b, only a is marked
	a := rule.Declare[rune, runes.Pos]("a").SetLeftRecursive(true)
	b := rule.Declare[rune, runes.Pos]("b")
	a.Define(altR(concR(b, runeR('a')), runeR('b')))
	b.Define(concR(a, runeR('c')))
	full := concR(a, endR())

	for in, want := range map[string]bool{"b": true, "bca": true, "bcaca": true, "bcacaca": true, "": false, "bc": false, "bcac": false, "ca": false} {
		ok, _, err := ebnf.Parse[rune, runes.Pos](full, newRunes(in))
		if err != nil || ok != want {
			t.Errorf("%q: ok=%v err=%v, want %v", in, ok, err, want)
		}
	}
}

func TestLeftRecursionWithANullableBase(t *testing.T) {
	// l = l "a" | "b"*   which is b* a*
	l := rule.Declare[rune, runes.Pos]("l").SetLeftRecursive(true)
	l.Define(altR(concR(l, runeR('a')), repR(runeR('b'), 0, 0)))
	full := concR(l, endR())

	for in, want := range map[string]bool{"": true, "a": true, "b": true, "aa": true, "bba": true, "bbaa": true, "ab": false, "aba": false} {
		ok, _, err := ebnf.Parse[rune, runes.Pos](full, newRunes(in))
		if err != nil || ok != want {
			t.Errorf("%q: ok=%v err=%v, want %v", in, ok, err, want)
		}
	}
}

func TestLeftRecursionWithoutABaseCaseFails(t *testing.T) {
	// l = l "a": nothing to start from
	l := rule.Declare[rune, runes.Pos]("l").SetLeftRecursive(true)
	l.Define(concR(l, runeR('a')))

	ok, _, err := ebnf.Parse[rune, runes.Pos](l, newRunes("aaa"))
	if ok || err != nil {
		t.Errorf("ok=%v err=%v", ok, err)
	}
}

// the same language written with left recursion and with a loop
func arithmetic(leftRecursive bool) ebnf.Pattern[rune, runes.Pos] {
	expr := rule.Declare[rune, runes.Pos]("expr").SetLeftRecursive(leftRecursive)
	num := rule.New[rune, runes.Pos]("num", runeR('1'))
	term := rule.Declare[rune, runes.Pos]("term")
	term.Define(altR(num, concR(runeR('('), expr, runeR(')'))))
	op := altR(runeR('+'), runeR('-'))

	if leftRecursive {
		expr.Define(altR(concR(expr, op, term), term))
	} else {
		expr.Define(concR(term, repR(concR(op, term), 0, 0)))
	}

	return expr
}

func TestLeftRecursiveGrammarMatchesTheLoopGrammar(t *testing.T) {
	left, loop := arithmetic(true), arithmetic(false)
	rng := rand.New(rand.NewSource(7))
	idx := func(p runes.Pos) int { return p.Index }

	matched := 0

	for range 3000 {
		in := make([]byte, rng.Intn(10))
		for i := range in {
			in[i] = "11+-()"[rng.Intn(6)]
		}

		for _, memo := range []bool{false, true} {
			parse := func(p ebnf.Pattern[rune, runes.Pos], full bool) (bool, int) {
				if full {
					p = concR(p, endR())
				}
				var rd ebnf.Reader[rune, runes.Pos] = newRunes(string(in))
				if memo {
					rd = ebnf.WithMemo[rune, runes.Pos](rd, idx)
				}
				ok, m, err := ebnf.Parse(p, rd)
				if err != nil {
					t.Fatalf("%q: %v", in, err)
				}
				if !ok {
					return false, -1
				}
				return true, m.End.Index
			}

			for _, full := range []bool{false, true} {
				okL, endL := parse(left, full)
				okI, endI := parse(loop, full)
				if okL != okI || endL != endI {
					t.Fatalf("%q memo=%v full=%v: left recursive (%v, %d), loop (%v, %d)", in, memo, full, okL, endL, okI, endI)
				}
				if okL {
					matched++
				}
			}
		}
	}

	if matched < 500 {
		t.Errorf("only %d matches, the inputs are too random to test anything", matched)
	}
}

func TestLeftRecursionSharedBetweenGoroutines(t *testing.T) {
	_, g := subtractionGrammar(true)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				if ok, _, err := ebnf.Parse[rune, runes.Pos](g, newRunes("9-8-7-6")); !ok || err != nil {
					t.Errorf("ok=%v err=%v", ok, err)
					return
				}
				if ok, _, _ := ebnf.Parse[rune, runes.Pos](g, newRunes("9-8-")); ok {
					t.Error("matched 9-8-")
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestLongLeftRecursiveChain(t *testing.T) {
	_, g := subtractionGrammar(true)
	in := "1" + strings.Repeat("-1", 400)

	start := time.Now()
	ok, _, err := ebnf.Parse[rune, runes.Pos](g, newRunes(in))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("a chain of 400 terms took %v", d)
	}
}

func TestLeftRecursionPlainAndMemoizedAgree(t *testing.T) {
	expr, _ := subtractionGrammar(true)
	idx := func(p runes.Pos) int { return p.Index }

	for _, in := range []string{"1", "1-2", "1-2-3-4", "1-2-x", "x"} {
		var plain, memo []int
		for m, _ := range ebnf.Matches[rune, runes.Pos](expr, newRunes(in)) {
			plain = append(plain, m.End.Index)
		}
		for m, _ := range ebnf.Matches[rune, runes.Pos](expr, ebnf.WithMemo[rune, runes.Pos](newRunes(in), idx)) {
			memo = append(memo, m.End.Index)
		}
		if len(plain) != len(memo) {
			t.Errorf("%q: plain %v, memo %v", in, plain, memo)
			continue
		}
		for i := range plain {
			if plain[i] != memo[i] {
				t.Errorf("%q: plain %v, memo %v", in, plain, memo)
				break
			}
		}
	}
}

func TestMatchesIteratesInOrderAndStopsEarly(t *testing.T) {
	// (a | ab | abc) followed by anything: three ways, longest first
	g := altR(vecR("a"), vecR("ab"), vecR("abc"))
	rd := newRunes("abcd")

	var ends []int
	for m, err := range ebnf.Matches[rune, runes.Pos](g, rd) {
		if err != nil {
			t.Fatal(err)
		}
		ends = append(ends, m.End.Index)

		// the reader stands at the end of the match while the loop body runs
		if pos, _ := rd.Position(); pos.Index != m.End.Index {
			t.Errorf("reader at %d, match ends at %d", pos.Index, m.End.Index)
		}
	}
	if len(ends) != 3 || ends[0] != 3 || ends[1] != 2 || ends[2] != 1 {
		t.Errorf("ends = %v", ends)
	}

	// stopping early: the first match only
	count := 0
	for range ebnf.Matches[rune, runes.Pos](g, newRunes("abcd")) {
		count++
		break
	}
	if count != 1 {
		t.Errorf("count = %d", count)
	}

	// no match: an empty iteration
	for range ebnf.Matches[rune, runes.Pos](g, newRunes("x")) {
		t.Error("matched x")
	}
}

func TestMatchesReportsErrors(t *testing.T) {
	var got error
	for m, err := range ebnf.Matches[rune, runes.Pos](rule.Declare[rune, runes.Pos]("later"), newRunes("x")) {
		if m != nil {
			t.Error("got a match")
		}
		got = err
	}
	if got == nil || !strings.Contains(got.Error(), "later") {
		t.Errorf("err = %v", got)
	}
}

func TestMatchesFindsAmbiguity(t *testing.T) {
	// (a | a)  has two parses of "a" that end at the same place, a* a* has several
	ambiguous := concR(repR(runeR('a'), 0, 0), repR(runeR('a'), 0, 0), endR())
	unambiguous := concR(repR(runeR('a'), 0, 0), runeR('b'), endR())

	count := func(p ebnf.Pattern[rune, runes.Pos], in string) int {
		n := 0
		for range ebnf.Matches[rune, runes.Pos](p, newRunes(in)) {
			n++
		}
		return n
	}

	if n := count(ambiguous, "aa"); n != 3 {
		t.Errorf("a* a* on aa: %d parses, want 3", n)
	}
	if n := count(unambiguous, "aab"); n != 1 {
		t.Errorf("a* b on aab: %d parses, want 1", n)
	}
}
