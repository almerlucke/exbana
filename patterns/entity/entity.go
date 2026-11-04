package entity

import (
	"fmt"
	"io"

	ebnf "github.com/almerlucke/exbana/v3"
)

// Entity represents a single entity pattern
type Entity[T, P any] struct {
	ebnf.Label
	matchFunc func(T) bool
	genFunc   func(*ebnf.GenContext) T
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
		ebnf.LogMismatch(rd, ebnf.Pattern[T, P](e), func() *ebnf.Mismatch[T, P] { return ebnf.NewMismatch[T, P](e, pos, pos, nil, nil) })
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

		ebnf.LogMismatch(rd, ebnf.Pattern[T, P](e), func() *ebnf.Mismatch[T, P] { return ebnf.NewMismatch[T, P](e, pos, endPos, nil, nil) })
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
