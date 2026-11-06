# The Covenant language

Covenant is a small language for programs that people and agents both read, write and check. It is statically typed, it says in
a function's signature what the function may do, and it has one way to write most things down.

Every example in this document is checked: the tests type-check each program, and run the `example` clauses in it (see
[Promises](#promises-requires-and-example)) with both ways of running a program. When a program here gives a result, it is
because an `example` says so, and the example is true.

- [Getting started](#getting-started)
- [The shape of a program](#the-shape-of-a-program)
- [Values and types](#values-and-types)
- [Names: let and var](#names-let-and-var)
- [Control flow](#control-flow)
- [Functions](#functions)
- [Lists, maps and sets](#lists-maps-and-sets)
- [Option, Result and try](#option-result-and-try)
- [Structs](#structs)
- [Enums and match](#enums-and-match)
- [Closures and function types](#closures-and-function-types)
- [Traits and generics](#traits-and-generics)
- [Trait objects: dyn](#trait-objects-dyn)
- [Effects](#effects)
- [Promises: requires and example](#promises-requires-and-example)
- [Faults and limits](#faults-and-limits)
- [Modules](#modules)
- [Data and JSON](#data-and-json)
- [The standard library](#the-standard-library)
- [Programs: main and permissions](#programs-main-and-permissions)
- [Tools for agents](#tools-for-agents)
- [The prelude](#the-prelude)
- [Running programs from Go](#running-programs-from-go)
- [What Covenant does not have](#what-covenant-does-not-have)

## Getting started

A program is a directory of `.cov` files, or a `.cov` file on its own. A directory is a module (see [Modules](#modules)), and so
is a file on its own, named by the file without the ending.

```covenant
use io

fn main() with io::print {
    io::print("hello, covenant")
}
```

Save it as `hello.cov` and run the function `main` of the module `hello`:

```text
$ go run ./examples/covenant/cmd/covenant run hello.cov hello::main
hello, covenant
```

The program is a directory (`covenant run myapp/ args...`), or the `.cov` files at the start (`covenant run app.cov args...`), and
`-lib DIR` adds the modules of another directory to it. The command has these ways to look at a program and to run it:

| Command | What it does |
|---|---|
| `covenant check files...` | checks the types and the effects, and says what is wrong |
| `covenant test files...` | checks, and then runs every `example` of every function |
| `covenant run program args...` | runs `main` of the program (a directory, files, or `name=directory`), with the arguments |
| `covenant run program module::function args...` | calls another function; the arguments are numbers, `true`, `false`, or text |
| `covenant describe files...` | writes the public functions as JSON, for an agent to read |
| `covenant call files... module::function '{"a": 1}'` | calls a public function with arguments in JSON, and writes JSON |
| `covenant dis files... module::function` | writes the bytecode of a function |
| `covenant fmt files...` | writes the source the one way it is written (see [Formatting](#formatting)) |

`-bytecode` runs the program as bytecode and not by walking the tree (it gives the same, faster), and `-max-steps n` and
`-timeout d` end a call that goes on too long.

A program does nothing to the world unless it is given the permission to: it can print, and that is all. What more it can do is
[in its signatures](#effects), and what it gets is in flags such as `-allow-read=DIR`; see
[Programs: main and permissions](#programs-main-and-permissions).

## The shape of a program

Covenant has no semicolons and no automatic insertion of them: **a line ends a statement**, and nothing else does. A line can
be broken where the grammar says so: after an opening bracket, after a comma, after a binary operator, and before a closing
bracket. A list has no comma after its last item.

```covenant
fn both_big(a: Int, b: Int) -> Bool {
    example both_big(2, 3)
    example not both_big(1, 3)
    let ok = a > 1 and
        b > 2
    let items = [
        a,
        b
    ]
    return ok and items.length() == 2
}
```

Comments start with `//` and go to the end of the line. Blocks are in braces. By convention names of functions and variables
are written `like_this`, and names of types, traits, variants and effect groups `LikeThis`; a capital letter matters in one
place only, a [pattern](#enums-and-match).

A name is a letter or an underscore, and then letters, digits and underscores. A name followed directly by a colon is one token:
`radius: 2.0`, `n: Int`. That is why `x : Int`, with a space before the colon, is an error.

These words are keywords, and can not be names:

`and` `as` `const` `dyn` `effects` `else` `enum` `example` `false` `fn` `for` `if` `impl` `in` `let` `match` `not` `or` `pub`
`requires` `return` `self` `struct` `trait` `true` `try` `type` `use` `var` `while` `with` `Fn`

## Values and types

| Type | Is | Written |
|---|---|---|
| `Int` | a 64-bit signed integer | `0`, `42`, `1_000_000`, `0xff`, `0b1010_0101` |
| `Float` | a 64-bit IEEE 754 number | `1.5`, `1e9`, `2.5e-3` |
| `Text` | text | `"hello"`, with the escapes `\"` `\\` `\n` `\t` `\r` |
| `Bool` | true or false | `true`, `false` |
| `Unit` | nothing: what a function gives that gives nothing | `Unit` |

A number is written one way: an underscore sits between two digits, there are no leading zeros, and the letters in a number are
lower case. There is no `null`.

There are **no implicit conversions**. `Int` and `Float` are not mixed, and the checker says what to write instead.

```covenant
fn numbers() -> List<Int> {
    example numbers() == [255, 10, 1000000, 3, -7]
    return [0xff, 0b1010, 1_000_000, 7 % 4, -7]
}

fn floats() -> List<Float> {
    example floats() == [1.5, 1000.0, 0.25, 3.5]
    return [1.5, 1e3, 2.5e-1, 7.0 / 2.0]
}

fn convert(n: Int, x: Float) -> Float {
    example convert(2, 0.5) == 2.5
    return n.to_float() + x
}

fn round_down(x: Float) -> Int {
    example round_down(2.7) == 2
    example round_down(-2.5) == -3
    return x.floor()
}
```

```covenant-error
// error: use .to_float()
fn wrong(n: Int, x: Float) -> Float {
    return n + x
}
```

**Operators**, from the loosest to the tightest:

| Operators | For |
|---|---|
| `or`, `and` | `Bool`; one of them in a row: `a and b or c` has to be written with parentheses |
| `not` | `Bool` |
| `==` `!=` `<` `<=` `>` `>=` | `==` and `!=` on any two values of one type (but not functions); the others on `Int`, `Float` and `Text`; they do not chain |
| `+` `-` | `Int`, `Float`; `+` also joins `Text` |
| `*` `/` `%` | `Int`, `Float` (`%` only `Int`); `Int / Int` cuts off towards zero |
| `-`, `try` | before a value |

An `Int` that overflows, and a division by zero, are [faults](#faults-and-limits), and not wrapped around.

Text has methods:

```covenant
fn shout(name: Text) -> Text {
    example shout("  covenant ") == "COVENANT!"
    return name.trim().upper() + "!"
}

fn words(text: Text) -> List<Text> {
    example words("a,b,c") == ["a", "b", "c"]
    return text.split(",")
}

fn long_enough(text: Text) -> Bool {
    example long_enough("héllo")
    example not long_enough("abc")
    return text.length() > 4 and text.contains("l")
}
```

`length` counts characters. The whole list of methods of every type is in [the prelude](#the-prelude).

## Names: let and var

`let` makes a name that does not change. `var` makes one that can be assigned again.

```covenant
fn counter() -> Int {
    example counter() == 3
    var n = 0
    n = n + 1
    n = n + 2
    return n
}
```

A name can be given a type, `let n: Int = 0`, and has to be when its type can not be worked out: `let items = []` is an error
unless something puts an item in it.

```covenant-error
// error: declare it with var
fn f() {
    let a = 1
    a = 2
}
```

A parameter is like a `let`. A field is not assigned at all: a changed copy is made with [`with`](#structs). A name can be made
again in a block inside, and not twice in the same block:

```covenant
fn shadow() -> Int {
    example shadow() == 1
    let x = 1
    if true {
        let x = 100
    }
    return x
}
```

## Control flow

`if`, `while` and `for` are statements. An `if` gives no value; to choose a value, use `match` (below).

```covenant
fn sign(n: Int) -> Int {
    example sign(-5) == -1
    example sign(0) == 0
    example sign(9) == 1
    if n < 0 {
        return -1
    } else if n == 0 {
        return 0
    }
    return 1
}

fn sum_to(n: Int) -> Int {
    example sum_to(4) == 10
    var total = 0
    var i = 1
    while i <= n {
        total = total + i
        i = i + 1
    }
    return total
}

fn sum_list(items: List<Int>) -> Int {
    example sum_list([1, 2, 3]) == 6
    var total = 0
    for item in items {
        total = total + item
    }
    return total
}

fn total_age(ages: Map<Text, Int>) -> Int {
    example total_age({"ann": 31, "bob": 27}) == 58
    var total = 0
    for _, age in ages {
        total = total + age
    }
    return total
}

fn pick(flag: Bool, a: Int, b: Int) -> Int {
    example pick(true, 1, 2) == 1
    example pick(false, 1, 2) == 2
    return match flag {
        true => a
        false => b
    }
}
```

A `for` goes over a `List`, a `Set` (one name) or a `Map` (two: the key and the value), in the order things were added. A
function that gives something has to return on every path through it:

```covenant-error
// error: not every way through it ends in a return
fn f(n: Int) -> Int {
    if n > 0 {
        return 1
    }
}
```

## Functions

```covenant
fn double(n: Int) -> Int {
    example double(4) == 8
    return n * 2
}

fn apply_twice(f: Fn(Int) -> Int, x: Int) -> Int {
    example apply_twice(double, 3) == 12
    return f(f(x))
}

fn factorial(n: Int) -> Int {
    requires n >= 0
    example factorial(0) == 1
    example factorial(5) == 120
    if n == 0 {
        return 1
    }
    return n * factorial(n - 1)
}

fn nothing() {
}
```

A function says the type of every parameter and of its result, and the result of a function that gives nothing is left out
(it is `Unit`). A function is a value, as in `apply_twice(double, 3)`. Anything inside a function is worked out from that, so a
signature is all a caller has to read.

## Lists, maps and sets

A list is `List<T>`, a map `Map<K, V>`, a set `Set<T>`. **They never change**: what looks like adding to one makes a new one,
and the old one is as it was.

```covenant
fn lists() -> List<Int> {
    example lists() == [1, 2, 3, 4]
    let a = [1, 2, 3]
    return a.push(4)
}

fn untouched() -> List<Int> {
    example untouched() == [3, 4]
    let a = [1, 2, 3]
    let b = a.push(4)
    return [a.length(), b.length()]
}

fn queries(items: List<Int>) -> List<Int> {
    example queries([5, 6, 7]) == [3, 5, 7]
    return [items.length(), items.first().unwrap_or(0), items.last().unwrap_or(0)]
}

fn transform(items: List<Int>) -> Int {
    example transform([1, 2, 3, 4]) == 20
    return items.map(fn(x) => x * x).filter(fn(x) => x % 2 == 0).fold(0, fn(sum, x) => sum + x)
}

fn from_range() -> List<Int> {
    example from_range() == [0, 2, 4]
    return range(0, 3).map(fn(i) => i * 2)
}
```

There is no `items[i]`: an index can be out of range, and there are no exceptions. `items.get(i)` gives an `Option`, and so does
`first()`. A list that is made by adding to the newest version of another one costs the same as appending to a slice, so
building one in a loop is not slow.

A map has its keys in the order they were added, and a key is an `Int`, a `Text`, a `Bool`, or a struct or an enum made of
those (not a `Float`):

```covenant
fn maps() -> List<Int> {
    example maps() == [2, 40, 3, 0]
    let ages = {"ann": 31, "bob": 27}
    let older = ages.set("cy", 40)
    return [ages.length(), older.get("cy").unwrap_or(0), older.length(), ages.get("cy").unwrap_or(0)]
}

fn empty_map() -> Map<Text, Int> {
    example empty_map().length() == 0
    return {:}
}

fn sets() -> List<Int> {
    example sets() == [3, 4]
    let s = Set::from([3, 1, 3, 2, 1])
    return [s.length(), s.add(9).length()]
}
```

An empty map is `{:}`, and an empty list `[]`.

## Option, Result and try

There is no null and there are no exceptions. A value that may not be there is an `Option<T>`, `Some(x)` or `None`. A call that
may fail gives a `Result<T, E>`, `Ok(x)` or `Err(e)`. Both are enums, and are taken apart with [`match`](#enums-and-match) or with
their methods.

`try` unwraps a `Result` or an `Option`, and **ends the function** with the error or the `None` if there is one. It is how a
function can leave early in the middle of an expression, and it is always written.

```covenant
fn port(text: Text) -> Result<Int, Text> {
    example port("8080") == Ok(8080)
    example port("x") == Err("not a number")
    example port("70000") == Err("out of range")
    let n = try text.to_int().map_error(fn(e) => "not a number")
    if n < 1 or n > 65535 {
        return Err("out of range")
    }
    return Ok(n)
}

fn first_plus_one(items: List<Int>) -> Option<Int> {
    example first_plus_one([4, 5]) == Some(5)
    example first_plus_one([]) == None
    let first = try items.first()
    return Some(first + 1)
}

fn first_or(items: List<Int>) -> Int {
    example first_or([]) == -1
    example first_or([9]) == 9
    return items.first().unwrap_or(-1)
}
```

The error of a `try` has to be the error the function gives; changing one into another is written, `.map_error(...)`, as in
`port`. The `Result` of a call can not be thrown away:

```covenant-error
// error: it is dropped
fn risky() -> Result<Int, Text> {
    return Ok(1)
}

fn f() {
    risky()
}
```

Use `try`, a `match`, or `let _ = risky()` to say that it does not matter.

## Structs

```covenant
struct Point { x: Float, y: Float }

impl Point {
    pub fn origin() -> Point {
        return Point(x: 0.0, y: 0.0)
    }

    pub fn moved(self, dx: Float, dy: Float) -> Point {
        example Point::origin().moved(1.0, 2.0) == Point(x: 1.0, y: 2.0)
        return self with { x: self.x + dx, y: self.y + dy }
    }

    pub fn length(self) -> Float {
        return (self.x * self.x + self.y * self.y).sqrt()
    }
}

fn demo() -> Float {
    example demo() == 5.0
    let p = Point(x: 3.0, y: 4.0)
    return p.length()
}
```

- A value of a struct is made like a call, with the fields **by label** or **by position** (not both): `Point(x: 3.0, y: 4.0)`
  or `Point(3.0, 4.0)`. Braces after a name are not used, because in `if ready {` the brace starts the block.
- `p.x` reads a field. Fields are never assigned; `p with { x: 1.0 }` is a copy of `p` with another `x`.
- `impl Point { ... }` gives a type methods. A method that takes `self` is called on a value, `p.length()`; one that does not is
  called on the type, `Point::origin()`. A method is private to its module unless it is `pub`.

## Enums and match

An enum is a type that is one of several cases, and a case can hold values.

```covenant
enum Shape {
    Circle(radius: Float)
    Rect(width: Float, height: Float)
    Empty
}

fn area(s: Shape) -> Float {
    example area(Shape::Circle(radius: 1.0)) == 3.0
    example area(Shape::Rect(2.0, 3.0)) == 6.0
    example area(Shape::Empty) == 0.0
    return match s {
        Circle(radius) => 3.0 * radius * radius
        Rect(width, height) => width * height
        Empty => 0.0
    }
}
```

A case is made with the name of the enum, `Shape::Circle(radius: 1.0)` (only `Some`, `None`, `Ok` and `Err` are written without
it). In a `match` on a `Shape` the cases are written without it. **A match has to cover every case**, and the checker names a
case that is left out:

```covenant-error
// error: Some(None) is left out
fn depth(o: Option<Option<Int>>) -> Int {
    return match o {
        Some(Some(n)) => n
        None => 0
    }
}
```

Patterns can be nested, and there are literals and `_`:

```covenant
fn depth(o: Option<Option<Int>>) -> Int {
    example depth(Some(Some(7))) == 7
    example depth(Some(None)) == 1
    example depth(None) == 0
    return match o {
        Some(Some(n)) => n
        Some(None) => 1
        None => 0
    }
}

fn word(n: Int) -> Text {
    example word(0) == "zero"
    example word(1) == "one"
    example word(7) == "many"
    return match n {
        0 => "zero"
        1 => "one"
        _ => "many"
    }
}

enum Tree {
    Leaf(value: Int)
    Node(left: Tree, right: Tree)
}

fn sum(t: Tree) -> Int {
    example sum(Tree::Node(Tree::Leaf(1), Tree::Node(Tree::Leaf(2), Tree::Leaf(3)))) == 6
    return match t {
        Leaf(value) => value
        Node(left, right) => sum(left) + sum(right)
    }
}
```

In a pattern, **a name that starts with a lower case letter is a new variable, and a name that starts with a capital has to be
a case**. So a typo in the name of a case is an error, and not a catch-all:

```covenant-error
// error: did you mean Blue?
enum Color {
    Red
    Blue
}

fn name(c: Color) -> Text {
    return match c {
        Red => "red"
        Blu => "blue"
    }
}
```

A struct can be taken apart the same way, `let Point(x, y) = p`, if the pattern covers everything.

## Closures and function types

A closure is `fn(parameters) => expression`, or `fn(parameters) { statements }` for more than one line. The types of the
parameters come from where it is passed; where they do not, write them.

```covenant
fn make_adder(base: Int) -> Fn(Int) -> Int {
    return fn(x) => x + base
}

fn closures() -> List<Int> {
    example closures() == [4, 11, 1, 2]
    let add3 = make_adder(3)
    let add10 = make_adder(10)
    var x = 1
    let snapshot = fn() => x
    x = 2
    return [add3(1), add10(1), snapshot(), x]
}

fn describe_all(items: List<Int>) -> List<Text> {
    example describe_all([1, 2]) == ["odd 1", "even 2"]
    return items.map(fn(n) -> Text {
        if n % 2 == 0 {
            return "even " + n.to_text()
        }
        return "odd " + n.to_text()
    })
}
```

The type of a function is `Fn(Int, Int) -> Int`, with the result left out for a function that gives nothing. A closure
**captures by value**: it has the values the names have when it is made, as `snapshot` shows, and it can not assign a `var` of
the function around it.

```covenant-error
// error: a closure has a copy of count
fn f() {
    var count = 0
    let bump = fn() {
        count = count + 1
    }
}
```

A closure is a whole expression: its body goes on as far as it can, so `fn(x) => x + 1` is a closure of `x + 1`. To call one on
the spot, or to add to it, it goes in parentheses: `(fn(x) => x + 1)(2)`.

## Traits and generics

A trait is a set of signatures. A type has a trait when there is an `impl` of it.

```covenant
trait Area {
    fn area(self) -> Float
}

struct Square { side: Float }
struct Circle { radius: Float }

impl Area for Square {
    fn area(self) -> Float {
        return self.side * self.side
    }
}

impl Area for Circle {
    fn area(self) -> Float {
        return 3.0 * self.radius * self.radius
    }
}

fn total_area<T: Area>(items: List<T>) -> Float {
    example total_area([Square(side: 2.0), Square(side: 3.0)]) == 13.0
    return items.fold(0.0, fn(sum, item) => sum + item.area())
}
```

`fn total_area<T: Area>` is a function for any type `T` that has `Area`; inside it the methods of `Area` can be called on a
`T`. The checker looks at every call to see that the type it is given has the trait. An `impl` has to have every method of the
trait, with the types the trait gives.

Types can have parameters too:

```covenant
struct Pair<A, B> { first: A, second: B }

impl Pair<A, B> {
    pub fn swap(self) -> Pair<B, A> {
        return Pair(first: self.second, second: self.first)
    }
}

fn swapped() -> Text {
    example swapped() == "a"
    return Pair(first: 1, second: "a").swap().first
}
```

In `impl Pair<A, B>` the names that are not types, here `A` and `B`, are parameters of the impl.

```covenant-error
// error: Int is not a type with the trait Area
trait Area {
    fn area(self) -> Float
}

fn total<T: Area>(items: List<T>) -> Int {
    return items.length()
}

fn f() -> Int {
    return total([1, 2])
}
```

## Trait objects: dyn

A generic function is for one type at a time, and a list has one type of item. To have things of different types in one list,
because they have a trait in common, they are made into `dyn Area` values. That is written where it is done, with `as`:

```covenant
trait Area {
    fn area(self) -> Float
}

struct Square { side: Float }
struct Circle { radius: Float }

impl Area for Square {
    fn area(self) -> Float {
        return self.side * self.side
    }
}

impl Area for Circle {
    fn area(self) -> Float {
        return 3.0 * self.radius * self.radius
    }
}

fn total_dyn(items: List<dyn Area>) -> Float {
    example total_dyn([Square(side: 2.0) as dyn Area, Circle(radius: 1.0) as dyn Area]) == 7.0
    return items.fold(0.0, fn(sum, item) => sum + item.area())
}
```

A `dyn Area` has the methods of `Area` and no others, and can not be taken apart with a pattern. There is no conversion that
is not written: a `Square` where a `dyn Area` is wanted is an error.

```covenant-error
// error: expected dyn Area, found Square
trait Area {
    fn area(self) -> Float
}

struct Square { side: Float }

impl Area for Square {
    fn area(self) -> Float {
        return self.side
    }
}

fn total(items: List<dyn Area>) -> Int {
    return items.length()
}

fn f() -> Int {
    return total([Square(side: 2.0)])
}
```

A value that is made into a `dyn` remembers the trait, so a type that has two traits with a method of one name calls the right
one. There is one trait to a `dyn`, and there is no way to get back to the type: if the cases are known, use an `enum`.

## Effects

**What a function may do is in its signature.** A function that only calculates has no effects. One that reads a file, asks the
time, or writes to the screen says so, between its parameters and its result:

```covenant
use clock
use fs

effects Storage = fs::read + fs::write

fn load(path: Text) with fs::read -> Result<Text, fs::IoError> {
    return fs::read(path)
}

fn stamp(path: Text) with Storage + time -> Result<Unit, fs::IoError> {
    let text = try load(path)
    return fs::write(path, text + clock::now().to_text())
}
```

The effects `fs::read`, `fs::write` and `time` are the ones of [the standard library](#the-standard-library), which has the modules
`fs` and `clock`. A group, `effects Storage = fs::read + fs::write`, is a name for a set of effects; it grants nothing, and can
be used wherever an effect can. Effects are separated by `+`.

The checker sees every call and what it needs:

- a call has the effects of the function it calls, and the function making it has to list them;
- a closure has the effects of what it calls, and that is part of its type: `Fn(Int) with time -> Int`;
- a function that takes a closure can be generic in the effects of it, `fn twice<E>(f: Fn() with E -> Int) with E -> Int`: the
  call has what the closure has, and a closure with none makes a call with none.

```covenant
use clock

fn twice<E>(f: Fn() with E -> Int) with E -> Int {
    return f() + f()
}

fn pure_use() -> Int {
    example pure_use() == 6
    return twice(fn() => 3)
}

fn effectful_use() with time -> Int {
    return twice(fn() => clock::now())
}

fn mapped(items: List<Int>) with time -> List<Int> {
    return items.map(fn(x) => x + clock::now())
}
```

```covenant-error
// error: this needs the effect time
use clock

fn f() -> Int {
    return clock::now()
}
```

```covenant-error
// error: does not allow them
use clock

fn run(f: Fn(Int) -> Int) -> Int {
    return f(1)
}

fn g() with time -> Int {
    return run(fn(x) => clock::now())
}
```

And the host decides what is granted. A function whose effects are not all granted is not called at all, with an error that
says which (see [Faults and limits](#faults-and-limits)). Because the checker has seen that nothing a function calls needs more
than its signature says, granting what the signature lists is enough: a program that is given `fs::read` can not write a file,
whatever it does.

## Promises: requires and example

A function can start with promises. They are the only things that may come before its statements.

```covenant
fn average(total: Int, count: Int) -> Int {
    requires count > 0
    example average(10, 2) == 5
    example average(7, 2) == 3
    return total / count
}
```

- `requires` is a condition on the arguments. **A false one is a fault, of the caller**, and the message says which call.
- `example` is a fact about the function: a `Bool` that has to be true. `covenant test` runs them all. An example is checked like
  the rest, and it can have no effects, so it can always be run. It does not see the parameters of the function; it calls it.

Both are in the function, next to the code, so a reader sees what a function promises and what it does for some input, and an
agent that changes it has something to run.

## Faults and limits

A **fault** is a bug in the program: something the language does not do. It ends the call into the program, and a program can not
catch one. The host gets a `*run.Fault` with the kind, the place, and the functions that were running.

| Kind | When |
|---|---|
| `overflow` | an `Int` does not fit: `+ - *`, `-` of the smallest, `abs` of the smallest |
| `division` | `/` or `%` by zero |
| `requires` | a `requires` is false when the function is called |
| `stack` | calls nest deeper than `MaxDepth` (2,000 by default) |
| `range` | a `Float` does not fit an `Int` (`floor`, `round`), a `Float` that JSON does not have, a range that is too long |

```text
$ go run ./examples/covenant/cmd/covenant run demo.cov demo::factorial 25
fault (overflow) at demo.cov:32:12: 21 * 2432902008176640000 does not fit in an Int
  in demo::factorial
  in demo::factorial
  ...
```

Anything that is expected to happen is not a fault: a missing key is an `Option`, a number that does not parse is a `Result`.

A **limit** is not a fault: a call that takes more steps than `MaxSteps`, or is cancelled through its `context.Context` or
runs past its deadline, ends with a `*run.Limit`. Nothing is wrong with the program. A call of a function whose effects are not
granted is a `*run.Denied`.

## Modules

**A directory is a module.** All the `.cov` files in it are one set of names: what one of them declares, the others can use, and
what is private is private to all of them and to nothing outside. That is how a module is split into parts that are easy to
handle. A directory in a directory is another module, named after where it is:

```text
myapp/
    main.cov          the module myapp
    greet.cov         the module myapp, too
    util/
        util.cov      the module myapp::util
```

There is no line in a file that says what module it is in, so what a file says can not be different from where it is. A `.cov` file
that is given on its own is a module of one file, named after it.

Where the files are is not always what they are called by: a project that is checked out as `myapp-main`, or a library that is kept
in a directory with another name than the one it is used by. Whoever runs the program can name them, as `name=directory`, and
nothing is in the files for it:

```text
$ covenant run app=./myapp-main args...
$ covenant run -lib mathx=./vendor/math-utils app=./myapp-main
```

The modules in a directory are under the name that it is given, so `app=./myapp-main` has `app` and `app::util`.

`use` brings in another module by its name, or one thing from it; only what is `pub` can be used from outside. A `use` is for the
whole module, not for the file it is in: two files that bring in the same thing is not a problem, and two that bring in different
things by one name is.

```covenant-modules
--- geometry types.cov
pub struct Point { x: Float, y: Float }

pub fn origin() -> Point {
    return Point(x: 0.0, y: 0.0)
}

fn square(n: Float) -> Float {
    return n * n
}
--- geometry distance.cov
pub fn distance(a: Point, b: Point) -> Float {
    example distance(origin(), Point(x: 3.0, y: 4.0)) == 5.0
    return (square(a.x - b.x) + square(a.y - b.y)).sqrt()
}
--- geometry::shapes shapes.cov
use geometry

pub fn corners() -> List<geometry::Point> {
    example corners().length() == 2
    return [geometry::origin(), geometry::Point(x: 1.0, y: 1.0)]
}
--- app app.cov
use geometry
use geometry::shapes

fn far() -> Float {
    example far() == 5.0
    return geometry::distance(geometry::origin(), geometry::Point(x: 3.0, y: 4.0))
}

fn count() -> Int {
    example count() == 2
    return shapes::corners().length()
}
```

`square` is private, and it is in `types.cov`; `distance.cov` is another file of the same module and uses it, while `app` can not:

```covenant-modules-error
// error: square is not public
--- geometry types.cov
pub fn origin() -> Int {
    return 0
}
--- geometry other.cov
fn square(n: Int) -> Int {
    return n * n
}
--- app app.cov
use geometry

fn f() -> Int {
    return geometry::square(2)
}
```

A name that is declared twice in a module is a problem even if it is in two files, and the message says where the first is. A module
that is used has to be there, and modules can not use each other in a circle. A problem is reported with the path of the file, and
the line and the column in it, so an editor can open it.

## Data and JSON

There is no `Any`. A value of a shape that is not known when the program is written, such as JSON, is a `Data`: an enum, with
the cases `Null`, `Bool`, `Int`, `Float`, `Text`, `List` and `Map`, written with the name of the enum (`Data::Int(3)`), and
taken apart with `match` like any enum.

```covenant
fn describe(d: Data) -> Text {
    example describe(Data::Int(3)) == "the number 3"
    example describe(Data::Null) == "nothing"
    example describe(Data::List([Data::Null, Data::Null])) == "a list of 2"
    return match d {
        Null => "nothing"
        Bool(value) => value.to_text()
        Int(value) => "the number " + value.to_text()
        Float(value) => "the float " + value.to_text()
        Text(value) => "the text " + value
        List(items) => "a list of " + items.length().to_text()
        Map(entries) => "a map of " + entries.length().to_text()
    }
}

fn name_of(json: Text) -> Text {
    example name_of("{\"name\": \"covenant\"}") == "covenant"
    example name_of("{\"name\": 1}") == "?"
    example name_of("not json") == "invalid"
    return match Data::from_json(json) {
        Ok(doc) => doc.get("name").map(fn(n) => n.as_text().unwrap_or("?")).unwrap_or("?")
        Err(_) => "invalid"
    }
}

fn normalized() -> Text {
    example normalized() == "{\"b\":1,\"a\":[1.0,null]}"
    let doc = Data::from_json(" { \"b\": 1, \"a\": [1.0, null] } ").unwrap_or(Data::Null)
    return doc.to_json()
}
```

- `Data::from_json(text)` gives a `Result<Data, ParseError>`: `Invalid` if the text is not JSON (or nests more than 1,000 deep),
  `OutOfRange` if a number does not fit.
- `to_json()` writes it compactly. A number written without a point and an exponent is an `Int`, and the others are `Float`s, and
  a `Float` is written as `1.0` and not `1`, so what is written is read back as the same value. The keys of an object keep the
  order they were written in.

## The standard library

A program does things in the world through the modules of the standard library, which the command and the Go package `std` provide.
Each of them is behind an effect, so a signature says which of them a function uses, and behind a **policy**, which says how far
the host lets it go. Nothing is allowed that the policy does not name.

| Module | Functions | Effect | The policy |
|---|---|---|---|
| `io` | `print(text)`, `eprint(text)`, `read_line()`, `read_all()` | `io::print`, `io::read` | printing is always allowed; reading stdin is a flag |
| `fs` | `read`, `exists`, `list`, `write`, `append`, `remove`, `make_dir`; paths are `Text` | `fs::read`, `fs::write` | directories: a file has to be under one |
| `net` | `get(url)`, `post(url, body, headers)`; a `Response` has `status`, `body`, `headers` | `net::fetch` | hosts: `example.com`, `example.com:8080`, `*.example.com` |
| `env` | `get(name)` | `env::read` | the names of the variables |
| `process` | `run(program, args)`; an `Output` has `code`, `stdout`, `stderr` | `process::run` | the programs, by name |
| `clock` | `now()` (milliseconds since 1970), `sleep(millis)` | `time` | a flag |
| `random` | `int(low, high)`, `float()` | `random` | a flag |

A function that can fail gives a `Result`, with an error that says what kind of failure it is (`fs::IoError` is `NotFound`,
`Denied` or `Failed(message)`), and **a place that the policy does not allow is `Denied`**, which a program can see and handle:

```covenant
use env
use fs
use net
use process

fn token() with env::read -> Text {
    return env::get("API_TOKEN").unwrap_or("")
}

fn download(url: Text, path: Text) with net::fetch + fs::write -> Result<Unit, Text> {
    let response = try net::get(url).map_error(fn(e) => "could not fetch " + url)
    return fs::write(path, response.body).map_error(fn(e) => "could not write " + path)
}

fn changed_files() with process::run -> Int {
    return match process::run("git", ["status", "--short"]) {
        Ok(output) => output.stdout.split("\n").length()
        Err(_) => -1
    }
}

fn describe_error(e: fs::IoError) -> Text {
    example describe_error(fs::IoError::NotFound) == "there is no such file"
    return match e {
        NotFound => "there is no such file"
        Denied => "that place is not allowed"
        Failed(message) => message
    }
}
```

What the policy holds to, besides the effect being granted:

- **Files** are looked at after links are followed, so a link inside a directory that leads out of it does not lead out, and
  neither does `..`.
- **Requests** go only to hosts that are listed, and so does a redirect; a scheme other than `http` and `https` is `Denied`; the
  loopback and private addresses are not reached unless the host says so (a program that can be sent anywhere can be made to talk
  to what is inside the network); a response is read only up to a limit, and a request ends after a time.
- **A program that is run** is one that is listed, with no shell, no environment of the caller's but the path, a time, and a limit
  on what it writes.

## Programs: main and permissions

A program that is run from the command line starts at `main`, in the first file. It may have one parameter, the arguments, and
may give nothing, an `Int` (the exit code), or a `Result` of those: an `Err` is written to the standard error, and the exit code
is 1.

```covenant
use fs
use io

fn main(args: List<Text>) with fs::read + io::print -> Int {
    let missing = args.filter(fn(path) => not fs::exists(path).unwrap_or(false))
    for path in missing {
        io::print(path + " is missing")
    }
    return missing.length()
}
```

```text
$ covenant run check.cov a.txt b.txt
check::main needs permissions that were not granted:
  fs::read       -allow-read=DIR
The signature says what it needs, and the flags say what it gets. -allow-signature grants stdin, time and random.

$ covenant run -allow-read=. check.cov a.txt b.txt
b.txt is missing
$ echo $?
1
```

**What a program can do is decided by two parties.** The program says what it needs, in the signature of `main`, and the checker
has proven that nothing it calls needs more. The person who runs it says what it gets, with flags. The program never gives
itself a permission: that is why nothing is allowed unless a flag says so, and why the message above says which flag to give.

| Flag | Gives |
|---|---|
| `-allow-read=DIR,...` `-allow-write=DIR,...` | `fs::read`, `fs::write`, for files under these directories |
| `-allow-net=HOST,...` | `net::fetch`, for these hosts |
| `-allow-env=NAME,...` | `env::read`, for these variables |
| `-allow-run=PROGRAM,...` | `process::run`, for these programs |
| `-allow-stdin` `-allow-time` `-allow-random` | `io::read`, `time`, `random` |
| `-allow-private-net` | lets `net` reach loopback and private addresses |
| `-allow-signature` | the three that have nothing to scope: stdin, time and random |
| `-allow-all` | everything, anywhere |

A host that embeds a program (see [Running programs from Go](#running-programs-from-go)) does the same with a `std.Policy`.

## Tools for agents

The public functions of a program are tools. `covenant describe` writes them as JSON that an agent can read: for each function its
name, the comment above it, a JSON Schema for the arguments and for the result, the effects it needs, and its examples.

```covenant
use fs

// Adds two numbers.
pub fn add(a: Int, b: Int) -> Int {
    example add(1, 2) == 3
    return a + b
}

// Reads a note from a file, if the host allows it.
pub fn read_note(path: Text) with fs::read -> Result<Text, fs::IoError> {
    return fs::read(path)
}
```

```json-for-previous
{
  "tools": [
    {
      "name": "doc.add",
      "module": "doc",
      "function": "add",
      "description": "Adds two numbers.",
      "inputSchema": {
        "additionalProperties": false,
        "properties": {
          "a": {
            "type": "integer"
          },
          "b": {
            "type": "integer"
          }
        },
        "required": [
          "a",
          "b"
        ],
        "type": "object"
      },
      "outputSchema": {
        "type": "integer"
      },
      "effects": [],
      "examples": [
        "add(1, 2) == 3"
      ]
    },
    {
      "name": "doc.read_note",
      "module": "doc",
      "function": "read_note",
      "description": "Reads a note from a file, if the host allows it.",
      "inputSchema": {
        "additionalProperties": false,
        "properties": {
          "path": {
            "type": "string"
          }
        },
        "required": [
          "path"
        ],
        "type": "object"
      },
      "outputSchema": {
        "$defs": {
          "fs.IoError": {
            "oneOf": [
              {
                "const": "NotFound"
              },
              {
                "const": "Denied"
              },
              {
                "additionalProperties": false,
                "properties": {
                  "Failed": {
                    "additionalProperties": false,
                    "properties": {
                      "message": {
                        "type": "string"
                      }
                    },
                    "required": [
                      "message"
                    ],
                    "type": "object"
                  }
                },
                "required": [
                  "Failed"
                ],
                "type": "object"
              }
            ]
          }
        },
        "oneOf": [
          {
            "additionalProperties": false,
            "properties": {
              "ok": {
                "type": "string"
              }
            },
            "required": [
              "ok"
            ],
            "type": "object"
          },
          {
            "additionalProperties": false,
            "properties": {
              "err": {
                "$ref": "#/$defs/fs.IoError"
              }
            },
            "required": [
              "err"
            ],
            "type": "object"
          }
        ]
      },
      "effects": [
        "fs::read"
      ]
    }
  ]
}
```

`covenant call` runs one of them, with the arguments as a JSON object by the names of the parameters (or an array), and writes the
result as JSON. It needs the permissions the function's effects ask for, like any other run:

```text
$ covenant call notes.cov notes::add '{"a": 20, "b": 22}'
42
$ covenant call notes.cov notes::read_note '{"path": "a.txt"}'
notes::read_note needs permissions that were not granted:
  fs::read       -allow-read=DIR
The signature says what it needs, and the flags say what it gets. -allow-signature grants stdin, time and random.
$ covenant call -allow-read=. notes.cov notes::read_note '{"path": "a.txt"}'
{"ok":"a note\n"}
$ covenant call -allow-read=. notes.cov notes::read_note '{"path": "/etc/hostname"}'
{"err":"Denied"}
```

The JSON for a type goes by fixed rules, the same for the schema and for the arguments and the results:

| Type | JSON |
|---|---|
| `Int`, `Float`, `Text`, `Bool`, `Unit` | a number, a number, a string, a boolean, `null` |
| `List<T>`, `Set<T>` | an array |
| `Map<Text, V>` | an object, with the keys in the order they are written; any other `Map<K, V>` is an array of pairs `[key, value]` |
| `Option<T>` | `null`, or the value |
| `Result<T, E>` | `{"ok": value}` or `{"err": error}` |
| a struct | an object with all of its fields, and no others |
| an enum | `"Name"` for a variant that holds nothing, and `{"Name": {"field": value}}` for one that does |
| `Data` | any JSON |

A function is not a tool, and `describe` says so under `skipped`, if it is generic, if a parameter or the result is a function or a
`dyn`, or if it has an `Option` of an `Option` (`Some(None)` could not be told from `None`). Only a public function is a tool: a
private one can not be called by name. A wrong argument is said with where it is: `items[1].price: expected a number, found a
string`.

## The prelude

This is what every program has. It is the file `check/prelude.cov`, which the checker reads like the source of a module the
host provides: signatures with no bodies, and a trait named like a type holds the methods of that type.

```covenant-prelude
// The prelude: what every program has. The checker reads it as the source of a module that the host provides, so there are
// signatures and no bodies: a trait named like a type holds the methods of that type, and the trait Functions holds functions.

enum Option<T> {
    Some(value: T)
    None
}

enum Result<T, E> {
    Ok(value: T)
    Err(error: E)
}

enum ParseError {
    NotANumber
    OutOfRange
    Invalid
}

// Data is a value whose shape is not known when the program is written: JSON, a configuration, what the host gives. It is taken
// apart with match, as any enum is, and the cases of it are written with the name of the enum: Data::Int(3).
enum Data {
    Null
    Bool(value: Bool)
    Int(value: Int)
    Float(value: Float)
    Text(value: Text)
    List(items: List<Data>)
    Map(entries: Map<Text, Data>)
}

trait Functions {
    fn range(from: Int, to: Int) -> List<Int>
}

trait Int {
    fn to_float(self) -> Float
    fn to_text(self) -> Text
    fn abs(self) -> Int
    fn min(self, other: Int) -> Int
    fn max(self, other: Int) -> Int
}

trait Float {
    fn to_text(self) -> Text
    fn abs(self) -> Float
    fn min(self, other: Float) -> Float
    fn max(self, other: Float) -> Float
    fn sqrt(self) -> Float
    fn floor(self) -> Int
    fn round(self) -> Int
}

trait Bool {
    fn to_text(self) -> Text
}

trait Text {
    fn length(self) -> Int
    fn is_empty(self) -> Bool
    fn to_int(self) -> Result<Int, ParseError>
    fn to_float(self) -> Result<Float, ParseError>
    fn contains(self, part: Text) -> Bool
    fn starts_with(self, part: Text) -> Bool
    fn split(self, separator: Text) -> List<Text>
    fn trim(self) -> Text
    fn upper(self) -> Text
    fn lower(self) -> Text
}

trait List<T> {
    fn length(self) -> Int
    fn is_empty(self) -> Bool
    fn get(self, index: Int) -> Option<T>
    fn first(self) -> Option<T>
    fn last(self) -> Option<T>
    fn push(self, item: T) -> List<T>
    fn concat(self, other: List<T>) -> List<T>
    fn reverse(self) -> List<T>
    fn map<U, E>(self, f: Fn(T) with E -> U) with E -> List<U>
    fn filter<E>(self, f: Fn(T) with E -> Bool) with E -> List<T>
    fn fold<A, E>(self, start: A, f: Fn(A, T) with E -> A) with E -> A
    fn any<E>(self, f: Fn(T) with E -> Bool) with E -> Bool
    fn all<E>(self, f: Fn(T) with E -> Bool) with E -> Bool
}

trait Map<K, V> {
    fn length(self) -> Int
    fn get(self, key: K) -> Option<V>
    fn has(self, key: K) -> Bool
    fn set(self, key: K, value: V) -> Map<K, V>
    fn remove(self, key: K) -> Map<K, V>
    fn keys(self) -> List<K>
    fn values(self) -> List<V>
}

trait Set<T> {
    fn from(items: List<T>) -> Set<T>
    fn length(self) -> Int
    fn has(self, item: T) -> Bool
    fn add(self, item: T) -> Set<T>
    fn remove(self, item: T) -> Set<T>
    fn to_list(self) -> List<T>
}

trait Option<T> {
    fn is_some(self) -> Bool
    fn is_none(self) -> Bool
    fn unwrap_or(self, fallback: T) -> T
    fn or_error<E>(self, error: E) -> Result<T, E>
    fn map<U, F>(self, f: Fn(T) with F -> U) with F -> Option<U>
}

trait Result<T, E> {
    fn is_ok(self) -> Bool
    fn is_err(self) -> Bool
    fn unwrap_or(self, fallback: T) -> T
    fn map_error<X, F>(self, f: Fn(E) with F -> X) with F -> Result<T, X>
    fn ok(self) -> Option<T>
}

trait Data {
    fn from_json(text: Text) -> Result<Data, ParseError>
    fn to_json(self) -> Text
    fn is_null(self) -> Bool
    fn as_bool(self) -> Option<Bool>
    fn as_int(self) -> Option<Int>
    fn as_float(self) -> Option<Float>
    fn as_text(self) -> Option<Text>
    fn as_list(self) -> Option<List<Data>>
    fn as_map(self) -> Option<Map<Text, Data>>
    fn get(self, key: Text) -> Option<Data>
    fn at(self, index: Int) -> Option<Data>
}
```

## Formatting

There is one way to write a Covenant program down, and `covenant fmt` writes it. It reads the source and writes the tree of it
again, so the layout of the lines, the spaces and the blank lines are the formatter's, and so are the parentheses: the ones an
expression needs stay, and the others go.

```covenant-format
// Adds   two numbers.
fn  add(a:Int,b:Int)->Int{
requires a>=0



    return (a+b)*(1)   // the sum
}
const  limit:Int=((100))
--- formatted
// Adds   two numbers.
fn add(a: Int, b: Int) -> Int {
    requires a >= 0

    return (a + b) * 1 // the sum
}

const limit: Int = 100
```

What it does not do is change what the program says, or lose a word of what is written in it:

- **Comments stay where they are.** A comment on a line of its own goes in front of what follows it, one at the end of a line stays
  at the end of it, and its text is not touched (but for the spaces at the end). A comment that is in the middle of an expression,
  where no line can hold it, goes in front of the line the expression is on.
- **A blank line is kept, once,** between two statements, and between two declarations that are small. Between a function, an `impl`,
  a `trait` or an `enum` and what is next to it there is always one, and there is none at the start or at the end of a block.
- **A list that is on several lines stays so,** one item to a line, and so does an expression that goes over several lines after an
  operator. One that is on one line stays so if it fits in 100 columns, and if not it is broken after the opening bracket, at the commas
  and before the closing bracket, or after the operators. A closure with a block that is the last argument of a call stays on the line
  of the call: `items.map(fn(x) {`.
- **It checks itself.** Before the formatter gives back what it wrote, it reads it again, and gives it only if the tree is the same
  and so are the comments. If it ever is not, the answer is an error that says it is the formatter's, and the file is left as it was.

The command:

```text
$ covenant fmt app.cov          the formatted source, on the standard output
$ covenant fmt -                the same for the standard input, which is what an editor wants
$ covenant fmt -w app/          rewrite the files in the directory (the .cov files below it, but not the names that start with . or _)
$ covenant fmt -l app/          list the files that are not formatted
$ covenant fmt -check app/      the same, and the exit code is 1 if there are any: for a build to ask
```

A file that is not Covenant is an error with the line and the column, and it is not written. From Go, `format.Source(src)` does the
same: it gives the formatted source, or the error.

## Running programs from Go

A program is checked, and then run:

```go
res := check.Check(check.Config{}, map[string]string{"app": source})
if !res.OK() {
	return res.Error()
}

prog, err := run.New(res, run.Config{Bytecode: true, Grant: []string{"fs::read"}})
if err != nil {
	return err
}

v, err := prog.Call(ctx, "app", "double", int64(21))
fmt.Println(run.Format(v)) // 42
```

With the standard library the host gives the modules to the checker, and the functions and the policy to the runner:

```go
policy := std.Policy{ReadRoots: []string{"/data"}, NetHosts: []string{"api.example.com"}}
host := std.New(policy)

res := check.Check(check.Config{Native: std.Natives()}, map[string]string{"app": source})
prog, err := run.New(res, run.Config{Host: host.Host(), Grant: policy.Granted(), Bytecode: true})
host.Bind(prog)

manifest, _ := tool.Describe(res)                                                    // what an agent can call
out, err := tool.Call(ctx, prog, res, "app", "search", []byte(`{"query": "covenant"}`)) // JSON in, JSON out
```

- `check.Config` says which effects the host knows, and gives the modules it provides (`Native`): source with signatures and no
  bodies, in which a trait named `Functions` is the functions of the module. `std.Natives()` is the standard library's.
- `run.Config` has the host functions that go with those modules (`Host`), the effects that are granted (`Grant`), the limits
  (`MaxSteps`, `MaxDepth`), and `Bytecode` to run bytecode.
- A call gives a `run.Value`: a Go `int64`, `float64`, `string`, `bool`, or `run.Unit`, `*run.List`, `*run.Map`, `*run.Set`,
  `*run.Struct`, `*run.Enum`. A value is written with `run.Format`. For `Data`, `program.DataFromGo` and `run.DataToGo`.
- The checker reports problems as data: a line, a column, a code that says what kind of problem it is, and the message.

## What Covenant does not have

Left out on purpose, because each of them is a way for something to happen that the signature does not say:

- no `null`, and no exceptions: a `Result` or an `Option` says it in the type;
- no implicit conversions, no operator overloading, no macros;
- no classes or inheritance: structs, enums, traits and composition;
- no `xs[i]`: `xs.get(i)` says what happens when there is no such item;
- no assigning a field, and no shared mutable state: values never change;
- no semicolons, and one way to write a number, a list and a block.

And not there yet: a limit on memory, sized integer types (`Int32`, `UInt8`), traits with parameters, a `dyn` of more than one trait, and concurrency.
