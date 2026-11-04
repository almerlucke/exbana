package repetition

import (
	"fmt"
	"io"
	"slices"

	ebnf "github.com/almerlucke/exbana/v2"
)

// Repetition matches a sub-pattern multiple times.
// By default, it's greedy and allows backtracking in concatenation patterns.
// If Possessive is set to true, it will not give back matches during backtracking.
type Repetition[T, P any] struct {
	*ebnf.BasePattern[T, P]
	pattern      ebnf.Pattern[T, P]
	min          int
	max          int
	maxGen       int
	possessive   bool
	countWeights []float64
	countFunc    func(*ebnf.GenContext) int
}

// New creates a new repetition pattern
func New[T, P any](pattern ebnf.Pattern[T, P], min int, max int) *Repetition[T, P] {
	rep := &Repetition[T, P]{
		BasePattern: ebnf.NewBasePattern[T, P](),
		pattern:     pattern,
		min:         min,
		max:         max,
		maxGen:      1,
	}

	rep.SetSelf(rep)

	return rep
}

func Optional[T, P any](pattern ebnf.Pattern[T, P]) *Repetition[T, P] {
	return New[T, P](pattern, 0, 1)
}

func Any[T, P any](pattern ebnf.Pattern[T, P]) *Repetition[T, P] {
	return New[T, P](pattern, 0, 0)
}

func OneOrMore[T, P any](pattern ebnf.Pattern[T, P]) *Repetition[T, P] {
	return New[T, P](pattern, 1, 0)
}

func (rep *Repetition[T, P]) Possessive() bool {
	return rep.possessive
}

func (rep *Repetition[T, P]) SetPossessive(possessive bool) *Repetition[T, P] {
	rep.possessive = possessive
	return rep
}

func (rep *Repetition[T, P]) Min() int {
	return rep.min
}

func (rep *Repetition[T, P]) Max() int {
	return rep.max
}

// Match matches the repetition pattern aginst a stream
func (rep *Repetition[T, P]) Match(r ebnf.Reader[T, P]) (bool, *ebnf.Match[T, P], error) {
	return ebnf.FirstMatch(r, rep.MatchCont)
}

// MatchCont offers the matches of the repetition to the continuation, most repetitions first (greedy). If the
// continuation rejects a match the repetition gives back one repetition at a time until min is reached.
// A possessive repetition does not backtrack, it only offers the greedy match and is faster as it does not recurse.
func (rep *Repetition[T, P]) MatchCont(r ebnf.Reader[T, P], k ebnf.Continuation[T, P]) (bool, error) {
	return ebnf.MatchMemo(ebnf.Pattern[T, P](rep), r, k, rep.matchCont)
}

func (rep *Repetition[T, P]) matchCont(r ebnf.Reader[T, P], k ebnf.Continuation[T, P]) (bool, error) {
	beginPos, err := r.Position()
	if ebnf.IsStreamError(err) {
		return false, err
	}

	if rep.possessive {
		return rep.matchPossessive(r, beginPos, k)
	}

	var matches []*ebnf.Match[T, P]

	failed := ebnf.NewFailSet(r)

	var next func() (bool, error)

	var nextFrom func(herePos P) (bool, error)

	// next is called with the reader positioned after the repetitions matched so far
	next = func() (bool, error) {
		herePos, err := r.Position()
		if ebnf.IsStreamError(err) {
			return false, err
		}

		// once min is reached and there is no max, the outcome only depends on the position and not on the
		// number of repetitions, so a dead end at this position stays a dead end
		stateless := rep.max == 0 && len(matches) >= rep.min

		if stateless && failed.Has(0, herePos) {
			return false, nil
		}

		matched, err := nextFrom(herePos)
		if err == nil && !matched && stateless {
			failed.Add(0, herePos)
		}

		return matched, err
	}

	nextFrom = func(herePos P) (bool, error) {
		n := len(matches)

		if rep.max == 0 || n < rep.max {
			matched, err := rep.pattern.MatchCont(r, func(m *ebnf.Match[T, P]) (bool, error) {
				matches = append(matches[:n], m)

				// a repetition that consumed nothing would repeat forever, so it ends the repetition and counts
				// as all the repetitions that are still needed
				if r.Length(herePos, m.End) <= 0 {
					return k(ebnf.NewMatch(rep, beginPos, m.End, nil, slices.Clone(matches)))
				}

				return next()
			})
			if err != nil || matched {
				return matched, err
			}

			matches = matches[:n]

			err = r.SetPosition(herePos)
			if ebnf.IsStreamError(err) {
				return false, err
			}
		}

		if n < rep.min {
			ebnf.LogMismatch(r, ebnf.Pattern[T, P](rep), func() *ebnf.Mismatch[T, P] {
				return ebnf.NewMismatch(rep, beginPos, herePos, nil, slices.Clone(matches))
			})
			return false, nil
		}

		return k(ebnf.NewMatch(rep, beginPos, herePos, nil, slices.Clone(matches)))
	}

	return next()
}

