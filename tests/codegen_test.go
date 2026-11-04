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
	In      []byte // may be invalid UTF-8
	Ok      bool
	End     int
	Err     string // the message of the farthest failure, empty if the grammar logged none

	// what ParseAll must do for the grammars that recover: skip to the next c after the farthest failure
	HasRecovery  bool
	RecNoTree    bool
	RecEnd       int
	RecNodes     int
	RecErrs      []string
	RecErrsKnown bool
}

func TestGeneratedParsersAgreeWithTheirGrammars(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles two hundred generated parsers")
	}

	rng := rand.New(rand.NewSource(424242))

	files := map[string]string{}

	var (
		cases      []codegenCase
		registry   strings.Builder
		recoverers strings.Builder
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

		// the same parser reading a slice of runes and reading UTF-8 text
		for _, utf8 := range []bool{false, true} {
			name := fmt.Sprintf("G%d", i)
			parse := "New%sParser().ParseString(s)"
			end := "tree.End"

			if utf8 {
				name = fmt.Sprintf("U%d", i)
				parse = "New%sParser().Parse(s)"
				end = "utf8.RuneCountInString(s[:tree.End])" // the grammar counts characters, the parser bytes
			}

			src, err := codegen.Generate[rune, runes.Pos](start, codegen.Config[rune, runes.Pos]{Package: "probe", Name: name, UTF8: utf8})
			if err != nil {
				t.Fatalf("grammar %s: %v", printedGrammar.String(), err)
			}

			files[strings.ToLower(name)+".go"] = string(src)

			fmt.Fprintf(&registry, "\t{%q, func(s string) (bool, int, string, bool) {\n\t\ttree, err := "+parse+"\n\t\tif err != nil {\n\t\t\treturn false, 0, err.Error(), false\n\t\t}\n\t\tn := tree.Nodes\n\t\treturn true, "+end+", \"\", len(n) == 1 && n[0].Begin == 0 && int(n[0].End) == tree.End && n[0].Size == 1\n\t}},\n", name, name)
		}

		// the first hundred also get a parser that recovers from an error in the grammar: it skips to the next c
		if i < 100 {
			rname := fmt.Sprintf("R%d", i)

			rsrc, err := codegen.Generate[rune, runes.Pos](start, codegen.Config[rune, runes.Pos]{
				Package: "probe", Name: rname,
				Recover: map[string]codegen.Recovery[rune, runes.Pos]{"start": {Until: leafNode('c').p, Consume: true}},
			})
			if err != nil {
				t.Fatalf("grammar %s: %v", printedGrammar.String(), err)
			}

			files[strings.ToLower(rname)+".go"] = string(rsrc)

			fmt.Fprintf(&recoverers, "\t%d: func(s string) (bool, int, int, []string) {\n\t\ttree, errs := New%sParser().ParseAll([]rune(s))\n\t\tvar msgs []string\n\t\tfor _, e := range errs {\n\t\t\tmsgs = append(msgs, e.Error())\n\t\t}\n\t\tif tree == nil {\n\t\t\treturn true, 0, 0, msgs\n\t\t}\n\t\treturn false, tree.End, len(tree.Nodes), msgs\n\t},\n", i, rname)
		}

		for range 14 {
			// mostly the characters the grammars know, with some that take several bytes, a byte that is not text, and a line break
			var in []byte
			for range rng.Intn(9) {
				switch r := rng.Intn(14); {
				case r < 9:
					in = append(in, byte('a'+r%3))
				default:
					in = append(in, []string{"é", "€", "😀", "\xff", "\n"}[r-9]...)
				}
			}

			rd := newRunes(string(in))
			log := runes.NewFarthestLog(rd)

			ok, m, perr := ebnf.Parse[rune, runes.Pos](start, ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(log))
			if perr != nil {
				t.Fatal(perr)
			}

			c := codegenCase{G: i, Grammar: printedGrammar.String(), In: in, Ok: ok}

			if ok {
				c.End = m.End.Index
				matched++
			} else {
				if e := log.Err(); e != nil {
					c.Err = e.Error()
				}
				failed++
			}

			if i < 100 {
				c.HasRecovery = true
				chars := []rune(string(in))

				switch last := log.Last(); {
				case ok:
					c.RecEnd, c.RecNodes = m.End.Index, 1
				case last == nil || last.Pos.Index <= 0:
					// nothing matched before the failure, so the rule does not recover and the parse fails
					c.RecNoTree = true
					c.RecErrs, c.RecErrsKnown = []string{c.Err}, c.Err != ""
				default:
					// the error is the one the grammar reports, and the rule skips to the next c after it and takes it
					end := len(chars)
					for j := last.Pos.Index; j < len(chars); j++ {
						if chars[j] == 'c' {
							end = j + 1
							break
						}
					}

					c.RecEnd, c.RecNodes = end, 2
					c.RecErrs, c.RecErrsKnown = []string{c.Err}, true
				}
			}

			cases = append(cases, c)
		}
	}

	if matched < len(cases)/10 || failed < len(cases)/10 {
		t.Fatalf("%d matched and %d failed: the cases are too one sided", matched, failed)
	}

	data, _ := json.Marshal(cases)
	files["cases.json"] = string(data)
	files["probe_test.go"] = strings.Replace(strings.Replace(codegenHarness, "/*REGISTRY*/", registry.String(), 1), "/*RECOVERERS*/", recoverers.String(), 1)

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
	"unicode/utf8"
)

