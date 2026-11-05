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

## Describing grammars

A pattern built with `entity.New(func)` is a function, and nobody but the function knows what it accepts. For a tool that wants to
analyse a grammar or compile it into something faster that is a closed box. The declarative leaves say what they are:

```go
letter := entity.In[rune, runes.Pos](entity.Range[rune]{Lo: 'a', Hi: 'z'}, entity.Range[rune]{Lo: '_', Hi: '_'})
digit := entity.Between[rune, runes.Pos]('0', '9')
notQuote := entity.NotIn[rune, runes.Pos](entity.Range[rune]{Lo: '"', Hi: '"'})
let := vector.Text[runes.Pos]("let") // or vector.Literal(values...) for other element types
```

They match exactly like the closure versions. `entity.OneOf`, `entity.Equals` and `entity.Any` complete the set, and `Equals` and
`Text` can generate their value without a generate function.

`ebnf.Analyze(pattern)` turns a grammar into a `Grammar`: a tree of nodes and a table of rules, where a use of a rule is a reference
by name, so recursive grammars are finite. Every pattern of the library describes itself through its `Shape` (kind, parts, min and
max, possessive, orthogonal, class ranges, literal values), and a pattern that does not, like one written from scratch, is a node of
kind `KindOpaque` that can only be called. `Analyze` reports rules that were never defined, two different rules with the same name,
and recursion that does not go through a named rule. `Grammar.String()` prints it as EBNF:

```
declaration = ("let", identifier, ("=", integer)?)
identifier = ([a-z_], ([a-z_] | [0-9])*)
integer = [0-9]++
```

This is the base for tools that read a grammar: an analysis that finds where a repetition can safely be possessive, or a generator that
writes a parser as Go source. The tests check that the described tree means the same as the grammar by interpreting it with a
second, independent implementation, over thousands of random grammars.

## Making a grammar fast

A grammar made of patterns backtracks: a repetition gives back what it matched when what follows fails, and an alternation tries
its other branches. That is what makes it easy to write, and most of it is never needed. `optimize` finds the places where it
provably is not, and turns them into the faster possessive repetition and orthogonal alternation:

```go
result, err := optimize.Optimize(grammar, optimize.Options{})
fmt.Print(result)
```

```
5 can be made possessive or orthogonal, 2 can not
  safe possessive: [0-9]+: the item starts with '0' to '9', what follows never does
  safe orthogonal: ("+" | "-"): the branches start with different elements, so at most one can match
  not safe possessive: "a"*: the repeated item and what follows it can both start with 'a'
```

A repetition can stop giving back when its item can not start with anything that may follow it. An alternation can commit to the
first branch that matches when its branches start with different elements and none of them can match nothing. The result
matches exactly what the original did, in any context: the tests optimize random grammars and require the same answer alone and
in front of other random grammars, and a fuzz target keeps looking. The report says why every other place was left alone, and
a pattern that is used in several places is only changed if it is safe in all of them.

What you give the analysis decides what it can do:

- **Declarative leaves** (`entity.Between`, `In`, `Equals`, `vector.Text` and the like) say which elements they accept. A leaf made from
  a function does not, so it counts as "could be anything" and keeps the places around it as they are; the report says so.
- **Use `Options{Closed: true}`** if the grammar is the whole input, or end it with an `end` pattern. Otherwise it may be used inside
  anything, and a repetition at its very end can not be changed.
- **Put whitespace after tokens**, not before. With `ws "[" ...` and `ws digits` two alternatives both start with whitespace and the
  choice can not be made by looking at the next element; with `"[" ws ...` and `digits ws` it can. The report names the overlap.
- The elements have to be integers (runes, bytes, integer tokens).

On a JSON grammar written the natural way, with 29 repetitions and alternations all found safe, parsing 2,000 objects takes 122 ms
instead of 208 ms with 21% fewer allocations. That is what removing the backtracking is worth; a faster engine would need to stop
building a tree of objects, which is a different project (see `ebnf.Analyze`, which is the first step towards it).

## Generating a parser

A grammar of patterns is flexible and slow: it builds a tree of objects and backtracks. When the grammar is done, `codegen`
writes the parser as Go source, with none of that. The grammar stays the specification and the test; the generated parser
does not use this library at all.

```go
// gen/main.go, run by //go:generate
g := grammar.JSON()
optimize.Optimize(g, optimize.Options{})          // take out the backtracking wherever it is provably safe
src, err := codegen.Generate(g, codegen.Config[rune, runes.Pos]{Package: "parser", Name: "JSON", UTF8: true})
os.WriteFile("parser/parser.go", src, 0o644)
```

```go
tree, err := parser.NewJSONParser().Parse(input)           // a string; err is a *parser.JSONSyntaxError
object, _ := tree.FirstObject(0)                           // the first node of a rule below a node
for member := range tree.FindMember(object) {              // the nodes of a rule below a node
	key, _ := tree.FirstString(member)
	begin, _ := tree.Span(key)
	line, col := tree.Position(begin)                      // counted in characters
	fmt.Println(tree.Text(key), line, col)                 // "name" 1 2
}
fmt.Print(tree.Dump())                                      // the whole tree, indented, for looking at
```

`examples/jsongen` and `examples/calcgen` are the whole thing: the grammar, the generator, the generated parser, and tests that fail if the
generated file is out of date and that fuzz the generated parser against the grammar it was made from.

