package tests

import (
	"fmt"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

// paren = "(" paren ")" | "a", matched by paren "z" against nested input, so the tail always fails
func nestedParenGrammar() ebnf.Pattern[rune, runes.Pos] {
	paren := rule.Declare[rune, runes.Pos]("paren")
	paren.Define(altR(concR(runeR('('), paren, runeR(')')), runeR('a')))
	return concR(paren, runeR('z'))
}

func BenchmarkNestedAltFail(b *testing.B) {
	for _, depth := range []int{4, 8, 16, 32} {
		input := strings.Repeat("(", depth) + "a" + strings.Repeat(")", depth)
		b.Run(fmt.Sprint(depth), func(b *testing.B) {
			g := nestedParenGrammar()
			for b.Loop() {
				ebnf.Parse(g, newRunes(input))
			}
		})
	}
}

// the same shape, nested through alternations that share a prefix: a nested alternation inside every branch
func chainGrammar(levels int) ebnf.Pattern[rune, runes.Pos] {
	var p ebnf.Pattern[rune, runes.Pos] = runeR('a')
	for range levels {
		p = altR(concR(runeR('a'), p), p)
	}
	return concR(p, runeR('z'))
}

func BenchmarkAltChainFail(b *testing.B) {
	// without the memo this grammar takes time exponential in the number of levels, so the plain runs stay small
	for _, levels := range []int{4, 8} {
		input := strings.Repeat("a", levels+1)
		b.Run(fmt.Sprint(levels), func(b *testing.B) {
			g := chainGrammar(levels)
			for b.Loop() {
				ebnf.Parse(g, newRunes(input))
			}
		})
	}

	for _, levels := range []int{4, 8, 16, 32} {
		input := strings.Repeat("a", levels+1)
		b.Run(fmt.Sprint(levels, "-memo"), func(b *testing.B) {
			g := chainGrammar(levels)
			for b.Loop() {
				ebnf.Parse(g, ebnf.WithMemo[rune, runes.Pos](newRunes(input), func(p runes.Pos) int { return p.Index }))
			}
		})
	}
}

// many keywords with common prefixes, followed by something that fails
func BenchmarkKeywordAltFail(b *testing.B) {
	words := []string{"if", "else", "elif", "end", "endif", "for", "foreach", "func", "function", "while", "do", "done"}
	var alts []ebnf.Pattern[rune, runes.Pos]
	for _, w := range words {
		alts = append(alts, vecR(w))
	}
	g := concR(altR(alts...), runeR('!'))
	for b.Loop() {
		ebnf.Parse(g, newRunes("function?"))
	}
}

// e(n) = e(n-1) "+" a | e(n-1) "-" a | e(n-1), the kind of unfactored grammar people write: every level tries the
// level below three times at the same position
func unfactoredGrammar(levels int) ebnf.Pattern[rune, runes.Pos] {
	var p ebnf.Pattern[rune, runes.Pos] = runeR('a')
	for range levels {
		p = altR(concR(p, runeR('+'), runeR('a')), concR(p, runeR('-'), runeR('a')), p)
	}
	return p
}

func BenchmarkUnfactoredSuccess(b *testing.B) {
	// without the memo every level triples the work, so the plain runs stay small
	for _, levels := range []int{4, 8} {
		g := unfactoredGrammar(levels)
		b.Run(fmt.Sprint(levels), func(b *testing.B) {
			for b.Loop() {
				ebnf.Parse(g, newRunes("a+a"))
			}
		})
	}
	for _, levels := range []int{4, 8, 16, 32} {
		g := unfactoredGrammar(levels)
		b.Run(fmt.Sprint(levels, "-memo"), func(b *testing.B) {
			for b.Loop() {
				ebnf.Parse(g, ebnf.WithMemo[rune, runes.Pos](newRunes("a+a"), func(p runes.Pos) int { return p.Index }))
			}
		})
	}
}
