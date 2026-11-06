// Package samples is Covenant source that the language accepts, and source it does not, for the tests of every stage to share.
package samples

// Accepted are programs, by what they show.
var Accepted = map[string]string{
	"empty":   "",
	"comment": "// nothing here\n",

	"function with promises and try": `
fn parse_port(text: Text) -> Result<Port, ParseError> {
    requires text.length() > 0
    example parse_port("8080") == Ok(Port(8080))
    example parse_port("99999") == Err(OutOfRange)

    let number = try text.to_int()
    if number < 1 or number > 65535 {
        return Err(OutOfRange)
    }
    return Ok(Port(number))
}
`,
	"effects": `
fn save(path: Path, data: Bytes) with fs::write + fs::read -> Result<Unit, IoError> {
    try fs::write_all(path, data)
    return Ok(Unit)
}
`,
	"struct enum trait impl": `
use geometry::shapes
use geometry::shapes::Shape
use text::format as fmt

pub struct Point { x: Float, y: Float }

struct Empty {}

pub enum Shape {
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
}

const limit: Int = 100
type Ports = List<Port>
`,
	"values of structs and enums are made with named arguments": `
fn origin() -> Point {
    let p = Point(x: 0.0, y: 0.0)
    let c = Shape::Circle(radius: 2.0)
    let r = Shape::Rect(2.0, 3.0)
    return p
}
`,
	"generics": `
fn first<T, U: Area + Show>(items: List<T>, other: U) -> Option<T> {
    return items.first()
}

struct Pair<A, B> { first: A, second: B }

type Table = Map<Text, List<List<Int>>>
`,
	"loops and mutation": `
fn total(values: List<Int>) -> Int {
    var sum = 0
    var typed: Int = 1
    for value in values {
        sum = sum + value
    }
    while sum > 100 {
        sum = sum - 100
    }
    return sum
}
`,
	"lines may break after operators and commas": `
fn long(a: Int, b: Int) -> Bool {
    let ok = a > 1 and
        b > 2
    let total = a +
        b
    let r = call(
        a,
        b
    )
    let items = [
        1,
        2
    ]
    return ok
}
`,
	"if else chain": `
fn sign(n: Int) -> Int {
    if n < 0 {
        return -1
    } else if n == 0 {
        return 0
    } else {
        return 1
    }
}
`,
	"comments and blank lines": `
// the first
fn one() -> Int {   // trailing

    // inside
    return 1
}


// the last
`,
	"windows line ends":   "fn one() -> Int {\r\n    return 1\r\n}\r\n",
	"no trailing newline": "fn one() -> Int {\n    return 1\n}",

	"effect groups": `
effects Storage = fs::read + fs::write

pub effects Everything = Storage + net::fetch +
    time +
    random

fn backup(from: Path, to: Path) with Storage -> Result<Unit, IoError> {
    let data = try fs::read_all(from)
    return fs::write_all(to, data)
}

fn sync(path: Path) with Everything +
    log::write -> Unit {
    return Unit
}

fn make(base: Int) with fs::read -> Fn(Int) with time -> Int {
    return fn(x) => x + base
}

fn noisy() with log::write {
    log::write("hello")
}

fn plain() -> Int {
    return 1
}

trait Store {
    fn put(self, key: Text) with Storage -> Unit
    fn count(self) -> Int
}
`,
	"closures": `
fn demo(items: List<Item>) -> Int {
    let double = fn(x) => x * 2
    let typed = fn(a: Int, b: Int) -> Int => a + b
    let names = items.filter(fn(i) => i.active).map(fn(i) => i.name)
    let total = items.fold(0, fn(sum, item) => sum + item.price)
    let check = fn(order: Order) -> Result<Unit, OrderError> {
        requires order.items.length() > 0
        try validate(order)
        return Ok(Unit)
    }
    let lines = items.map(fn(i) {
        let label = i.name
        return label
    })
    let called = (fn(x) => x + 1)(2)
    return total
}
`,
	"function types": `
fn apply(f: Fn(Int) -> Int, value: Int) -> Int {
    return f(value)
}

fn load(read: Fn(Path) with fs::read -> Text, path: Path) -> Text {
    return read(path)
}

fn twice(f: Fn(Int, Int) -> Int) -> Fn(Int) -> Int {
    return fn(x) => f(x, x)
}

fn run(action: Fn()) {
    action()
}

fn both(a: Fn(Int) with fs::read + net::fetch -> Int, b: Int) -> Int {
    return b
}
`,
	"collections": `
fn demo() -> Int {
    let numbers = [1, 2, 3]
    let none = []
    let ages = {"ann": 31, "bob": 27}
    let nobody = {:}
    let nested = {
        "a": [1, 2],
        "b": []
    }
    let byName = {name: 1, other.field: 2, Color::Red: 3, (1 + 2): 4}
    let unique = Set::from([1, 2, 2])
    let older = ages.set("cy", 40)
    let first = numbers.get(0)
    let built = Map::build(fn(b) {
        for user in users {
            b.set(user.name, user.age)
        }
    })
    for key, value in ages {
        total = total + value
    }
    for i in range(0, 10) {
        total = total + i
    }
    return 0
}
`,
	"numbers": `
fn numbers() {
    let a = [0, 7, 1_000_000, 0xff, 0xde_ad_be_ef, 0b1010_0101, 1.5, 0.5, 1_000.000_1, 1e9, 2.5e-3, 6.02e+23, -3, -0.5]
    let b = 1.max(2)
    let c = 1.e3
}
`,
	"strings and patterns": `
fn describe(n: Int, s: Text) -> Text {
    return match n {
        0 => "zero"
        0xff => "byte"
        _ => s
    }
}
`,
	"trait objects": `
trait Area {
    fn area(self) -> Float
}

fn total(items: List<dyn Area>) -> Float {
    return items.fold(0.0, fn(sum, item) => sum + item.area())
}

fn one(x: dyn Area) -> dyn Area {
    return x
}

fn demo() -> Float {
    let shapes = [Circle(radius: 1.0) as dyn Area, Rect(2.0, 3.0) as dyn Area]
    let again = one(shapes.first().unwrap_or(Circle(radius: 2.0) as dyn Area))
    return total(shapes) + (again.area() as dyn Area).area() + Circle(radius: 1.0).wider() as dyn Area
}

fn closure_over(f: Fn(dyn Area) -> dyn Area) -> Fn(dyn Area) -> dyn Area {
    return f
}
`,
	"assignments": `
fn f() {
    var x = 1
    x = 2
    self.count = self.count + 1
}
`,
}

