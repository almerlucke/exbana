package vector

import (
	ebnf "github.com/almerlucke/exbana/v2"
)

// Vector represents a series of entities to match
type Vector[T, P any] struct {
	*ebnf.BasePattern[T, P]
	eq     func(T, T) bool
	vector []T
}

// New creates a new vector pattern
func New[T, P any](eq func(T, T) bool, vec ...T) *Vector[T, P] {
	v := &Vector[T, P]{
		BasePattern: ebnf.NewBasePattern[T, P](),
		eq:          eq,
		vector:      vec,
	}

	v.SetSelf(v)

	return v
}

// Match matches the vector pattern against a stream
func (v *Vector[T, P]) Match(rd ebnf.Reader[T, P]) (bool, *ebnf.Match[T, P], error) {
	beginPos, err := rd.Position()
	if ebnf.IsStreamError(err) {
		return false, nil, err
	}

	for _, e1 := range v.vector {
		e2, err := rd.Read1()
		if ebnf.IsStreamError(err) {
			return false, nil, err
		}

		if !v.eq(e1, e2) {
			endPos, err := rd.Position()
			if ebnf.IsStreamError(err) {
				return false, nil, err
			}

			v.Logger().LogMismatch(ebnf.NewMismatch[T, P](v, beginPos, endPos, nil, nil))

			return false, nil, nil
		}
	}

	endPos, err := rd.Position()
	if ebnf.IsStreamError(err) {
		return false, nil, err
	}

	val, err := rd.Range(beginPos, endPos)
	if err != nil {
		return false, nil, err
	}

	return true, ebnf.NewMatch(v, beginPos, endPos, val, nil), nil
}

// Generate writes a series of entities to a writer
func (v *Vector[T, P]) Generate(wr ebnf.Writer[T]) error {
	return wr.Write(v.vector...)
}
