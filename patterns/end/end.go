package end

import (
	ebnf "github.com/almerlucke/exbana/v2"
)

// End matches the end of stream
type End[T, P any] struct {
	*ebnf.BasePattern[T, P]
}

// New creates a new end of stream pattern
func New[T, P any]() *End[T, P] {
	return &End[T, P]{
		BasePattern: ebnf.NewBasePattern[T, P](),
	}
}

// Match matches a end of stream pattern against a stream
func (e *End[T, P]) Match(r ebnf.Reader[T, P]) (bool, *ebnf.Match[T, P], error) {
	pos, err := r.Position()
	if ebnf.IsStreamError(err) {
		return false, nil, err
	}

	if r.Finished() {
		return true, ebnf.NewMatch[T, P](e, pos, pos, nil, nil), nil
	}

	e.Logger().LogMismatch(ebnf.NewMismatch[T, P](e, pos, pos, nil, nil))

	return false, nil, nil
}

// Generate sends finish to writer
func (e *End[T, P]) Generate(w ebnf.Writer[T]) error {
	return w.Finish()
}
