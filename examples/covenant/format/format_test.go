package format

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/almerlucke/exbana/v3/examples/covenant/internal/samples"
)

func must(t *testing.T, src string) string {
	t.Helper()

	out, err := Source(src)
	if err != nil {
		t.Fatalf("%v\n%s", err, src)
	}

	return out
}

// Each case is a source and what the formatter makes of it; what it makes of that is itself.
var cases = []struct{ name, in, want string }{
	{"spaces", "fn  main( )  {\n  io::print( \"hi\" )\n}\n", "fn main() {\n    io::print(\"hi\")\n}\n"},
	{"no final line end", "const a: Int = 1", "const a: Int = 1\n"},
	{"crlf", "const a: Int = 1\r\nconst b: Int = 2\r\n", "const a: Int = 1\nconst b: Int = 2\n"},
	{"empty", "", ""},
	{"blank lines are one", "const a: Int = 1\n\n\n\nconst b: Int = 2\n", "const a: Int = 1\n\nconst b: Int = 2\n"},
	{"a blank line at the start of a block goes", "fn f() {\n\n    let a = 1\n\n\n    let b = 2\n\n}\n", "fn f() {\n    let a = 1\n\n    let b = 2\n}\n"},
	{"an empty block", "fn f() {\n}\n", "fn f() {}\n"},

	// the blank lines between declarations
	{"large declarations get a blank line", "fn a() {\n}\nfn b() {\n}\n", "fn a() {}\n\nfn b() {}\n"},
	{"small declarations keep theirs", "const a: Int = 1\nconst b: Int = 2\n\nconst c: Int = 3\n", "const a: Int = 1\nconst b: Int = 2\n\nconst c: Int = 3\n"},
	{"uses are apart from the rest", "use a\nuse b\nconst c: Int = 1\n", "use a\nuse b\n\nconst c: Int = 1\n"},

	// declarations
	{"struct", "struct P<T>{x:Float,y:T}\n", "struct P<T> { x: Float, y: T }\n"},
	{"empty struct", "struct P {  }\n", "struct P {}\n"},
	{"a struct in lines stays so", "struct P {\n    x: Float,\n    y: Float\n}\n", "struct P {\n    x: Float,\n    y: Float\n}\n"},
	{"enum", "enum E{A(x:Int,y:Int)\nB\n}\n", "enum E {\n    A(x: Int, y: Int)\n    B\n}\n"},
	{"trait and impl", "trait T{fn f(self)->Int\n}\nimpl T for S{pub fn f(self)->Int{\nreturn 1\n}\n}\n",
		"trait T {\n    fn f(self) -> Int\n}\n\nimpl T for S {\n    pub fn f(self) -> Int {\n        return 1\n    }\n}\n"},
	{"effects", "effects Storage=fs::read+fs::write\nfn f()with Storage+time->Int{\nreturn 1\n}\n",
		"effects Storage = fs::read + fs::write\n\nfn f() with Storage + time -> Int {\n    return 1\n}\n"},
	{"generics", "fn f<T,U:Area+Show>(a:T)->U{\nreturn a\n}\n", "fn f<T, U: Area + Show>(a: T) -> U {\n    return a\n}\n"},
	{"function types", "fn f(g:Fn(Int,Int)with fs::read->Int)->Fn()->Int{\nreturn g\n}\n",
		"fn f(g: Fn(Int, Int) with fs::read -> Int) -> Fn() -> Int {\n    return g\n}\n"},
	{"pub", "pub fn f() {\n}\npub struct S{a:Int}\n", "pub fn f() {}\n\npub struct S { a: Int }\n"},
	{"promises come first", "fn f(a:Int)->Int{\nrequires a>0\nexample f(1)==1\nreturn a\n}\n",
		"fn f(a: Int) -> Int {\n    requires a > 0\n    example f(1) == 1\n    return a\n}\n"},

	// the parentheses an expression needs, and no others
	{"parentheses that are not needed go", "const a: Int = ((1)+(2))*3\n", "const a: Int = (1 + 2) * 3\n"},
	{"parentheses that are needed stay", "const a: Int = 1-(2-3)\nconst b: Int = (1-2)-3\n", "const a: Int = 1 - (2 - 3)\nconst b: Int = 1 - 2 - 3\n"},
	{"a product in a sum", "const a: Int = (1*2)+(3*4)\n", "const a: Int = 1 * 2 + 3 * 4\n"},
	{"a comparison in a comparison", "const a: Bool = (1<2)==(3<4)\n", "const a: Bool = (1 < 2) == (3 < 4)\n"},
	{"and in or", "const a: Bool = (x and y) or z\nconst b: Bool = x and (y or z)\n", "const a: Bool = (x and y) or z\nconst b: Bool = x and (y or z)\n"},
	{"a row of and", "const a: Bool = (x and y) and z\nconst b: Bool = x and (y and z)\n", "const a: Bool = x and y and z\nconst b: Bool = x and (y and z)\n"},
	{"not", "const a: Bool = not (x and y)\nconst b: Bool = not (x == y)\nconst c: Bool = (not x) == y\n", "const a: Bool = not (x and y)\nconst b: Bool = not x == y\nconst c: Bool = (not x) == y\n"},
	{"signs", "const a: Int = -(-x)\nconst b: Int = -(x+1)\nconst c: Int = -x.y\nconst d: Int = (-x).y\n", "const a: Int = -(-x)\nconst b: Int = -(x + 1)\nconst c: Int = -x.y\nconst d: Int = (-x).y\n"},
	{"try", "fn f() {\nlet a = (try x).y\nlet b = try x.y\nlet c = try (x+1)\n}\n", "fn f() {\n    let a = (try x).y\n    let b = try x.y\n    let c = try (x + 1)\n}\n"},
	{"a closure in an expression", "const a: Int = (fn(x)=>x)(1)\nconst b: Int = 1+(fn(x)=>x)(1)\n", "const a: Int = (fn(x) => x)(1)\nconst b: Int = 1 + (fn(x) => x)(1)\n"},
	{"a closure before a brace", "fn f() {\nif (fn() => true)() {\n}\nfor a in (fn() => xs)() {\n}\n}\n", "fn f() {\n    if (fn() => true)() {}\n    for a in (fn() => xs)() {}\n}\n"},
	{"a closure as a condition", "fn f() {\nif (fn() => true) {\n}\nfor a in (fn() => xs) {\n}\n}\n", "fn f() {\n    if (fn() => true) {}\n    for a in (fn() => xs) {}\n}\n"},
	{"a member of a sum", "const a: Int = (a+b).c\nconst b: Int = a+b.c\n", "const a: Int = (a + b).c\nconst b: Int = a + b.c\n"},
	{"a cast", "const a: Int = (x+y) as dyn Area\nconst b: Int = x as dyn Area\n", "const a: Int = (x + y) as dyn Area\nconst b: Int = x as dyn Area\n"},

	// the shapes of the expressions
	{"collections", "fn f() {\nlet a=[1,2,3]\nlet b={\"a\":1,\"b\":2}\nlet c={:}\nlet d=[]\nlet e={k:1}\n}\n",
		"fn f() {\n    let a = [1, 2, 3]\n    let b = {\"a\": 1, \"b\": 2}\n    let c = {:}\n    let d = []\n    let e = {k: 1}\n}\n"},
	{"update", "fn f() {\nlet a=p with{x:1.0,y:2.0}\nlet b=p with{}\n}\n", "fn f() {\n    let a = p with { x: 1.0, y: 2.0 }\n    let b = p with {}\n}\n"},
	{"calls", "fn f() {\nlet a=g(1,x:2)\nlet b=g()\nlet c=a.b(1).c\n}\n", "fn f() {\n    let a = g(1, x: 2)\n    let b = g()\n    let c = a.b(1).c\n}\n"},
	{"patterns", "fn f() {\nlet Pair(a,_)=p\nmatch x{\nSome(Pair(1,\"a\"))=>1\nNone=>2\n_=>3\n}\n}\n",
		"fn f() {\n    let Pair(a, _) = p\n    match x {\n        Some(Pair(1, \"a\")) => 1\n        None => 2\n        _ => 3\n    }\n}\n"},
	{"statements", "fn f() {\nvar a:Int=1\nvar b=2\na=a+1\nwhile a<10 {\na=a+1\n}\nfor i,j in m {\nreturn\n}\n}\n",
		"fn f() {\n    var a: Int = 1\n    var b = 2\n    a = a + 1\n    while a < 10 {\n        a = a + 1\n    }\n    for i, j in m {\n        return\n    }\n}\n"},
	{"if and else", "fn f() {\nif a {\nx\n} else if b {\ny\n} else {\nz\n}\n}\n", "fn f() {\n    if a {\n        x\n    } else if b {\n        y\n    } else {\n        z\n    }\n}\n"},
	{"a closure with a block as an argument", "fn f() {\nlet a=xs.map(fn(x)->Int{\nreturn x\n})\n}\n", "fn f() {\n    let a = xs.map(fn(x) -> Int {\n        return x\n    })\n}\n"},
	{"closures", "fn f() {\nlet a=fn(x:Int,y)=>x+y\nlet b=fn()=>1\n}\n", "fn f() {\n    let a = fn(x: Int, y) => x + y\n    let b = fn() => 1\n}\n"},

	{"an if as an argument stays on the line", "fn f() {\nfoo(if a {\nx\n} else {\ny\n})\n}\n", "fn f() {\n    foo(if a {\n        x\n    } else {\n        y\n    })\n}\n"},
	{"enums are apart", "enum A {\nX\n}\nenum B {\nY\n}\n", "enum A {\n    X\n}\n\nenum B {\n    Y\n}\n"},
	{"methods are apart", "impl S {\nfn a(self) {\n}\nfn b(self) {\n}\n}\n", "impl S {\n    fn a(self) {}\n\n    fn b(self) {}\n}\n"},
	{"promises keep their order", "fn f(a: Int) {\nexample f(1)\nrequires a > 0\nexample f(2)\n}\n", "fn f(a: Int) {\n    example f(1)\n    requires a > 0\n    example f(2)\n}\n"},

	// comments
	{"a comment in an empty map", "fn f() {\nlet a = {\n// c\n:\n}\n}\n", "fn f() {\n    let a = {\n        // c\n        :\n    }\n}\n"},
	{"a comment that is moved into a body is not apart from it", "fn f() with fs::read + // c\nnet::fetch {\n    let a = 1\n}\n", "fn f() with fs::read + net::fetch {\n    // c\n    let a = 1\n}\n"},
	{"a comment inside an argument", "fn f() {\nlet a = g(x + // c\ny, z)\n}\n", "fn f() {\n    let a = g(\n        // c\n        x +\n            y,\n        z\n    )\n}\n"},
	{"comment lines", "// a\n\n// b\nfn f() {\n    // c\n    x\n    // d\n}\n// e\n", "// a\n\n// b\nfn f() {\n    // c\n    x\n    // d\n}\n// e\n"},
	{"comment at the end of a line", "fn f() { // a\n    x // b\n}\nconst a: Int = 1   // c\n", "fn f() { // a\n    x // b\n}\n\nconst a: Int = 1 // c\n"},
	{"comments are moved in", "fn  f( ) {\n        // c\n      x\n}\n", "fn f() {\n    // c\n    x\n}\n"},
	{"comments only", "// a\n// b\n", "// a\n// b\n"},
	{"comments in a list", "fn f() {\nlet a = g(\n// c\n1, // d\n2\n// e\n)\n}\n", "fn f() {\n    let a = g(\n        // c\n        1, // d\n        2\n        // e\n    )\n}\n"},
	{"comments in enum and match", "enum E {\n// a\nA // b\n}\nfn f() {\nmatch x { // c\n// d\nA => 1 // e\n}\n}\n", "enum E {\n    // a\n    A // b\n}\n\nfn f() {\n    match x { // c\n        // d\n        A => 1 // e\n    }\n}\n"},
	{"a comment in an expression goes up", "fn f() {\nlet a = 1 + // c\n2\n}\n", "fn f() {\n    // c\n    let a = 1 +\n        2\n}\n"},
	{"the text of a comment is not touched", "//no space\n//   many   spaces   \nconst a: Int = 1\n", "//no space\n//   many   spaces\nconst a: Int = 1\n"},
	{"a doc comment stays on its function", "// Adds.\nfn add(a: Int) -> Int {\n    return a\n}\n", "// Adds.\nfn add(a: Int) -> Int {\n    return a\n}\n"},

	// lines
	{"a long call is broken", "fn f() {\nlet e = very_long_function_name_number_one(argument_number_one, argument_number_two, argument_number_three)\n}\n",
		"fn f() {\n    let e = very_long_function_name_number_one(\n        argument_number_one,\n        argument_number_two,\n        argument_number_three\n    )\n}\n"},
	{"a long row of and is broken after the operators", "fn f() {\nlet g = first_condition_is_long and second_condition_is_long and third_condition_is_long and fourth_one\n}\n",
		"fn f() {\n    let g = first_condition_is_long and\n        second_condition_is_long and\n        third_condition_is_long and\n        fourth_one\n}\n"},
	{"a line broken after an operator stays so", "fn f() {\nlet a = x and\ny\n}\n", "fn f() {\n    let a = x and\n        y\n}\n"},
	{"a list in lines stays so", "fn f() {\nlet a = [\n1,\n2\n]\n}\n", "fn f() {\n    let a = [\n        1,\n        2\n    ]\n}\n"},
	{"a long signature is broken", "fn long_function_name(first_parameter: Int, second_parameter: Text, third_parameter: List<Int>) with fs::read + net::fetch -> Result<Int, Text> {\n}\n",
		"fn long_function_name(\n    first_parameter: Int,\n    second_parameter: Text,\n    third_parameter: List<Int>\n) with fs::read + net::fetch -> Result<Int, Text> {}\n"},
}

