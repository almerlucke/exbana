package tests

import (
	"strings"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	"github.com/almerlucke/exbana/v3/patterns/end"
	ent "github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/except"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
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

func newRunes(s string) *runes.Reader {
	rd, _ := runes.New(strings.NewReader(s))
	return rd
}

func anyR(p ebnf.Pattern[rune, runes.Pos]) *repetition.Repetition[rune, runes.Pos] {
	return repetition.Any[rune, runes.Pos](p)
}

func lit(r rune) *ent.Entity[rune, runes.Pos] {
	return ebnf.Text(runeR(r), `"`+string(r)+`"`)
}
