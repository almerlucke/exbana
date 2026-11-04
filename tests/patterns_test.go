package tests

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	"github.com/almerlucke/exbana/v3/patterns/end"
	ent "github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/except"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/patterns/vector"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

// Helper functions for patterns with rune type
func altR(patterns ...ebnf.Pattern[rune, runes.Pos]) *alternation.Alternation[rune, runes.Pos] {
	return alternation.New[rune, runes.Pos](patterns...)
}

func concR(patterns ...ebnf.Pattern[rune, runes.Pos]) *concatenation.Concatenation[rune, runes.Pos] {
	return concatenation.New[rune, runes.Pos](patterns...)
}

func endR() *end.End[rune, runes.Pos] {
	return end.New[rune, runes.Pos]()
}

func entR(matchFunc func(rune) bool) *ent.Entity[rune, runes.Pos] {
	return ent.New[rune, runes.Pos](matchFunc)
}

func runeR(r rune) *ent.Entity[rune, runes.Pos] {
	e := entR(func(obj rune) bool {
		return obj == r
	})
	e.SetGenerateFunc(func() rune { return r })
	return e
}

func exceptR(must ebnf.Pattern[rune, runes.Pos], exception ebnf.Pattern[rune, runes.Pos]) *except.Except[rune, runes.Pos] {
	return except.New[rune, runes.Pos](must, exception)
}

func repR(pattern ebnf.Pattern[rune, runes.Pos], min int, max int) *repetition.Repetition[rune, runes.Pos] {
	return repetition.New[rune, runes.Pos](pattern, min, max)
}

func vecR(s string) *vector.Vector[rune, runes.Pos] {
	return vector.New[rune, runes.Pos](func(r1, r2 rune) bool {
		return r1 == r2
	}, []rune(s)...)
}

func TestEntity(t *testing.T) {
	isDigit := entR(unicode.IsDigit)
	rd, _ := runes.New(strings.NewReader("1a"))

	matched, _, err := isDigit.Match(rd)
	if err != nil || !matched {
		t.Errorf("expected match for '1', got %v, %v", matched, err)
	}

	matched, _, err = isDigit.Match(rd)
	if err != nil || matched {
		t.Errorf("expected mismatch for 'a', got %v, %v", matched, err)
	}
}

func TestVector(t *testing.T) {
	vec := vecR("hello")
	rd, _ := runes.New(strings.NewReader("hello world"))

	matched, _, err := vec.Match(rd)
	if err != nil || !matched {
		t.Errorf("expected match for 'hello', got %v, %v", matched, err)
	}

	rd, _ = runes.New(strings.NewReader("hellp"))
	matched, _, err = vec.Match(rd)
	if err != nil || matched {
		t.Errorf("expected mismatch for 'hellp', got %v, %v", matched, err)
	}
}

func TestConcatenation(t *testing.T) {
	c := concR(runeR('a'), runeR('b'), runeR('c'))
	rd, _ := runes.New(strings.NewReader("abc"))

	matched, _, err := c.Match(rd)
	if err != nil || !matched {
		t.Errorf("expected match for 'abc', got %v, %v", matched, err)
	}

	rd, _ = runes.New(strings.NewReader("abd"))
	matched, _, err = c.Match(rd)
	if err != nil || matched {
		t.Errorf("expected mismatch for 'abd', got %v, %v", matched, err)
	}
}

