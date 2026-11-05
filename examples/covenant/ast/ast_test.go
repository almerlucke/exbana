package ast_test

import (
	"testing"

	"github.com/almerlucke/exbana/v3/examples/covenant"
	"github.com/almerlucke/exbana/v3/examples/covenant/ast"
	"github.com/almerlucke/exbana/v3/examples/covenant/internal/samples"
)

func build(t *testing.T, src string) *ast.File {
	t.Helper()

	f, err := covenant.Parse(src)
	if err != nil {
		t.Fatalf("%v\n%s", err, src)
	}

	return f.AST()
}

// expression is the tree of an expression, from a function that is only that.
func expression(t *testing.T, src string) string {
	t.Helper()

	f := build(t, "fn f() {\n    let x = "+src+"\n}\n")

	return ast.Dump(f.Items[0].Decl.(*ast.Fn).Body.Stmts[0].(*ast.Let).Value)
}

func TestExpressions(t *testing.T) {
	for src, want := range map[string]string{
		"1":                      `(Lit Text:1)`,
		"1.5":                    `(Lit Kind:1 Text:1.5)`,
		`"a b"`:                  `(Lit Kind:2 Text:"\"a b\"")`,
		"true":                   `(Lit Kind:3 Text:true)`,
		"a":                      `(Name Path:[a])`,
		"a::b::c":                `(Name Path:[a b c])`,
		"self":                   `(Name Path:[self])`,
		"a + b * c":              `(Binary Op:+ L:(Name Path:[a]) R:(Binary Op:* L:(Name Path:[b]) R:(Name Path:[c])))`,
		"a - b - c":              `(Binary Op:- L:(Binary Op:- L:(Name Path:[a]) R:(Name Path:[b])) R:(Name Path:[c]))`,
		"(a + b) * c":            `(Binary Op:* L:(Binary Op:+ L:(Name Path:[a]) R:(Name Path:[b])) R:(Name Path:[c]))`,
		"a < b":                  `(Binary Op:< L:(Name Path:[a]) R:(Name Path:[b]))`,
		"a <= b":                 `(Binary Op:<= L:(Name Path:[a]) R:(Name Path:[b]))`,
		"a == b":                 `(Binary Op:== L:(Name Path:[a]) R:(Name Path:[b]))`,
		"a and b and c":          `(Binary Op:and L:(Binary Op:and L:(Name Path:[a]) R:(Name Path:[b])) R:(Name Path:[c]))`,
		"a or b":                 `(Binary Op:or L:(Name Path:[a]) R:(Name Path:[b]))`,
		"not a":                  `(Unary Op:not X:(Name Path:[a]))`,
		"-a":                     `(Unary Op:- X:(Name Path:[a]))`,
		"try f()":                `(Unary Op:try X:(Call Fun:(Name Path:[f])))`,
		"-a * b":                 `(Binary Op:* L:(Unary Op:- X:(Name Path:[a])) R:(Name Path:[b]))`,
		"not a and b":            `(Binary Op:and L:(Unary Op:not X:(Name Path:[a])) R:(Name Path:[b]))`,
		"not a == b":             `(Unary Op:not X:(Binary Op:== L:(Name Path:[a]) R:(Name Path:[b])))`,
		"f(1, x: 2)":             `(Call Fun:(Name Path:[f]) Args:[(Arg Value:(Lit Text:1)) (Arg Label:x Value:(Lit Text:2))])`,
		"a.b.c(d)":               `(Call Fun:(Member X:(Member X:(Name Path:[a]) Name:b) Name:c) Args:[(Arg Value:(Name Path:[d]))])`,
		"a.b().c":                `(Member X:(Call Fun:(Member X:(Name Path:[a]) Name:b)) Name:c)`,
		"p with { x: 1 }":        `(Update X:(Name Path:[p]) Fields:[(FieldInit Name:x Value:(Lit Text:1))])`,
		"[1, 2]":                 `(List Elems:[(Lit Text:1) (Lit Text:2)])`,
		"[]":                     `(List)`,
		`{"a": 1, b: 2}`:         `(Map Entries:[(Entry Key:(Lit Kind:2 Text:"\"a\"") Value:(Lit Text:1)) (Entry Key:(Name Path:[b]) Value:(Lit Text:2))])`,
		"{:}":                    `(Map)`,
		"fn(x) => x":             `(Closure Params:[(ClosureParam Name:x)] Value:(Name Path:[x]))`,
		"fn(x: Int) -> Int => x": `(Closure Params:[(ClosureParam Name:x Type:(TypeName Path:[Int]))] Ret:(TypeName Path:[Int]) Value:(Name Path:[x]))`,
		"fn(x) {\n return x\n }": `(Closure Params:[(ClosureParam Name:x)] Body:(Block Stmts:[(Return Value:(Name Path:[x]))]))`,
		"(fn(x) => x)(1)":        `(Call Fun:(Closure Params:[(ClosureParam Name:x)] Value:(Name Path:[x])) Args:[(Arg Value:(Lit Text:1))])`,
	} {
		if got := expression(t, src); got != want {
			t.Errorf("%s\n got %s\nwant %s", src, got, want)
		}
	}
}

