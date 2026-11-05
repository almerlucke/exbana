# Covenant

A small language meant to be read by people and by agents alike, and to be embedded in Go programs as a scripting language.
So far there is a lexer (`token/`), a grammar over its tokens (`grammar/`), a parser generated from the grammar (`parser/`,
regenerate it with `go generate` here), `covenant.Parse`, the program as plain Go values (`ast/`), a type and effect checker
(`check/`), an interpreter for checked programs that walks the tree or runs bytecode (`run/`), a command (`cmd/covenant`), and
this note, which records the
decisions the grammar does not show.

## Syntax the parser needed

Every choice in the grammar is made by looking at the next token, which the optimizer proves (the tests fail if a change
breaks it), so a parser could be generated. That is why the syntax is the way it is:

- **Values of structs are made like calls with named arguments:** `Point(x: 1.0, y: 2.0)`, `Shape::Circle(radius: 2.0)`.
  Braces behind a name would be read as the block in `if ready {`.
- **A name directly followed by a colon is one token** (`x: Int`, `radius: 2.0`), so a space in front of the colon, `x : Int`,
  is an error. In a map, `{x: 1}` has the variable `x` as its key.
- **No trailing commas.** A list may go on over several lines, and the last item has no comma.
- **Effects are separated with `+`,** not commas: `with fs::read + net::fetch`. A comma would end the parameter in
  `fn f(g: Fn(Int) with fs::read, n: Int)`.
- **A closure is a whole expression,** and its body goes on as far as it can: `fn(x) => x + 1` is a closure of `x + 1`. To call
  a closure, or to use it as an operand, it goes between parentheses.
- **A line end is a token.** The lexer drops blank and comment lines, so there is at most one in a row, and the last line always
  ends.
- **An assignment starts like an expression** (`x = 1`, `self.count = 2`), so the grammar accepts any expression on the left;
  the checker says if it can be assigned to. The same goes for the two types after `impl`: the first is the trait if there is a
  `for`.

## Numbers

| Type | Meaning |
|---|---|
| `Int` | 64-bit signed integer: the integer to use |
| `Float` | 64-bit IEEE 754 double: the float to use |
| `Int8` `Int16` `Int32` `UInt8` `UInt16` `UInt32` `UInt64` `Float32` | for binary formats and for talking to Go; `Byte` is `UInt8` |

- There are no implicit conversions: `1 + 2.5` is an error, and so is `Int32` plus `Int`.
- Widening can not fail (`byte.to_int()`). Narrowing can: `Int32::from(n)` is a `Result<Int32, RangeError>`.
- A literal is checked against its type at compile time: `let b: UInt8 = 256` does not compile.
- `Float` is not a key of a `Map` or a member of a `Set`, because NaN is not equal to itself.
- `Int / Int` truncates towards zero.

A number is written one way: `0`, `42`, `1_000_000`, `0xff`, `0b1010_0101`, `1.5`, `1e9`, `2.5e-3`. An underscore sits between
two digits, there are no leading zeros, and hex digits and the `e` are lower case. The formatter has nothing to choose.

## Faults

A fault is a bug in the program, and not something a caller is expected to handle. It ends the current call into the script.

- **What faults:**
  - integer overflow, in `+`, `-`, `*` and negation;
  - division or remainder by zero;
  - a `requires` clause that is false when the function is called: the caller is to blame, and the fault names the call;
  - a call stack that is too deep.
- **What does not fault:** everything that is expected to happen. A missing key or a position outside a list is an `Option`
  (`get`), a number that does not parse or does not fit is a `Result`, and a failing file or network is a `Result`.
- **No catching.** A script can not catch a fault, or look at one. There is no `try` for it, because `try` is for errors that
  are values.
- **What the host gets.** `Call` returns a Go error, a `*Fault`, with the kind, a message, the source span, and the stack of
  calls. The same goes for a `requires` that failed, so the message says which clause and with which values.
- **Not a fault:** a call that runs out of steps or memory, or is cancelled through its `context.Context`. It ends in the same
  way, but the host gets a `*Limit`: nothing is wrong with the program.
- **Wrapping, checking and saturating** are asked for by name: `wrapping_add`, `checked_add` (an `Option`) and
  `saturating_add`, and the same for the other operations.