func TestAlternation(t *testing.T) {
	// Test basic alternation
	a := altR(vecR("abc"), vecR("abcd"), vecR("ab"))
	rd, _ := runes.New(strings.NewReader("abcd"))

	matched, result, err := a.Match(rd)
	if err != nil || !matched {
		t.Errorf("expected match for 'abcd', got %v, %v", matched, err)
	}

	// Should match the longest one: "abcd"
	s, _ := rd.Range(result.Begin, result.End)
	if string(s) != "abcd" {
		t.Errorf("expected longest match 'abcd', got %s", string(s))
	}

	// Test orthogonal alternation
	a2 := altR(vecR("abc"), vecR("abcd")).SetOrthogonal(true)
	rd2, _ := runes.New(strings.NewReader("abcd"))
	matched, result, err = a2.Match(rd2)
	if err != nil || !matched {
		t.Errorf("expected match for 'abcd', got %v, %v", matched, err)
	}

	// Should match the first one in orthogonal mode: "abc"
	s2, _ := rd2.Range(result.Begin, result.End)
	if string(s2) != "abc" {
		t.Errorf("expected first match 'abc' in orthogonal mode, got %s", string(s2))
	}
}

func TestRepetition(t *testing.T) {
	// Test 1 or more
	r := repetition.OneOrMore[rune, runes.Pos](runeR('a'))
	rd, _ := runes.New(strings.NewReader("aaaab"))

	matched, result, err := r.Match(rd)
	if err != nil || !matched {
		t.Errorf("expected match for 'aaaa', got %v, %v", matched, err)
	}

	if len(result.Components) != 4 {
		t.Errorf("expected 4 components, got %d", len(result.Components))
	}

	// Test min/max
	r2 := repR(runeR('a'), 2, 3)
	rd2, _ := runes.New(strings.NewReader("aaaaa"))
	matched, result, err = r2.Match(rd2)
	if err != nil || !matched {
		t.Errorf("expected match for 'aaa', got %v, %v", matched, err)
	}
	if len(result.Components) != 3 {
		t.Errorf("expected 3 components, got %d", len(result.Components))
	}
}

func TestExcept(t *testing.T) {
	// Match any digit except '0'
	isDigit := entR(unicode.IsDigit)
	isZero := runeR('0')
	ex := exceptR(isDigit, isZero)

	rd, _ := runes.New(strings.NewReader("1"))
	matched, _, err := ex.Match(rd)
	if err != nil || !matched {
		t.Errorf("expected match for '1', got %v, %v", matched, err)
	}

	rd, _ = runes.New(strings.NewReader("0"))
	matched, _, err = ex.Match(rd)
	if err != nil || matched {
		t.Errorf("expected mismatch for '0', got %v, %v", matched, err)
	}
}

func TestEnd(t *testing.T) {
	e := endR()
	rd, _ := runes.New(strings.NewReader("a"))

	// Should not match before end
	matched, _, err := e.Match(rd)
	if err != nil || matched {
		t.Errorf("expected mismatch before end, got %v, %v", matched, err)
	}

	// Consume 'a'
	rd.Read1()

	// Should match at end
	matched, _, err = e.Match(rd)
	if err != nil || !matched {
		t.Errorf("expected match at end, got %v, %v", matched, err)
	}
}

func TestBacktracking(t *testing.T) {
	// (a)* , a
	// Greedily (a)* will take all 'a's, but then 'a' will fail.
	// Backtracking should make (a)* give back one 'a'.

	aStar := repetition.Any[rune, runes.Pos](runeR('a'))
	c := concR(aStar, runeR('a'))

	rd, _ := runes.New(strings.NewReader("aaa"))
	matched, result, err := c.Match(rd)
	if err != nil || !matched {
		t.Fatalf("expected match with backtracking, got %v, %v", matched, err)
	}

	if len(result.Components) != 2 {
		t.Errorf("expected 2 main components, got %d", len(result.Components))
	}

	starMatch := result.Components[0]
	if len(starMatch.Components) != 2 {
		t.Errorf("expected (a)* to match 2 'a's after backtracking, got %d", len(starMatch.Components))
	}
}

