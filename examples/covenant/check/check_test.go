package check

import (
	"strings"
	"testing"
)

var testConfig = Config{
	Effects: []string{"time", "random"},
	Native: map[string]string{
		"fs": `
struct Path {}
struct IoError {}

trait Functions {
    fn read_all(path: Path) with fs::read -> Result<Text, IoError>
    fn write_all(path: Path, data: Text) with fs::write -> Result<Unit, IoError>
}
`,
		"log": `
trait Functions {
    fn write(message: Text) with log::write
}
`,
	},
}

func check(t *testing.T, src string) *Result {
	t.Helper()

	return Check(testConfig, map[string]string{"app": src})
}

var accepted = map[string]string{
	"control flow": `
fn sign(n: Int) -> Int {
    if n < 0 {
        return -1
    } else if n == 0 {
        return 0
    } else {
        return 1
    }
}

fn total(values: List<Int>) -> Int {
    var sum = 0
    for value in values {
        sum = sum + value
    }
    while sum > 100 {
        sum = sum - 100
    }
    return sum
}
`,
	"structs, enums, traits and generics": `
struct Point { x: Float, y: Float }

enum Shape {
    Circle(radius: Float)
    Rect(width: Float, height: Float)
    Empty
}

trait Area {
    fn area(self) -> Float
}

impl Area for Shape {
    fn area(self) -> Float {
        return match self {
            Circle(radius) => 3.14159 * radius * radius
            Rect(width, height) => width * height
            Empty => 0.0
        }
    }
}

impl Point {
    pub fn moved(self, dx: Float) -> Point {
        return self with { x: self.x + dx }
    }

    pub fn origin() -> Point {
        return Point(x: 0.0, y: 0.0)
    }
}

fn total_area<T: Area>(items: List<T>) -> Float {
    return items.fold(0.0, fn(sum, item) => sum + item.area())
}

fn main() -> Float {
    let shapes = [Shape::Circle(radius: 1.0), Shape::Rect(2.0, 3.0), Shape::Empty]
    let p = Point::origin().moved(1.5)
    return total_area(shapes) + p.x
}
`,
	"effects": `
use fs
use fs::Path
use fs::IoError

effects Storage = fs::read + fs::write

fn load(path: Path) with fs::read -> Result<Text, IoError> {
    let text = try fs::read_all(path)
    return Ok(text)
}

fn copy(from: Path, to: Path) with Storage -> Result<Unit, IoError> {
    let data = try fs::read_all(from)
    return fs::write_all(to, data)
}

fn reading(path: Path, n: Int) with fs::read -> Int {
    return n
}

fn apply<E>(f: Fn(Int) with E -> Int, value: Int) with E -> Int {
    return f(value)
}

fn pure() -> Int {
    return apply(fn(x) => x + 1, 2)
}

fn noisy(path: Path) with fs::read -> Int {
    return apply(fn(x) => reading(path, x), 1)
}

fn mapped(path: Path, items: List<Int>) with fs::read -> List<Int> {
    return items.map(fn(n) => reading(path, n))
}

fn does_less(path: Path) with Storage -> Int {
    return reading(path, 1)
}
`,
	"closures": `
fn make_adder(base: Int) -> Fn(Int) -> Int {
    return fn(x) => x + base
}

fn demo(items: List<Int>) -> Int {
    let double = fn(x: Int) => x * 2
    let total = items.map(double).fold(0, fn(a, b) => a + b)
    let big = items.filter(fn(i) => i > 10).length()
    let add = make_adder(3)
    let block = fn(n: Int) -> Int {
        let twice = n * 2
        return twice
    }
    return total + big + add(1) + block(2)
}
`,
	"collections": `
fn demo() -> Int {
    var total = 0
    let ages = {"ann": 31, "bob": 27}
    let older = ages.set("cy", 40)
    let nobody: Map<Text, Int> = {:}
    let unique = Set::from([1, 2, 2])
    let first = older.get("ann").unwrap_or(0)
    let names = older.keys()
    let numbers = [1, 2, 3]
    let none = []
    for key, value in older {
        total = total + value + key.length()
    }
    for i in range(0, 10) {
        total = total + i
    }
    for n in unique.to_list() {
        total = total + n
    }
    let more = none.push(1)
    return total + first + names.length() + numbers.length() + more.length() + nobody.length()
}
`,
	"generic structs and impls": `
struct Pair<A, B> { first: A, second: B }

impl Pair<A, B> {
    pub fn swap(self) -> Pair<B, A> {
        return Pair(first: self.second, second: self.first)
    }
}

fn demo() -> Text {
    let p = Pair(first: 1, second: "a")
    let q = p.swap()
    return q.first
}
`,
	"try, Option and Result": `
fn port(text: Text) -> Result<Int, Text> {
    let n = try text.to_int().map_error(fn(e) => "bad number")
    if n < 1 or n > 65535 {
        return Err("out of range")
    }
    return Ok(n)
}

fn next(items: List<Int>) -> Option<Int> {
    let x = try items.first()
    return Some(x + 1)
}

fn or_default(items: List<Int>) -> Int {
    return items.first().unwrap_or(0)
}
`,
	"promises": `
fn clamp(n: Int, lo: Int, hi: Int) -> Int {
    requires lo <= hi
    example clamp(5, 0, 3) == 3
    if n < lo {
        return lo
    }
    if n > hi {
        return hi
    }
    return n
}
`,
	"patterns": `
enum Color {
    Red
    Green
    Blue
}

enum Tree {
    Leaf(value: Int)
    Node(left: Tree, right: Tree)
}

fn name(c: Color) -> Text {
    return match c {
        Red => "red"
        Color::Green => "green"
        Blue => "blue"
    }
}

fn sum(t: Tree) -> Int {
    return match t {
        Leaf(value) => value
        Node(left, right) => sum(left) + sum(right)
    }
}

fn depth(o: Option<Option<Int>>) -> Int {
    return match o {
        Some(Some(n)) => n
        Some(None) => 1
        None => 0
    }
}

fn word(n: Int, b: Bool) -> Text {
    let first = match n {
        0 => "zero"
        1 => "one"
        _ => "many"
    }
    let second = match b {
        true => "yes"
        false => "no"
    }
    return first + second
}

fn pair(r: Result<Int, Text>) -> Int {
    return match r {
        Ok(n) => n
        Err(_) => 0
    }
}
`,
	"constants, aliases and groups": `
const limit: Int = 100
const names: List<Text> = ["a", "b"]

type Ports = List<Int>
type Table<V> = Map<Text, V>

effects Reading = fs::read
effects Everything = Reading + fs::write + time

fn first_port(ports: Ports) -> Int {
    return ports.first().unwrap_or(limit)
}

fn lookup(t: Table<Int>) -> Int {
    return t.get("a").unwrap_or(0)
}

fn clock() with Everything -> Int {
    return 1
}

fn caller() with fs::read + fs::write + time -> Int {
    return clock()
}
`,
	"assignment and shadowing": `
fn f() -> Int {
    var x = 1
    x = x + 1
    if true {
        let x = 7
    }
    return x
}
`,
	"types and values of unit": `
fn nothing() {
}

fn also() -> Unit {
    return Unit
}

fn result() -> Result<Unit, Text> {
    return Ok(Unit)
}
`,
}

