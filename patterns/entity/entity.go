package entity

import (
	ebnf "github.com/almerlucke/exbana/v2"
)

// Entity represents a single entity pattern
type Entity[T, P any] struct {
	*ebnf.BasePattern[T, P]
	matchFunc func(T) bool
	genFunc   func() T
}

// New creates a new entity pattern
func New[T, P any](matchFunc func(T) bool) *Entity[T, P] {
	e := &Entity[T, P]{
		BasePattern: ebnf.NewBasePattern[T, P](),
		matchFunc:   matchFunc,
		genFunc:     nil,
	}

	e.SetSelf(e)

	return e
}

func (e *Entity[T, P]) SetGenerateFunc(f func() T) *Entity[T, P] {
	e.genFunc = f
	return e
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

		e.Logger().LogMismatch(ebnf.NewMismatch[T, P](e, pos, endPos, nil, nil))
	}

	return false, nil, nil
}

// Generate writes an entity to a writer
func (e *Entity[T, P]) Generate(w ebnf.Writer[T]) error {
	if e.genFunc != nil {
		return w.Write(e.genFunc())
	}

	return nil
}