**What you get.** A `Parser` that reuses its buffers, and a flat tree of the rules that matched: a node is followed by its children, so
walking it needs no pointers. The tree has `Walk`, `Children`, `Find` and `First` (and a `Find` and `First` for each rule by name),
`Span`, `Text`, `Position` (line and column, counted in characters) and `Dump`. Errors are a `SyntaxError` with the same message as
`FarthestLog`: `syntax error at 1:4: unexpected '2', expected "," or "]"` (a leaf without a name or print text is left out of the expected
list, as it is with `FarthestLog`: give it one with `.Printed`). Evaluation is yours: walk the tree and build what you need.

**A typed AST.** With `Config.AST` the parser also has a type for every rule, with a method for each rule that can match inside it, made from how
often the grammar lets it: a rule that matches exactly once is `member.Value()`, one that may be missing is `value.Number()` (returning `ok`), any other
number is `object.Members()`, an `iter.Seq`. A rule that is a choice has `Kind()`. Nothing is copied; a view is a tree and a node number.
When a rule matches more than once inside another (`pair = value ":" value`) `Config.Positions` names them by where they are: `pair.Key()` and `pair.Val()`.

```go
tree, _ := parser.NewJSONParser().Parse(input)
if object, ok := tree.Top().Value().Object(); ok {          // Value() is the one value of the document
	for member := range object.Members() {
		fmt.Println(member.String().Text(), member.Value().Text())
	}
}
```

`examples/jsongen/decode.go` turns a document into Go values this way, in a page of code, and the tests compare it with `encoding/json`.

**Fast.** On a 200 KB JSON document the generated parser takes about 2 ms (95 MB/s, two allocations); the grammar as written takes 208 ms and
the optimized grammar 122 ms. For scale, the standard library needs 4.5 ms to unmarshal the same document.

**Text without a conversion.** With `UTF8: true` the parser reads a string (`ParseBytes` takes bytes), decodes characters as it goes, and
`Text` is a slice of the input. Starting from a string that is 1.6 times faster than converting it to runes first, and it takes 185 KB instead of
6.4 MB for this document. Positions are byte offsets in the tree; lines and columns in errors count characters.

**Left recursion.** A rule written the way it reads, `expr = expr addop term | term` (mark it with `SetLeftRecursive`), is generated as a loop: the
base, then the tails for as long as they match. It is one node with the terms and the operators as its children, in order, which is the
order to evaluate them in, and a chain of ten thousand terms is a loop and not ten thousand calls. The optimizer proves that this is what the rule
matches when asked for its longest match: one base branch and one tail can match at each step, and what a tail starts with cannot also start
what follows the rule. Anything else, left recursion through other rules included, is refused with the reason.

**Error recovery.** `Config.Recover` makes a rule survive a syntax error: `ParseAll` notes the error, skips to where the grammar says to go on
(a `;`, a `}`), and carries on, returning the tree and every error in the document. A rule recovers when it fails after it has matched
something; failing at the first element is how a repetition of it ends, not an error in it. `Parse` is unchanged and stops at the first error.
Garbage *between* statements is `Config.Skip`: for a rule that is the item of a repetition (`{ statement }`), `ParseAll` notes the error when the
rule cannot start where the repetition does not end, skips to the next place the rule matches, and goes on. `Stop` says where else the
repetition ends (the `}` of a block; the end of the input always ends it) and `Until` where to resume if not "where the rule matches".

```go
Skip: map[string]codegen.Skip[rune, runes.Pos]{"statement": {Stop: text("}")}},
Recover: map[string]codegen.Recovery[rune, runes.Pos]{"statement": {Until: text(";"), Consume: true}},
```

**What it needs from the grammar.** The generated parser never backtracks, so every repetition has to be possessive and every alternation
orthogonal, because you said so or because `optimize` proved it. A grammar that still backtracks is refused, and the error lists the
places (a place that needs it can be rewritten, or accepted with `Config.Commit`, which means "first match wins"). Leaves have to be
declarative (`entity.Between`, `In`, `Equals`, `vector.Text` and the like) so the generator can see what they accept. Every problem is reported at once.

**Other element types.** (`examples/argsgen` parses command line arguments this way.) The generated parser reads a slice of any element type, with an int as the position; the position type of the grammar does
not matter, and nothing of the library is needed at run time. Runes, bytes, integers, strings and floats work out of the box. For another type, like a
struct for a token, set `Config.ElemType` and give `Config.Class` the Go expression for each leaf that is a function (`x.Kind == KindPlus`). The
`optimize` analysis works on integer elements, so for other types you mark the possessive and orthogonal places yourself or set `Commit`.

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
- `examples/jsongen`: JSON as a grammar, optimized and turned into a generated parser that reads UTF-8 text, with the generated file checked in and kept honest by tests and a fuzz target.
- `examples/calcgen`: arithmetic with left recursive rules, generated as loops, evaluated by walking the tree, and fuzzed against the grammar as it was written.
- `examples/argsgen`: the command line of a small package manager, as a grammar over the arguments (strings, no lexer and no text) with typed views, errors that name the argument, and a leaf that is a function, which shows what the optimizer can and cannot prove about those.
- `examples/json`, `examples/xml`: parsers with evaluation functions that build values.
- `examples/golang`: Go literal tokens, scanning and seeded generation.
- `examples/melody`: seeded melody generation with weights and state.