- **Same inputs, same fault.** Nothing in a fault depends on the machine, so a fault can be reproduced.
- **State.** Values never change, so a fault leaves no half-changed value in the script. What the program already did through
  its effects (a file it wrote) is not undone.
- **Not in the signature.** Any function can fault, so faults are not effects. `requires` is how a function says when it
  will not. A host function that panics is turned into a fault.

## Effects

What a function may do is in its signature, between the parameters and the result: `fn save(path: Path) with fs::write ->
Result<Unit, IoError>`. A set of effects can be given a name, `effects Storage = fs::read, fs::write`, and is used like an
effect. A group is only a name, it does not grant anything.

## What the checker does

`check.Check(config, modules)` takes the modules of a program by name, as source, and gives diagnostics: a line and column, a
code that says what kind of problem it is, and a message. The prelude (`check/prelude.cov`) is part of every program: `Option`,
`Result`, `ParseError`, `range`, and the methods of `Int`, `Float`, `Bool`, `Text`, `List`, `Map` and `Set`. The host can add
modules (`Config.Native`) with signatures and no bodies: a trait named `Functions` is the functions of the module, and a trait
named like a type adds methods to it. Every effect in a signature has to be one the host lists (`Config.Effects`), one a native
module's signatures mention, or a group.

**Types.** Signatures are always written out, and what is inside a function is worked out from them, left to right: a closure
gets the types of its parameters from where it is passed. There are no implicit conversions (`Int` and `Float` are not mixed,
and the message says `.to_float()`), no null, and a name whose type can not be worked out is an error (`let a = []` with nothing
ever put in it).

**Effects.** A call has the effects of the function it calls, and the function making the call has to list them (the message
says what the list would be). A closure has the effects of what it calls, and that is part of its type: `Fn(Int) with fs::read
-> Int`. A function can be generic in its effects, `fn map<U, E>(self, f: Fn(T) with E -> U) with E -> List<U>`: the effects of
the closure that is passed are the `E`, and the call has them. That is how `items.map(fn(x) => read(x))` is a call with the
effect `fs::read`, and `items.map(fn(x) => x + 1)` is a call with none. A name in a `with` that is also a generic parameter of the
function is an effect parameter. Promises (`requires`, `example`) can have no effects at all. An impl of a trait can do less
than the trait allows and not more.

**Control flow.** A `match` has to cover every case, with a method of saying which is left out (`Some(None)`), and so does a `let`
that takes a value apart. A function that gives something has to return on every path. The `Result` of a call can not be dropped
as a statement: use `try`, `match` on it, or `let _ =`. `try` needs the function to give a `Result` with the same error (or an
`Option`); an error of another type is converted explicitly, `.map_error(...)`.

**Names.** A `var` can be assigned; a `let`, a parameter and a field can not (a changed copy is `x with { f: v }`). A closure
captures by value, so it can not assign a `var` of the function around it. A name in a pattern that starts with a capital letter
has to be a variant (a typo does not become a new variable); a lower case name is a new variable. `if` is a statement: it gives
nothing, and a name can not be made of it. A module is used by name (`use geometry`, then `geometry::origin()`), or an item of it
(`use geometry::Point`); only `pub` things can be used from another module, and an inherent method has to be `pub` to be called
from another module. Modules can not use each other in a circle.

**Traits.** A trait is a set of signatures. `impl Trait for Type` has to have all of them, with the same types and with at most
the effects the trait gives (the check names what differs). A generic function can ask for a trait of a type parameter,
`fn total<T: Area>(...)`, and the call checks it; inside, the methods of the trait are the methods of `T`. In `impl Pair<A, B>`
the names `A` and `B` that are not types are parameters of the impl.

**Not checked yet.**
- The sized number types (`Int32`, `UInt8`, ...) and the range of literals: there are `Int` and `Float`.
- Traits with parameters (the checker says so), and traits as types: a trait is only a bound.
- Whether an `example` is true (it is only checked to be a `Bool` that does nothing), and whether `requires` can hold.
- Faults (overflow, division by zero): they happen when the program runs.
- Unused names, unreachable code, a name that shadows another.
- An `impl` for a type of another module (there is no rule on who may write it yet), and two impls of one trait that overlap.