func TestPrint(t *testing.T) {
	// Test Print for various patterns
	a := ebnf.Name(altR(ebnf.Text(vecR("abc"), "abc"), ebnf.Text(vecR("def"), "def")), "Alt")
	c := ebnf.Name(concR(ebnf.Text(runeR('x'), "x"), a), "Conc")
	r := ebnf.Name(repetition.Any[rune, runes.Pos](c), "Rep")
	ex := ebnf.Name(exceptR(ebnf.Text(runeR('a'), "a"), ebnf.Text(runeR('b'), "b")), "Ex")

	var buf strings.Builder
	_ = a.Print(&buf)
	if buf.String() != "(abc | def)" {
		t.Errorf("expected (abc | def), got %s", buf.String())
	}

	buf.Reset()
	_ = c.Print(&buf)
	if buf.String() != "(x, Alt)" {
		t.Errorf("expected (x, Alt), got %s", buf.String())
	}

	buf.Reset()
	_ = r.Print(&buf)
	if buf.String() != "Conc*" {
		t.Errorf("expected Conc*, got %s", buf.String())
	}

	buf.Reset()
	_ = ex.Print(&buf)
	if buf.String() != "a - b" {
		t.Errorf("expected a - b, got %s", buf.String())
	}
}

func newRunes(s string) *runes.Reader {
	rd, _ := runes.New(strings.NewReader(s))
	return rd
}

func TestEntityAndVectorAtEOF(t *testing.T) {
	notQuote := entR(func(r rune) bool { return r != '"' })
	matched, _, err := notQuote.Match(newRunes(""))
	if err != nil || matched {
		t.Errorf("entity at EOF: expected clean mismatch, got %v, %v", matched, err)
	}

	matched, _, err = vecR("ab").Match(newRunes("a"))
	if err != nil || matched {
		t.Errorf("vector past EOF: expected clean mismatch, got %v, %v", matched, err)
	}

	rd := newRunes("a")
	rd.Skip(1)
	matched, _, err = vecR("").Match(rd)
	if err != nil || !matched {
		t.Errorf("empty vector at EOF: expected match, got %v, %v", matched, err)
	}
}

