package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/codegen"
	"github.com/almerlucke/exbana/v3/internal/gentest"
	"github.com/almerlucke/exbana/v3/optimize"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

// The generated parsers are tested against the grammars they were made from. Random grammars are made deterministic (every
// repetition possessive, every alternation orthogonal), a parser is generated for each, and both are run on random inputs:
// they must agree on whether the input matches, where the match ends, and what the syntax error says, which for the
// grammar is what ebnf.FarthestLog reports. All parsers are compiled together as one module with the go tool.

type codegenCase struct {
	G       int
	Grammar string
	In      string
	Ok      bool
	End     int
	Err     string // the message of the farthest failure, empty if the grammar logged none
}

func TestGeneratedParsersAgreeWithTheirGrammars(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles two hundred generated parsers")
	}

	rng := rand.New(rand.NewSource(424242))

	files := map[string]string{}

	var (
		cases    []codegenCase
		registry strings.Builder
	)

	matched, failed := 0, 0

	// random grammars, and grammars that repeat something that can match nothing, which the random ones avoid
	var roots []pat
	for range 200 {
		roots = append(roots, randomNode(rng, 3).p)
	}

	roots = append(roots, nullableItems()...)

	for i, root := range roots {
		start := rule.New[rune, runes.Pos]("start", root)

		res, err := optimize.Analyze[rune, runes.Pos](start, optimize.Options{})
		if err != nil {
			t.Fatal(err)
		}

		res.Force()

		var printedGrammar bytes.Buffer
		_ = start.Print(&printedGrammar)

		name := fmt.Sprintf("G%d", i)

		src, err := codegen.Generate[rune, runes.Pos](start, codegen.Config[rune, runes.Pos]{Package: "probe", Name: name})
		if err != nil {
			t.Fatalf("grammar %s: %v", printedGrammar.String(), err)
		}

		files[strings.ToLower(name)+".go"] = string(src)

		fmt.Fprintf(&registry, "\t{%q, func(s string) (bool, int, string, bool) {\n\t\ttree, err := New%sParser().ParseString(s)\n\t\tif err != nil {\n\t\t\treturn false, 0, err.Error(), false\n\t\t}\n\t\tn := tree.Nodes\n\t\treturn true, tree.End, \"\", len(n) == 1 && n[0].Begin == 0 && int(n[0].End) == tree.End && n[0].Size == 1\n\t}},\n", name, name)

		for range 14 {
			in := make([]rune, rng.Intn(9))
			for j := range in {
				in[j] = rune('a' + rng.Intn(3))
			}

			rd := newRunes(string(in))
			log := runes.NewFarthestLog(rd)

			ok, m, perr := ebnf.Parse[rune, runes.Pos](start, ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(log))
			if perr != nil {
				t.Fatal(perr)
			}

			c := codegenCase{G: i, Grammar: printedGrammar.String(), In: string(in), Ok: ok}

			if ok {
				c.End = m.End.Index
				matched++
			} else {
				if e := log.Err(); e != nil {
					c.Err = e.Error()
				}
				failed++
			}

			cases = append(cases, c)
		}
	}

	if matched < len(cases)/10 || failed < len(cases)/10 {
		t.Fatalf("%d matched and %d failed: the cases are too one sided", matched, failed)
	}

	data, _ := json.Marshal(cases)
	files["cases.json"] = string(data)
	files["probe_test.go"] = strings.Replace(codegenHarness, "/*REGISTRY*/", registry.String(), 1)

	out := gentest.Run(t, files)
	t.Logf("%d parsers, %d cases (%d matched, %d failed): %s", len(roots), len(cases), matched, failed, strings.TrimSpace(out))
}

// nullableItems are repetitions of items that can match nothing, with counts: how an empty iteration ends a repetition
// and counts towards its minimum is a detail that has to be the same in the generated code
func nullableItems() []pat {
	a, b := leafNode('a').p, leafNode('b').p

	return []pat{
		repR(repR(a, 0, 0), 0, 0),
		repR(repR(a, 0, 0), 2, 0),
		repR(repR(a, 0, 1), 3, 0),
		repR(repR(a, 0, 1), 0, 4),
		repR(concR(), 0, 0),
		repR(concR(), 2, 3),
		repR(altR(a, repR(b, 0, 0)), 2, 5),
		concR(repR(repR(a, 0, 1), 1, 0), b),
		concR(repR(endR(), 0, 0), a),
		repR(exceptR(repR(a, 0, 0), b), 1, 3),
	}
}

const codegenHarness = `package probe

import (
	"encoding/json"
	"os"
	"testing"
)

type testCase struct {
	G       int
	Grammar string
	In      string
	Ok      bool
	End     int
	Err     string
}

var parsers = []struct {
	name  string
	parse func(string) (ok bool, end int, err string, shapeOk bool)
}{
/*REGISTRY*/}

func TestAgainstTheGrammars(t *testing.T) {
	data, err := os.ReadFile("cases.json")
	if err != nil {
		t.Fatal(err)
	}

	var cases []testCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}

	bad := 0

	for _, c := range cases {
		ok, end, msg, shapeOk := parsers[c.G].parse(c.In)

		switch {
		case ok != c.Ok:
			t.Errorf("%s %s on %q: matched=%v, the grammar says %v", parsers[c.G].name, c.Grammar, c.In, ok, c.Ok)
			bad++
		case ok && (end != c.End || !shapeOk):
			t.Errorf("%s %s on %q: ends at %d (tree ok %v), the grammar says %d", parsers[c.G].name, c.Grammar, c.In, end, shapeOk, c.End)
			bad++
		case !ok && c.Err != "" && msg != c.Err:
			t.Errorf("%s %s on %q:\n  generated %q\n  grammar   %q", parsers[c.G].name, c.Grammar, c.In, msg, c.Err)
			bad++
		}

		if bad >= 10 {
			t.Fatal("too many differences")
		}
	}
}
`
