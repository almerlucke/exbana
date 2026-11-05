package run_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/almerlucke/exbana/v3/examples/covenant/check"
	"github.com/almerlucke/exbana/v3/examples/covenant/run"
)

var checkConfig = check.Config{
	Effects: []string{"time", "random"},
	Native: map[string]string{
		"fs": `
struct Path {}
struct IoError {}

trait Functions {
    fn read_all(path: Path) with fs::read -> Result<Text, IoError>
    fn make_path(text: Text) -> Path
}
`,
	},
}

// bytecode is the way the tests run the programs now: they are run both ways, and have to give the same.
var bytecode bool

func engines(t *testing.T, f func(t *testing.T)) {
	for _, bc := range []bool{false, true} {
		name := "tree"
		if bc {
			name = "bytecode"
		}

		t.Run(name, func(t *testing.T) {
			bytecode = bc

			defer func() { bytecode = false }()

			f(t)
		})
	}
}

func program(t *testing.T, src string, cfg run.Config) *run.Program {
	t.Helper()

	return programB(t, src, cfg)
}

func programB(t testing.TB, src string, cfg run.Config) *run.Program {
	t.Helper()

	cfg.Bytecode = bytecode

	// the module the host provides is only there for the tests that give the host
	cc := check.Config{Effects: checkConfig.Effects}
	if cfg.Host != nil {
		cc = checkConfig
	}

	res := check.Check(cc, map[string]string{"app": src})
	if !res.OK() {
		t.Fatalf("the program does not check:\n%v\n%s", res.Error(), src)
	}

	p, err := run.New(res, cfg)
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func call(t *testing.T, src, fn string, args ...run.Value) string {
	t.Helper()

	p := program(t, src, run.Config{})

	v, err := p.Call(context.Background(), "app", fn, args...)
	if err != nil {
		t.Fatalf("%v\n%s", err, src)
	}

	return run.Format(v)
}

func TestPrograms(t *testing.T) { engines(t, testPrograms) }

func testPrograms(t *testing.T) {
	for _, c := range []struct {
		name, src, fn, want string
		args                []run.Value
	}{
		{"arithmetic", `
fn f() -> Int {
    return (2 + 3) * 4 - 10 / 3 + 7 % 4
}
`, "f", "20", nil},
		{"recursion", `
fn fib(n: Int) -> Int {
    if n < 2 {
        return n
    }
    return fib(n - 1) + fib(n - 2)
}
`, "fib", "55", []run.Value{int64(10)}},
		{"loops", `
fn total(values: List<Int>) -> Int {
    var sum = 0
    for value in values {
        sum = sum + value
    }
    var i = 0
    while i < 3 {
        sum = sum + 100
        i = i + 1
    }
    return sum
}
`, "total", "306", []run.Value{run.NewList([]run.Value{int64(1), int64(2), int64(3)})}},
		{"numbers", `
fn f() -> List<Int> {
    return [0xff, 0b1010_0101, 1_000_000, 7]
}
`, "f", "[255, 165, 1000000, 7]", nil},
		{"floats", `
fn f() -> List<Float> {
    return [1.5 + 2.0, 1e3, 2.5e-1, 7.0 / 2.0, 3.0]
}
`, "f", "[3.5, 1000.0, 0.25, 3.5, 3.0]", nil},
		{"text", `
fn f() -> List<Text> {
    return ["a" + "b", "  x ".trim(), "a,b,c".split(",").first().unwrap_or(""), "héllo".length().to_text(), "q\"uote\n".trim()]
}
`, "f", `["ab", "x", "a", "5", "q\"uote"]`, nil},
		{"structs enums traits", `
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
            Circle(radius) => 3.0 * radius * radius
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

fn total_area<T: Area>(items: List<T>) -> Float {
    return items.fold(0.0, fn(sum, item) => sum + item.area())
}

fn main() -> Text {
    let shapes = [Shape::Circle(radius: 2.0), Shape::Rect(2.0, 3.0), Shape::Empty]
    let p = Point(x: 1.0, y: 2.0).moved(1.5)
    return total_area(shapes).to_text() + " " + p.x.to_text() + " " + shapes.length().to_text()
}
`, "main", `"18.0 2.5 3"`, nil},
		{"values written out", `
struct P { x: Int, y: Text }
enum E {
    A(n: Int)
    B
}
fn f() -> List<Text> {
    return ["x"]
}
fn g() -> Int {
    return 1
}
`, "f", `["x"]`, nil},
		{"closures capture values", `
fn f() -> List<Int> {
    var x = 1
    let g = fn() => x
    x = 2
    let h = fn() => x
    return [g(), h(), x]
}
`, "f", "[1, 2, 2]", nil},
		{"closures that return closures", `
fn make_adder(base: Int) -> Fn(Int) -> Int {
    return fn(x) => x + base
}
fn f() -> Int {
    let add3 = make_adder(3)
    let add10 = make_adder(10)
    return add3(1) + add10(1)
}
`, "f", "15", nil},
		{"lists are values", `
fn f() -> List<List<Int>> {
    let a = [1]
    let b = a.push(2)
    let c = a.push(3)
    let d = b.push(4)
    let e = b.push(5)
    return [a, b, c, d, e]
}
`, "f", "[[1], [1, 2], [1, 3], [1, 2, 4], [1, 2, 5]]", nil},
		{"maps are values and keep their order", `
fn f() -> List<Text> {
    let a = {"x": 1, "y": 2}
    let b = a.set("z", 3)
    let c = a.set("w", 4)
    let d = b.set("x", 10)
    let e = d.remove("y")
    return [a.keys().length().to_text(), b.keys().concat(["|"]).concat(c.keys()).fold("", fn(s, k) => s + k), d.get("x").unwrap_or(0).to_text(), a.get("x").unwrap_or(0).to_text(), e.keys().fold("", fn(s, k) => s + k)]
}
`, "f", `["2", "xyz|xyw", "10", "1", "xz"]`, nil},
		{"sets", `
fn f() -> List<Int> {
    let s = Set::from([3, 1, 3, 2, 1])
    let t = s.add(9).remove(1)
    return s.to_list().concat(t.to_list())
}
`, "f", "[3, 1, 2, 3, 2, 9]", nil},
		{"for over a map", `
fn f() -> Int {
    var total = 0
    for k, v in {"a": 1, "b": 2, "c": 3} {
        total = total * 10 + v + k.length()
    }
    return total
}
`, "f", "234", nil},
		{"Option and Result", `
fn port(text: Text) -> Result<Int, Text> {
    let n = try text.to_int().map_error(fn(e) => "bad number")
    if n < 1 or n > 65535 {
        return Err("out of range")
    }
    return Ok(n)
}

fn f() -> List<Result<Int, Text>> {
    return [port("8080"), port("99999"), port("x"), port("")]
}

fn first_plus(items: List<Int>) -> Option<Int> {
    let x = try items.first()
    return Some(x + 1)
}

fn g() -> List<Option<Int>> {
    return [first_plus([4]), first_plus([])]
}
`, "f", `[Ok(8080), Err("out of range"), Err("bad number"), Err("bad number")]`, nil},
		{"try on Option", `
fn first_plus(items: List<Int>) -> Option<Int> {
    let x = try items.first()
    return Some(x + 1)
}

fn g() -> List<Option<Int>> {
    return [first_plus([4]), first_plus([])]
}
`, "g", "[Some(5), None]", nil},
		{"matching", `
enum Tree {
    Leaf(value: Int)
    Node(left: Tree, right: Tree)
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

fn f() -> List<Text> {
    let t = Tree::Node(left: Tree::Leaf(1), right: Tree::Node(Tree::Leaf(2), Tree::Leaf(3)))
    return [sum(t).to_text(), depth(Some(Some(7))).to_text(), depth(Some(None)).to_text(), depth(None).to_text(), word(0, true), word(9, false)]
}
`, "f", `["6", "7", "1", "0", "zeroyes", "manyno"]`, nil},
		{"equality", `
struct P { x: Int, y: List<Int> }
fn f() -> List<Bool> {
    return [[1, 2] == [1, 2], [1] == [1, 2], P(x: 1, y: [2]) == P(x: 1, y: [2]), P(x: 1, y: [2]) == P(x: 1, y: [3]),
        {"a": 1, "b": 2} == {"b": 2, "a": 1}, Set::from([1, 2]) == Set::from([2, 1]), Some(1) == Some(1), Some(1) == None, 1.5 == 1.5]
}
`, "f", "[true, false, true, false, true, true, true, false, true]", nil},
		{"constants and aliases", `
const limit: Int = 40 + 2
type Ports = List<Int>
fn f(ports: Ports) -> Int {
    return ports.first().unwrap_or(limit) + limit
}
`, "f", "84", []run.Value{run.NewList(nil)}},
		{"closures in closures capture from every level", `
fn f() -> List<Int> {
    let a = 1
    let outer = fn(b: Int) -> Fn(Int) -> Int {
        let c = 10
        return fn(d) => a + b + c + d
    }
    let g = outer(100)
    let h = outer(200)
    return [g(1000), h(1000), g(2000)]
}
`, "f", "[1111, 1211, 2111]", nil},
		{"a closure that calls the function around it", `
fn twice(f: Fn(Int) -> Int, x: Int) -> Int {
    return f(f(x))
}
fn f() -> Int {
    let k = 3
    return twice(fn(n) => twice(fn(m) => m + k, n), 0)
}
`, "f", "12", nil},
		{"return from inside loops", `
fn find(items: List<Int>, target: Int) -> Int {
    var index = 0
    for item in items {
        if item == target {
            return index
        }
        index = index + 1
    }
    return -1
}
fn f() -> List<Int> {
    return [find([5, 6, 7], 7), find([5, 6, 7], 9)]
}
`, "f", "[2, -1]", nil},
		{"try inside a loop and a closure", `
fn parse_all(items: List<Text>) -> Result<Int, Text> {
    var total = 0
    for item in items {
        let n = try item.to_int().map_error(fn(e) => "bad: " + item)
        total = total + n
    }
    return Ok(total)
}
fn f() -> List<Result<Int, Text>> {
    return [parse_all(["1", "2", "3"]), parse_all(["1", "x", "3"])]
}
`, "f", `[Ok(6), Err("bad: x")]`, nil},
		{"methods of traits on parameters", `
trait Name {
    fn name(self) -> Text
}
struct Dog {}
struct Cat { lives: Int }
impl Name for Dog {
    fn name(self) -> Text {
        return "dog"
    }
}
impl Name for Cat {
    fn name(self) -> Text {
        return "cat" + self.lives.to_text()
    }
}
fn names<T: Name>(items: List<T>) -> List<Text> {
    return items.map(fn(x) => x.name())
}
fn f() -> List<List<Text>> {
    return [names([Dog(), Dog()]), names([Cat(lives: 9)])]
}
`, "f", `[["dog", "dog"], ["cat9"]]`, nil},
		{"structs inside structs", `
struct Inner { n: Int }
struct Outer { inner: Inner, items: List<Inner> }
fn f() -> Outer {
    let o = Outer(inner: Inner(n: 1), items: [Inner(n: 2)])
    let p = o with { inner: Inner(n: 5) }
    return p with { items: o.items.push(Inner(n: 3)) }
}
`, "f", "Outer(inner: Inner(n: 5), items: [Inner(n: 2), Inner(n: 3)])", nil},
		{"labels in a different order", `
struct P { a: Int, b: Text, c: Bool }
fn f() -> P {
    return P(c: true, a: 1, b: "x")
}
`, "f", `P(a: 1, b: "x", c: true)`, nil},
		{"and and or stop early", `
fn boom() -> Bool {
    return 1 / 0 == 1
}
fn f() -> List<Bool> {
    return [false and boom(), true or boom(), true and true, false or false]
}
`, "f", "[false, true, true, false]", nil},
		{"early return in a nested if", `
fn classify(n: Int) -> Text {
    if n < 0 {
        if n < -100 {
            return "very negative"
        }
        return "negative"
    } else if n == 0 {
        return "zero"
    }
    return "positive"
}
fn f() -> List<Text> {
    return [classify(-500), classify(-5), classify(0), classify(7)]
}
`, "f", `["very negative", "negative", "zero", "positive"]`, nil},
		{"a constant that uses a function", `
fn base() -> Int {
    return 7
}
const answer: Int = base() * 6
fn f() -> Int {
    return answer + answer
}
`, "f", "84", nil},
		{"destructuring in a let", `
struct P { x: Int, y: Int }
fn f() -> Int {
    let P(a, b) = P(x: 3, y: 4)
    return a * 10 + b
}
`, "f", "34", nil},
		{"mutual recursion", `
fn even(n: Int) -> Bool {
    if n == 0 {
        return true
    }
    return odd(n - 1)
}
fn odd(n: Int) -> Bool {
    if n == 0 {
        return false
    }
    return even(n - 1)
}
fn f() -> List<Bool> {
    return [even(10), odd(10), even(7)]
}
`, "f", "[true, false, false]", nil},
		{"a promise inside a closure", `
fn f() -> Int {
    let g = fn(n: Int) -> Int {
        requires n > 0
        return n * 2
    }
    return g(4)
}
`, "f", "8", nil},
		{"a promise that holds", `
fn clamp(n: Int, lo: Int, hi: Int) -> Int {
    requires lo <= hi
    if n < lo {
        return lo
    }
    if n > hi {
        return hi
    }
    return n
}
`, "clamp", "3", []run.Value{int64(9), int64(0), int64(3)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := call(t, c.src, c.fn, c.args...); got != c.want {
				t.Errorf("got  %s\nwant %s", got, c.want)
			}
		})
	}
}

func fault(t *testing.T, src, fn string, args ...run.Value) *run.Fault {
	t.Helper()

	p := program(t, src, run.Config{MaxDepth: 200})

	_, err := p.Call(context.Background(), "app", fn, args...)

	var f *run.Fault
	if !errors.As(err, &f) {
		t.Fatalf("no fault: %v\n%s", err, src)
	}

	return f
}

func TestFaults(t *testing.T) { engines(t, testFaults) }

type faultCase struct {
	name, src, fn, kind, msg string
	line                     int
	args                     []run.Value
}

var faultCases = []faultCase{
	{"overflow in +", "fn f(n: Int) -> Int {\n    return n + 1\n}\n", "f", "overflow", "9223372036854775807 + 1", 2, []run.Value{int64(9223372036854775807)}},
	{"overflow in -", "fn f(n: Int) -> Int {\n    return n - 1\n}\n", "f", "overflow", "does not fit", 2, []run.Value{int64(-9223372036854775808)}},
	{"overflow in *", "fn f(n: Int) -> Int {\n    return n * 2\n}\n", "f", "overflow", "does not fit", 2, []run.Value{int64(5000000000000000000)}},
	{"overflow in negation", "fn f(n: Int) -> Int {\n    return -n\n}\n", "f", "overflow", "does not fit", 2, []run.Value{int64(-9223372036854775808)}},
	{"overflow in the smallest divided by -1", "fn f(n: Int) -> Int {\n    return n / -1\n}\n", "f", "overflow", "does not fit", 2, []run.Value{int64(-9223372036854775808)}},
	{"division by zero", "fn f(n: Int) -> Int {\n    return 10 / n\n}\n", "f", "division", "10 / 0", 2, []run.Value{int64(0)}},
	{"remainder by zero", "fn f(n: Int) -> Int {\n    return 10 % n\n}\n", "f", "division", "10 % 0", 2, []run.Value{int64(0)}},
	{"a promise that fails", "fn f(n: Int) -> Int {\n    requires n > 0\n    return n\n}\n", "f", "requires", "requires n > 0 is false", 2, []run.Value{int64(-1)}},
	{"a promise that fails inside", "fn g(n: Int) -> Int {\n    requires n > 0\n    return n\n}\nfn f() -> Int {\n    return g(0)\n}\n", "f", "requires", "for the call of g", 2, nil},
	{"calls nested too deep", "fn f(n: Int) -> Int {\n    return f(n + 1)\n}\n", "f", "stack", "too deep", 2, []run.Value{int64(0)}},
	{"a Float that is not an Int", "fn f(x: Float) -> Int {\n    return x.floor()\n}\n", "f", "range", "does not fit", 2, []run.Value{1e30}},
	{"abs of the smallest", "fn f(n: Int) -> Int {\n    return n.abs()\n}\n", "f", "overflow", "does not fit", 2, []run.Value{int64(-9223372036854775808)}},
}

func testFaults(t *testing.T) {
	for _, c := range faultCases {
		t.Run(c.name, func(t *testing.T) {
			f := fault(t, c.src, c.fn, c.args...)

			if f.Kind != c.kind || !strings.Contains(f.Msg, c.msg) || f.Line != c.line || f.Module != "app" {
				t.Errorf("got %v\nwant %s with %q at line %d", f, c.kind, c.msg, c.line)
			}
		})
	}

	// the functions that were running
	f := fault(t, "fn g(n: Int) -> Int {\n    return 1 / n\n}\nfn f(n: Int) -> Int {\n    return g(n)\n}\n", "f", int64(0))
	if len(f.Stack) != 2 || f.Stack[0] != "app::g" || f.Stack[1] != "app::f" {
		t.Errorf("stack %v", f.Stack)
	}
}

// The tree and the bytecode are two ways to run one program: a fault is the same, down to the column and the stack.
func TestTheWaysAgreeOnFaults(t *testing.T) {
	for _, c := range faultCases {
		t.Run(c.name, func(t *testing.T) {
			var got [2]*run.Fault

			for i, bc := range []bool{false, true} {
				bytecode = bc
				got[i] = fault(t, c.src, c.fn, c.args...)
			}

			bytecode = false

			if !reflect.DeepEqual(got[0], got[1]) {
				t.Errorf("the tree says\n%v\nand the bytecode says\n%v", got[0], got[1])
			}
		})
	}
}

func TestLimits(t *testing.T) { engines(t, testLimits) }

func testLimits(t *testing.T) {
	src := "fn spin() {\n    while true {\n    }\n}\n"
	p := program(t, src, run.Config{MaxSteps: 10000})

	_, err := p.Call(context.Background(), "app", "spin")

	var l *run.Limit
	if !errors.As(err, &l) || l.Kind != "steps" {
		t.Errorf("steps: %v", err)
	}

	p = program(t, src, run.Config{MaxSteps: -1})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)

	defer cancel()

	_, err = p.Call(ctx, "app", "spin")
	if !errors.As(err, &l) || l.Kind != "cancelled" || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("cancelled: %v", err)
	}
}

