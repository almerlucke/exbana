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

	ebnf.LogMismatch(r, ebnf.Pattern[T, P](e), func() *ebnf.Mismatch[T, P] { return ebnf.NewMismatch[T, P](e, pos, pos, nil, nil) })

	return false, nil, nil
}

// Generate sends finish to writer
func (e *End[T, P]) Generate(_ *ebnf.GenContext, w ebnf.Writer[T]) error {
	return w.Finish()
}
