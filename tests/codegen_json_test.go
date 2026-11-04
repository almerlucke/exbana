package tests

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/codegen"
	"github.com/almerlucke/exbana/v3/examples/jsongen/grammar"
	"github.com/almerlucke/exbana/v3/internal/gentest"
	"github.com/almerlucke/exbana/v3/optimize"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

// jsonGrammar is JSON written the way the grammar reads, with the default backtracking repetitions and alternations.
func jsonGrammar() pat { return grammar.JSON() }

func jsonDocument(n int) string {
	var sb strings.Builder

	sb.WriteString("[")

	for i := range n {
		if i > 0 {
			sb.WriteString(", ")
		}

		fmt.Fprintf(&sb, `{"id": %d, "name": "item %d", "tags": ["a", "b", "c"], "price": %d.5, "ok": true, "none": null}`, i, i, i)
	}

	sb.WriteString("]")

	return sb.String()
}

// flatten lists the rule nodes of a match in the order they began: the rule, where it starts and ends, how deep it is
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

type jsonCase struct {
	In   string
	Ok   bool
	End  int
	Tree string
	Err  string
}

func TestGeneratedJSONParserBuildsTheSameTreeAsTheGrammar(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a generated parser")
	}

	original, optimized := jsonGrammar(), jsonGrammar()

	res, err := optimize.Optimize[rune, runes.Pos](optimized, optimize.Options{})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Blocked()) != 0 {
		t.Fatalf("not everything is deterministic:\n%v", res)
	}

	src, err := codegen.Generate[rune, runes.Pos](optimized, codegen.Config[rune, runes.Pos]{Package: "probe", Name: "JSON"})
	if err != nil {
		t.Fatal(err)
	}

	docs := []string{
		jsonDocument(20), `[]`, `{}`, ` [ 1 , 2 ] `, `{"a": [1, {"b": null}], "c": "x\"y\u0041", "d": -0.5e+10}`, `"text"`, `-12`, ` true `,
		// not JSON
		``, `[1,]`, `{"a" 1}`, `[1 2]`, `tru`, `"open`, `01`, `{"a":}`, `[`, `{"a": 1,`, `[1] x`, "\n\n  [1,\n 2,\n  x]",
	}

	var cases []jsonCase

	for _, doc := range docs {
		c := jsonCase{In: doc}

		// the generated parser must agree with the grammar as it was written, and with the optimized one
		for i, g := range []pat{original, optimized} {
			rd := newRunes(doc)
			log := runes.NewFarthestLog(rd)

			ok, m, perr := ebnf.Parse[rune, runes.Pos](g, ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(log))
			if perr != nil {
				t.Fatal(perr)
			}

			if i == 0 {
				c.Ok = ok
				if ok {
					c.End, c.Tree = m.End.Index, flatten(m)
				} else if e := log.Err(); e != nil {
					c.Err = e.Error()
				}

				continue
			}

			if ok != c.Ok || (ok && (m.End.Index != c.End || flatten(m) != c.Tree)) {
				t.Fatalf("the optimized grammar differs from the original on %.40q", doc)
			}
		}

		cases = append(cases, c)
	}

	data, _ := json.Marshal(cases)

	gentest.Run(t, map[string]string{"json.go": string(src), "cases.json": string(data), "json_test.go": jsonHarness})
}