func TestEffectsNeedAGrant(t *testing.T) { engines(t, testEffectsNeedAGrant) }

func testEffectsNeedAGrant(t *testing.T) {
	src := `
use fs
use fs::Path

fn tick() with time -> Int {
    return 1
}

fn load(p: Path) with fs::read -> Text {
    return fs::read_all(p).unwrap_or("none")
}

fn both(p: Path) with fs::read + time -> Text {
    return load(p) + tick().to_text()
}

fn pure() -> Int {
    return 2
}
`
	host := map[string]run.HostFunc{}

	var prog *run.Program

	host["fs::make_path"] = func(ctx context.Context, a []run.Value) (run.Value, error) {
		s, _ := prog.Struct("fs", "Path", a[0])
		return s, nil
	}
	host["fs::read_all"] = func(ctx context.Context, a []run.Value) (run.Value, error) {
		return prog.Ok("contents of " + a[0].(*run.Struct).Host.(string)), nil
	}

	prog = program(t, src, run.Config{Host: host, Grant: []string{"fs::read"}})

	path, _ := prog.Struct("fs", "Path", "/etc/hosts")

	v, err := prog.Call(context.Background(), "app", "load", path)
	if err != nil || v != "contents of /etc/hosts" {
		t.Fatalf("load: %v, %v", v, err)
	}

	// time was not granted
	_, err = prog.Call(context.Background(), "app", "both", path)

	var d *run.Denied
	if !errors.As(err, &d) || len(d.Effects) != 1 || d.Effects[0] != "time" {
		t.Errorf("both: %v", err)
	}

	_, err = prog.Call(context.Background(), "app", "tick")
	if !errors.As(err, &d) {
		t.Errorf("tick: %v", err)
	}

	// and nothing is needed for a function with no effects
	if v, err := prog.Call(context.Background(), "app", "pure"); err != nil || v != int64(2) {
		t.Errorf("pure: %v, %v", v, err)
	}
}

