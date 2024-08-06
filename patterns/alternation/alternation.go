package alternation

import (
	ebnf "github.com/almerlucke/exbana/v2"
	"io"
	"math/rand"
)

// Alternation matches a series of patterns OR style in order (alternation)
type Alternation[T, P any] struct {
	*ebnf.BasePattern[T, P]
	patterns ebnf.Patterns[T, P]
}

// New creates a new Alternation pattern
func New[T, P any](patterns ...ebnf.Pattern[T, P]) *Alternation[T, P] {
	a := &Alternation[T, P]{
		BasePattern: ebnf.NewBasePattern[T, P](),
		patterns:    patterns,
	}

	a.SetSelf(a)

	return a
}

// Match matches the Alternation sub patterns against a stream, fails if there is no match. If there are more than one match,
// the longest match returns, if two or more matches are the longest, the first of those is returned. So order of the sub
// patterns matters when creating an Alternation pattern
func (a *Alternation[T, P]) Match(r ebnf.Reader[T, P]) (bool, *ebnf.Match[T, P], error) {
	var matches []*ebnf.Match[T, P]

	beginPos, err := r.Position()
	if ebnf.IsStreamError(err) {
		return false, nil, err
	}

	for _, pm := range a.patterns {
		err := r.SetPosition(beginPos)
		if ebnf.IsStreamError(err) {
			return false, nil, err
		}

		matched, result, err := pm.Match(r)
		if err != nil {
			return false, nil, err
		}

		if matched {
			endPos, err := r.Position()
			if ebnf.IsStreamError(err) {
				return false, nil, err
			}

			matches = append(matches, ebnf.NewMatch(a, beginPos, endPos, nil, []*ebnf.Match[T, P]{result}))
		}
	}

	if len(matches) > 0 {
		var (
			longestMatch *ebnf.Match[T, P]
			length       int
		)

		for _, match := range matches {
			matchLength := r.Length(match.Begin, match.End)
			if matchLength > length {
				length = matchLength
				longestMatch = match
			}
		}

		return true, longestMatch, nil
	}

	endPos, err := r.Position()
	if ebnf.IsStreamError(err) {
		return false, nil, err
	}

	a.Logger().LogMismatch(ebnf.NewMismatch[T, P](a, beginPos, endPos, nil, nil))

	return false, nil, nil
}

// Generate writes an alternation of patterns to a writer, randomly chosen
func (a *Alternation[T, P]) Generate(w ebnf.Writer[T]) error {
	return a.patterns[rand.Intn(len(a.patterns))].Generate(w)
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

		err = child.PrintAsChild(w)
		if err != nil {
			return err
		}

		first = false
	}

	_, err = w.Write([]byte(")"))

	return err
}
