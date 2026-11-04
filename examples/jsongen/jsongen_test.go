package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/examples/jsongen/grammar"
	"github.com/almerlucke/exbana/v3/examples/jsongen/parser"
	"github.com/almerlucke/exbana/v3/optimize"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/readers/runes"
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
		t.Fatal("parser/parser.go is not what the grammar generates: run go generate in examples/jsongen")
	}
}

func TestSummary(t *testing.T) {
	tree, err := parser.NewJSONParser().ParseString(`{"a": [1, 2, {"b": null}], "c": "x"}`)
	if err != nil {
		t.Fatal(err)
	}

	want := "20 nodes\n  array    1\n  literal  1\n  member   3\n  number   2\n  object   2\n  string   4\n  value    7\n"
	if got := Summary(tree); got != want {
		t.Errorf("summary:\n%s\nwant:\n%s", got, want)
	}
}

// flatten lists the rule nodes of the grammar's match in the order they began: rule, start-end, depth
func flatten(m *ebnf.Match[rune, runes.Pos]) string {
	var sb strings.Builder

	var walk func(m *ebnf.Match[rune, runes.Pos], depth int)

	walk = func(m *ebnf.Match[rune, runes.Pos], depth int) {
		if _, isRule := m.Pattern.(*rule.Rule[rune, runes.Pos]); isRule && m.ID() != "" {
			fmt.Fprintf(&sb, "%s %d-%d %d\n", m.ID(), m.Begin.Index, m.End.Index, depth)
			depth++
		}

		for _, c := range m.Components {
			walk(c, depth)
		}
	}

	walk(m, 0)

	return sb.String()
}

func flattenTree(tree *parser.JSONTree) string {
	var sb strings.Builder

	var ends []int

	for i, n := range tree.Nodes {
		for len(ends) > 0 && ends[len(ends)-1] <= i {
			ends = ends[:len(ends)-1]
		}

		fmt.Fprintf(&sb, "%s %d-%d %d\n", n.Rule, n.Begin, n.End, len(ends))
		ends = append(ends, i+int(n.Size))
	}

	return sb.String()
}

var grammarOnce = func() ebnf.Pattern[rune, runes.Pos] {
	g := grammar.JSON()
	if _, err := optimize.Optimize[rune, runes.Pos](g, optimize.Options{}); err != nil {
		panic(err)
	}

	return g
}()

// agree parses with the generated parser and with the grammar, and fails if they differ
func agree(t testing.TB, p *parser.JSONParser, input string) {
	t.Helper()

	rd, err := runes.New(strings.NewReader(input))
	if err != nil {
		t.Skip()
	}

	// the runes reader makes line endings uniform, the generated parser reads what it is given
	if strings.ContainsRune(input, '\r') {
		t.Skip()
	}

	log := runes.NewFarthestLog(rd)

	ok, m, perr := ebnf.Parse[rune, runes.Pos](grammarOnce, ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(log))
	if perr != nil {
		t.Fatal(perr)
	}

	tree, err := p.ParseString(input)

	if (err == nil) != ok {
		t.Fatalf("%.60q: the generated parser matched=%v (%v), the grammar %v", input, err == nil, err, ok)
	}

	if ok {
		if tree.End != m.End.Index || flattenTree(tree) != flatten(m) {
			t.Fatalf("%.60q: the trees differ\ngenerated:\n%s\ngrammar:\n%s", input, flattenTree(tree), flatten(m))
		}

		return
	}

	if e := log.Err(); e != nil && e.Error() != err.Error() {
		t.Fatalf("%.60q:\n  generated %q\n  grammar   %q", input, err.Error(), e.Error())
	}
}

func TestAgreesWithTheGrammarOnDocuments(t *testing.T) {
	p := parser.NewJSONParser()

	for _, doc := range []string{
		`{}`, `[]`, `[1, 2, 3]`, ` { "a" : [ true , false , null ] } `, `"x\ny\u00e9"`, `-0.5e+10`, `[[[[[[]]]]]]`,
		`{"a": {"b": {"c": [1, {"d": "e"}]}}}`,
		``, `[`, `[1,]`, `{"a" 1}`, `{"a":}`, `tru`, `"open`, `01`, `[1 2]`, `{"a": 1,`, `[1] x`, "[1,\n2,\n x]", "\n\n{",
	} {
		agree(t, p, doc)
	}
}

// FuzzGeneratedParserAgainstGrammar: on any input the generated parser and the grammar it was generated from must give the
// same answer, the same tree and the same syntax error.
func FuzzGeneratedParserAgainstGrammar(f *testing.F) {
	for _, seed := range []string{`{"a": [1, 2.5e3, null, true, "x"]}`, `[1,]`, ``, `{`, `"\u00zz"`, "[\n1,\n2\n", `-`, `[0123]`, `{"k": {"k": {}}}`} {
		f.Add(seed)
	}

	p := parser.NewJSONParser()

	f.Fuzz(func(t *testing.T, input string) {
		agree(t, p, input)
	})
}
