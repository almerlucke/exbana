package tests

import (
	"bytes"
	"encoding/json"
	"errors"
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

// A left recursive rule is generated as a loop (a base branch, then tails for as long as they match) when the optimizer
// proves that is what the rule matches when asked for its longest match. This tests that proof against the evaluation of
// the same rule by the grammar, which grows a seed and offers all its matches: on random left recursive grammars the
// generator either refuses, or the parser has to agree with the grammar on every input.

// randomLeftRecursive makes r = base... | r tail..., from random parts that cannot match nothing, in a random order.
func randomLeftRecursive(rng *rand.Rand) pat {
	r := rule.Declare[rune, runes.Pos]("r").SetLeftRecursive(true)

	part := func() pat {
		for {
			if n := randomNode(rng, 2); !n.nullable {
				return n.p
			}
		}
	}

	var branches []pat

	for range 1 + rng.Intn(2) {
		branches = append(branches, part())
	}

	for range 1 + rng.Intn(2) {
		if rng.Intn(3) == 0 {
			branches = append(branches, concR(r, part(), part()))
		} else {
			branches = append(branches, concR(r, part()))
		}
	}

	rng.Shuffle(len(branches), func(i, j int) { branches[i], branches[j] = branches[j], branches[i] })

	r.Define(altR(branches...))

	if rng.Intn(3) == 0 {
		return concR(r, endR())
	}

	return r
}

type lrCase struct {
	G       int
	Grammar string
	In      []byte
	Ok      bool
	End     int
	Err     string
}

func TestGeneratedLoopsAgreeWithLeftRecursiveGrammars(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles generated parsers")
	}

	rng := rand.New(rand.NewSource(777))

	files := map[string]string{}

	var (
		cases    []lrCase
		registry strings.Builder
	)

	generated, refused := 0, 0

	for i := range 300 {
		root := randomLeftRecursive(rng)

		// the constructs inside the rule are made deterministic; the rule itself is a loop or it is refused
		res, err := optimize.Analyze[rune, runes.Pos](root, optimize.Options{})
		if err != nil {
			t.Fatal(err)
		}

		res.Force()

		var printed bytes.Buffer
		_ = root.Print(&printed)

		src, err := codegen.Generate[rune, runes.Pos](root, codegen.Config[rune, runes.Pos]{Package: "probe", Name: fmt.Sprintf("L%d", i)})
		if err != nil {
			var e *codegen.Error
			if !errors.As(err, &e) {
				t.Fatalf("grammar %s: %v", printed.String(), err)
			}

			refused++

			continue
		}

		generated++

		name := fmt.Sprintf("L%d", i)
		files[strings.ToLower(name)+".go"] = string(src)

		fmt.Fprintf(&registry, "\t%d: func(s string) (bool, int, string, bool) {\n\t\ttree, err := New%sParser().ParseString(s)\n\t\tif err != nil {\n\t\t\treturn false, 0, err.Error(), false\n\t\t}\n\t\t// one node for the rule, however long the chain of tails\n\t\tn := tree.Nodes\n\t\treturn true, tree.End, \"\", len(n) >= 1 && n[0].Begin == 0 && int(n[0].End) == tree.End\n\t},\n", i, name)

		for range 16 {
			var in []byte
			for range rng.Intn(10) {
				in = append(in, byte('a'+rng.Intn(3)))
			}

			rd := newRunes(string(in))
			log := runes.NewFarthestLog(rd)

			ok, m, perr := ebnf.Parse[rune, runes.Pos](root, ebnf.NewParseContext[rune, runes.Pos](rd).WithLogger(log))
			if perr != nil {
				t.Fatal(perr)
			}

			c := lrCase{G: i, Grammar: printed.String(), In: in, Ok: ok}
			if ok {
				c.End = m.End.Index
			} else if e := log.Err(); e != nil {
				c.Err = e.Error()
			}

			cases = append(cases, c)
		}
	}

	// the test only means something if a good part of the grammars are loops
	if generated < 60 {
		t.Fatalf("only %d of 300 left recursive grammars could be generated (%d refused)", generated, refused)
	}

	data, _ := json.Marshal(cases)
	files["cases.json"] = string(data)
	files["probe_test.go"] = strings.Replace(lrHarness, "/*REGISTRY*/", registry.String(), 1)

	out := gentest.Run(t, files)
	t.Logf("%d of 300 grammars generated as loops (%d refused), %d cases: %s", generated, refused, len(cases), strings.TrimSpace(out))
}

const lrHarness = `package probe

import (
	"encoding/json"
	"os"
	"testing"
)

type testCase struct {
	G       int
	Grammar string
	In      []byte
	Ok      bool
	End     int
	Err     string
}

var parsers = map[int]func(string) (ok bool, end int, err string, treeOk bool){
/*REGISTRY*/}

func TestLoopsAgainstTheGrammars(t *testing.T) {
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
		ok, end, msg, treeOk := parsers[c.G](string(c.In))

		switch {
		case ok != c.Ok:
			t.Errorf("L%d %s on %q: matched=%v, the grammar says %v", c.G, c.Grammar, c.In, ok, c.Ok)
			bad++
		case ok && (end != c.End || !treeOk):
			t.Errorf("L%d %s on %q: ends at %d (tree ok %v), the grammar says %d", c.G, c.Grammar, c.In, end, treeOk, c.End)
			bad++
		case !ok && c.Err != "" && msg != c.Err:
			t.Errorf("L%d %s on %q:\n  generated %q\n  grammar   %q", c.G, c.Grammar, c.In, msg, c.Err)
			bad++
		}

		if bad >= 10 {
			t.Fatal("too many differences")
		}
	}
}
`
