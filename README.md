# exbana

Exbana is a versatile and generic EBNF-based pattern matching and generation library for Go. It allows you to define complex patterns using common EBNF constructs and match them against any stream of objects.

## Features

- **Generic**: Works with any type of stream elements (`T`) and any position type (`P`).
- **EBNF Constructs**: Supports concatenation, alternation, repetition (optional, any, one-or-more), and exceptions.
- **Backtracking**: Full backtracking across concatenations, alternations and repetitions, with opt-outs (possessive, orthogonal) and optional failure memoization.
- **Rules**: Named patterns with evaluation functions and forward declarations for recursive grammars, including left recursion.
- **All parses**: Iterate over every way a pattern can match, to backtrack by hand or to find ambiguity.
- **Syntax errors**: Reports the farthest failure and what was expected there.
- **Safe to share**: Everything specific to one parse lives in a `ParseContext`, so one grammar can serve many goroutines at once.
- **Generation**: Seeded, weighted generation from the same patterns.
- **EBNF Printing**: Can print defined patterns back into EBNF format.

## Installation

```bash
go get github.com/almerlucke/exbana/v3
```

Requires Go 1.24 or newer. The library is licensed under the MIT license.

## Core Concepts

### `Pattern[T, P]`
The fundamental interface representing an EBNF pattern. It has three methods: `MatchCont` (match against a `Reader`, offering every candidate to a continuation so patterns can backtrack), `Generate` (write to a `Writer`) and `Print` (EBNF format). Use `ebnf.Parse(pattern, reader)` to get the first match.

Everything else is optional and found by asking the pattern: `Named` (an `ID`, which `Match.Unpack`, syntax errors and printing use), `Evaluator` (turns a match into a value, which `Match.Eval` calls) and `Unpackable`. To write your own pattern, implement the three methods; `ebnf.Single` helps with patterns that have one candidate. Names go on patterns with `.Named("digit")`, print text with `.Printed("\"+\"")`, and evaluation functions on a `rule.Rule`. `Named` and `Printed` return the pattern, so they chain with the other setters (`repetition.Any(p).Named("ws").SetPossessive(true)`). The generic helpers `ebnf.Name(p, "digit")` and `ebnf.Text(p, ...)` do the same for any type that embeds `ebnf.Label`, such as your own patterns.

### `Reader[T, P]`
An interface for a stream that serves objects of type `T` at positions of type `P`. The library provides a `runes` reader for text and a `slice` reader (`slice.New(items)`, positions are indexes) for anything else: tokens from a lexer, bytes, events, records.

### `Match[T, P]`
Contains the result of a successful match, including the matched pattern, the range (begin/end positions), and any sub-component matches. `m.ID()` gives the name of the matched pattern, `m.Eval(reader)` evaluates it and `m.Unpack()` looks through unnamed wrappers like an alternation.

### `ParseContext[T, P]`
Wraps a reader and carries everything that belongs to one parse: the logger and the failure memo. Patterns are never changed while matching, so a grammar can be shared between goroutines as long as each parse has its own context:

```go
ctx := ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(log).WithMemo(func(p runes.Pos) int { return p.Index })
matched, match, err := ebnf.Parse(grammar, ctx)
```

Both `WithLogger` and `WithMemo` are optional. Any `Reader` works too, you just get no logging and no memo.

### `Logger[T, P]`
Receives the mismatches of a parse, which is useful for debugging complex grammars to find why and where a match failed. See `FarthestLog` (below), or write your own: a `Logger` is a single method. Mismatches are only built when a logger is listening.

## Built-in Patterns

- **`entity`**: Matches a single element based on a predicate function.
- **`vector`**: Matches a specific sequence of elements (like a string).
- **`concatenation`**: Matches a sequence of sub-patterns.
- **`alternation`**: Matches one of several sub-patterns (returns the longest match by default).
- **`repetition`**: Matches a sub-pattern multiple times (supports min/max counts and possessive matching).
- **`except`**: Matches a pattern only if another "exception" pattern does not match at the same position.
- **`end`**: Matches the end of the stream.
- **`rule`**: A named pattern with an evaluation function, see Rules.

## Quick Example (JSON-like Number)

