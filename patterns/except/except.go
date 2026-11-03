package except

import (
	"io"

	ebnf "github.com/almerlucke/exbana/v2"
)

// Except must not match the except pattern but must match the must pattern
type Except[T, P any] struct {
	*ebnf.BasePattern[T, P]
	must      ebnf.Pattern[T, P]
	exception ebnf.Pattern[T, P]
}

// New creates a new exception pattern
func New[T, P any](must ebnf.Pattern[T, P], exception ebnf.Pattern[T, P]) *Except[T, P] {
	e := &Except[T, P]{
		BasePattern: ebnf.NewBasePattern[T, P](),
		must:        must,
		exception:   exception,
	}

	e.SetSelf(e)

	return e
}

// Match matches the exception against a stream
func (e *Except[T, P]) Match(r ebnf.Reader[T, P]) (bool, *ebnf.Match[T, P], error) {
	return ebnf.FirstMatch(r, e.MatchCont)
}

// MatchCont offers the matches of the must pattern, unless the exception pattern matches at the same position
func (e *Except[T, P]) MatchCont(r ebnf.Reader[T, P], k ebnf.Continuation[T, P]) (bool, error) {
	return ebnf.MatchMemo(ebnf.Pattern[T, P](e), r, k, e.matchCont)
}

func (e *Except[T, P]) matchCont(r ebnf.Reader[T, P], k ebnf.Continuation[T, P]) (bool, error) {
	beginPos, err := r.Position()
	if ebnf.IsStreamError(err) {
		return false, err
	}

	// First check for the exception match, we do not want to match the exception
	matched, result, err := e.exception.Match(r)
	if err != nil {
		return false, err
	}

	if matched {
		endPos, err := r.Position()
		if ebnf.IsStreamError(err) {
			return false, err
		}

		e.Logger().LogMismatch(ebnf.NewMismatch(e, beginPos, endPos, result, nil))

		return false, nil
	}

	// Reset the position and offer the must matches
	err = r.SetPosition(beginPos)
	if ebnf.IsStreamError(err) {
		return false, err
	}

	return e.must.MatchCont(r, k)
}

// Generate let's MustMatch generate to writer
func (e *Except[T, P]) Generate(w ebnf.Writer[T]) error {
	return e.must.Generate(w)
}

// Print EBNF exception pattern
func (e *Except[T, P]) Print(w io.Writer) error {
	err := e.must.Print(w)
	if err != nil {
		return err
	}
	_, err = w.Write([]byte(" - "))
	if err != nil {
		return err
	}

	err = e.exception.Print(w)

	return err
}

// Children returns the must pattern. The exception is left out on purpose: it is lookahead, so it is supposed to
// fail on every successful match and its failures are not syntax errors.
func (e *Except[T, P]) Children() ebnf.Patterns[T, P] {
	return ebnf.Patterns[T, P]{e.must}
}
