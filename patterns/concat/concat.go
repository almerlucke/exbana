package concat

import (
	ebnf "github.com/almerlucke/exbana/v2"
	"io"
)

// Concat matches a series of patterns AND style in order (concatenation)
type Concat[T, P any] struct {
	*ebnf.BasePattern[T, P]
	patterns ebnf.Patterns[T, P]
}

// New creates a new concat pattern
func New[T, P any](patterns ...ebnf.Pattern[T, P]) *Concat[T, P] {
	c := &Concat[T, P]{
		BasePattern: ebnf.NewBasePattern[T, P](),
		patterns:    patterns,
	}

	c.SetSelf(c)

	return c
}

// Match matches AND against a stream, fails if any of the patterns mismatches
func (c *Concat[T, P]) Match(rd ebnf.Reader[T, P]) (bool, *ebnf.Match[T, P], error) {
	var matches []*ebnf.Match[T, P]

	beginPos, err := rd.Position()
	if ebnf.IsStreamError(err) {
		return false, nil, err
	}

	for _, pm := range c.patterns {
		subBeginPos, err := rd.Position()
		if ebnf.IsStreamError(err) {
			return false, nil, err
		}

		matched, result, err := pm.Match(rd)
		if err != nil {
			return false, nil, err
		}

		if matched {
			matches = append(matches, result)
		} else {
			subEndPos, err := rd.Position()
			if ebnf.IsStreamError(err) {
				return false, nil, err
			}

			c.Logger().LogMismatch(ebnf.NewMismatch(c, beginPos, subEndPos, ebnf.NewMatch(pm, subBeginPos, subEndPos, nil, nil), matches))

			return false, nil, nil
		}
	}

	endPos, err := rd.Position()
	if ebnf.IsStreamError(err) {
		return false, nil, err
	}

	return true, ebnf.NewMatch(c, beginPos, endPos, nil, matches), nil
}

// Generate writes a concatenation of patterns to a writer
func (c *Concat[T, P]) Generate(w ebnf.Writer[T]) error {
	for _, child := range c.patterns {
		err := child.Generate(w)
		if err != nil {
			return err
		}
	}

	return nil
}

// Print EBNF concatenation group
func (c *Concat[T, P]) Print(w io.Writer) error {
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
