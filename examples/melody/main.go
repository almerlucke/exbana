// Command melody generates melodies from a grammar, as a test of exbana's generation: seeded randomness, weighted
// alternatives and generation state.
//
//	piece  = phrase , phrase
//	phrase = bar , bar , bar , cadence
//	bar    = q q q q | h q q | q q h | h h | q h q      (weighted, every alternative adds up to 4 beats)
//	cadence = whole note on the tonic
//
// The pitch of every note is a weighted random walk over the scale degrees of C major (mostly steps, sometimes
// leaps), where the weights depend on the previous note, which is kept in the generation state.
// The same seed always gives the same melody.
package main

import (
	"flag"
	"fmt"
	"strings"

	ebnf "github.com/almerlucke/exbana/v2"
	"github.com/almerlucke/exbana/v2/patterns/alternation"
	"github.com/almerlucke/exbana/v2/patterns/concatenation"
	ent "github.com/almerlucke/exbana/v2/patterns/entity"
	"github.com/almerlucke/exbana/v2/patterns/repetition"
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

type (
	pattern = ebnf.Pattern[Note, struct{}]
	state   struct{ last int }
)

// nextDegree takes a step or a leap from the previous note, small steps are the most likely
func nextDegree(ctx *ebnf.GenContext) int {
	st := ctx.State.(*state)
	deltas := []int{-3, -2, -1, 0, 1, 2, 3}
	weights := []float64{1, 2, 4, 1, 4, 2, 1}

	for i, d := range deltas {
		if n := st.last + d; n < 0 || n > maxDegree {
			weights[i] = 0
		}
	}

	i, err := ctx.Choose(weights)
	if err != nil {
		panic(err) // not possible, there is always a position that stays inside the range
	}

	st.last += deltas[i]

	return st.last
}

func note(beats float64) pattern {
	return ent.New[Note, struct{}](func(n Note) bool { return n.Beats == beats }).
		SetGenerateFuncCtx(func(ctx *ebnf.GenContext) Note { return Note{nextDegree(ctx), beats} })
}

func conc(ps ...pattern) pattern {
	return concatenation.New(ps...)
}

// Grammar returns the pattern for a piece
func Grammar() pattern {
	q := func() pattern { return note(1) }
	h := func() pattern { return note(2) }

	bar := alternation.New(
		conc(q(), q(), q(), q()),
		conc(h(), q(), q()),
		conc(q(), q(), h()),
		conc(h(), h()),
		conc(q(), h(), q()),
	).SetWeights(4, 2, 2, 2, 1)

	cadence := ent.New[Note, struct{}](func(n Note) bool { return n.Degree == 0 && n.Beats == 4 }).
		SetGenerateFuncCtx(func(ctx *ebnf.GenContext) Note {
			ctx.State.(*state).last = 0
			return Note{0, 4}
		})

	phrase := conc(repetition.New[Note, struct{}](bar, 3, 3), cadence)

	return conc(phrase, phrase)
}

type collector struct{ notes []Note }

func (c *collector) Write(ns ...Note) error { c.notes = append(c.notes, ns...); return nil }
func (c *collector) Finish() error          { return nil }

// Compose generates a melody for a seed
func Compose(seed int64) ([]Note, error) {
	ctx := ebnf.NewGenContext(seed)
	ctx.State = &state{}

	var c collector
	if err := Grammar().Generate(ctx, &c); err != nil {
		return nil, err
	}

	return c.notes, nil
}

func main() {
	seed := flag.Int64("seed", 1, "random seed")
	count := flag.Int("n", 3, "number of melodies")
	flag.Parse()

	for i := 0; i < *count; i++ {
		notes, err := Compose(*seed + int64(i))
		if err != nil {
			fmt.Println("error:", err)
			return
		}

		var parts []string
		beats := 0.0
		for _, n := range notes {
			parts = append(parts, n.String())
			beats += n.Beats
			if beats == float64(int(beats)) && int(beats)%4 == 0 {
				parts = append(parts, "|")
				beats = 0
			}
		}

		fmt.Printf("seed %d: %s\n", *seed+int64(i), strings.Join(parts, " "))
	}
}
