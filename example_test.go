package exbana_test

import (
	"fmt"
	"io"
	"strings"
	"unicode"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	"github.com/almerlucke/exbana/v3/patterns/end"
	ent "github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/patterns/vector"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

type (
	pattern = ebnf.Pattern[rune, runes.Pos]
	reader  = ebnf.Reader[rune, runes.Pos]
)

func text(s string) *runes.Reader {
	rd, err := runes.New(strings.NewReader(s))
	if err != nil {
		panic(err)
	}

	return rd
}

func char(c rune) *ent.Entity[rune, runes.Pos] {
	return ent.New[rune, runes.Pos](func(r rune) bool { return r == c }).Printed(`"` + string(c) + `"`)
}

func digit() *ent.Entity[rune, runes.Pos] {
	return ent.New[rune, runes.Pos](unicode.IsDigit).Named("digit")
}

// integer = [ "-" ] digit { digit }
func integer() *concatenation.Concatenation[rune, runes.Pos] {
	return concatenation.New[rune, runes.Pos](
		repetition.Optional[rune, runes.Pos](char('-')),
		digit(),
		repetition.Any[rune, runes.Pos](digit()),
	).Named("integer")
}

// Parse matches a grammar against a reader and returns the most preferred match.
func ExampleParse() {
	rd := text("-123 apples")

	ok, match, err := ebnf.Parse[rune, runes.Pos](integer(), rd)
	if err != nil || !ok {
		fmt.Println("no match", err)
		return
	}

	matched, _ := rd.Range(match.Begin, match.End)
	fmt.Printf("matched %q, %s\n", string(matched), match.ID())

	// Output: matched "-123", integer
}

// Matches iterates over every way a pattern matches, best first. Here the alternation can match one, two or three
// characters of the input, and offers the longest first.
func ExampleMatches() {
	g := alternation.New[rune, runes.Pos](
		vector.New[rune, runes.Pos](func(a, b rune) bool { return a == b }, 'a'),
		vector.New[rune, runes.Pos](func(a, b rune) bool { return a == b }, 'a', 'b'),
		vector.New[rune, runes.Pos](func(a, b rune) bool { return a == b }, 'a', 'b', 'c'),
	)

	for m, err := range ebnf.Matches[rune, runes.Pos](g, text("abcd")) {
		if err != nil {
			fmt.Println(err)
			return
		}

		fmt.Println("can end at", m.End.Index)
	}

	// Output:
	// can end at 3
	// can end at 2
	// can end at 1
}

// A ParseContext wraps the reader and carries what belongs to one parse. With a FarthestLog as its logger a failed parse
// turns into a syntax error that says where the input went wrong and what would have been accepted there.
func ExampleFarthestLog() {
	// call = "(" digit { "," digit } ")"
	call := concatenation.New[rune, runes.Pos](
		char('('),
		digit(),
		repetition.Any[rune, runes.Pos](concatenation.New[rune, runes.Pos](char(','), digit())),
		char(')'),
	)

	rd := text("(1,2;3)")
	log := runes.NewFarthestLog(rd)

	ok, _, _ := ebnf.Parse[rune, runes.Pos](call, ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(log))
	fmt.Println(ok)
	fmt.Println(log.Err())

	// Output:
	// false
	// syntax error at 1:5: unexpected ';', expected "," or ")"
}

// WithMemo remembers the ways each pattern can end at each position. It makes no difference to the result, but keeps an
// ambiguous grammar from taking exponential time: (a*)* followed by "c" has to try every way of splitting the a's
// before it can fail.
func ExampleParseContext_WithMemo() {
	a := char('a')
	nested := repetition.Any[rune, runes.Pos](repetition.Any[rune, runes.Pos](a))
	g := concatenation.New[rune, runes.Pos](nested, char('c'))

	rd := text(strings.Repeat("a", 60) + "b")
	ctx := ebnf.NewParseContext[rune, runes.Pos](rd).WithMemo(func(p runes.Pos) int { return p.Index })

	ok, _, _ := ebnf.Parse[rune, runes.Pos](g, ctx)
	fmt.Println(ok)

	// Output: false
}

// PrintRules prints a grammar in EBNF. Named patterns print as their name inside other patterns.
func ExamplePrintRules() {
	number := concatenation.New[rune, runes.Pos](digit(), repetition.Any[rune, runes.Pos](digit())).Named("number")
	sum := concatenation.New[rune, runes.Pos](
		number,
		repetition.Any[rune, runes.Pos](concatenation.New[rune, runes.Pos](char('+'), number)),
	).Named("sum")

	rules, err := ebnf.PrintRules([]pattern{number, sum})
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Print(rules)

	// Output:
	// number = (digit, digit*)
	// sum = (number, ("+", number)*)
}

type sink struct{ sb strings.Builder }

func (s *sink) Write(rs ...rune) error { s.sb.WriteString(string(rs)); return nil }
func (s *sink) Finish() error          { return nil }

// Generate writes what a pattern matches. All randomness comes from the GenContext, so the same seed gives the same
// output.
func ExampleGenContext() {
	vowel := ent.New[rune, runes.Pos](func(r rune) bool { return strings.ContainsRune("aeiou", r) }).
		SetGenerateFuncCtx(func(ctx *ebnf.GenContext) rune { return rune("aeiou"[ctx.Intn(5)]) })
	consonant := ent.New[rune, runes.Pos](func(r rune) bool { return !strings.ContainsRune("aeiou", r) }).
		SetGenerateFuncCtx(func(ctx *ebnf.GenContext) rune { return rune("bdklmnrst"[ctx.Intn(9)]) })

	syllable := concatenation.New[rune, runes.Pos](consonant, vowel)
	word := repetition.New[rune, runes.Pos](syllable, 2, 3)

	for _, seed := range []int64{1, 1, 2} {
		var out sink
		if err := word.Generate(ebnf.NewGenContext(seed), &out); err != nil {
			fmt.Println(err)
			return
		}

		fmt.Printf("seed %d: %s\n", seed, out.sb.String())
	}

	// Output:
	// seed 1: rikera
	// seed 1: rikera
	// seed 2: rinu
}

// anyRune is a pattern written from scratch: it matches any single rune. A pattern has three methods, and Single
// helps when there is only one way to match.
type anyRune struct{}

func (anyRune) MatchCont(r reader, k ebnf.Continuation[rune, runes.Pos]) (bool, error) {
	return ebnf.Single(r, k, func(r reader) (bool, *ebnf.Match[rune, runes.Pos], error) {
		begin, _ := r.Position()
		if _, err := r.Read1(); err != nil {
			return false, nil, nil
		}

		end, _ := r.Position()

		return true, ebnf.NewMatch[rune, runes.Pos](anyRune{}, begin, end, nil, nil), nil
	})
}

func (anyRune) Generate(_ *ebnf.GenContext, w ebnf.Writer[rune]) error { return w.Write('?') }

func (anyRune) Print(w io.Writer) error {
	_, err := io.WriteString(w, "any")
	return err
}

func ExampleSingle() {
	g := concatenation.New[rune, runes.Pos](anyRune{}, char('-'), anyRune{}, end.New[rune, runes.Pos]())

	for _, in := range []string{"a-b", "x-y", "ab-"} {
		ok, _, _ := ebnf.Parse[rune, runes.Pos](g, text(in))
		fmt.Printf("%s: %v\n", in, ok)
	}

	// Output:
	// a-b: true
	// x-y: true
	// ab-: false
}

// Scan looks for a pattern at every position of a stream and returns all the matches, skipping over what does not match.
func ExampleScan() {
	rd := text("order 66 shipped 7 of 12 boxes")

	words, err := ebnf.Scan[rune, runes.Pos](rd, integer())
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, m := range words {
		matched, _ := rd.Range(m.Begin, m.End)
		fmt.Print(string(matched), " ")
	}
	fmt.Println()

	// Output: 66 7 12
}