func TestFormat(t *testing.T) {
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := must(t, c.in)
			if got != c.want {
				t.Errorf("source:\n%s\ngot:\n%s\nwant:\n%s", c.in, got, c.want)
			}

			if again := must(t, got); again != got {
				t.Errorf("not stable: formatting it again gives\n%s", again)
			}
		})
	}
}

// The formatter does not change what a program means, whatever the program: Source reads what it wrote and gives an error if it is not
// the same. So all that is needed here is programs, and that nothing is an error and that a formatted program is not formatted again.
func corpus(t *testing.T) map[string]string {
	t.Helper()

	out := map[string]string{}

	for name, src := range samples.Accepted {
		out["sample "+name] = src
	}

	filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(path, ".cov") {
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			out[path] = string(b)
		}

		return nil
	})

	doc, err := os.ReadFile("../LANGUAGE.md")
	if err != nil {
		t.Fatal(err)
	}

	for i, m := range regexp.MustCompile("(?s)```covenant\n(.*?)```").FindAllStringSubmatch(string(doc), -1) {
		out["LANGUAGE.md block "+string(rune('A'+i%26))+string(rune('a'+i/26))] = m[1]
	}

	return out
}

func TestEverythingWeHaveIsFormatted(t *testing.T) {
	for name, src := range corpus(t) {
		out, err := Source(src)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}

		if again := must(t, out); again != out {
			t.Errorf("%s: not stable:\n%s", name, again)
		}

		for i, line := range strings.Split(out, "\n") {
			if len(line) > Width+20 && !strings.Contains(line, "//") && !strings.Contains(line, "\"") {
				t.Logf("%s: line %d is %d columns", name, i+1, len(line))
			}
		}
	}
}