## Running a program

`run.New(result, config)` makes a program of what the checker accepted (one with problems in it is refused), and
`program.Call(ctx, "app", "main", args...)` runs a function. The interpreter walks the tree; it uses what the checker found out
(which function a name stands for, which variant a pattern is for) and nothing else, so it can not run what was not checked.

- **Values** are Go values: `int64`, `float64`, `string`, `bool`, `run.Unit`, `*run.List`, `*run.Map`, `*run.Set`, `*run.Struct`,
  `*run.Enum` (`Option` and `Result` are enums) and `*run.Func`. They never change. A new list, map or set that is made by adding
  to the latest version of another shares its storage, so building one in a loop is not quadratic; making one from an older version
  copies. `run.Format` writes a value the way a program would.
- **Faults** are the promises of the language: `Int` overflow (including `-` of the smallest and `abs` of it), division and
  remainder by zero, a `requires` that is false (the message says which, and for which call), calls nested more than
  `MaxDepth` deep, and a `Float` that does not fit an `Int`. The call ends with a `*run.Fault` that has the kind, the place in the
  source and the functions that were running. A program can not catch one.
- **Limits** are not faults: more than `MaxSteps` steps, or a cancelled or expired context, end the call with a `*run.Limit`.
- **Effects** have to be granted: `Config.Grant` lists them, and a function whose signature has an effect that is not granted is
  not called (`*run.Denied`). The checker has proven that nothing the function calls needs more. The functions of a native module
  are `Config.Host["module::name"]`, and `New` refuses a program for a module whose functions the host does not give.
- **Examples** are run by `program.Examples`: each is a Bool that has to be true, and none of them needs the host, since an
  example can have no effects. An example does not see the parameters of its function; it is a fact about the function and calls
  it.
- A closure takes the values of the names it sees when it is made. A `var` that changes later is not changed in the closure.
- The order of a map and of a set is the order things were added, so a program that goes over one gives the same result every
  time.

`go run ./examples/covenant/cmd/covenant check|test|run files...` does this from the command line. Every file is a module, and a
program can print with `io::print`.

**Not there yet.** Memory is not limited, only steps and depth; the standard library is what `check/prelude.cov` lists; and
`Map.set` of a key that is already there, `Map.remove` and `List.concat` copy.

## Bytecode

`run.Config{Bytecode: true}` (`-bytecode` in the command) runs the program as bytecode on a machine with a stack, and not by
walking the tree. It does what the tree does: the same values, the same faults down to the column and the stack, the same
limits, grants and host functions, and the same library. The tests run every program both ways, and compare the faults field
by field.

- **The compiler** (`run/compile.go`) turns each function and each closure into a *proto*: instructions, the values they refer
  to, and the protos of the closures made in it. It does that the first time a function is needed, together with the functions it
  calls, and keeps it for the program. `covenant dis files... module::function` (or `Program.Disassemble`) writes it out.
- **Names are numbers.** A parameter, a let, a var and every name a pattern binds have a local variable of their own, which is a
  place on the stack. A closure captures by value, which is all the language promises, so it carries an array of the values it
  uses, made when it is made; there are no cells to share and nothing to close over.
- **Calls** to a known function are an instruction that names the function; a method is found from the value it is called on,
  as for the tree; a function value is called through the stack. Calls do not use the stack of Go, so they nest as deep as
  `MaxDepth` allows (100,000 calls deep is fine); a library function that calls a closure, like `map`, does.
- **A step is an instruction** here, and an expression or a statement for the tree, so `MaxSteps` means about three times as much
  work in bytecode.
- **How much faster.** On this machine, `fib(22)` takes 30 ms by the tree and 2.5 ms by the bytecode (12 times), a loop of
  200,000 additions 68 ms and 22 ms (3 times), and `range(0, 100000).map(...).filter(...).fold(...)` 67 ms and 19 ms (3.5
  times). What is left in the loops is the library and the boxing of `Int`s in Go values, which both ways pay.
- **Not done.** No optimisation: there is no folding of constants, no specialised instructions for `Int` and `Float` beyond a fast
  path for `Int` addition, subtraction and comparison, and no inlining.