```go
package main

import (
	"fmt"
	"strings"
	"unicode"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	ent "github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

func main() {
	// Helper functions for readability
	alt := alternation.New[rune, runes.Pos]
	conc := concatenation.New[rune, runes.Pos]
	rep := repetition.Any[rune, runes.Pos]
	opt := repetition.Optional[rune, runes.Pos]

	char := func(c rune) *ent.Entity[rune, runes.Pos] {
		return ent.New[rune, runes.Pos](func(r rune) bool { return r == c }).Printed(`"` + string(c) + `"`)
	}
	digit := ent.New[rune, runes.Pos](unicode.IsDigit).Named("digit")
	digit1to9 := ent.New[rune, runes.Pos](func(r rune) bool { return r >= '1' && r <= '9' }).Named("digit1to9")

	// integer = [ "-" ] ( "0" | digit1to9 { digit } )
	integer := conc(
		opt(char('-')),
		alt(char('0'), conc(digit1to9, rep(digit))),
	).Named("integer")

	// Create a reader for a string
	rd, err := runes.New(strings.NewReader("-123"))
	if err != nil {
		panic(err)
	}

	matched, match, err := ebnf.Parse[rune, runes.Pos](integer, rd)
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
- `ebnf.Parse` is simply "the first candidate that fully matches".

So `(a*), a` matches `aa`, and `(a | ab), "bc"` matches `abc`.

Backtracking costs time and stack (a non-possessive repetition recurses once per repetition), so for the parts of a
grammar that are unambiguous, opt out:

- `repetition.SetPossessive(true)`: iterative, never gives matches back.
- `alternation.SetOrthogonal(true)`: commits to the first branch that matches.

Running out of stack is a fatal error in Go that can not be recovered from, so a match that nests so deeply that it comes close
(a million items in a non-possessive repetition, or the same number of nested parentheses in a recursive rule) returns
`ebnf.ErrTooDeep` instead. A possessive repetition does not recurse, and neither does a repetition matched through the
memoizing reader below.

For grammars that do backtrack a lot, ask the parse context to remember dead ends:

```go
rd, _ := runes.New(strings.NewReader(input))
ctx := ebnf.NewParseContext[rune, runes.Pos](rd).WithMemo(func(p runes.Pos) int { return p.Index })
matched, result, err := ebnf.Parse(pattern, ctx)
```

For each composite pattern and position the context remembers the distinct ways it can end (one match per end position, the
preferred one). Whether the rest of a parse succeeds depends on where a pattern ended and not on how it matched, so the result
of a parse is the same with or without the memo, but the work is bounded by patterns x positions instead of by the number of
derivations. That keeps cases like `(a*)*, c` and nested unfactored alternations (`e = e "+" a | e "-" a | e`) polynomial, where they
are exponential without it. The price is a map entry per pattern and position, and that the first time a pattern is tried at a
position all its ways to match are computed, not just the first. Use one context per input; it assumes the input does not
change. Mismatches already seen are not logged again.

### Iterating over all matches

`ebnf.Matches(pattern, reader)` is an iterator (`iter.Seq2`) over every way the pattern matches at the current position, in order of
preference. `Parse` is the first of them. Breaking out of the loop stops the matcher, no work is done for the matches you don't ask for:

```go
for m, err := range ebnf.Matches(expr, rd) {
	if err != nil { return err }
	fmt.Println("can end at", m.End)
}
```

While the loop body runs the reader stands at the end of the match. To find out if a grammar is ambiguous for an input, count the
matches of the grammar followed by `end`: more than one means more than one parse. With a memoizing context there is one match per
end position.

## Generation

Every pattern can also generate output. All randomness comes from a `GenContext`, so output is reproducible per seed:

```go
ctx := ebnf.NewGenContext(42) // same seed, same output
err := pattern.Generate(ctx, writer)
```

- `entity.SetGenerateFuncCtx(func(ctx *ebnf.GenContext) T)` creates the generated object, draw from `ctx.Intn` / `ctx.Float64` to stay seedable (`SetGenerateFunc` is the version without context).
- `alternation.SetWeights(3, 1, 0)` sets relative odds per sub pattern (0 means never generated). `SetWeightFunc` decides the weights on every
  generate, from `ctx.State` (free for your own use, e.g. a key or the previous note) or `ctx.Depth()`.
- `ctx.MaxDepth` (default 1000) limits the nesting of composite patterns. Generating a recursive grammar that doesn't end returns `ebnf.ErrMaxDepth`
  instead of overflowing the stack; use a weight function on `ctx.Depth()` to steer a recursive grammar to its end instead.
- `repetition.SetCountWeights(w0, w1, ...)` sets the odds of `min`, `min+1`, ... repetitions; `SetCountFunc(func(*ebnf.GenContext) int)` decides the count
  on every generate. A count outside min and max is an error.
- Invalid weights (wrong count, negative, all zero) and an empty alternation return an error. So does an `entity` without a generate function.
- `except` generates from its `must` pattern and tries again (100 times by default, `SetGenerateAttempts`) while the exception matches what was
  generated. The exception is checked through `ebnf.Recognizer`, which `entity`, `vector` and `alternation` implement; for any other exception
  provide `SetAcceptFunc`, otherwise `Generate` returns an error. The exception counts as matching if it matches a prefix of the generated values.

`examples/melody` generates seeded melodies with all of this: weighted bar rhythms, a weighted random walk over scale degrees whose weights depend on the previous note (kept in `ctx.State`), and a cadence.

Generation is forward only: a rule can't look ahead, so a melody can't "plan" its way to the cadence (see the example). Use state and weight functions to steer, or enumerate the endings in the grammar.

## Syntax Errors

`FarthestLog` turns the mismatch log into a syntax error: it keeps the failure that got furthest into the input and
what would have matched there.

```go
rd, _ := runes.New(strings.NewReader("(12x)"))
log := runes.NewFarthestLog(rd)
ctx := ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(log)

