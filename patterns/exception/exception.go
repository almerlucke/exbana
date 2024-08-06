package exception

import (
	ebnf "github.com/almerlucke/exbana/v2"
	"io"
)

// Exception must not match the except pattern but must match the must pattern
type Exception[T, P any] struct {
	*ebnf.BasePattern[T, P]
	must      ebnf.Pattern[T, P]
	exception ebnf.Pattern[T, P]
}

// New creates a new exception pattern
func New[T, P any](must ebnf.Pattern[T, P], exception ebnf.Pattern[T, P]) *Exception[T, P] {
	e := &Exception[T, P]{
		BasePattern: ebnf.NewBasePattern[T, P](),
		must:        must,
		exception:   exception,
	}

	e.SetSelf(e)

	return e
}

// Match matches the exception against a stream
func (e *Exception[T, P]) Match(r ebnf.Reader[T, P]) (bool, *ebnf.Match[T, P], error) {
	beginPos, err := r.Position()
	if ebnf.IsStreamError(err) {
		return false, nil, err
	}

	// First check for the exception match, we do not want to match the exception
	matched, result, err := e.exception.Match(r)
	if err != nil {
		return false, nil, err
	}

	if matched {
		endPos, err := r.Position()
		if ebnf.IsStreamError(err) {
			return false, nil, err
		}

		e.Logger().LogMismatch(ebnf.NewMismatch(e, beginPos, endPos, result, nil))

		return false, nil, nil
	}

	// Reset the position and return must match result
	err = r.SetPosition(beginPos)
	if ebnf.IsStreamError(err) {
		return false, nil, err
	}

	return e.must.Match(r)
}

// Generate let's MustMatch generate to writer
func (e *Exception[T, P]) Generate(w ebnf.Writer[T]) error {
	return e.must.Generate(w)
}

// Print EBNF exception pattern
func (e *Exception[T, P]) Print(w io.Writer) error {
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