const jsonHarness = `package probe

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

type jsonCase struct {
	In   string
	Ok   bool
	End  int
	Tree string
	Err  string
}

// flatten lists the rule nodes in the order they began: rule, start-end, depth
func flatten(tree *JSONTree) string {
	var sb strings.Builder

	var ends []int // where the nodes we are inside of end, innermost last

	for i, n := range tree.Nodes {
		for len(ends) > 0 && ends[len(ends)-1] <= i {
			ends = ends[:len(ends)-1]
		}

		fmt.Fprintf(&sb, "%s %d-%d %d\n", n.Rule, n.Begin, n.End, len(ends))

		ends = append(ends, i+int(n.Size))
	}

	return sb.String()
}

func TestJSON(t *testing.T) {
	data, _ := os.ReadFile("cases.json")

	var cases []jsonCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}

	p := NewJSONParser()

	for _, c := range cases {
		tree, err := p.ParseString(c.In)

		if (err == nil) != c.Ok {
			t.Errorf("%.40q: matched=%v, the grammar says %v (%v)", c.In, err == nil, c.Ok, err)
			continue
		}

		if err != nil {
			if c.Err != "" && err.Error() != c.Err {
				t.Errorf("%.40q:\n  generated %q\n  grammar   %q", c.In, err.Error(), c.Err)
			}

			continue
		}

		if tree.End != c.End {
			t.Errorf("%.40q: ends at %d, the grammar says %d", c.In, tree.End, c.End)
		}

		if got := flatten(tree); got != c.Tree {
			t.Errorf("%.40q: the trees differ:\n%s\nwant:\n%s", c.In, got, c.Tree)
		}
	}
}
`

// TestGeneratedJSONParserSpeed is not run by default (EXBANA_BENCH=1 go test ./tests -run GeneratedJSONParserSpeed -v): it
// compiles the generated parser into a module and runs a benchmark there, next to the standard library for scale.
func TestGeneratedJSONParserSpeed(t *testing.T) {
	if os.Getenv("EXBANA_BENCH") == "" {
		t.Skip("set EXBANA_BENCH=1 to run the benchmark")
	}

	optimized := jsonGrammar()
	if _, err := optimize.Optimize[rune, runes.Pos](optimized, optimize.Options{}); err != nil {
		t.Fatal(err)
	}

	src, err := codegen.Generate[rune, runes.Pos](optimized, codegen.Config[rune, runes.Pos]{Package: "probe", Name: "JSON"})
	if err != nil {
		t.Fatal(err)
	}

	textSrc, err := codegen.Generate[rune, runes.Pos](optimized, codegen.Config[rune, runes.Pos]{Package: "probe", Name: "Text", UTF8: true})
	if err != nil {
		t.Fatal(err)
	}

	doc := jsonDocument(2000)

	bench := `package probe

import (
	"encoding/json"
	"os"
	"testing"
)

func document(b *testing.B) string {
	data, err := os.ReadFile("doc.json")
	if err != nil {
		b.Fatal(err)
	}

	return string(data)
}

func BenchmarkGenerated(b *testing.B) {
	doc := []rune(document(b))
	p := NewJSONParser()

	b.ReportAllocs()
	b.SetBytes(int64(len(doc)))

	for b.Loop() {
		if _, err := p.Parse(doc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGeneratedFromString(b *testing.B) {
	doc := document(b)
	p := NewTextParser()

	b.ReportAllocs()
	b.SetBytes(int64(len(doc)))

	for b.Loop() {
		if _, err := p.Parse(doc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGeneratedFromStringWithTheConversion(b *testing.B) {
	doc := document(b)

	b.ReportAllocs()
	b.SetBytes(int64(len(doc)))

	for b.Loop() {
		if _, err := NewJSONParser().ParseString(doc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStdlibValid(b *testing.B) {
	doc := []byte(document(b))

	b.ReportAllocs()
	b.SetBytes(int64(len(doc)))

	for b.Loop() {
		if !json.Valid(doc) {
			b.Fatal("invalid")
		}
	}
}

func BenchmarkStdlibUnmarshal(b *testing.B) {
	doc := []byte(document(b))

	b.ReportAllocs()
	b.SetBytes(int64(len(doc)))

	for b.Loop() {
		var v any
		if err := json.Unmarshal(doc, &v); err != nil {
			b.Fatal(err)
		}
	}
}
`

	out := gentest.RunWith(t, map[string]string{"json.go": string(src), "text.go": string(textSrc), "doc.json": doc, "bench_test.go": bench}, "-run", "^$", "-bench", ".", "-benchtime", "30x")
	t.Logf("document of %d bytes:\n%s", len(doc), out)
}