type testCase struct {
	G       int
	Grammar string
	In      []byte
	Ok      bool
	End     int
	Err     string

	HasRecovery  bool
	RecNoTree    bool
	RecEnd       int
	RecNodes     int
	RecErrs      []string
	RecErrsKnown bool
}

var parsers = []struct {
	name  string
	parse func(string) (ok bool, end int, err string, shapeOk bool)
}{
/*REGISTRY*/}

// recoverers: no tree, where the match ends, how many nodes, the error messages
var recoverers = map[int]func(string) (bool, int, int, []string){
/*RECOVERERS*/}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

var _ = utf8.RuneError

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
		// both ways of reading the input
		for _, pr := range parsers[2*c.G : 2*c.G+2] {
			ok, end, msg, shapeOk := pr.parse(string(c.In))

			switch {
			case ok != c.Ok:
				t.Errorf("%s %s on %q: matched=%v, the grammar says %v", pr.name, c.Grammar, c.In, ok, c.Ok)
				bad++
			case ok && (end != c.End || !shapeOk):
				t.Errorf("%s %s on %q: ends at %d (tree ok %v), the grammar says %d", pr.name, c.Grammar, c.In, end, shapeOk, c.End)
				bad++
			case !ok && c.Err != "" && msg != c.Err:
				t.Errorf("%s %s on %q:\n  generated %q\n  grammar   %q", pr.name, c.Grammar, c.In, msg, c.Err)
				bad++
			}
		}

		if c.HasRecovery {
			noTree, end, nodes, msgs := recoverers[c.G](string(c.In))

			switch {
			case noTree != c.RecNoTree:
				t.Errorf("R%d %s on %q: no tree=%v, expected %v (errors %v)", c.G, c.Grammar, c.In, noTree, c.RecNoTree, msgs)
				bad++
			case !noTree && (end != c.RecEnd || nodes != c.RecNodes):
				t.Errorf("R%d %s on %q: ends at %d with %d nodes, expected %d and %d", c.G, c.Grammar, c.In, end, nodes, c.RecEnd, c.RecNodes)
				bad++
			case c.RecErrsKnown && !equalStrings(msgs, c.RecErrs):
				t.Errorf("R%d %s on %q: errors %q, expected %q", c.G, c.Grammar, c.In, msgs, c.RecErrs)
				bad++
			case !c.RecErrsKnown && len(msgs) != len(c.RecErrs):
				t.Errorf("R%d %s on %q: %d errors, expected %d", c.G, c.Grammar, c.In, len(msgs), len(c.RecErrs))
				bad++
			}
		}

		if bad >= 10 {
			t.Fatal("too many differences")
		}
	}
}
`
