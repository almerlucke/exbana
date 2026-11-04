package except

import (
	"errors"
	"fmt"
	"io"

	ebnf "github.com/almerlucke/exbana/v3"
)

// Except must not match the except pattern but must match the must pattern
type Except[T, P any] struct {
	ebnf.Label
	must      ebnf.Pattern[T, P]
	exception ebnf.Pattern[T, P]
	accept    func([]T) bool
	attempts  int
}

// New creates a new exception pattern
func New[T, P any](must ebnf.Pattern[T, P], exception ebnf.Pattern[T, P]) *Except[T, P] {
	e := &Except[T, P]{
		must:      must,
		exception: exception,
	}

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
	// the exception is lookahead, it is supposed to fail on every successful match so it must not be logged
	matched, result, err := ebnf.Parse(e.exception, ebnf.Silent(r))
	if err != nil {
		return false, err
	}

	if matched {
		endPos, err := r.Position()
		if ebnf.IsStreamError(err) {
			return false, err
		}

		ebnf.LogMismatch(r, ebnf.Pattern[T, P](e), func() *ebnf.Mismatch[T, P] { return ebnf.NewMismatch(e, beginPos, endPos, result, nil) })

		return false, nil
	}

	// Reset the position and offer the must matches
	err = r.SetPosition(beginPos)
	if ebnf.IsStreamError(err) {
		return false, err
	}

	return e.must.MatchCont(r, k)
}

// DefaultGenerateAttempts is the number of times Except tries to generate something the exception allows
const DefaultGenerateAttempts = 100

// SetAcceptFunc sets a function that decides if generated values are allowed (true) or are matched by the exception
// (false). It is needed when the exception can't recognize values itself (see ebnf.Recognizer), and takes
// precedence when set.
func (e *Except[T, P]) SetAcceptFunc(f func([]T) bool) *Except[T, P] {
	e.accept = f
	return e
}

// SetGenerateAttempts sets how many times to try generating something the exception allows
func (e *Except[T, P]) SetGenerateAttempts(n int) *Except[T, P] {
	e.attempts = n
	return e
}

type bufferWriter[T any] struct {
	values   []T
	finished bool
}

// Write implements ebnf.Writer, it collects what the must pattern generates.
func (b *bufferWriter[T]) Write(values ...T) error {
	b.values = append(b.values, values...)
	return nil
}

// Finish implements ebnf.Writer, it notes that the must pattern finished the stream.
func (b *bufferWriter[T]) Finish() error {
	b.finished = true
	return nil
}

// Generate generates from the must pattern, and tries again if the exception matches what was generated. If the
// exception can't be checked (it isn't an ebnf.Recognizer and there is no accept function) it returns an error, as
// the result could not be trusted. The exception matches what was generated if it matches a prefix of it.
func (e *Except[T, P]) Generate(ctx *ebnf.GenContext, w ebnf.Writer[T]) error {
	if err := ctx.Enter(); err != nil {
		return err
	}
	defer ctx.Leave()

	accept := e.accept
	if accept == nil {
		recognizer, ok := e.exception.(ebnf.Recognizer[T])
		if !ok {
			return errors.New("exbana: cannot check the exception while generating, use SetAcceptFunc")
		}

		accept = func(values []T) bool {
			matched, known := recognizer.Recognize(values)
			// an exception that can't tell is not trusted
			return known && !matched
		}
	}

	attempts := e.attempts
	if attempts <= 0 {
		attempts = DefaultGenerateAttempts
	}

	for range attempts {
		var buf bufferWriter[T]

		if err := e.must.Generate(ctx, &buf); err != nil {
			return err
		}

		if !accept(buf.values) {
			continue
		}

		if err := w.Write(buf.values...); err != nil {
			return err
		}

		if buf.finished {
			return w.Finish()
		}

		return nil
	}

	return fmt.Errorf("exbana: no value the exception allows after %d attempts", attempts)
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

// Named sets the identifier of the pattern and returns the pattern, so it can be chained: names are what syntax
// errors and printed grammars call the pattern.
func (e *Except[T, P]) Named(id string) *Except[T, P] {
	e.SetID(id)
	return e
}

// Printed sets the text the pattern prints as and returns the pattern.
func (e *Except[T, P]) Printed(text string) *Except[T, P] {
	e.SetText(text)
	return e
}

// Shape implements ebnf.Structured.
func (e *Except[T, P]) Shape() ebnf.Shape[T, P] {
	return ebnf.Shape[T, P]{Kind: ebnf.KindExcept, ID: e.ID(), Text: e.Text(), Parts: ebnf.Patterns[T, P]{e.must, e.exception}}
}
