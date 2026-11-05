package covenant

import (
	"bytes"
	"math/rand"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/almerlucke/exbana/v3/examples/covenant/grammar"
	"github.com/almerlucke/exbana/v3/examples/covenant/internal/samples"
	"github.com/almerlucke/exbana/v3/examples/covenant/parser"
	"github.com/almerlucke/exbana/v3/examples/covenant/token"
)

func TestGeneratedCodeIsUpToDate(t *testing.T) {
	want, err := grammar.Source()
	if err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile("parser/parser.go")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, want) {
		t.Fatal("parser/parser.go is not what the grammar generates: run go generate in examples/covenant")
	}
}

func TestParse(t *testing.T) {
	for name, src := range samples.Accepted {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(src); err != nil {
				t.Fatalf("%v\n%s", err, src)
			}
		})
	}

	for name, src := range samples.Rejected {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(src); err == nil {
				t.Fatalf("parses, and should not:\n%s", src)
			}
		})
	}
}

func TestErrorsSayWhereAndWhat(t *testing.T) {
	for _, c := range []struct {
		src, at, contains string
	}{
		{"fn f() {\n    let a = 1 let b = 2\n}\n", "2:15", `unexpected "let"`},
		{"fn f() {\n    let a = 1;\n}\n", "2:14", "semicolons"},
		{"fn f() -> Int\n", "1:14", "unexpected end of line"},
		{"fn f() {\n    return a\n        + b\n}\n", "3:9", `unexpected "+"`},
		{"fn f() {\n    let x : Int = 1\n}\n", "2:11", `unexpected ":"`},
		{"fn f() {\n    let a = fn(x)\n}\n", "2:18", "unexpected end of line, expected `->`, `=>`, `{`"},
		{"fn f() {", "1:9", "unexpected end of input"},
		{"fn f() {\n    let é = 1\n}\n", "2:9", "ASCII"},
		{"struct P {\n    x\n}\n", "2:5", "unexpected \"x\", expected `}`, `name:`"},
		{"fn f() {\n    let p = Point { x: 1 }\n}\n", "2:19", `unexpected "{"`},
	} {
		_, err := Parse(c.src)
		if err == nil {
			t.Errorf("%q parses", c.src)
			continue
		}

		if !strings.HasPrefix(err.Error(), c.at+": ") || !strings.Contains(err.Error(), c.contains) {
			t.Errorf("%q\n got %v\nwant at %s, with %q", c.src, err, c.at, c.contains)
		}
	}
}

func TestTreeHasTheRules(t *testing.T) {
	f, err := Parse(samples.Accepted["function with promises and try"])
	if err != nil {
		t.Fatal(err)
	}

	var fns []string

	for item := range f.Tree.Top().File().Items() {
		fn, ok := item.Fn()
		if !ok {
			t.Fatal("an item that is not a function")
		}

		begin, end := fn.Name().Span()
		fns = append(fns, f.Source[f.Tokens[begin].Begin:f.Tokens[end-1].End])

		if got := len(slices.Collect(fn.Block().Requireses())); got != 1 {
			t.Errorf("%d requires clauses", got)
		}

		if got := len(slices.Collect(fn.Block().Examples())); got != 2 {
			t.Errorf("%d examples", got)
		}
	}

	if !slices.Equal(fns, []string{"parse_port"}) {
		t.Errorf("functions %q", fns)
	}
}

// The generated parser and the grammar, which tries every way, say the same about tokens that are nearly right: it is what
// shows that the parser matches what the grammar matches.
func TestParserIsTheGrammar(t *testing.T) {
	rng := rand.New(rand.NewSource(20260105))
	all := []token.Kind{}

	for k := token.Kind(1); !strings.HasPrefix(k.String(), "token("); k++ {
		all = append(all, k)
	}

	agree := func(kinds []int) bool {
		_, err := parser.NewCovenantParser().Parse(kinds)
		return (err == nil) == grammar.Accepts(kinds)
	}

	accepted, rejected := 0, 0

	for name, src := range samples.Accepted {
		tokens, err := token.Lex(src)
		if err != nil {
			t.Fatal(err)
		}

		base := token.Kinds(tokens)

		for range 120 {
			kinds := slices.Clone(base)

			// a few changes: take a token out, put one in, change one, or swap two
			for range 1 + rng.Intn(3) {
				if len(kinds) == 0 {
					break
				}

				i := rng.Intn(len(kinds))

				switch rng.Intn(4) {
				case 0:
					kinds = slices.Delete(kinds, i, i+1)
				case 1:
					kinds = slices.Insert(kinds, i, int(all[rng.Intn(len(all))]))
				case 2:
					kinds[i] = int(all[rng.Intn(len(all))])
				default:
					j := rng.Intn(len(kinds))
					kinds[i], kinds[j] = kinds[j], kinds[i]
				}
			}

			if !agree(kinds) {
				t.Fatalf("%s: the parser and the grammar differ on %v", name, kinds)
			}

			if _, err := parser.NewCovenantParser().Parse(kinds); err == nil {
				accepted++
			} else {
				rejected++
			}
		}
	}

	// the changes have to make both kinds of tokens, or the test shows nothing
	if accepted < 100 || rejected < 1000 {
		t.Fatalf("%d accepted, %d rejected", accepted, rejected)
	}

	t.Logf("%d accepted, %d rejected", accepted, rejected)
}

// Whatever source the fuzzer makes, the generated parser and the grammar, which tries every way, say the same.
func FuzzGeneratedParserAgainstGrammar(f *testing.F) {
	for _, src := range samples.Accepted {
		f.Add(src)
	}

	for _, src := range samples.Rejected {
		f.Add(src)
	}

	f.Fuzz(func(t *testing.T, src string) {
		tokens, err := token.Lex(src)
		if err != nil || len(tokens) > 300 {
			return
		}

		kinds := token.Kinds(tokens)
		_, perr := parser.NewCovenantParser().Parse(kinds)

		if (perr == nil) != grammar.Accepts(kinds) {
			t.Fatalf("the parser says %v, the grammar says the opposite, for %q", perr, src)
		}
	})
}
