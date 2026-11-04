package concatenation

import (
	"io"
	"slices"

	ebnf "github.com/almerlucke/exbana/v3"
)

// Concatenation matches a series of patterns AND style in order (concatenation)
type Concatenation[T, P any] struct {
	ebnf.Label
	patterns ebnf.Patterns[T, P]
}

// New creates a new concatenation pattern
func New[T, P any](patterns ...ebnf.Pattern[T, P]) *Concatenation[T, P] {
	c := &Concatenation[T, P]{
		patterns: patterns,
	}

	return c
}

// Match matches AND against a stream, fails if any of the sub patterns mismatches. It returns the first
// combination of sub pattern matches that matches completely.
func (c *Concatenation[T, P]) Match(rd ebnf.Reader[T, P]) (bool, *ebnf.Match[T, P], error) {
	return ebnf.FirstMatch(rd, c.MatchCont)
}

// MatchCont offers every way the sub patterns can match in sequence to the continuation. If a sub pattern
// fails, the previous sub patterns are asked for their next alternative (backtracking).
func (c *Concatenation[T, P]) MatchCont(rd ebnf.Reader[T, P], k ebnf.Continuation[T, P]) (bool, error) {
	return ebnf.MatchMemo(ebnf.Pattern[T, P](c), rd, k, c.matchCont)
}

func (c *Concatenation[T, P]) matchCont(rd ebnf.Reader[T, P], k ebnf.Continuation[T, P]) (bool, error) {
	beginPos, err := rd.Position()
	if ebnf.IsStreamError(err) {
		return false, err
	}

	matches := make([]*ebnf.Match[T, P], 0, len(c.patterns))
	failed := ebnf.NewFailSet(rd)

	var step func(i int) (bool, error)

	step = func(i int) (bool, error) {
		subBeginPos, err := rd.Position()
		if ebnf.IsStreamError(err) {
			return false, err
		}

		// the rest of the concatenation (and the continuation) already failed from here
		if failed.Has(i, subBeginPos) {
			return false, nil
		}

		if i == len(c.patterns) {
			matched, err := k(ebnf.NewMatch(c, beginPos, subBeginPos, nil, slices.Clone(matches)))
			if err == nil && !matched {
				failed.Add(i, subBeginPos)
			}

			return matched, err
		}

		pm := c.patterns[i]

		matched, err := pm.MatchCont(rd, func(m *ebnf.Match[T, P]) (bool, error) {
			matches = append(matches[:i], m)
			return step(i + 1)
		})
		if err != nil || matched {
			return matched, err
		}

		subEndPos, err := rd.Position()
		if ebnf.IsStreamError(err) {
			return false, err
		}

		ebnf.LogMismatch(rd, ebnf.Pattern[T, P](c), func() *ebnf.Mismatch[T, P] {
			return ebnf.NewMismatch(c, beginPos, subEndPos, ebnf.NewMatch(pm, subBeginPos, subEndPos, nil, nil), slices.Clone(matches[:i]))
		})

		failed.Add(i, subBeginPos)

		return false, nil
	}

	return step(0)
}

// Generate writes a concatenation of patterns to a writer
func (c *Concatenation[T, P]) Generate(ctx *ebnf.GenContext, w ebnf.Writer[T]) error {
	if err := ctx.Enter(); err != nil {
		return err
	}
	defer ctx.Leave()

	for _, child := range c.patterns {
		err := child.Generate(ctx, w)
		if err != nil {
			return err
		}
	}

	return nil
}

// Print EBNF concatenation group
func (c *Concatenation[T, P]) Print(w io.Writer) error {
	_, err := w.Write([]byte("("))
	if err != nil {
		return err
	}

	first := true

	for _, child := range c.patterns {
		if !first {
			_, err = w.Write([]byte(", "))
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

func (c *Concatenation[T, P]) SetPatterns(patterns ebnf.Patterns[T, P]) *Concatenation[T, P] {
	c.patterns = patterns
	return c
}

// Children returns the sub patterns
func (c *Concatenation[T, P]) Children() ebnf.Patterns[T, P] {
	return c.patterns
}