if matched, _, _ := ebnf.Parse(grammar, ctx); !matched {
	fmt.Println(log.Err()) // syntax error at 1:4: unexpected 'x', expected digit or ")"
}
```

Give the leaf patterns of your grammar an ID (`.Named("digit")`) or print text (`.Printed("\")\"")`), they are used
as the "expected" descriptions; leaves without either are left out. `Last()` returns the `*SyntaxError` with the
position, the expected list and the unexpected element for your own formatting. The exception of an `except` is lookahead: it is supposed to fail on every successful match, so it is matched silently and never reaches the logger.

## Rules

`rule.New(name, pattern)` gives a pattern a name and an evaluation function; it is the unit a grammar is made of. A rule can be
declared before it is defined, which is how recursion is written, so no placeholder patterns are needed:

```go
expr := rule.Declare[rune, runes.Pos]("expression").SetEval(evalBinary)
paren := rule.New("parenthesis", conc(char('('), expr, char(')')))
expr.Define(alt(number, paren))
```

A match of a rule has the rule as its pattern (`m.ID()` is the name, `m.Eval` runs the rule's function) and the components of the
pattern it wraps. Matching a rule that was declared but never defined returns an error. `SetAtomic(true)` makes syntax errors say
`expected number` instead of listing the leaves inside it, which suits token-like rules.

### Left recursion

A rule that starts with a call to itself (`expr = expr "-" number | number`) would recurse forever if it was matched by trying its
definition. Mark it, and it works out its matches by growing a seed instead: first without the recursive alternatives, then again
with what it found handed to the recursive calls, until nothing new appears.

```go
expr := rule.Declare[rune, runes.Pos]("expr").SetLeftRecursive(true)
sub := rule.New("sub", conc(expr, char('-'), number))
expr.Define(alt(sub, number))
// "1-2-3" matches as ((1-2)-3), the tree is left associative
```

Indirect recursion (`a = b "x"`, `b = a "y"`) works if one rule in the cycle is marked. A marked rule works out *all* its matches
(longest first) before it offers the first, so mark only the rules that need it. The work is linear in the number of matches, and
what a marked rule found at a position is remembered for the rest of the parse, so an expression inside the parentheses of an
expression costs no more than parsing the text once. A rule that is not marked and is entered again at the same position returns
`rule.ErrLeftRecursion` (naming the rule) and does not overflow the stack.

## EBNF Printing

Exbana can output your defined patterns as EBNF strings. Named patterns print as their name inside other patterns, and leaves print as the text you gave them with `.Printed`:

```go
rules, _ := ebnf.PrintRules([]ebnf.Pattern[rune, runes.Pos]{integer})
fmt.Print(rules)
// Output: integer = ("-"?, ("0" | (digit1to9, digit*)))
```

## Examples

- `examples/expr`: an expression calculator (precedence, functions, variables, syntax errors) in three modes: backtracking, memoized and committed. Also shows concurrent use of one grammar.
- `examples/tokens`: a calculator in two stages: a lexer produces a `[]Token` and the grammar parses it through a slice reader, with left recursive rules for left associative operators and syntax errors that point back into the text.
- `examples/json`, `examples/xml`: parsers with evaluation functions that build values.
- `examples/golang`: Go literal tokens, scanning and seeded generation.
- `examples/melody`: seeded melody generation with weights and state.
