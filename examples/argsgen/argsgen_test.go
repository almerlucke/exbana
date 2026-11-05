package main

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/examples/argsgen/grammar"
	"github.com/almerlucke/exbana/v3/examples/argsgen/parser"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/readers/slice"
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
		t.Fatal("parser/parser.go is not what the grammar generates: run go generate in examples/argsgen")
	}
}

func TestOptimizerFindsAllButTwoPlaces(t *testing.T) {
	res, err := grammar.Analysis()
	if err != nil {
		t.Fatal(err)
	}

	// the two places where a flag and an argument could start the same, as far as a function that is opaque tells
	if got := len(res.Blocked()); got != 2 {
		t.Errorf("%d places blocked, want 2:\n%v", got, res)
	}
}

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want Invocation
	}{
		{[]string{"list"}, Invocation{Command: "list"}},
		{[]string{"list", "--json"}, Invocation{Command: "list", JSON: true}},
		{[]string{"remove", "a"}, Invocation{Command: "remove", Packages: []string{"a"}}},
		{[]string{"remove", "a", "b", "c"}, Invocation{Command: "remove", Packages: []string{"a", "b", "c"}}},
		{[]string{"install", "left-pad"}, Invocation{Command: "install", Packages: []string{"left-pad"}}},
		{
			[]string{"-v", "--config", "c.toml", "install", "--save", "-D", "--version", "1.2", "left-pad", "lodash"},
			Invocation{Verbose: true, Config: "c.toml", Command: "install", Save: true, Dev: true, Version: "1.2", Packages: []string{"left-pad", "lodash"}},
		},
		{[]string{"--verbose", "-v", "list"}, Invocation{Verbose: true, Command: "list"}},
		{[]string{"install", "--version", "install", "list"}, Invocation{Command: "install", Version: "install", Packages: []string{"list"}}}, // a value is any argument
	} {
		got, err := Parse(tc.args)
		if err != nil || !reflect.DeepEqual(*got, tc.want) {
			t.Errorf("%q = %+v, %v, want %+v", tc.args, got, err, tc.want)
		}
	}
}

func TestErrorsSayWhichArgumentAndWhatWasWanted(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, `the command line ends too soon: expected "-v", "--verbose", "--config", "install", "remove", "list"`},
		{[]string{"remove"}, `the command line ends too soon: expected argument`},
		{[]string{"install", "--amnd", "x"}, `argument 2, "--amnd", was not expected: expected "--save", "-D", "--version", argument`},
		{[]string{"list", "--json", "extra"}, `argument 3, "extra", was not expected: expected end of the arguments`},
		{[]string{"-v", "--config"}, `the command line ends too soon: expected argument`},
		{[]string{"install", ""}, `argument 2, "", was not expected: expected "--save", "-D", "--version", argument`}, // an empty argument is not one
		{[]string{"frobnicate"}, `argument 1, "frobnicate", was not expected: expected "-v", "--verbose", "--config", "install", "remove", "list"`},
	} {
		_, err := Parse(tc.args)
		if err == nil {
			t.Errorf("%q parsed", tc.args)
			continue
		}

		if err.Error() != tc.want {
			t.Errorf("%q:\n  got  %s\n  want %s", tc.args, err, tc.want)
		}
	}
}

// the tree of the generated parser, and of the grammar, as a list of rule, first and last argument, and depth
func flattenTree(tree *parser.PkgTree) string {
	var sb strings.Builder

	tree.Walk(func(node, depth int) bool {
		b, e := tree.Span(node)
		fmt.Fprintf(&sb, "%s %d-%d %d\n", tree.Rule(node), b, e, depth)

		return true
	})

	return sb.String()
}

func flatten(m *ebnf.Match[string, int]) string {
	var sb strings.Builder

	var walk func(m *ebnf.Match[string, int], depth int)

	walk = func(m *ebnf.Match[string, int], depth int) {
		if _, isRule := m.Pattern.(*rule.Rule[string, int]); isRule && m.ID() != "" {
			fmt.Fprintf(&sb, "%s %d-%d %d\n", m.ID(), m.Begin, m.End, depth)
			depth++
		}

		for _, c := range m.Components {
			walk(c, depth)
		}
	}

	walk(m, 0)

	return sb.String()
}

// agree parses with the generated parser and with the grammar as it was written, which backtracks, and fails if they differ
func agree(t testing.TB, p *parser.PkgParser, args []string) {
	t.Helper()

	rd := slice.New(args)
	log := slice.NewFarthestLog(rd, strconv.Quote)

	ok, m, err := ebnf.Parse[string, int](grammar.CLI(), ebnf.NewParseContext[string, int](rd).WithLogger(log))
	if err != nil {
		t.Fatal(err)
	}

	tree, perr := p.Parse(args)

	if (perr == nil) != ok {
		t.Fatalf("%q: the generated parser matched=%v (%v), the grammar %v", args, perr == nil, perr, ok)
	}

	if ok {
		if tree.End != m.End || flattenTree(tree) != flatten(m) {
			t.Fatalf("%q: the trees differ\ngenerated:\n%s\ngrammar:\n%s", args, flattenTree(tree), flatten(m))
		}

		return
	}

	if e := log.Err(); e != nil && e.Error() != perr.Error() {
		t.Fatalf("%q:\n  generated %q\n  grammar   %q", args, perr.Error(), e.Error())
	}
}

var vocabulary = []string{"-v", "--verbose", "--config", "install", "--save", "-D", "--version", "remove", "list", "--json", "pkg", "left-pad", "-x", "--", "-", "", "1.2"}

// TestAgreesWithTheGrammar compares the two on every command line of up to four arguments from the vocabulary, and on random
// longer ones. This is what checks the two places the optimizer could not, which were taken on our word.
func TestAgreesWithTheGrammar(t *testing.T) {
	p := parser.NewPkgParser()

	var all func(prefix []string, n int)

	count := 0

	all = func(prefix []string, n int) {
		agree(t, p, append([]string(nil), prefix...))
		count++

		if n == 0 {
			return
		}

		for _, v := range vocabulary {
			all(append(prefix, v), n-1)
		}
	}

	all(nil, 4)

	rng := rand.New(rand.NewSource(5))

	for range 20000 {
		args := make([]string, rng.Intn(10))
		for i := range args {
			args[i] = vocabulary[rng.Intn(len(vocabulary))]
		}

		agree(t, p, args)
		count++
	}

	t.Logf("%d command lines", count)
}

// FuzzGeneratedParserAgainstGrammar: on any arguments the generated parser and the grammar must give the same answer, the same
// tree and the same syntax error. The input is cut into arguments at the NUL characters.
func FuzzGeneratedParserAgainstGrammar(f *testing.F) {
	for _, seed := range []string{"install\x00--save\x00x", "-v\x00list", "remove", "", "list\x00--json\x00--json", "--config\x00install\x00install\x00list", "install\x00\x00x", "\x00"} {
		f.Add(seed)
	}

	p := parser.NewPkgParser()

	f.Fuzz(func(t *testing.T, input string) {
		agree(t, p, strings.Split(input, "\x00"))
	})
}