func TestTheHostHasToProvideWhatItDeclared(t *testing.T) {
	res := check.Check(checkConfig, map[string]string{"app": "fn f() {\n}\n"})

	_, err := run.New(res, run.Config{})
	if err == nil || !strings.Contains(err.Error(), "fs::make_path") {
		t.Errorf("%v", err)
	}
}

func TestAProgramWithProblemsDoesNotRun(t *testing.T) {
	res := check.Check(checkConfig, map[string]string{"app": "fn f() -> Int {\n    return \"x\"\n}\n"})

	if _, err := run.New(res, run.Config{}); err == nil {
		t.Error("a program that does not check was made")
	}
}

func TestExamples(t *testing.T) { engines(t, testExamples) }

func testExamples(t *testing.T) {
	src := `
fn double(n: Int) -> Int {
    example double(2) == 4
    example double(0) == 0
    example double(3) == 7
    example double(9223372036854775807) == 0
    return n * 2
}
`
	p := program(t, src, run.Config{})

	got := p.Examples(context.Background())
	if len(got) != 4 {
		t.Fatalf("%d examples", len(got))
	}

	for i, ok := range []bool{true, true, false, false} {
		if (got[i].Err == nil) != ok {
			t.Errorf("example %d (%s): %v", i, got[i].Text, got[i].Err)
		}
	}

	if got[2].Err.Error() != "it is false" || got[2].Line != 5 || got[2].Function != "double" {
		t.Errorf("%+v", got[2])
	}

	var f *run.Fault
	if !errors.As(got[3].Err, &f) || f.Kind != "overflow" {
		t.Errorf("%v", got[3].Err)
	}
}