func TestTheFilesOfTheRepositoryAreFormatted(t *testing.T) {
	for name, src := range corpus(t) {
		if !strings.HasSuffix(name, ".cov") {
			continue
		}

		if out := must(t, src); out != src {
			t.Errorf("%s is not formatted:\n%s", name, out)
		}
	}
}

func TestASourceThatIsNotCovenant(t *testing.T) {
	_, err := Source("fn f( {\n")
	if err == nil || errors.Is(err, ErrBug) {
		t.Fatalf("a syntax error is the error of the source: %v", err)
	}

	if !strings.Contains(err.Error(), "1:") {
		t.Errorf("the error says where: %v", err)
	}
}

func FuzzFormat(f *testing.F) {
	for _, src := range samples.Accepted {
		f.Add(src)
	}

	for _, c := range cases {
		f.Add(c.in)
	}

	f.Fuzz(func(t *testing.T, src string) {
		out, err := Source(src)
		if err != nil {
			if errors.Is(err, ErrBug) {
				t.Fatalf("%v\n%q", err, src)
			}

			return
		}

		again, err := Source(out)
		if err != nil || again != out {
			t.Fatalf("not stable: %v\n%q\n%q\n%q", err, src, out, again)
		}
	})
}

func TestAMapKeyThatEndsInAName(t *testing.T) {
	got := must(t, "fn f() {\nlet a = {(x % y): 1, x.y: 2, A::B: 3, \"k\": 4, f(1): 5, c: 6}\n}\n")
	want := "fn f() {\n    let a = {(x % y): 1, x.y: 2, A::B: 3, \"k\": 4, f(1): 5, c: 6}\n}\n"

	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