func TestRepetitionEmptyMatchTerminates(t *testing.T) {
	// Any(Optional(a)) can match without consuming input
	p := repetition.Any[rune, runes.Pos](repetition.Optional[rune, runes.Pos](runeR('a')))
	done := make(chan struct{})
	go func() {
		defer close(done)
		matched, _, err := p.Match(newRunes("b"))
		if err != nil || !matched {
			t.Errorf("expected (empty) match, got %v, %v", matched, err)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("repetition did not terminate on empty sub-match")
	}
}

func TestScanEmptyMatchTerminates(t *testing.T) {
	p := repetition.Any[rune, runes.Pos](runeR('a'))
	done := make(chan struct{})
	go func() {
		defer close(done)
		res, err := ebnf.Scan[rune, runes.Pos](newRunes("baab"), p)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(res) == 0 {
			t.Errorf("expected results")
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Scan did not terminate on empty match")
	}
}

func TestPrintRepetitionUnbounded(t *testing.T) {
	x := runeR('x')
	ebnf.Text(x, `"x"`)

	var buf bytes.Buffer
	if err := repR(x, 2, 0).Print(&buf); err != nil {
		t.Fatal(err)
	}
	if got, want := buf.String(), `(2 * "x", "x"*)`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}

	// children with an ID are printed by reference
	ebnf.Name(x, "ex")
	buf.Reset()
	if err := repR(x, 1, 3).Print(&buf); err != nil {
		t.Fatal(err)
	}
	if got, want := buf.String(), `(1 * ex, 2 * ex?)`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func anyR(p ebnf.Pattern[rune, runes.Pos]) *repetition.Repetition[rune, runes.Pos] {
	return repetition.Any[rune, runes.Pos](p)
}

func TestBacktrackingDeep(t *testing.T) {
	cases := []struct {
		name    string
		pattern ebnf.Pattern[rune, runes.Pos]
		input   string
		want    bool
	}{
		{"two repetitions", concR(anyR(runeR('a')), anyR(runeR('a')), runeR('b')), "aab", true},
		{"repetition inside alternation", concR(altR(anyR(runeR('a'))), runeR('a')), "aa", true},
		{"alternation longest first then shorter", concR(altR(vecR("a"), vecR("ab")), vecR("bc")), "abc", true},
		{"repetition of alternation", concR(anyR(altR(vecR("a"), vecR("ab"))), vecR("bc")), "aabc", true},
		{"nested concatenation", concR(concR(anyR(runeR('a')), runeR('b')), anyR(runeR('b')), runeR('b')), "abbb", true},
		{"except inside repetition", concR(anyR(exceptR(entR(unicode.IsLetter), runeR('x'))), runeR('x')), "abx", true},
		{"still fails", concR(anyR(runeR('a')), runeR('b')), "aac", false},
		{"min is respected", concR(repR(runeR('a'), 2, 0), runeR('a')), "aa", false},
		{"max is respected", concR(repR(runeR('a'), 0, 2), runeR('a'), endR()), "aaaa", false},
	}

	for _, c := range cases {
		matched, result, err := ebnf.Parse(c.pattern, newRunes(c.input))
		if err != nil || matched != c.want {
			t.Errorf("%s: got %v, %v, want %v", c.name, matched, err, c.want)
			continue
		}
		if matched && result.End.Index != len(c.input) && c.name != "max is respected" {
			t.Errorf("%s: match ended at %d, want %d", c.name, result.End.Index, len(c.input))
		}
	}
}

func TestPossessiveAndOrthogonalDoNotBacktrack(t *testing.T) {
	p := concR(anyR(runeR('a')).SetPossessive(true), runeR('a'))
	if matched, _, _ := p.Match(newRunes("aaa")); matched {
		t.Error("possessive repetition gave back a match")
	}

	o := concR(altR(vecR("ab"), vecR("a")).SetOrthogonal(true), vecR("bc"))
	if matched, _, _ := o.Match(newRunes("abc")); matched {
		t.Error("orthogonal alternation backtracked")
	}
}

func TestPositionAfterMatch(t *testing.T) {
	rd := newRunes("aaab")
	p := concR(anyR(runeR('a')), runeR('a'))
	matched, result, err := p.Match(rd)
	if err != nil || !matched {
		t.Fatal(matched, err)
	}
	if pos, _ := rd.Position(); pos.Index != result.End.Index || pos.Index != 3 {
		t.Errorf("reader at %d, match ends at %d, want 3", pos.Index, result.End.Index)
	}
}

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

func TestLongNonPossessiveRepetition(t *testing.T) {
	in := strings.Repeat("a", 200000)
	matched, _, err := concR(anyR(runeR('a')), runeR('a')).Match(newRunes(in))
	if err != nil || !matched {
		t.Errorf("got %v, %v", matched, err)
	}
}

func lit(r rune) *ent.Entity[rune, runes.Pos] {
	return ebnf.Text(runeR(r), `"`+string(r)+`"`)
}

// call = "(", digit+, ")"
func callGrammar() ebnf.Pattern[rune, runes.Pos] {
	digit := ebnf.Name(entR(unicode.IsDigit), "digit")
	return concR(lit('('), repetition.OneOrMore[rune, runes.Pos](digit), lit(')'))
}

func parseWithLog(g ebnf.Pattern[rune, runes.Pos], input string) (bool, *ebnf.FarthestLog[rune, runes.Pos]) {
	rd := newRunes(input)
	log := runes.NewFarthestLog(rd)
	matched, _, _ := ebnf.Parse(g, ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(log))
	return matched, log
}

func TestFarthestLogSyntaxError(t *testing.T) {
	matched, log := parseWithLog(callGrammar(), "(12x)")
	if matched {
		t.Fatal("expected mismatch")
	}

	e := log.Last()
	if e == nil {
		t.Fatal("expected a syntax error")
	}
	if e.Pos.Index != 3 || e.AtEnd || e.Found != 'x' {
		t.Errorf("wrong location: pos %v found %q atEnd %v", e.Pos, e.Found, e.AtEnd)
	}
	if got, want := strings.Join(e.Expected, ","), `digit,")"`; got != want {
		t.Errorf("expected %s, got %s", want, got)
	}
	if got, want := log.Err().Error(), `syntax error at 1:4: unexpected 'x', expected digit or ")"`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// the reader is left where it was
	rd := newRunes("(12x)")
	l := runes.NewFarthestLog(rd)
	g := callGrammar()
	ebnf.Parse(g, ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(l))
	before, _ := rd.Position()
	l.Last()
	if after, _ := rd.Position(); after != before {
		t.Errorf("Last moved the reader from %v to %v", before, after)
	}
}

func TestFarthestLogEndOfInput(t *testing.T) {
	_, log := parseWithLog(callGrammar(), "(12")
	if got, want := log.Err().Error(), `syntax error at 1:4: unexpected end of input, expected digit or ")"`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	_, log = parseWithLog(callGrammar(), "")
	if got, want := log.Err().Error(), `syntax error at 1:1: unexpected end of input, expected "("`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFarthestLogMultiLinePosition(t *testing.T) {
	g := concR(lit('a'), lit('\n'), lit('b'))
	_, log := parseWithLog(g, "a\nc")
	if got, want := log.Err().Error(), `syntax error at 2:1: unexpected 'c', expected "b"`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFarthestLogNothingLogged(t *testing.T) {
	matched, log := parseWithLog(callGrammar(), "(1)")
	if !matched {
		t.Fatal("expected match")
	}
	// the repetition fails its last iteration at ')', that is logged, but it is not an error: Err is only
	// meaningful after a failed match. After Reset there is nothing.
	log.Reset()
	if log.Err() != nil {
		t.Error("expected no error after reset")
	}
}

func TestFarthestLogKeepsFarthestAcrossAlternatives(t *testing.T) {
	// ("ab" , "c") | ("a" , "x")  on "abd": first branch fails at 2, second at 1
	g := altR(concR(lit('a'), lit('b'), lit('c')), concR(lit('a'), lit('x')))
	_, log := parseWithLog(g, "abd")
	e := log.Last()
	if e.Pos.Index != 2 || strings.Join(e.Expected, ",") != `"c"` {
		t.Errorf("got pos %d expected %v", e.Pos.Index, e.Expected)
	}
}

func TestFarthestLogIgnoresException(t *testing.T) {
	// string = '"', (char - '"')*, '"' : the exception '"' fails on every normal character and must not show up
	quote := lit('"')
	char := exceptR(ebnf.Name(entR(func(r rune) bool { return true }), "char"), quote)
	g := concR(quote, anyR(char), quote)
	matched, log := parseWithLog(g, `"abc`)
	if matched {
		t.Fatal("expected mismatch")
	}
	e := log.Last()
	if !e.AtEnd || e.Pos.Index != 4 {
		t.Errorf("expected failure at end of input, got %+v", e)
	}
	for _, x := range e.Expected {
		if x == "char" {
			continue
		}
		if x != `"\""` && x != `"""` {
			t.Errorf("unexpected entry %s", x)
		}
	}
}

func TestContextLoggerRecursiveGrammar(t *testing.T) {
	// list = "[" , (list | "x")* , "]"
	list := concR()
	inner := anyR(altR(list, lit('x')))
	list.SetPatterns(ebnf.Patterns[rune, runes.Pos]{lit('['), inner, lit(']')})
	ebnf.Name(list, "list")

	matched, log := parseWithLog(list, "[x[xy]]")
	if matched {
		t.Fatal("expected mismatch")
	}
	if e := log.Last(); e == nil || e.Pos.Index != 4 || e.Found != 'y' {
		t.Errorf("got %+v", e)
	}
}

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

func TestRuleMatchEvalAndRecursion(t *testing.T) {
	digit := ebnf.Name(entR(func(r rune) bool { return r >= '0' && r <= '9' }), "digit")
	number := rule.New[rune, runes.Pos]("number", concR(digit, repetition.New[rune, runes.Pos](digit, 0, 0))).
		WithEval(func(m *ebnf.Match[rune, runes.Pos], rd ebnf.Reader[rune, runes.Pos]) (any, error) {
			rs, err := rd.Range(m.Begin, m.End)
			return len(rs), err
		})

	// nest = number | '(' nest ')', recursion through a forward declaration
	nest := rule.Declare[rune, runes.Pos]("nest")
	nest.Define(altR(number, concR(runeR('('), nest, runeR(')'))))

	rd, _ := runes.New(strings.NewReader("((123))"))
	ok, m, err := nest.Match(rd)
	if err != nil || !ok || m.ID() != "nest" {
		t.Fatalf("ok=%v id=%v err=%v", ok, m, err)
	}

	// the number match sits two parentheses deep and evaluates through its rule
	n := m.Components[0].Components[1]
	n = n.Components[0].Components[1]
	n = n.Components[0].Unpack()
	if n.ID() != "number" {
		t.Fatalf("got %q", n.ID())
	}
	if v, err := n.Eval(rd); err != nil || v != 3 {
		t.Fatalf("eval = %v, %v", v, err)
	}
}

func TestRuleUndefinedIsAnError(t *testing.T) {
	r := rule.Declare[rune, runes.Pos]("later")
	rd, _ := runes.New(strings.NewReader("x"))
	if _, _, err := r.Match(rd); err == nil || !strings.Contains(err.Error(), "later") {
		t.Fatalf("err = %v", err)
	}
	if r.Defined() {
		t.Fatal("should not be defined")
	}
}

func TestRulePrintAndGenerate(t *testing.T) {
	r := rule.New[rune, runes.Pos]("ab", concR(ebnf.Text(runeR('a'), "a"), ebnf.Text(runeR('b'), "b")))
	var buf bytes.Buffer
	if err := r.Print(&buf); err != nil || buf.String() != "(a, b)" {
		t.Fatalf("print = %q, %v", buf.String(), err)
	}
	if rules, err := ebnf.PrintRules([]ebnf.Pattern[rune, runes.Pos]{r}); err != nil || rules != "ab = (a, b)\n" {
		t.Fatalf("rules = %q, %v", rules, err)
	}
	buf.Reset()
	if err := concR(r, r).Print(&buf); err != nil || buf.String() != "(ab, ab)" {
		t.Fatalf("print as child = %q, %v", buf.String(), err)
	}
}

func TestRuleAtomicNamesTheRuleInSyntaxErrors(t *testing.T) {
	digit := ebnf.Name(entR(func(r rune) bool { return r >= '0' && r <= '9' }), "digit")
	sign := ebnf.Name(entR(func(r rune) bool { return r == '-' }), "minus")
	for _, atomic := range []bool{false, true} {
		number := rule.New[rune, runes.Pos]("number", concR(repetition.New[rune, runes.Pos](sign, 0, 1), digit)).SetAtomic(atomic)

		rd, _ := runes.New(strings.NewReader("x"))
		log := runes.NewFarthestLog(rd)
		ctx := ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(log)
		if ok, _, _ := number.Match(ctx); ok {
			t.Fatal("should not match")
		}

		msg := log.Err().Error()
		if atomic && !strings.HasSuffix(msg, "expected number") {
			t.Fatalf("atomic: %s", msg)
		}
		if !atomic && !strings.Contains(msg, "digit") {
			t.Fatalf("non atomic: %s", msg)
		}
	}
}

// anyOne is a pattern written against the three method core: it matches any single rune
type anyOne struct{}

func (anyOne) MatchCont(r ebnf.Reader[rune, runes.Pos], k ebnf.Continuation[rune, runes.Pos]) (bool, error) {
	return ebnf.Single(r, k, func(r ebnf.Reader[rune, runes.Pos]) (bool, *ebnf.Match[rune, runes.Pos], error) {
		begin, _ := r.Position()
		if _, err := r.Read1(); err != nil {
			return false, nil, nil
		}
		end, _ := r.Position()
		return true, ebnf.NewMatch[rune, runes.Pos](anyOne{}, begin, end, nil, nil), nil
	})
}

func (anyOne) Generate(_ *ebnf.GenContext, w ebnf.Writer[rune]) error { return w.Write('?') }

func (anyOne) Print(w io.Writer) error {
	_, err := io.WriteString(w, "any")
	return err
}

func TestCustomPatternNeedsOnlyThreeMethods(t *testing.T) {
	g := concR(anyOne{}, runeR('b'), anyOne{})

	ok, m, err := ebnf.Parse[rune, runes.Pos](g, newRunes("abc"))
	if err != nil || !ok || len(m.Components) != 3 {
		t.Fatalf("ok=%v m=%v err=%v", ok, m, err)
	}

	// no name, no evaluator: ID is empty and Eval gives the value
	if m.Components[0].ID() != "" {
		t.Error("unnamed pattern has an ID")
	}
	if v, err := m.Components[0].Eval(newRunes("abc")); err != nil || v == nil {
		t.Errorf("eval should give the (empty) value, got %v, %v", v, err)
	}

	var buf bytes.Buffer
	if err := g.Print(&buf); err != nil || buf.String() != "(any, , any)" {
		t.Errorf("print = %q, %v", buf.String(), err)
	}

	if ok, _, _ := ebnf.Parse[rune, runes.Pos](g, newRunes("abx")); !ok {
		t.Error("abx should match, the last pattern takes any rune")
	}
	if ok, _, _ := ebnf.Parse[rune, runes.Pos](g, newRunes("axc")); ok {
		t.Error("axc should not match")
	}
}

func TestNameAndTextHelpers(t *testing.T) {
	d := ebnf.Name(entR(unicode.IsDigit), "digit")
	if d.ID() != "digit" || ebnf.IDOf[rune, runes.Pos](d) != "digit" {
		t.Error("name not set")
	}

	plus := ebnf.Text(runeR('+'), `"+"`)
	var buf bytes.Buffer
	if err := concR(plus, d).Print(&buf); err != nil || buf.String() != `("+", digit)` {
		t.Errorf("print = %q, %v", buf.String(), err)
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

func TestNamedAndPrintedChainOnEveryPattern(t *testing.T) {
	a := runeR('a')

	named := map[string]ebnf.Named{
		"entity":        entR(unicode.IsDigit).Named("n"),
		"vector":        vecR("ab").Named("n"),
		"end":           endR().Named("n"),
		"alternation":   altR(a).Named("n"),
		"concatenation": concR(a).Named("n"),
		"repetition":    repR(a, 0, 0).Named("n"),
		"except":        exceptR(a, a).Named("n"),
	}
	for name, p := range named {
		if p.ID() != "n" {
			t.Errorf("%s: ID = %q", name, p.ID())
		}
	}

	// text is what leaves print as, and chains with the other setters
	var buf bytes.Buffer
	leaf := vecR("ab").Printed("<ab>").Named("ab")
	if err := leaf.Print(&buf); err != nil || buf.String() != "<ab>" {
		t.Errorf("print = %q, %v", buf.String(), err)
	}
	buf.Reset()
	if err := ebnf.PrintChild[rune, runes.Pos](&buf, leaf); err != nil || buf.String() != "ab" {
		t.Errorf("as child = %q, %v", buf.String(), err)
	}
	if got := repR(a, 0, 0).Named("r").SetPossessive(true); !got.Possessive() || got.ID() != "r" {
		t.Error("Named does not chain into the other setters")
	}
	buf.Reset()
	if err := endR().Printed("$").Print(&buf); err != nil || buf.String() != "$" {
		t.Errorf("end = %q, %v", buf.String(), err)
	}
}
