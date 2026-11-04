package alternation

import (
	"errors"
	"fmt"
	"io"
	"slices"

	ebnf "github.com/almerlucke/exbana/v3"
)

// Alternation matches a series of patterns OR style in order (alternation)
type Alternation[T, P any] struct {
	ebnf.Label
	patterns     ebnf.Patterns[T, P]
	isOrthogonal bool // if orthogonal we stop at first match as we know input is not related
	weights      []float64
	weightFunc   func(*ebnf.GenContext) []float64
}

// New creates a new Alternation pattern
func New[T, P any](patterns ...ebnf.Pattern[T, P]) *Alternation[T, P] {
	a := &Alternation[T, P]{
		patterns: patterns,
	}

	return a
}

// SetOrthogonal sets orthogonal or not
func (a *Alternation[T, P]) SetOrthogonal(ortho bool) *Alternation[T, P] {
	a.isOrthogonal = ortho
	return a
}

// Match matches the Alternation sub patterns against a stream, fails if there is no match. If there are more than one match,
// the longest match returns, if two or more matches are the longest, the first of those is returned. So order of the sub
// patterns matters when creating an Alternation pattern
func (a *Alternation[T, P]) Match(r ebnf.Reader[T, P]) (bool, *ebnf.Match[T, P], error) {
	return ebnf.FirstMatch(r, a.MatchCont)
}

// MatchCont offers the matches of the sub patterns to the continuation, longest first (ties in pattern order).
// A sub pattern that matches in more than one way offers its other matches after its first one.
// An orthogonal alternation only offers the first sub pattern that matches, and does not backtrack into it.
func (a *Alternation[T, P]) MatchCont(r ebnf.Reader[T, P], k ebnf.Continuation[T, P]) (bool, error) {
	return ebnf.MatchMemo(ebnf.Pattern[T, P](a), r, k, a.matchCont)
}

func (a *Alternation[T, P]) matchCont(r ebnf.Reader[T, P], k ebnf.Continuation[T, P]) (bool, error) {
	type candidate struct {
		index  int
		length int
		match  *ebnf.Match[T, P]
	}

	var candidates []candidate

	beginPos, err := r.Position()
	if ebnf.IsStreamError(err) {
		return false, err
	}

	wrap := func(m *ebnf.Match[T, P]) *ebnf.Match[T, P] {
		return ebnf.NewMatch(a, beginPos, m.End, nil, []*ebnf.Match[T, P]{m})
	}

	// first match of every sub pattern, this decides the order in which we offer them
	for i, pm := range a.patterns {
		err = r.SetPosition(beginPos)
		if ebnf.IsStreamError(err) {
			return false, err
		}

		matched, result, err := ebnf.Parse(pm, r)
		if err != nil {
			return false, err
		}

		if !matched {
			continue
		}

		// if set of alternations is orthogonal we know there is no relation between the entities in the set
		// so we can stop at first match
		if a.isOrthogonal {
			return k(wrap(result))
		}

		candidates = append(candidates, candidate{index: i, length: r.Length(beginPos, result.End), match: result})
	}

	if len(candidates) == 0 {
		endPos, err := r.Position()
		if ebnf.IsStreamError(err) {
			return false, err
		}

		ebnf.LogFailure(r, ebnf.Pattern[T, P](a), beginPos, endPos)

		return false, nil
	}

	slices.SortStableFunc(candidates, func(x, y candidate) int { return y.length - x.length })

	for _, c := range candidates {
		err = r.SetPosition(c.match.End)
		if ebnf.IsStreamError(err) {
			return false, err
		}

		matched, err := k(wrap(c.match))
		if err != nil || matched {
			return matched, err
		}

		// the sub pattern may have more ways to match, offer those as well. Its first match is the one we
		// just offered, so skip that.
		err = r.SetPosition(beginPos)
		if ebnf.IsStreamError(err) {
			return false, err
		}

		skipped := false

		matched, err = a.patterns[c.index].MatchCont(r, func(m *ebnf.Match[T, P]) (bool, error) {
			if !skipped {
				skipped = true
				return false, nil
			}

			return k(wrap(m))
		})
		if err != nil || matched {
			return matched, err
		}
	}

	return false, nil
}