// Rejected are sources that are not Covenant, by what is wrong with them.
var Rejected = map[string]string{
	"two statements on a line":         "fn f() {\n    let a = 1 let b = 2\n}\n",
	"a semicolon":                      "fn f() {\n    let a = 1;\n}\n",
	"a keyword as a name":              "fn f() {\n    let match = 1\n}\n",
	"chained comparison":               "fn f() -> Bool {\n    return 1 < 2 < 3\n}\n",
	"and mixed with or":                "fn f() -> Bool {\n    return a and b or c\n}\n",
	"a line break before an operator":  "fn f() -> Int {\n    return a\n        + b\n}\n",
	"a promise after a statement":      "fn f() {\n    let a = 1\n    requires a > 0\n}\n",
	"a missing block":                  "fn f() -> Int\n",
	"a statement outside a function":   "let a = 1\n",
	"an enum without variants":         "enum E {\n}\n",
	"a use after an item":              "fn f() {\n}\nuse a::b\n",
	"a keyword as a function name":     "fn with() {\n}\n",
	"effects after the result":         "fn f() -> Int with fs::read {\n}\n",
	"effects after a function type":    "fn f(g: Fn(Int) -> Int with fs::read) {\n}\n",
	"a group without members":          "effects Empty =\n",
	"a group inside a function":        "fn f() {\n    effects A = fs::read\n}\n",
	"effects as a name":                "fn effects() {\n}\n",
	"an index":                         "fn f() {\n    let a = xs[0]\n}\n",
	"an empty brace as a value":        "fn f() {\n    let a = {}\n}\n",
	"a closure without a body":         "fn f() {\n    let a = fn(x)\n}\n",
	"a closure with a semicolon":       "fn f() {\n    let a = fn(x) => x;\n}\n",
	"a map entry without a value":      "fn f() {\n    let a = {\"a\"}\n}\n",
	"a function type without parens":   "fn f(g: Fn -> Int) {\n}\n",
	"a trailing comma in a call":       "fn f() {\n    g(a, b,)\n}\n",
	"a trailing comma in a list":       "fn f() {\n    let a = [1, 2,]\n}\n",
	"a trailing comma in parameters":   "fn f(a: Int,) {\n}\n",
	"a closure as an operand":          "fn f() {\n    let a = 1 + fn(x) => x\n}\n",
	"a space before a colon":           "fn f() {\n    let a : Int = 1\n}\n",
	"braces behind a name":             "fn f() {\n    let p = Point { x: 1 }\n}\n",
	"effects separated by commas":      "fn f() with a, b {\n}\n",
	"a field without a type":           "struct P {\n    x\n}\n",
	"a method without a body":          "impl P {\n    fn f(self)\n}\n",
	"a cast to a type that is not dyn": "fn f() {\n    let a = 1 as Int\n}\n",
	"a cast with nothing after as":     "fn f() {\n    let a = 1 as dyn\n}\n",
	"two traits in a dyn":              "fn f(x: dyn A + B) {\n}\n",
	"dyn as a name":                    "fn f() {\n    let dyn = 1\n}\n",
	"a number with a leading zero":     "fn f() {\n    let a = 007\n}\n",
	"a hex number in capitals":         "fn f() {\n    let a = 0xFF\n}\n",
	"a number as a word":               "fn f() {\n    let a = 12abc\n}\n",
	"a text that is not closed":        "fn f() {\n    let a = \"abc\n}\n",
	"a character that is not ASCII":    "fn f() {\n    let é = 1\n}\n",
}
