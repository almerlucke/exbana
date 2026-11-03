# exbana

Exbana is a versatile and generic EBNF-based pattern matching and generation library for Go. It allows you to define complex patterns using common EBNF constructs and match them against any stream of objects.

## Features

- **Generic**: Works with any type of stream elements (`T`) and any position type (`P`).
- **EBNF Constructs**: Supports concatenation, alternation, repetition (optional, any, one-or-more), and exceptions.
- **Backtracking**: Full backtracking across concatenations, alternations and repetitions, with opt-outs (possessive, orthogonal) and optional failure memoization.
- **Evaluation**: Easily attach evaluation functions to patterns to process match results.
- **Generation**: Can generate output based on defined patterns.
- **EBNF Printing**: Can print defined patterns back into EBNF format.
- **Debugging**: Integrated logging system for tracking matches and troubleshooting mismatches.

## Installation

```bash
go get github.com/almerlucke/exbana/v2
```

## Core Concepts

### `Pattern[T, P]`
The fundamental interface representing an EBNF pattern. It can match against a `Reader`, generate output to a `Writer`, and print itself in EBNF format.

### `Reader[T, P]`
An interface for a stream that serves objects of type `T` at positions of type `P`. The library provides a `runes` reader for matching against strings or rune slices.

### `Match[T, P]`
Contains the result of a successful match, including the matched pattern, the range (begin/end positions), and any sub-component matches.

### `Logger[T, P]`
Allows you to track the matching process, which is especially useful for debugging complex grammars to find why and where a match failed.

## Built-in Patterns

- **`entity`**: Matches a single element based on a predicate function.
- **`vector`**: Matches a specific sequence of elements (like a string).
- **`concatenation`**: Matches a sequence of sub-patterns.
- **`alternation`**: Matches one of several sub-patterns (returns the longest match by default).
- **`repetition`**: Matches a sub-pattern multiple times (supports min/max counts and possessive matching).
- **`except`**: Matches a pattern only if another "exception" pattern does not match at the same position.

## Quick Example (JSON-like Number)

```go
package main

import (
	"fmt"
	ebnf "github.com/almerlucke/exbana/v2"
	"github.com/almerlucke/exbana/v2/patterns/alternation"
	"github.com/almerlucke/exbana/v2/patterns/concatenation"
	ent "github.com/almerlucke/exbana/v2/patterns/entity"
	"github.com/almerlucke/exbana/v2/patterns/repetition"
	"github.com/almerlucke/exbana/v2/readers/runes"
	"unicode"
)

func main() {
	// Helper functions for readability
	alt := alternation.New[rune, runes.Pos]
	conc := concatenation.New[rune, runes.Pos]
	rep := repetition.Any[rune, runes.Pos]
	opt := repetition.Optional[rune, runes.Pos]
	
	digit := ent.New[rune, runes.Pos](unicode.IsDigit)
	digit1to9 := ent.New[rune, runes.Pos](func(r rune) bool {
		return r >= '1' && r <= '9'
	})
	
	// integer = [ "-" ] ( "0" | digit1to9 { digit } )
	integer := conc(
		opt(ent.New[rune, runes.Pos](func(r rune) bool { return r == '-' })),
		alt(
			ent.New[rune, runes.Pos](func(r rune) bool { return r == '0' }),
			conc(digit1to9, rep(digit)),
		),
	)

	// Create a reader for a string
	rd := runes.New([]rune("-123"))
	
	matched, match, err := integer.Match(rd)
	if err != nil {
		panic(err)
	}
	
	if matched {
		// Get matched content
		content, _ := rd.Range(match.Begin, match.End)
		fmt.Printf("Matched: %s\n", string(content))
	}
}
```

## Backtracking and Memoization

Patterns offer their matches to a continuation (`MatchCont`), so when something later in a concatenation fails, earlier
patterns are asked for their next alternative. Matches are offered in order of preference:

- `alternation`: longest first (ties in pattern order), and each branch's other matches after its first.
- `repetition`: greedy, giving back one repetition at a time down to `min`.
- `Match` is simply "the first candidate that fully matches".

So `(a*), a` matches `aa`, and `(a | ab), "bc"` matches `abc`.

Backtracking costs time and stack (a non-possessive repetition recurses once per repetition), so for the parts of a
grammar that are unambiguous, opt out:

- `repetition.SetPossessive(true)`: iterative, never gives matches back.
- `alternation.SetOrthogonal(true)`: commits to the first branch that matches.

For grammars that do backtrack a lot, wrap the reader to remember dead ends:

```go
rd, _ := runes.New(strings.NewReader(input))
mrd := ebnf.WithMemo[rune, runes.Pos](rd, func(p runes.Pos) int { return p.Index })
matched, result, err := pattern.Match(mrd)
```

This remembers failures only (a pattern with no match at a position; a concatenation or repetition whose remainder
already failed from a position), so catastrophic cases like `(a*)*, c` stay polynomial. It does not cache successes.
Use one wrapper per input; it assumes the input does not change. Mismatches already seen are not logged again.

## EBNF Printing

Exbana can output your defined patterns as EBNF strings:

```go
rules, _ := ebnf.PrintRules([]ebnf.Pattern[rune, runes.Pos]{integer.SetID("integer")})
fmt.Println(rules)
// Output: integer = [ "-" ] ( "0" | digit1to9 { digit } )
```

## Advanced Usage

For more complex examples including full JSON parsing and Go-like grammar, see the `examples/` directory.

