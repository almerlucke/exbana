package main

import (
	"fmt"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/examples/melody/abc"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	"github.com/almerlucke/exbana/v3/patterns/end"
	ent "github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
)

// The folk grammar is a grammar over the cells of a tune (see Cell). It says how a tune is built, and a model says
// what is likely inside that building:
//
//	tune    = part , { part } , end
//	part    = { half phrase } , full phrase , end mark          (a part of 4 to 32 bars)
//	        | { half phrase } , second ending , full bar , end mark
//	phrase  = bar , bar , bar , cadence
//	bar     = slot × (the slots in a bar)
//	cadence = slots , the last note , holds , rests              (the last note is the one the phrase comes to rest on)
//	slot    = onset | hold | rest
//
// A phrase of four bars ends with a half cadence, except the last of a part, which ends with a full one. They are the
// same shape, and are different rules because they come to rest in different places, which is what the model learns.
const (
	maxPhrases = 8 // phrases in a part
	maxParts   = 4
	maxRun     = 16 // the rhythm looks back this many slots at the most, to the start of the note
	maxStep    = 8  // steps of the scale a melody can take at once
)

type cpattern = ebnf.Pattern[Cell, int]

// PartKind says how long a part is: the phrases of four bars, and if it has a second ending of a bar.
type PartKind struct{ Phrases, Ending int }

// Shape is the parts of a tune, and how likely that is.
type Shape struct {
	Parts  []PartKind
	Weight float64
}

// FolkModel is what the grammar generates with: how tunes are shaped, where phrases come to rest, how the rhythm goes
// and how far the notes step. A model that has no shapes is for reading, not for generating: it has no opinion on
// how many parts a tune has.
type FolkModel struct {
	Meter  abc.Meter
	Shapes []Shape

	// Closing is how likely the last note of a phrase is to start at each slot of its bar, for "half" and "full" cadences.
	Closing map[string][]float64

	// Final is how likely the last note of a phrase is to be each step of the scale (0 to 6, from the tonic).
	Final map[string][]float64

	// Rhythm is, for each slot of the bar and for how long the note has been going, the odds of a hold, a new note, a rest.
	Rhythm [][maxRun][3]float64

	// Step is the odds of each step from the note before to the next. Three views of where the melody is each have their
	// say: the step it took before and how strong the place in the bar is, the note it is on, and how high it is in its
	// range. Each row has the odds of the steps -maxStep to maxStep, and Marginal is the odds of a step without a view.
	Step StepModel

	// Start is the odds of the first note, by step of the scale from Lo.
	Start []float64

	// Lo and Hi are the lowest and highest step of the scale that melodies keep to.
	Lo, Hi int
}

// StepModel is the odds of the steps of a melody, in views, see FolkModel.
type StepModel struct {
	Motion   [][]float64 // by the step before (-3 to 3 at the most) and the strength of the place: 7 × 4 rows
	Degree   [][]float64 // by the step of the scale the melody is on, and if the place is on a beat: 7 × 2 rows
	Zone     [][]float64 // by the third of the range the melody is in
	Marginal []float64
}

func motionRow(prev, strength int) int { return (min(max(prev, -3), 3)+3)*4 + strength }

func degreeRow(last, strength int) int {
	if strength <= 1 {
		return (last%7 + 7) % 7 * 2
	}

	return (last%7+7)%7*2 + 1
}

func zoneRow(last, lo, hi int) int { return min(max(3*(last-lo)/(hi-lo+1), 0), 2) }

func rows(n int) [][]float64 {
	out := make([][]float64, n)
	for i := range out {
		out[i] = ones(2*maxStep + 1)
	}

	return out
}

// NewFolkModel is a model with no opinion: everything is as likely as everything else.
func NewFolkModel(m abc.Meter) *FolkModel {
	n := slotsOf(m)
	f := &FolkModel{Meter: m, Closing: map[string][]float64{}, Final: map[string][]float64{}, Lo: -2, Hi: 14}

	for _, kind := range []string{"half", "full"} {
		f.Closing[kind] = ones(n)
		f.Final[kind] = ones(7)
	}

	f.Rhythm = make([][maxRun][3]float64, n)
	for s := range f.Rhythm {
		for r := range f.Rhythm[s] {
			f.Rhythm[s][r] = [3]float64{1, 1, 0}
		}
	}

	f.Step = StepModel{Motion: rows(28), Degree: rows(14), Zone: rows(3), Marginal: ones(2*maxStep + 1)}

	f.Start = ones(f.Hi - f.Lo + 1)

	return f
}

func ones(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = 1
	}

	return out
}

// slots is the number of cells in a bar.
func (f *FolkModel) slots() int { return slotsOf(f.Meter) }

// beatSlots is the number of slots in a beat: a quarter note is four, and in compound meters (6/8) a beat is a dotted quarter.
func beatSlots(m abc.Meter) int {
	unit := map[int]int{2: 8, 4: 4, 8: 2, 16: 1}[m.Den]
	if unit == 0 {
		unit = 4
	}

	if m.Num > 3 && m.Num%3 == 0 {
		return 3 * unit
	}

	return unit
}