func (rep *Repetition[T, P]) matchPossessive(r ebnf.Reader[T, P], beginPos P, k ebnf.Continuation[T, P]) (bool, error) {
	var matches []*ebnf.Match[T, P]

	for rep.max == 0 || len(matches) < rep.max {
		resetPos, err := r.Position()
		if ebnf.IsStreamError(err) {
			return false, err
		}

		matched, result, err := rep.pattern.Match(r)
		if err != nil {
			return false, err
		}

		if !matched {
			err = r.SetPosition(resetPos)
			if ebnf.IsStreamError(err) {
				return false, err
			}

			break
		}

		matches = append(matches, result)

		// a match that consumed nothing would match forever, stop repeating
		if r.Length(resetPos, result.End) <= 0 {
			break
		}
	}

	endPos, err := r.Position()
	if ebnf.IsStreamError(err) {
		return false, err
	}

	if len(matches) < rep.min {
		ebnf.LogMismatch(r, ebnf.Pattern[T, P](rep), func() *ebnf.Mismatch[T, P] { return ebnf.NewMismatch(rep, beginPos, endPos, nil, matches) })
		return false, nil
	}

	return k(ebnf.NewMatch(rep, beginPos, endPos, nil, matches))
}

// SetMaxGen sets the maximum generated entities on top of min
func (rep *Repetition[T, P]) SetMaxGen(maxGen int) *Repetition[T, P] {
	rep.maxGen = maxGen
	return rep
}

// SetCountWeights sets the relative odds of the number of repetitions when generating: weights[i] is the weight of
// min+i repetitions. With a max, min+len(weights)-1 must not be more than max. Weights do not influence matching.
func (rep *Repetition[T, P]) SetCountWeights(weights ...float64) *Repetition[T, P] {
	rep.countWeights = weights
	return rep
}

// SetCountFunc sets a function that decides the number of repetitions each time the repetition generates, for counts
// that depend on the generation state or depth. The count must be between min and max (if there is a max).
// It takes precedence over SetCountWeights and SetMaxGen.
func (rep *Repetition[T, P]) SetCountFunc(f func(*ebnf.GenContext) int) *Repetition[T, P] {
	rep.countFunc = f
	return rep
}

// Generate writes pattern to a writer a random number of times
func (rep *Repetition[T, P]) Generate(ctx *ebnf.GenContext, w ebnf.Writer[T]) error {
	if err := ctx.Enter(); err != nil {
		return err
	}
	defer ctx.Leave()

	n, err := rep.generateCount(ctx)
	if err != nil {
		return err
	}

	for i := 0; i < n; i++ {
		err := rep.pattern.Generate(ctx, w)
		if err != nil {
			return err
		}
	}

	return nil
}

func (rep *Repetition[T, P]) generateCount(ctx *ebnf.GenContext) (int, error) {
	var n int

	switch {
	case rep.countFunc != nil:
		n = rep.countFunc(ctx)
	case rep.countWeights != nil:
		i, err := ctx.Choose(rep.countWeights)
		if err != nil {
			return 0, err
		}

		n = rep.min + i
	default:
		repMax := rep.max
		if rep.max == 0 {
			repMax = rep.min + rep.maxGen
		}

		n = ctx.Intn(repMax+1-rep.min) + rep.min
	}

	if n < rep.min || (rep.max != 0 && n > rep.max) {
		return 0, fmt.Errorf("exbana: repetition count %d is outside min %d and max %d", n, rep.min, rep.max)
	}

	return n, nil
}

// printAny prints EBNF zero or more
func (rep *Repetition[T, P]) printAny(w io.Writer) error {
	err := rep.pattern.PrintAsChild(w)
	if err != nil {
		return err
	}

	_, err = w.Write([]byte("*"))

	return err
}

// printAny prints EBNF optional
func (rep *Repetition[T, P]) printOptional(w io.Writer) error {
	err := rep.pattern.PrintAsChild(w)
	if err != nil {
		return err
	}

	_, err = w.Write([]byte("?"))

	return err
}

// printAny prints EBNF at least one
func (rep *Repetition[T, P]) printAtLeastOne(w io.Writer) error {
	err := rep.pattern.PrintAsChild(w)
	if err != nil {
		return err
	}

	_, err = w.Write([]byte("+"))

	return err
}

// Print EBNF repetition pattern
func (rep *Repetition[T, P]) Print(w io.Writer) error {
	if rep.min == 0 && rep.max == 0 {
		return rep.printAny(w)
	} else if rep.min == 0 && rep.max == 1 {
		return rep.printOptional(w)
	} else if rep.min == 1 && rep.max == 0 {
		return rep.printAtLeastOne(w)
	}

	var err error

	// n or more: "n * x, x*"
	if rep.max == 0 {
		_, err = w.Write([]byte(fmt.Sprintf("(%v * ", rep.min)))
		if err != nil {
			return err
		}

		err = rep.pattern.PrintAsChild(w)
		if err != nil {
			return err
		}

		_, err = w.Write([]byte(", "))
		if err != nil {
			return err
		}

		err = rep.printAny(w)
		if err != nil {
			return err
		}

		_, err = w.Write([]byte(")"))

		return err
	}

	oneValue := rep.min == rep.max

	if !oneValue {
		_, err = w.Write([]byte("("))
		if err != nil {
			return err
		}
	}

	_, err = w.Write([]byte(fmt.Sprintf("%v * ", rep.min)))
	if err != nil {
		return err
	}

	err = rep.pattern.PrintAsChild(w)
	if err != nil {
		return err
	}

	if !oneValue {
		_, err = w.Write([]byte(fmt.Sprintf(", %v * ", rep.max-rep.min)))
		if err != nil {
			return err
		}

		err = rep.pattern.PrintAsChild(w)
		if err != nil {
			return err
		}

		_, err = w.Write([]byte("?)"))
		if err != nil {
			return err
		}
	}

	return nil
}

// Children returns the repeated pattern
func (rep *Repetition[T, P]) Children() ebnf.Patterns[T, P] {
	return ebnf.Patterns[T, P]{rep.pattern}
}
