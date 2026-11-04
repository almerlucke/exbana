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

	s := &concatCall[T, P]{
		c:        c,
		rd:       rd,
		k:        k,
		beginPos: beginPos,
		matches:  make([]*ebnf.Match[T, P], 0, len(c.patterns)),
	}
	ebnf.InitFailSet(&s.failed, rd)

	return s.step(0)
}

// concatCall is the state of one MatchCont call of a concatenation.
type concatCall[T, P any] struct {
	c        *Concatenation[T, P]
	rd       ebnf.Reader[T, P]
	k        ebnf.Continuation[T, P]
	beginPos P

	// matches holds the match of every sub pattern so far. When the concatenation is complete the slice itself is handed
	// to the continuation, which saves a copy on the way that succeeds. It is then shared, and has to be copied before
	// backtracking changes it.
	matches []*ebnf.Match[T, P]
	shared  bool

	failed ebnf.FailSet[P]
}

// set records the match of sub pattern i, forgetting the ones after it
func (s *concatCall[T, P]) set(i int, m *ebnf.Match[T, P]) {
	if s.shared {
		s.matches = slices.Clone(s.matches[:i:i])
		s.matches = slices.Grow(s.matches, len(s.c.patterns)-i)
		s.shared = false
	}

	s.matches = append(s.matches[:i], m)
}

func (s *concatCall[T, P]) step(i int) (bool, error) {
	c, rd := s.c, s.rd

	subBeginPos, err := rd.Position()
	if ebnf.IsStreamError(err) {
		return false, err
	}

	// the rest of the concatenation (and the continuation) already failed from here
	if s.failed.Has(i, subBeginPos) {
		return false, nil
	}

	if i == len(c.patterns) {
		s.shared = true

		matched, err := s.k(ebnf.NewMatch(c, s.beginPos, subBeginPos, nil, s.matches[:i:i]))
		if err == nil && !matched {
			s.failed.Add(i, subBeginPos)
		}

		return matched, err
	}

	pm := c.patterns[i]

	matched, err := pm.MatchCont(rd, func(m *ebnf.Match[T, P]) (bool, error) {
		s.set(i, m)
		return s.step(i + 1)
	})
	if err != nil || matched {
		return matched, err
	}

	subEndPos, err := rd.Position()
	if ebnf.IsStreamError(err) {
		return false, err
	}

	ebnf.LogMismatch(rd, ebnf.Pattern[T, P](c), func() *ebnf.Mismatch[T, P] {
		return ebnf.NewMismatch(c, s.beginPos, subEndPos, ebnf.NewMatch(pm, subBeginPos, subEndPos, nil, nil), slices.Clone(s.matches[:i]))
	})

	s.failed.Add(i, subBeginPos)

	return false, nil
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

// SetPatterns replaces the patterns the concatenation is made of.
func (c *Concatenation[T, P]) SetPatterns(patterns ebnf.Patterns[T, P]) *Concatenation[T, P] {
	c.patterns = patterns
	return c
}

// Children returns the sub patterns
func (c *Concatenation[T, P]) Children() ebnf.Patterns[T, P] {
	return c.patterns
}

// Named sets the identifier of the pattern and returns the pattern, so it can be chained: names are what syntax
// errors and printed grammars call the pattern.
func (c *Concatenation[T, P]) Named(id string) *Concatenation[T, P] {
	c.SetID(id)
	return c
}

// Printed sets the text the pattern prints as and returns the pattern.
func (c *Concatenation[T, P]) Printed(text string) *Concatenation[T, P] {
	c.SetText(text)
	return c
}

// Shape implements ebnf.Structured.
func (c *Concatenation[T, P]) Shape() ebnf.Shape[T, P] {
	return ebnf.Shape[T, P]{Kind: ebnf.KindSeq, ID: c.ID(), Text: c.Text(), Parts: c.patterns}
}