func TestAccepted(t *testing.T) {
	for name, src := range accepted {
		t.Run(name, func(t *testing.T) {
			r := check(t, src)
			if !r.OK() {
				t.Fatalf("%v\n%s", r.Error(), src)
			}
		})
	}
}

type badCase struct {
	name string
	src  string
	line int
	code string
	msg  string
}

var rejected = []badCase{
	// types
	{"a wrong literal", "fn f() {\n    let a: Int = \"x\"\n}\n", 2, "type-mismatch", "expected Int, found Text"},
	{"a wrong result", "fn f() -> Int {\n    return \"x\"\n}\n", 2, "type-mismatch", "expected Int, found Text"},
	{"Int and Float", "fn f(a: Int, b: Float) -> Float {\n    return a + b\n}\n", 2, "type-mismatch", "use .to_float()"},
	{"a wrong argument", "fn g(a: Int) -> Int {\n    return a\n}\nfn f() -> Int {\n    return g(\"x\")\n}\n", 5, "type-mismatch", "expected Int, found Text"},
	{"too many arguments", "fn g(a: Int) -> Int {\n    return a\n}\nfn f() -> Int {\n    return g(1, 2)\n}\n", 5, "arity", "2 arguments are given, and 1 are expected"},
	{"a condition that is not a Bool", "fn f() {\n    if 1 {\n    }\n}\n", 2, "type-mismatch", "expected Bool, found Int"},
	{"adding a Bool", "fn f() -> Bool {\n    return true + false\n}\n", 2, "type-mismatch", "+ is for Int, Float and Text"},
	{"comparing functions", "fn f() -> Bool {\n    return f == f\n}\n", 2, "type-mismatch", "functions can not be compared"},
	{"a list of two types", "fn f() {\n    let a = [1, \"x\"]\n}\n", 2, "type-mismatch", "expected Int, found Text"},
	{"a Float as a key", "fn f() {\n    let a = {1.5: 1}\n}\n", 2, "key", "Float can not be the key"},
	{"an Option used as its value", "fn f(o: Option<Int>) -> Int {\n    return o\n}\n", 2, "type-mismatch", "may not have a value"},
	{"a missing type", "fn f(a: Nope) {\n}\n", 1, "unknown-name", "there is no type Nope"},
	{"the wrong number of type arguments", "fn f(a: List<Int, Int>) {\n}\n", 1, "arity", "List takes 1 types"},

	// names
	{"an unknown name", "fn f() -> Int {\n    let count = 1\n    return cuont\n}\n", 3, "unknown-name", "did you mean count?"},
	{"an unknown method", "fn f(a: List<Int>) -> Int {\n    return a.lenght()\n}\n", 2, "no-method", "did you mean length?"},
	{"an unknown field", "struct P { x: Int }\nfn f(p: P) -> Int {\n    return p.y\n}\n", 3, "no-field", "P has no field y"},
	{"a method used as a field", "fn f(a: List<Int>) -> Int {\n    return a.length\n}\n", 2, "method", "is a method"},
	{"an unknown variant", "enum E {\n    A\n}\nfn f() -> E {\n    return E::B\n}\n", 5, "unknown-name", "no variant or function B"},
	{"a type used as a value", "struct P { x: Int }\nfn f() {\n    let p = P\n}\n", 3, "type-as-value", "P is a type"},

	// constructors
	{"a missing field", "struct P { x: Int, y: Int }\nfn f() -> P {\n    return P(x: 1)\n}\n", 3, "label", "the field y of P is missing"},
	{"an unknown label", "struct P { x: Int }\nfn f() -> P {\n    return P(z: 1)\n}\n", 3, "label", "P has no field z"},
	{"labels and no labels", "struct P { x: Int, y: Int }\nfn f() -> P {\n    return P(x: 1, 2)\n}\n", 3, "label", "all given by label, or none are"},
	{"labels on a function", "fn g(a: Int) -> Int {\n    return a\n}\nfn f() -> Int {\n    return g(a: 1)\n}\n", 5, "label", "functions take their arguments by position"},
	{"a variant without its values", "enum E {\n    A(n: Int)\n}\nfn f() -> E {\n    return E::A\n}\n", 5, "arity", "carries 1 values"},

	// mutability
	{"assigning a let", "fn f() {\n    let a = 1\n    a = 2\n}\n", 3, "immutable", "declare it with var"},
	{"assigning a field", "struct P { x: Int }\nfn f(p: P) {\n    p.x = 2\n}\n", 3, "not-assignable", "p with { x: ... }"},
	{"assigning a parameter", "fn f(a: Int) {\n    a = 2\n}\n", 2, "immutable", "declare it with var"},
	{"a closure that assigns", "fn f() {\n    var a = 1\n    let g = fn() {\n        a = 2\n    }\n}\n", 4, "capture", "a closure has a copy of a"},
	{"assigning the wrong type", "fn f() {\n    var a = 1\n    a = \"x\"\n}\n", 3, "type-mismatch", "expected Int, found Text"},

	// effects
	{"an effect that is not allowed", "use fs\nuse fs::Path\nfn f(p: Path) {\n    let r = fs::read_all(p)\n}\n", 4, "effect", "this needs the effect fs::read"},
	{"an effect that is missing from the list", "use fs\nuse fs::Path\nfn f(p: Path) with fs::read -> Int {\n    let r = fs::write_all(p, \"x\")\n    return 1\n}\n", 4, "effect", "(it would be: with fs::read + fs::write)"},
	{"a closure with an effect for a pure parameter", "use fs\nuse fs::Path\nfn pure_apply(f: Fn(Int) -> Int) -> Int {\n    return f(1)\n}\nfn reading(p: Path, n: Int) with fs::read -> Int {\n    return n\n}\nfn f(p: Path) with fs::read -> Int {\n    return pure_apply(fn(x) => reading(p, x))\n}\n", 10, "effect", "has the effects fs::read"},
	{"an effect in a promise", "use fs\nuse fs::Path\nfn f(p: Path) -> Int {\n    requires fs::read_all(p).is_ok()\n    return 1\n}\n", 4, "effect", "a promise only looks"},
	{"an unknown effect", "fn f() with disk::read {\n}\n", 1, "unknown-effect", "disk::read is not an effect"},
	{"a group in a group in a group", "effects A = B\neffects B = A\nfn f() with A {\n}\n", 1, "cycle", "has itself in it"},
	{"effects of an impl beyond the trait", "trait T {\n    fn go(self)\n}\nstruct S {}\nimpl T for S {\n    fn go(self) with time {\n    }\n}\n", 6, "effect", "does not allow them"},
	{"a function value called without its effect", "fn g() with time -> Int {\n    return 1\n}\nfn f() -> Int {\n    let h = g\n    return h()\n}\n", 6, "effect", "this needs the effect time"},

	// control flow
	{"a missing return", "fn f(n: Int) -> Int {\n    if n > 0 {\n        return 1\n    }\n}\n", 5, "return-missing", "not every way through it ends in a return"},
	{"a return of nothing", "fn f() -> Int {\n    return\n}\n", 2, "type-mismatch", "return has no value"},
	{"a match with a case left out", "enum C {\n    A\n    B\n    D\n}\nfn f(c: C) -> Int {\n    return match c {\n        A => 1\n        B => 2\n    }\n}\n", 7, "not-exhaustive", "D is left out"},
	{"a match with a nested case left out", "fn f(o: Option<Option<Int>>) -> Int {\n    return match o {\n        Some(Some(n)) => n\n        None => 0\n    }\n}\n", 2, "not-exhaustive", "Some(None) is left out"},
	{"a match on numbers without a catch all", "fn f(n: Int) -> Int {\n    return match n {\n        0 => 1\n        1 => 2\n    }\n}\n", 2, "not-exhaustive", "_ is left out"},
	{"a match on a bool", "fn f(b: Bool) -> Int {\n    return match b {\n        true => 1\n    }\n}\n", 2, "not-exhaustive", "false is left out"},
	{"a let that can fail", "fn f(o: Option<Int>) {\n    let Some(x) = o\n}\n", 2, "not-exhaustive", "None is left out"},
	{"a mistyped variant", "enum C {\n    Red\n    Blue\n}\nfn f(c: C) -> Int {\n    return match c {\n        Red => 1\n        Blu => 2\n    }\n}\n", 8, "unknown-name", "did you mean Blue?"},
	{"arms of two types", "fn f(b: Bool) {\n    let a = match b {\n        true => 1\n        false => \"x\"\n    }\n}\n", 4, "type-mismatch", "expected Int, found Text"},
	{"a pattern of the wrong type", "fn f(n: Int) -> Int {\n    return match n {\n        \"a\" => 1\n        _ => 2\n    }\n}\n", 3, "type-mismatch", "this pattern is Text"},
	{"a pattern with too few values", "enum E {\n    A(x: Int, y: Int)\n}\nfn f(e: E) -> Int {\n    return match e {\n        A(x) => x\n    }\n}\n", 6, "arity", "holds 2 values"},
	{"a promise after the start", "fn f(a: Int) {\n    if a > 1 {\n        requires a > 0\n    }\n}\n", 3, "promise", "for the start of a function"},

	// try, Result
	{"try in a function that does not give a Result", "fn f(s: Text) -> Int {\n    let n = try s.to_int()\n    return n\n}\n", 2, "try", "it has to give a Result"},
	{"try with another error", "fn f(s: Text) -> Result<Int, Text> {\n    let n = try s.to_int()\n    return Ok(n)\n}\n", 2, "try", ".map_error(...)"},
	{"try on an Int", "fn f() -> Result<Int, Text> {\n    let n = try 1\n    return Ok(n)\n}\n", 2, "try", "try is for a Result or an Option"},
	{"a Result that is dropped", "fn g() -> Result<Int, Text> {\n    return Ok(1)\n}\nfn f() {\n    g()\n}\n", 5, "unused-result", "it is dropped"},

	// traits
	{"a missing method", "trait T {\n    fn a(self)\n    fn b(self)\n}\nstruct S {}\nimpl T for S {\n    fn a(self) {\n    }\n}\n", 6, "trait", "missing the method b"},
	{"a method that is not in the trait", "trait T {\n    fn a(self)\n}\nstruct S {}\nimpl T for S {\n    fn a(self) {\n    }\n    fn b(self) {\n    }\n}\n", 8, "trait", "no method b"},
	{"a method with another signature", "trait T {\n    fn a(self) -> Int\n}\nstruct S {}\nimpl T for S {\n    fn a(self) -> Text {\n        return \"x\"\n    }\n}\n", 6, "trait", "gives Text, and the trait says Int"},
	{"a type that lacks the trait", "trait T {\n    fn a(self)\n}\nfn g<X: T>(x: X) {\n}\nfn f() {\n    g(1)\n}\n", 7, "trait", "Int is not a type with the trait T"},
	{"a method of a parameter without a bound", "fn g<X>(x: X) {\n    x.go()\n}\n", 2, "no-method", "X has no method go"},
	{"an unknown trait", "struct S {}\nimpl Nope for S {\n}\n", 2, "unknown-name", "Nope is not a trait"},

	// closures
	{"a closure parameter that can not be worked out", "fn f() {\n    let g = fn(x) => x\n}\n", 2, "cannot-infer", "write it as x: Type"},
	{"a closure with the wrong number of parameters", "fn f(items: List<Int>) {\n    let a = items.map(fn(x, y) => x)\n}\n", 2, "arity", "takes 2 parameters, and 1 are expected"},

	// inference and values
	{"an empty list nobody uses", "fn f() {\n    let a = []\n}\n", 2, "cannot-infer", "the type of a is not known"},
	{"a name made of an if", "fn f() {\n    let a = if true {\n    }\n}\n", 2, "no-value", "gives Unit"},
	{"a list inside itself", "fn f() {\n    var a = []\n    a = [a]\n}\n", 3, "type-mismatch", "has itself inside it"},
	{"a call of a thing that is not a function", "fn f() -> Int {\n    let a = 1\n    return a(2)\n}\n", 3, "not-callable", "this is Int"},
	{"effects through a generic parameter", "fn tick() with time -> Int {\n    return 1\n}\nfn inner<E>(f: Fn() with E -> Int) with E -> Int {\n    return f()\n}\nfn f() -> Int {\n    return inner(fn() => tick())\n}\n", 8, "effect", "this needs the effect time"},
	{"an effect in a closure that is called", "fn tick() with time -> Int {\n    return 1\n}\nfn f() -> Int {\n    let g = fn() => tick()\n    return g()\n}\n", 6, "effect", "this needs the effect time"},
	{"a closure that returns what the type does not allow", "fn tick() with time -> Int {\n    return 1\n}\nfn f() -> Fn() -> Int {\n    return fn() => tick()\n}\n", 5, "effect", "does not allow them"},
	{"a match on the wrong enum", "enum A {\n    X\n}\nenum B {\n    X\n}\nfn f(a: A) -> Int {\n    return match a {\n        B::X => 1\n    }\n}\n", 9, "type-mismatch", "this pattern is for B"},
	{"the same name twice in a pattern", "enum E {\n    A(x: Int, y: Int)\n}\nfn f(e: E) -> Int {\n    return match e {\n        A(v, v) => v\n    }\n}\n", 6, "duplicate", "v is declared twice"},

	// numbers and examples
	{"an Int that is too large", "fn f() -> Int {\n    return 9223372036854775808\n}\n", 2, "number", "does not fit in an Int"},
	{"a Float that is too large", "fn f() -> Float {\n    return 1e999\n}\n", 2, "number", "does not fit in a Float"},
	{"an example that uses a parameter", "fn f(n: Int) -> Int {\n    example n == 1\n    return n\n}\n", 2, "unknown-name", "there is no n"},

	// declarations
	{"a function declared twice", "fn f() {\n}\nfn f() {\n}\n", 3, "duplicate", "f is declared twice"},
	{"a name declared twice in a block", "fn f() {\n    let a = 1\n    let a = 2\n}\n", 3, "duplicate", "a is declared twice in the same block"},
	{"self outside a method", "fn f(self) {\n}\n", 1, "self", "only for the methods of a type"},
}

func TestRejected(t *testing.T) {
	for _, c := range rejected {
		t.Run(c.name, func(t *testing.T) {
			r := check(t, c.src)

			if r.OK() {
				t.Fatalf("no problem found in\n%s", c.src)
			}

			for _, d := range r.Diagnostics {
				if d.Line == c.line && d.Code == c.code && strings.Contains(d.Msg, c.msg) {
					return
				}
			}

			t.Fatalf("want %s at line %d with %q, got:\n%v\n%s", c.code, c.line, c.msg, r.Error(), c.src)
		})
	}
}
