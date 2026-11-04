// Package exbana is a generic EBNF pattern matching and generation library. Grammars are built from patterns that
// match any stream of objects of type T, at positions of type P, and the same patterns can generate output and print
// themselves as EBNF.
//
// # Using the library
//
// Readers come from the readers packages: runes for text, slice for a slice of anything (tokens from a lexer, say).
// Build a grammar from the pattern packages (entity, vector, concatenation, alternation, repetition, except, end and
// rule), match it with [Parse], and read the result from the [Match]:
//
//	ok, match, err := ebnf.Parse(grammar, reader)
//
// [Parse] returns the most preferred match. [Matches] iterates over all of them. Patterns backtrack: when what follows a
// pattern fails, it offers its next alternative; possessive repetitions and orthogonal alternations opt out of that for
// speed.
//
// Everything that belongs to one parse lives in a [ParseContext] that wraps the reader, so a grammar is never changed
// by parsing it and can be shared between goroutines:
//
//	ctx := ebnf.NewParseContext(reader).WithLogger(log).WithMemo(indexOf)
//	ok, match, err := ebnf.Parse(grammar, ctx)
//
// A logger receives the mismatches of the parse; [FarthestLog] turns them into a [SyntaxError] that says where the
// input went wrong and what was expected. WithMemo remembers the ways each pattern can end at each position, which
// keeps ambiguous grammars from taking exponential time.
//
// Name patterns with Named and give leaves a print text with Printed. A rule (package rule) is a named pattern with an
// evaluation function that [Match.Eval] runs; rules can be declared before they are defined for recursive grammars,
// and marked left recursive.
//
// To generate, call Generate on a pattern with a [GenContext], which carries the seeded random source, the depth limit
// and your own state. [PrintRules] prints a grammar as EBNF.
//
// # Writing your own patterns
//
// A [Pattern] has three methods: MatchCont, Generate and Print. MatchCont offers every way the pattern can match to a
// continuation, in order of preference, and stops when the continuation accepts one. Patterns with exactly one way to
// match use [Single]. Everything else about a pattern is optional and found by asking it: [Named] (an ID), [Evaluator]
// (a value for a match), [Unpackable], [Parent] (the patterns it is built from, which loggers use to find out
// whether it is a composite) and [Recognizer] (a way to check generated output, used by except).
//
// A composite pattern that wants memoization wraps its matcher in [MatchMemo]. Its search can use [FailSet] to
// remember dead ends for the duration of one call, [FirstMatch] to run a sub pattern to its first match, and
// [DistinctMatches] to walk the matches of a sub pattern in a loop. Failures are reported with [LogFailure] (a leaf)
// or [LogMismatch] (a composite, with details), and lookahead is matched through [Silent] so that it does not reach
// the logger. Embed [Label] to be [Named] and to print leaves. [EnsureContext], [EndSet] and the rule frames are what
// a rule uses to find left recursion.
//
// # Reading a grammar
//
// [Analyze] describes a pattern and everything it is built from as a [Grammar]: a tree of [Node] and a table of rules.
// Each pattern describes itself with a [Shape] (see [Structured], which a pattern you write can implement too); one that
// does not is [KindOpaque]. The declarative leaves of the entity and vector packages (Between, In, Equals, Text and the
// like) say what they accept, where a leaf made from a function is only "one element".
package exbana

import (
	"bytes"
	"fmt"
	"io"
)

// IsStreamError check if err is set and not io.EOF
func IsStreamError(err error) bool {
	return err != nil && err != io.EOF
}

// Scan scans a stream for a pattern and returns all match results.
// It skips one element at a time if the pattern does not match at the current position.
func Scan[T, P any](stream Reader[T, P], pattern Pattern[T, P]) ([]*Match[T, P], error) {
	var results []*Match[T, P]

	for !stream.Finished() {
		pos, err := stream.Position()
		if IsStreamError(err) {
			return nil, err
		}
		matched, result, err := Parse(pattern, stream)
		if err != nil {
			return nil, err
		}
		if matched {
			results = append(results, result)

			endPos, err := stream.Position()
			if IsStreamError(err) {
				return nil, err
			}

			// the match consumed input, continue after it
			if stream.Length(pos, endPos) > 0 {
				continue
			}
		}

		// no match, or an empty match: step one element so we always make progress
		err = stream.SetPosition(pos)
		if IsStreamError(err) {
			return nil, err
		}
		_, err = stream.Skip(1)
		if IsStreamError(err) {
			return nil, err
		}
	}

	return results, nil
}

// PrintRules prints a slice of patterns in EBNF rule format (e.g., "id = pattern") and returns the result as a string.
func PrintRules[T, P any](patterns []Pattern[T, P]) (string, error) {
	var buf bytes.Buffer

	for _, pattern := range patterns {
		_, err := buf.WriteString(fmt.Sprintf("%s = ", IDOf(pattern)))
		if err != nil {
			return "", err
		}

		err = pattern.Print(&buf)
		if err != nil {
			return "", err
		}

		_, err = buf.WriteString("\n")
		if err != nil {
			return "", err
		}
	}

	return buf.String(), nil
}
