package entity

import (
	"cmp"
	"fmt"
	"io"

	ebnf "github.com/almerlucke/exbana/v3"
)

// Entity represents a single entity pattern
type Entity[T, P any] struct {
	ebnf.Label
	matchFunc func(T) bool
	genFunc   func(*ebnf.GenContext) T

	// what a declarative entity (Between, In, Equals and the like) is made of, which is how it describes itself
	ranges  [][2]T
	negated bool
	exact   bool
	literal bool // a single value, compared with ==
}

// New creates a new entity pattern
func New[T, P any](matchFunc func(T) bool) *Entity[T, P] {
	e := &Entity[T, P]{
		matchFunc: matchFunc,
		genFunc:   nil,
	}

	return e
}

// SetGenerateFunc sets the function that creates the object to generate
func (e *Entity[T, P]) SetGenerateFunc(f func() T) *Entity[T, P] {
	e.genFunc = func(*ebnf.GenContext) T { return f() }
	return e
}

// SetGenerateFuncCtx sets the function that creates the object to generate, it gets the generate context so it can
// use the seeded random source and the user state
func (e *Entity[T, P]) SetGenerateFuncCtx(f func(*ebnf.GenContext) T) *Entity[T, P] {
	e.genFunc = f
	return e
}

// MatchCont offers the one match of the entity
func (e *Entity[T, P]) MatchCont(rd ebnf.Reader[T, P], k ebnf.Continuation[T, P]) (bool, error) {
	return ebnf.Single(rd, k, e.Match)
}

// Match matches the entity to a stream
func (e *Entity[T, P]) Match(rd ebnf.Reader[T, P]) (bool, *ebnf.Match[T, P], error) {
	pos, err := rd.Position()
	if ebnf.IsStreamError(err) {
		return false, nil, err
	}

	obj, err := rd.Read1()
	if ebnf.IsStreamError(err) {
		return false, nil, err
	}

	// at end of stream there is no object to test, never call matchFunc with a zero value
	if err == io.EOF {
		ebnf.LogFailure(rd, ebnf.Pattern[T, P](e), pos, pos)
		return false, nil, nil
	}

	if e.matchFunc(obj) {
		endPos, err := rd.Position()
		if ebnf.IsStreamError(err) {
			return false, nil, err
		}

		val, err := rd.Range(pos, endPos)
		if err != nil {
			return false, nil, err
		}

		return true, ebnf.NewMatch(e, pos, endPos, val, nil), nil
	} else {
		endPos, err := rd.Position()
		if ebnf.IsStreamError(err) {
			return false, nil, err
		}

		ebnf.LogFailure(rd, ebnf.Pattern[T, P](e), pos, endPos)
	}

	return false, nil, nil
}

// Generate writes an entity to a writer
func (e *Entity[T, P]) Generate(ctx *ebnf.GenContext, w ebnf.Writer[T]) error {
	if e.genFunc != nil {
		return w.Write(e.genFunc(ctx))
	}

	return fmt.Errorf("%w (%s)", ebnf.ErrNoGenerator, describe(e))
}

// Recognize implements ebnf.Recognizer: an entity matches if its first value matches
func (e *Entity[T, P]) Recognize(values []T) (bool, bool) {
	return len(values) > 0 && e.matchFunc(values[0]), true
}

func describe[T, P any](p ebnf.Pattern[T, P]) string {
	if d := ebnf.Describe(p); d != "" {
		return d
	}

	return "unnamed"
}

// Print prints the text of the entity, see Printed.
func (e *Entity[T, P]) Print(w io.Writer) error {
	_, err := w.Write([]byte(e.Text()))
	return err
}

// Named sets the identifier of the pattern and returns the pattern, so it can be chained: names are what syntax
// errors and printed grammars call the pattern.
func (e *Entity[T, P]) Named(id string) *Entity[T, P] {
	e.SetID(id)
	return e
}

// Printed sets the text the pattern prints as and returns the pattern.
func (e *Entity[T, P]) Printed(text string) *Entity[T, P] {
	e.SetText(text)
	return e
}

// Range is an inclusive range of elements, from Lo to Hi.
type Range[T cmp.Ordered] struct {
	Lo, Hi T
}

func inRanges[T cmp.Ordered](ranges [][2]T, negated bool) func(T) bool {
	return func(x T) bool {
		for _, r := range ranges {
			if x >= r[0] && x <= r[1] {
				return !negated
			}
		}

		return negated
	}
}

func class[T cmp.Ordered, P any](negated bool, ranges []Range[T]) *Entity[T, P] {
	rs := make([][2]T, len(ranges))
	for i, r := range ranges {
		rs[i] = [2]T{r.Lo, r.Hi}
	}

	e := New[T, P](inRanges(rs, negated))
	e.ranges, e.negated, e.exact = rs, negated, true

	return e
}

// Between matches one element from lo to hi, both included: entity.Between[rune, runes.Pos]('a', 'z').
// Unlike an entity made with New, which is decided by a function nobody can look into, it describes itself, so tools
// can analyse the grammar (see ebnf.Analyze).
func Between[T cmp.Ordered, P any](lo, hi T) *Entity[T, P] {
	return class[T, P](false, []Range[T]{{lo, hi}})
}

// In matches one element that is in one of the ranges, like the class [a-zA-Z_] of a regular expression.
func In[T cmp.Ordered, P any](ranges ...Range[T]) *Entity[T, P] {
	return class[T, P](false, ranges)
}

// NotIn matches one element that is in none of the ranges, like the class [^"\\].
func NotIn[T cmp.Ordered, P any](ranges ...Range[T]) *Entity[T, P] {
	return class[T, P](true, ranges)
}

// OneOf matches one element that is one of the values.
func OneOf[T cmp.Ordered, P any](values ...T) *Entity[T, P] {
	ranges := make([]Range[T], len(values))
	for i, v := range values {
		ranges[i] = Range[T]{v, v}
	}

	return class[T, P](false, ranges)
}

// Equals matches one element that is equal to the value. It can generate that value without a generate function.
func Equals[T comparable, P any](value T) *Entity[T, P] {
	e := New[T, P](func(x T) bool { return x == value })
	e.ranges, e.exact, e.literal = [][2]T{{value, value}}, true, true
	e.SetGenerateFunc(func() T { return value })

	return e
}

// Any matches any one element.
func Any[T, P any]() *Entity[T, P] {
	e := New[T, P](func(T) bool { return true })
	e.negated, e.exact = true, true

	return e
}

// Shape implements ebnf.Structured. An entity made with New is a class of one element that only its function knows;
// the declarative ones know their ranges.
func (e *Entity[T, P]) Shape() ebnf.Shape[T, P] {
	s := ebnf.Shape[T, P]{Kind: ebnf.KindClass, ID: e.ID(), Text: e.Text(), Exact: e.exact}

	switch {
	case e.literal:
		s.Kind, s.Values = ebnf.KindLiteral, []T{e.ranges[0][0]}
	case e.exact:
		s.Ranges, s.Negated = e.ranges, e.negated
	}

	return s
}