// CanUnpack implements ebnf.Unpackable: the match of an alternation is a wrapper around the match of the branch that matched.
func (a *Alternation[T, P]) CanUnpack() bool {
	return true
}

// SetWeights sets the relative odds of the sub patterns when generating, one weight per sub pattern (in order).
// Weights are relative, {3, 1} means the first is chosen three times as often as the second. A weight of zero
// means the sub pattern is never generated. Without weights all sub patterns are equally likely.
// Weights do not influence matching.
func (a *Alternation[T, P]) SetWeights(weights ...float64) *Alternation[T, P] {
	a.weights = weights
	return a
}

// SetWeightFunc sets a function that decides the weights each time the alternation generates, for weights that depend on
// the generation state or depth, for example to steer a recursive grammar toward its end. The function must return one
// weight per sub pattern. It takes precedence over SetWeights.
func (a *Alternation[T, P]) SetWeightFunc(f func(*ebnf.GenContext) []float64) *Alternation[T, P] {
	a.weightFunc = f
	return a
}

// Generate writes one of the sub patterns to a writer, randomly chosen, by weight if weights are set
func (a *Alternation[T, P]) Generate(ctx *ebnf.GenContext, w ebnf.Writer[T]) error {
	if len(a.patterns) == 0 {
		return errors.New("exbana: cannot generate an alternation without patterns")
	}

	if err := ctx.Enter(); err != nil {
		return err
	}
	defer ctx.Leave()

	weights := a.weights
	if a.weightFunc != nil {
		weights = a.weightFunc(ctx)
	}

	index := 0

	switch {
	case weights != nil:
		if len(weights) != len(a.patterns) {
			return fmt.Errorf("exbana: alternation has %d patterns but %d weights", len(a.patterns), len(weights))
		}

		var err error

		index, err = ctx.Choose(weights)
		if err != nil {
			return err
		}
	default:
		index = ctx.Intn(len(a.patterns))
	}

	return a.patterns[index].Generate(ctx, w)
}

// Print EBNF alternation group
func (a *Alternation[T, P]) Print(w io.Writer) error {
	_, err := w.Write([]byte("("))
	if err != nil {
		return err
	}

	first := true

	for _, child := range a.patterns {
		if !first {
			_, err = w.Write([]byte(" | "))
			if err != nil {
				return err
			}
		}

		err = ebnf.PrintChild(w, child)
		if err != nil {
			return err
		}

		first = false
	}

	_, err = w.Write([]byte(")"))

	return err
}

// SetPatterns replaces the branches of the alternation.
func (a *Alternation[T, P]) SetPatterns(patterns ebnf.Patterns[T, P]) *Alternation[T, P] {
	a.patterns = patterns
	return a
}

// Children returns the sub patterns
func (a *Alternation[T, P]) Children() ebnf.Patterns[T, P] {
	return a.patterns
}

// Recognize implements ebnf.Recognizer: an alternation matches if one of the sub patterns matches. It can only tell
// when no sub pattern matches if all of them can tell.
func (a *Alternation[T, P]) Recognize(values []T) (bool, bool) {
	known := true

	for _, p := range a.patterns {
		r, ok := p.(ebnf.Recognizer[T])
		if !ok {
			known = false
			continue
		}

		matched, k := r.Recognize(values)
		if !k {
			known = false
		} else if matched {
			return true, true
		}
	}

	return false, known
}

// Named sets the identifier of the pattern and returns the pattern, so it can be chained: names are what syntax
// errors and printed grammars call the pattern.
func (a *Alternation[T, P]) Named(id string) *Alternation[T, P] {
	a.SetID(id)
	return a
}

// Printed sets the text the pattern prints as and returns the pattern.
func (a *Alternation[T, P]) Printed(text string) *Alternation[T, P] {
	a.SetText(text)
	return a
}

// Shape implements ebnf.Structured.
func (a *Alternation[T, P]) Shape() ebnf.Shape[T, P] {
	return ebnf.Shape[T, P]{Kind: ebnf.KindAlt, ID: a.ID(), Text: a.Text(), Orthogonal: a.isOrthogonal, Parts: a.patterns}
}