func TestMatchAndIf(t *testing.T) {
	got := expression(t, "match s {\n Circle(r) => r\n Shape::Empty => 0\n Other => 1\n _ => 2\n 3 => 4\n }")
	want := `(Match X:(Name Path:[s]) Arms:[` +
		`(Arm Pattern:(PatName Path:[Circle] HasArgs:true Args:[(PatName Path:[r])]) Value:(Name Path:[r])) ` +
		`(Arm Pattern:(PatName Path:[Shape Empty]) Value:(Lit Text:0)) ` +
		`(Arm Pattern:(PatName Path:[Other]) Value:(Lit Text:1)) ` +
		`(Arm Pattern:(PatWild) Value:(Lit Text:2)) ` +
		`(Arm Pattern:(PatLit Lit:(Lit Text:3)) Value:(Lit Text:4))])`

	if got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}

	if got := expression(t, "if a {\n f()\n } else if b {\n g()\n } else {\n h()\n }"); got !=
		`(If Cond:(Name Path:[a]) Then:(Block Stmts:[(ExprStmt X:(Call Fun:(Name Path:[f])))]) Else:(If Cond:(Name Path:[b]) `+
			`Then:(Block Stmts:[(ExprStmt X:(Call Fun:(Name Path:[g])))]) Else:(Block Stmts:[(ExprStmt X:(Call Fun:(Name Path:[h])))])))` {
		t.Errorf("if: %s", got)
	}
}

func TestDeclarations(t *testing.T) {
	f := build(t, `
use a::b as c
pub struct P<T> { x: T, y: List<T> }
enum E {
    A(n: Int)
    B
}
trait Show<T> {
    fn show(self, f: Fn(T) with io + log -> Text) with io -> Text
}
impl Show for P {
    pub fn show(self) -> Text {
        return "p"
    }
    fn hidden() {
    }
}
impl P {
}
const k: Int = 1
type Q<T> = Map<Text, T>
effects G = a::b + c
fn g<T, U: Area + Show>(x: T, self) with G -> Int {
    requires true
    example g(1) == 2
    var n = 0
    let m: Int = 1
    for a, b in xs {
    }
    while true {
    }
    n = 1
}
`)

	if len(f.Uses) != 1 || ast.Dump(f.Uses[0]) != `(Use Path:[a b] Alias:c)` {
		t.Errorf("use %s", ast.Dump(f.Uses))
	}

	var dumps []string
	for _, it := range f.Items {
		dumps = append(dumps, ast.Dump(it))
	}

	want := []string{
		`(Item Pub:true Decl:(Struct Name:P Generics:[(Generic Name:T)] Fields:[(Field Name:x Type:(TypeName Path:[T])) (Field Name:y Type:(TypeName Path:[List] Args:[(TypeName Path:[T])]))]))`,
		`(Item Decl:(Enum Name:E Variants:[(Variant Name:A Fields:[(Field Name:n Type:(TypeName Path:[Int]))]) (Variant Name:B)]))`,
		`(Item Decl:(Trait Name:Show Generics:[(Generic Name:T)] Methods:[(Fn Name:show Params:[(Param Name:self Self:true) (Param Name:f Type:(TypeFn Params:[(TypeName Path:[T])] Effects:[(Path Names:[io]) (Path Names:[log])] Ret:(TypeName Path:[Text])))] Effects:[(Path Names:[io])] Ret:(TypeName Path:[Text]))]))`,
		`(Item Decl:(Impl Trait:(TypeName Path:[Show]) Type:(TypeName Path:[P]) Methods:[` +
			`(Fn Name:show Pub:true Params:[(Param Name:self Self:true)] Ret:(TypeName Path:[Text]) Body:(Block Stmts:[(Return Value:(Lit Kind:2 Text:"\"p\""))])) ` +
			`(Fn Name:hidden Body:(Block))]))`,
		`(Item Decl:(Impl Type:(TypeName Path:[P])))`,
		`(Item Decl:(Const Name:k Type:(TypeName Path:[Int]) Value:(Lit Text:1)))`,
		`(Item Decl:(Alias Name:Q Generics:[(Generic Name:T)] Type:(TypeName Path:[Map] Args:[(TypeName Path:[Text]) (TypeName Path:[T])])))`,
		`(Item Decl:(Group Name:G Effects:[(Path Names:[a b]) (Path Names:[c])]))`,
	}

	for i, w := range want {
		if i >= len(dumps) || dumps[i] != w {
			got := "missing"
			if i < len(dumps) {
				got = dumps[i]
			}

			t.Errorf("item %d\n got %s\nwant %s", i, got, w)
		}
	}

	fn := f.Items[len(f.Items)-1].Decl.(*ast.Fn)
	if got := ast.Dump(fn.Generics); got != `[(Generic Name:T) (Generic Name:U Bounds:[(Path Names:[Area]) (Path Names:[Show])])]` {
		t.Errorf("generics %s", got)
	}

	if got := ast.Dump(fn.Effects); got != `[(Path Names:[G])]` {
		t.Errorf("effects %s", got)
	}

	b := fn.Body
	if len(b.Requires) != 1 || len(b.Examples) != 1 || len(b.Stmts) != 5 {
		t.Fatalf("body %s", ast.Dump(b))
	}

	if got := ast.Dump(b.Stmts[0]); got != `(Var Name:n Value:(Lit Text:0))` {
		t.Errorf("var %s", got)
	}

	if got := ast.Dump(b.Stmts[1]); got != `(Let Name:m Type:(TypeName Path:[Int]) Value:(Lit Text:1))` {
		t.Errorf("let %s", got)
	}

	if got := ast.Dump(b.Stmts[4]); got != `(Assign Target:(Name Path:[n]) Value:(Lit Text:1))` {
		t.Errorf("assign %s", got)
	}
}