// strength says how strong a place in the bar is: 0 for the start of the bar, 1 for another beat, 2 for an eighth, 3
// for a sixteenth.
func strength(m abc.Meter, slot int) int {
	beat := beatSlots(m)

	switch {
	case slot == 0:
		return 0
	case slot%beat == 0:
		return 1
	case slot%2 == 0:
		return 2
	}

	return 3
}

// folkState is what a generation remembers: where in the bar it is, how long the note has been going, and the last
// note.
type folkState struct {
	m       *FolkModel
	slot    int  // the cells so far
	run     int  // the cells since the last note or rest started, that one too
	started bool // there has been a note or a rest
	hasNote bool
	last    int // the step of the scale of the last note
	delta   int // the step the last note took
}

func stateOf(ctx *ebnf.GenContext) *folkState { return ctx.State.(*folkState) }

// choose picks an index with odds, or any index if no odds are positive.
func choose(ctx *ebnf.GenContext, weights []float64) int {
	i, err := ctx.Choose(weights)
	if err != nil {
		return ctx.Intn(len(weights))
	}

	return i
}

// pickStep takes the next step of the melody from the note before, with the odds of the model for this place in the bar.
func (st *folkState) pickStep(ctx *ebnf.GenContext) int {
	m := st.m

	if !st.hasNote {
		weights := append([]float64(nil), m.Start...)

		return m.Lo + choose(ctx, weights)
	}

	str := strength(m.Meter, st.slot%m.slots())
	motion := m.Step.Motion[motionRow(st.delta, str)]
	degree := m.Step.Degree[degreeRow(st.last, str)]
	zone := m.Step.Zone[zoneRow(st.last, m.Lo, m.Hi)]

	// the views are combined as if they were independent: what each says over what the steps are anyway
	weights := make([]float64, 2*maxStep+1)

	for i := range weights {
		mg := m.Step.Marginal[i]
		weights[i] = motion[i] * degree[i] * zone[i] / (mg * mg)

		if d := st.last + i - maxStep; d < m.Lo || d > m.Hi {
			weights[i] = 0
		}
	}

	// if the model has no odds for any step that stays in the range, all steps that do are as likely
	sum := 0.0
	for _, w := range weights {
		sum += w
	}

	if sum == 0 {
		for i := range weights {
			if d := st.last + i - maxStep; d >= m.Lo && d <= m.Hi {
				weights[i] = 1
			}
		}
	}

	return st.last + choose(ctx, weights) - maxStep
}

// finalStep is the note a phrase comes to rest on: a step of the scale with the odds of the cadence, in the octave
// nearest to the note before.
func (st *folkState) finalStep(ctx *ebnf.GenContext, kind string) int {
	m := st.m
	class := choose(ctx, append([]float64(nil), m.Final[kind]...))

	best, bestDist := -1<<30, 1<<30

	for d := m.Lo; d <= m.Hi; d++ {
		if (d%7+7)%7 != class {
			continue
		}

		dist := d - st.last
		if dist < 0 {
			dist = -dist
		}

		if dist < bestDist {
			best, bestDist = d, dist
		}
	}

	if best == -1<<30 {
		best = class // not in the range at all
	}

	return best
}

// begin is the start of a note or a rest.
func (st *folkState) begin(onset bool, step int) Cell {
	st.run, st.started = 1, true
	st.slot++

	if onset {
		st.delta = 0
		if st.hasNote {
			st.delta = step - st.last
		}

		st.hasNote, st.last = true, step

		return Cell{Kind: Onset, Degree: step}
	}

	return Cell{Kind: Rest}
}

func (st *folkState) hold() Cell {
	st.run++
	st.slot++

	return Cell{Kind: Hold}
}

func cellOf(name string, match func(Cell) bool, gen func(*ebnf.GenContext) Cell) cpattern {
	e := ent.New[Cell, int](match).Named(name)
	if gen != nil {
		e.SetGenerateFuncCtx(gen)
	}

	return e
}

func rep(p cpattern, n int) cpattern {
	if n == 1 {
		return p
	}

	return repetition.New[Cell, int](p, n, n)
}

func cconc(parts ...cpattern) *concatenation.Concatenation[Cell, int] {
	return concatenation.New(parts...)
}

