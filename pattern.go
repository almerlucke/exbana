package exbana

import (
	"io"
)

const (
	NoID = ""
)

// Pattern is what a grammar is built from. It can match objects from a stream, generate objects to a stream and print
// itself in EBNF. That is all the library needs to know of a pattern; everything else is optional and found by
// asking the pattern whether it implements one of Named, Evaluator or Unpackable.
type Pattern[T, P any] interface {
	// MatchCont offers every possible match to the continuation, in order of preference, until the continuation
	// accepts one. This is what makes backtracking possible: if what follows a pattern fails, the continuation
	// rejects the candidate and the pattern offers its next alternative. Returns true if a candidate was accepted.
	// The reader is positioned at the end of the offered match when the continuation is called. If MatchCont
	// returns false the reader position is undefined, callers must reset it.
	// The reader may be a ParseContext, which carries the logger and the failure memo of the parse.
	MatchCont(Reader[T, P], Continuation[T, P]) (bool, error)

	// Generate generates output to a writer based on the pattern. The context carries the random source,
	// the depth limit and user state, and must not be nil.
	Generate(*GenContext, Writer[T]) error

	// Print prints the pattern to a writer in EBNF format.
	Print(io.Writer) error
}

// Named is implemented by patterns that have an identifier, like a Rule. Named patterns are what Unpack stops at,
// what syntax errors call the pattern, and how the pattern prints as part of another pattern.
type Named interface {
	ID() string
}

// Evaluator is implemented by patterns that can turn their match into a value, like a Rule with an evaluation function.
type Evaluator[T, P any] interface {
	Eval(*Match[T, P], Reader[T, P]) (any, error)
}

// Unpackable is implemented by patterns whose match is only a wrapper around the match of one of their components,
// like an alternation. Unpack looks through such wrappers.
type Unpackable interface {
	CanUnpack() bool
}

// Patterns is a convenience type for a slice of pattern interfaces
type Patterns[T, P any] []Pattern[T, P]

// IDOf returns the identifier of a pattern, or NoID if the pattern is not Named.
func IDOf[T, P any](p Pattern[T, P]) string {
	if n, ok := p.(Named); ok {
		return n.ID()
	}

	return NoID
}

// Parse matches a pattern against a reader and returns the first (most preferred) match. If it matches, the reader
// is positioned at the end of the match. Pass a ParseContext as reader to log mismatches or to memoize.
func Parse[T, P any](p Pattern[T, P], r Reader[T, P]) (bool, *Match[T, P], error) {
	return FirstMatch(r, p.MatchCont)
}

// Single offers the one result of a deterministic match function to the continuation. It is how patterns that have
// exactly one candidate, like an entity, implement MatchCont.
func Single[T, P any](r Reader[T, P], k Continuation[T, P], match func(Reader[T, P]) (bool, *Match[T, P], error)) (bool, error) {
	matched, m, err := match(r)
	if err != nil || !matched {
		return false, err
	}

	return k(m)
}

// PrintChild prints a pattern as part of another pattern: by its name if it has one, in full otherwise.
func PrintChild[T, P any](w io.Writer, p Pattern[T, P]) error {
	if id := IDOf(p); id != NoID {
		_, err := io.WriteString(w, id)
		return err
	}

	return p.Print(w)
}

// Label holds the optional name and print text of a pattern. Patterns embed it to be Named and to print leaves;
// set the fields with the Name and Text functions.
type Label struct {
	id   string
	text string
}

// ID returns the identifier of the pattern
func (l *Label) ID() string { return l.id }

// Text returns the text the pattern prints as, used by patterns that have no components (entity, vector, end)
func (l *Label) Text() string { return l.text }

// SetID sets the identifier of the pattern, see Name
func (l *Label) SetID(id string) { l.id = id }

// SetText sets the print text of the pattern, see Text
func (l *Label) SetText(text string) { l.text = text }

// Name sets the identifier of a pattern and returns the pattern: ebnf.Name(entity.New(isDigit), "digit")
func Name[L interface{ SetID(string) }](p L, id string) L {
	p.SetID(id)
	return p
}

// Text sets the text a pattern prints as and returns the pattern: ebnf.Text(entity.New(isPlus), `"+"`).
func Text[L interface{ SetText(string) }](p L, text string) L {
	p.SetText(text)
	return p
}
