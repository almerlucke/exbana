package main

import (
	"fmt"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	ent "github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
)

// Note is a scale degree (0 is the tonic C4, 7 is C5) with a duration in beats
type Note struct {
	Degree int
	Beats  float64
}

const maxDegree = 14

var names = []string{"C", "D", "E", "F", "G", "A", "B"}
var durations = map[float64]string{4: "w", 2: "h", 1: "q"}

func (n Note) String() string {
	return fmt.Sprintf("%s%d/%s", names[n.Degree%7], 4+n.Degree/7, durations[n.Beats])
}

// The grammar is over notes, with the position of a note in the melody (the slice reader's) as position. It parses and
// generates: the same pattern reads a known melody and writes a new one.
type (
	pattern = ebnf.Pattern[Note, int]
	state   struct{ last int }
)

// barNames are the rhythms a bar can have, in the order of Model.Bars. They name the alternatives, so that a parse
// says which of them a bar is.
var barNames = []string{"qqqq", "hqq", "qqh", "hh", "qhq"}

// deltas are the steps from one note to the next that the melody can take, in the order of Model.Deltas.
var deltas = []int{-4, -3, -2, -1, 0, 1, 2, 3, 4}

// Model is what makes one melody generator differ from another: the odds of the rhythms of a bar, and of the steps
// from a note to the next. The grammar stays the same.
type Model struct {
	Bars   []float64 // one weight for each of barNames
	Deltas []float64 // one weight for each of deltas
}

// DefaultModel is a hand-made model: bars of quarters are the most likely, and small steps are.
func DefaultModel() *Model {
	return &Model{Bars: []float64{4, 2, 2, 2, 1}, Deltas: []float64{0.5, 1, 2, 4, 1, 4, 2, 1, 0.5}}
}

// nextDegree takes a step or a leap from the previous note, with the odds of the model; steps out of range have none
func nextDegree(ctx *ebnf.GenContext, m *Model) int {
	st := ctx.State.(*state)
	weights := append([]float64(nil), m.Deltas...)

	for i, d := range deltas {
		if n := st.last + d; n < 0 || n > maxDegree {
			weights[i] = 0
		}
	}

	i, err := ctx.Choose(weights)
	if err != nil {
		// there are no odds for any step that stays inside the range
		for i, d := range deltas {
			if n := st.last + d; n >= 0 && n <= maxDegree {
				weights[i] = 1
			}
		}

		i, err = ctx.Choose(weights)
		if err != nil {
			panic(err) // not possible, staying where it is is inside the range
		}
	}

	st.last += deltas[i]

	return st.last
}

func note(m *Model, beats float64) pattern {
	return ent.New[Note, int](func(n Note) bool { return n.Beats == beats }).
		SetGenerateFuncCtx(func(ctx *ebnf.GenContext) Note { return Note{nextDegree(ctx, m), beats} })
}

func conc(ps ...pattern) pattern {
	return concatenation.New(ps...)
}

// GrammarOf returns the pattern for a piece
//
//	piece   = phrase , phrase
//	phrase  = bar , bar , bar , cadence
//	bar     = q q q q | h q q | q q h | h h | q h q      (weighted, every alternative adds up to 4 beats)
//	cadence = whole note on the tonic
func GrammarOf(m *Model) pattern {
	q := func() pattern { return note(m, 1) }
	h := func() pattern { return note(m, 2) }

	rhythms := []pattern{
		conc(q(), q(), q(), q()),
		conc(h(), q(), q()),
		conc(q(), q(), h()),
		conc(h(), h()),
		conc(q(), h(), q()),
	}
	for i, r := range rhythms {
		ebnf.Name(r.(*concatenation.Concatenation[Note, int]), barNames[i])
	}

	bar := alternation.New(rhythms...).SetWeights(m.Bars...)

	cadence := ent.New[Note, int](func(n Note) bool { return n.Degree == 0 && n.Beats == 4 }).
		SetGenerateFuncCtx(func(ctx *ebnf.GenContext) Note {
			ctx.State.(*state).last = 0
			return Note{0, 4}
		})

	phrase := conc(repetition.New[Note, int](bar, 3, 3), cadence)

	return conc(phrase, phrase)
}

// Grammar returns the pattern for a piece, with the default model
func Grammar() pattern { return GrammarOf(DefaultModel()) }

type collector struct{ notes []Note }

func (c *collector) Write(ns ...Note) error { c.notes = append(c.notes, ns...); return nil }
func (c *collector) Finish() error          { return nil }

// Compose generates a melody for a seed, with the default model
func Compose(seed int64) ([]Note, error) { return ComposeWith(DefaultModel(), seed) }

// ComposeWith generates a melody for a seed
func ComposeWith(m *Model, seed int64) ([]Note, error) {
	ctx := ebnf.NewGenContext(seed)
	ctx.State = &state{}

	var c collector
	if err := GrammarOf(m).Generate(ctx, &c); err != nil {
		return nil, err
	}

	return c.notes, nil
}
