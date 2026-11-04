package end

import (
	"io"

	ebnf "github.com/almerlucke/exbana/v3"
)

// End matches the end of stream
type End[T, P any] struct {
	ebnf.Label
}

// New creates a new end of stream pattern
func New[T, P any]() *End[T, P] {
	e := &End[T, P]{}

	return e
}

// MatchCont offers the one match of the end pattern
func (e *End[T, P]) MatchCont(r ebnf.Reader[T, P], k ebnf.Continuation[T, P]) (bool, error) {
	return ebnf.Single(r, k, e.Match)
}

// Print prints the text of the pattern
func (e *End[T, P]) Print(w io.Writer) error {
	_, err := io.WriteString(w, e.Text())
	return err
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

	ebnf.LogFailure(r, ebnf.Pattern[T, P](e), pos, pos)

	return false, nil, nil
}

// Generate sends finish to writer
func (e *End[T, P]) Generate(_ *ebnf.GenContext, w ebnf.Writer[T]) error {
	return w.Finish()
}

// Named sets the identifier of the pattern and returns the pattern, so it can be chained: names are what syntax
// errors and printed grammars call the pattern.
func (e *End[T, P]) Named(id string) *End[T, P] {
	e.SetID(id)
	return e
}

// Printed sets the text the pattern prints as and returns the pattern.
func (e *End[T, P]) Printed(text string) *End[T, P] {
	e.SetText(text)
	return e
}