// A list that is made from another shares its elements; whatever is done to one version must not show in another. This does
// random things to random versions of lists and maps, and checks every version against a model made of plain slices.
func TestVersionsDoNotShowInEachOther(t *testing.T) {
	rng := rand.New(rand.NewSource(7))

	type listVersion struct {
		l     *run.List
		model []int64
	}

	lists := []listVersion{{run.NewList(nil), nil}}

	for range 3000 {
		from := lists[rng.Intn(len(lists))]
		n := int64(rng.Intn(1000))

		lists = append(lists, listVersion{from.l.Push(n), append(append([]int64(nil), from.model...), n)})
	}

	for i, v := range lists {
		if v.l.Len() != len(v.model) {
			t.Fatalf("list %d has %d items, and %d are expected", i, v.l.Len(), len(v.model))
		}

		for j, m := range v.model {
			if v.l.At(j) != m {
				t.Fatalf("list %d item %d is %v, and %d is expected", i, j, v.l.At(j), m)
			}
		}
	}

	type mapVersion struct {
		m     *run.Map
		keys  []string
		model map[string]int64
	}

	maps := []mapVersion{{run.NewMap(), nil, map[string]int64{}}}

	for range 3000 {
		from := maps[rng.Intn(len(maps))]
		key := fmt.Sprintf("k%d", rng.Intn(12))
		next := mapVersion{keys: append([]string(nil), from.keys...), model: map[string]int64{}}

		for k, v := range from.model {
			next.model[k] = v
		}

		if rng.Intn(4) == 0 {
			next.m = from.m.Remove(key)

			if _, there := next.model[key]; there {
				delete(next.model, key)

				for i, k := range next.keys {
					if k == key {
						next.keys = append(next.keys[:i], next.keys[i+1:]...)
						break
					}
				}
			}
		} else {
			v := int64(rng.Intn(100))
			next.m = from.m.Set(key, v)

			if _, there := next.model[key]; !there {
				next.keys = append(next.keys, key)
			}

			next.model[key] = v
		}

		maps = append(maps, next)
	}

	for i, v := range maps {
		if v.m.Len() != len(v.keys) {
			t.Fatalf("map %d has %d entries, and %d are expected", i, v.m.Len(), len(v.keys))
		}

		for j, k := range v.keys {
			gk, gv := v.m.Entry(j)
			if gk != k || gv != v.model[k] {
				t.Fatalf("map %d entry %d is %v: %v, and %s: %d is expected", i, j, gk, gv, k, v.model[k])
			}

			if got, ok := v.m.Get(k); !ok || got != v.model[k] {
				t.Fatalf("map %d key %s", i, k)
			}
		}

		for k := range 12 {
			key := fmt.Sprintf("k%d", k)
			_, want := v.model[key]

			if _, got := v.m.Get(key); got != want {
				t.Fatalf("map %d: key %s is there: %v, expected %v", i, key, got, want)
			}
		}
	}
}
