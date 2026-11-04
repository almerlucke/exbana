package tests

import (
	"slices"
	"sync"
	"testing"
	"unicode"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

func TestLogMismatchOnlyBuildsWhenListening(t *testing.T) {
	p := runeR('a')
	built := 0
	build := func() *ebnf.Mismatch[rune, runes.Pos] {
		built++
		return nil
	}

	ebnf.LogMismatch[rune, runes.Pos](newRunes("a"), p, build)
	ebnf.LogMismatch[rune, runes.Pos](ebnf.NewParseContext[rune, runes.Pos](newRunes("a")), p, build)
	if built != 0 {
		t.Errorf("mismatch was built %d times with nobody listening", built)
	}

	log := &recorder{}
	ebnf.LogMismatch[rune, runes.Pos](ebnf.NewParseContext[rune, runes.Pos](newRunes("a")).WithLogger(log), p, func() *ebnf.Mismatch[rune, runes.Pos] {
		built++
		return &ebnf.Mismatch[rune, runes.Pos]{}
	})
	if built != 1 || len(log.mismatches) != 1 {
		t.Errorf("built %d, logged %d", built, len(log.mismatches))
	}
}

// recorder is a Logger that keeps every mismatch it is given
type recorder struct {
	mismatches []*ebnf.Mismatch[rune, runes.Pos]
}

func (r *recorder) LogMismatch(m *ebnf.Mismatch[rune, runes.Pos]) {
	r.mismatches = append(r.mismatches, m)
}

// one grammar, many parses at the same time, each logging to its own logger (run with -race)
func TestParseContextSharedGrammar(t *testing.T) {
	g := concR(runeR('('), repetition.OneOrMore[rune, runes.Pos](ebnf.Name(entR(unicode.IsDigit), "digit")), runeR(')'))

	count := func() int {
		log := &recorder{}
		g.Match(ebnf.NewParseContext[rune, runes.Pos](newRunes("(12x)")).WithLogger(log))
		return len(log.mismatches)
	}
	want := count()
	if want == 0 {
		t.Fatal("nothing logged")
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				if got := count(); got != want {
					t.Errorf("logged %d mismatches, want %d", got, want)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestParseContextWithoutMemoBehavesPlain(t *testing.T) {
	g := concR(anyR(altR(vecR("a"), vecR("ab"))), vecR("bc"), endR())
	for _, in := range []string{"aabc", "abc", "abx", ""} {
		plain, _, _ := g.Match(newRunes(in))
		ctx, _, _ := g.Match(ebnf.NewParseContext[rune, runes.Pos](newRunes(in)))
		memo, _, _ := g.Match(ebnf.NewParseContext[rune, runes.Pos](newRunes(in)).WithMemo(func(p runes.Pos) int { return p.Index }))
		if plain != ctx || plain != memo {
			t.Errorf("%q: plain %v, context %v, memo %v", in, plain, ctx, memo)
		}
	}
}

// the exception of an except is lookahead: it fails on every normal match, and must not show up in syntax errors
func TestParseContextLoggerIgnoresException(t *testing.T) {
	quote := lit('"')
	char := exceptR(ebnf.Name(entR(func(r rune) bool { return true }), "char"), altR(quote, ebnf.Name(lit('\\'), "backslash")))
	g := concR(quote, anyR(char), quote)

	rd := newRunes(`"ab`)
	log := runes.NewFarthestLog(rd)
	ctx := ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(log)
	if matched, _, _ := g.Match(ctx); matched {
		t.Fatal("expected mismatch")
	}

	e := log.Last()
	if !e.AtEnd || e.Pos.Index != 3 {
		t.Fatalf("expected failure at end of input, got %+v", e)
	}
	for _, x := range e.Expected {
		if x == "backslash" || x == `"\\"` {
			t.Errorf("exception leaked into expected list: %v", e.Expected)
		}
	}
	t.Logf("expected: %v", e.Expected)
}

// failureRecorder is a logger that only wants to know where leaves failed
type failureRecorder struct {
	failures   []int
	mismatches int
}

func (f *failureRecorder) LogMismatch(*ebnf.Mismatch[rune, runes.Pos]) { f.mismatches++ }

func (f *failureRecorder) LogFailure(p ebnf.Pattern[rune, runes.Pos], begin runes.Pos) {
	f.failures = append(f.failures, begin.Index)
}

func TestFailureLoggerIsToldWithoutAMismatch(t *testing.T) {
	g := altR(concR(runeR('a'), runeR('b')), vecR("ac"))

	rec := &failureRecorder{}
	ebnf.Parse[rune, runes.Pos](g, ebnf.NewParseContext[rune, runes.Pos](newRunes("ax")).WithLogger(rec))

	// the entity b failed at 1, the vector ac at 0 and so did the alternation as a whole; the concatenation's own
	// mismatch (it carries details) still arrives as a Mismatch
	slices.Sort(rec.failures)
	if !slices.Equal(rec.failures, []int{0, 0, 1}) {
		t.Errorf("failures = %v", rec.failures)
	}
	if rec.mismatches == 0 {
		t.Error("a detailed mismatch should still be a Mismatch")
	}

	// a logger that is not a FailureLogger gets a Mismatch for the same failures, with the end position
	plain := &recorder{}
	ebnf.Parse[rune, runes.Pos](g, ebnf.NewParseContext[rune, runes.Pos](newRunes("ax")).WithLogger(plain))

	leaves := 0
	for _, m := range plain.mismatches {
		if m.Unmatched == nil && len(m.Matched) == 0 {
			leaves++
		}
	}
	if leaves != len(rec.failures) {
		t.Errorf("plain logger saw %d leaf mismatches, failure logger %d", leaves, len(rec.failures))
	}
}