// FolkGrammar returns the grammar of a folk tune for a model.
func FolkGrammar(m *FolkModel) cpattern {
	n := m.slots()

	slot := cellOf("slot", func(c Cell) bool { return c.Kind <= Rest }, func(ctx *ebnf.GenContext) Cell {
		st := stateOf(ctx)
		weights := st.m.Rhythm[st.slot%n][max(min(st.run, maxRun), 1)-1]
		w := weights[:]

		if !st.started {
			w = []float64{0, weights[1], 0} // nothing to hold, no rest to start with
		}

		switch choose(ctx, w) {
		case 0:
			return st.hold()
		case 1:
			return st.begin(true, st.pickStep(ctx))
		}

		return st.begin(false, 0)
	})

	hold := cellOf("hold", func(c Cell) bool { return c.Kind == Hold }, func(ctx *ebnf.GenContext) Cell { return stateOf(ctx).hold() })
	// what follows the last note of a phrase when the bar is cut short: a rest, and then rests and what holds them
	tail := cellOf("rest", func(c Cell) bool { return c.Kind == Rest || c.Kind == Hold }, func(ctx *ebnf.GenContext) Cell { return stateOf(ctx).hold() })
	rest := cellOf("rest", func(c Cell) bool { return c.Kind == Rest }, func(ctx *ebnf.GenContext) Cell { return stateOf(ctx).begin(false, 0) })

	final := map[string]cpattern{}
	for _, kind := range []string{"half", "full"} {
		final[kind] = cellOf("last note of a "+kind+" cadence", func(c Cell) bool { return c.Kind == Onset }, func(ctx *ebnf.GenContext) Cell {
			st := stateOf(ctx)

			return st.begin(true, st.finalStep(ctx, kind))
		})
	}

	bar := concatenation.New[Cell, int](rep(slot, n)).Named("bar")

	// a cadence is the slots before the last note, the last note, what holds it and rests that follow. The rests are the
	// part of a bar that a pickup left over, and are only there to be read.
	cadence := func(kind string) cpattern {
		var (
			alts    []cpattern
			weights []float64
		)

		for q := 0; q < n; q++ {
			for r := 0; q+1+r <= n; r++ {
				var parts []cpattern

				if q > 0 {
					parts = append(parts, rep(slot, q))
				}

				parts = append(parts, final[kind])

				if h := n - q - 1 - r; h > 0 {
					parts = append(parts, rep(hold, h))
				}

				if r > 0 {
					parts = append(parts, rest)

					if r > 1 {
						parts = append(parts, rep(tail, r-1))
					}
				}

				alts = append(alts, cconc(parts...).Named(fmt.Sprintf("%s:%d:%d", kind, q, r)))

				if r == 0 {
					weights = append(weights, m.Closing[kind][q])
				} else {
					weights = append(weights, 0)
				}
			}
		}

		return alternation.New(alts...).SetWeights(weights...)
	}

	half, full := cadence("half"), cadence("full")
	threeBars := rep(bar, 3)
	halfPhrase := cconc(threeBars, half)
	fullPhrase := cconc(threeBars, full)
	endMark := cellOf("end of part", func(c Cell) bool { return c.Kind == EndMark }, func(*ebnf.GenContext) Cell { return Cell{Kind: EndMark} })
	volta := cellOf("second ending", func(c Cell) bool { return c.Kind == Volta }, func(*ebnf.GenContext) Cell { return Cell{Kind: Volta} })

	part := func(k PartKind) cpattern {
		var parts []cpattern

		if k.Ending == 0 {
			for i := 1; i < k.Phrases; i++ {
				parts = append(parts, halfPhrase)
			}

			parts = append(parts, fullPhrase)
		} else {
			for i := 0; i < k.Phrases; i++ {
				parts = append(parts, halfPhrase)
			}

			parts = append(parts, volta, full)
		}

		return cconc(append(parts, endMark)...).Named(partName(k))
	}

	// a part of any kind, for reading
	var kinds []cpattern
	for k := 1; k <= maxPhrases; k++ {
		for e := 0; e <= 1; e++ {
			kinds = append(kinds, part(PartKind{k, e}))
		}
	}

	anyPart := alternation.New(kinds...)

	var tune cpattern = repetition.New[Cell, int](anyPart, 1, maxParts)

	if len(m.Shapes) > 0 {
		var (
			shapes  []cpattern
			weights []float64
		)

		for _, s := range m.Shapes {
			parts := make([]cpattern, len(s.Parts))
			for i, k := range s.Parts {
				parts[i] = part(k)
			}

			shapes = append(shapes, cconc(parts...))
			weights = append(weights, s.Weight)
		}

		tune = alternation.New(shapes...).SetWeights(weights...)
	}

	return cconc(tune, end.New[Cell, int]().Named("end of tune"))
}

func partName(k PartKind) string { return fmt.Sprintf("part:%d:%d", k.Phrases, k.Ending) }

type collectCells struct{ cells []Cell }

func (c *collectCells) Write(cs ...Cell) error { c.cells = append(c.cells, cs...); return nil }
func (c *collectCells) Finish() error          { return nil }

// ComposeFolk writes a tune with a model that has shapes, as cells.
func ComposeFolk(m *FolkModel, seed int64) ([]Cell, error) {
	if len(m.Shapes) == 0 {
		return nil, fmt.Errorf("the model has no shapes of tunes to choose from")
	}

	ctx := ebnf.NewGenContext(seed)
	ctx.State = &folkState{m: m}

	var out collectCells
	if err := FolkGrammar(m).Generate(ctx, &out); err != nil {
		return nil, err
	}

	return out.cells, nil
}