func TestFunctionTypes(t *testing.T) {
	f := build(t, "fn f(g: Fn(Int, Fn() -> Int) with a + b -> Fn(Int) -> Text, h: Fn()) {\n}\n")
	fn := f.Items[0].Decl.(*ast.Fn)

	want := `(TypeFn Params:[(TypeName Path:[Int]) (TypeFn Ret:(TypeName Path:[Int]))] Effects:[(Path Names:[a]) (Path Names:[b])] ` +
		`Ret:(TypeFn Params:[(TypeName Path:[Int])] Ret:(TypeName Path:[Text])))`

	if got := ast.Dump(fn.Params[0].Type); got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}

	if got := ast.Dump(fn.Params[1].Type); got != `(TypeFn)` {
		t.Errorf("Fn() is %s", got)
	}
}

func TestSpansAreInTheSource(t *testing.T) {
	src := "fn f(a: Int) -> Int {\n    return a + 1\n}\n"
	f := build(t, src)

	fn := f.Items[0].Decl.(*ast.Fn)
	ret := fn.Body.Stmts[0].(*ast.Return)

	for node, want := range map[ast.Node]string{
		fn:                        "fn f(a: Int) -> Int {\n    return a + 1\n}",
		fn.Params[0]:              "a: Int",
		fn.Ret:                    "Int",
		ret:                       "return a + 1",
		ret.Value:                 "a + 1",
		ret.Value.(*ast.Binary).L: "a",
		ret.Value.(*ast.Binary).R: "1",
	} {
		sp := node.At()
		if got := src[sp.Begin:sp.End]; got != want {
			t.Errorf("%T is %q, want %q", node, got, want)
		}
	}
}

// Every sample can be turned into a tree, and the tree has what the source has.
func TestEverySample(t *testing.T) {
	for name, src := range samples.Accepted {
		f := build(t, src)

		items := 0
		for _, line := range []string{"\nfn ", "\nstruct ", "\nenum ", "\ntrait ", "\nimpl ", "\nconst ", "\ntype ", "\neffects "} {
			items += countLines("\n"+src, line, "")
		}

		for _, line := range []string{"\npub fn ", "\npub struct ", "\npub enum ", "\npub trait ", "\npub effects "} {
			items += countLines("\n"+src, line, "")
		}

		if len(f.Items) != items {
			t.Errorf("%s: %d items, and the source has %d", name, len(f.Items), items)
		}
	}
}

func countLines(src, prefix, _ string) int {
	n := 0

	for i := 0; i+len(prefix) <= len(src); i++ {
		if src[i:i+len(prefix)] == prefix {
			n++
		}
	}

	return n
}
