package tests

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/codegen"
	"github.com/almerlucke/exbana/v3/internal/gentest"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

// The methods of a view follow from how often the grammar lets a rule match inside another rule. This checks the
// promise against parses, on random grammars of several rules that call each other: for every node of every tree, the
// methods of its view together have to give all its children, each of them once, as nodes of one rule per method, and
// a method that promises a child (exactly once) has to have one.

// an is a random grammar, as data, so that text can be made from it.
type an struct {
	kind     int // 0 literal, 1 rule, 2 sequence, 3 choice, 4 repetition
	ch       rune
	ref      int
	kids     []*an
	min, max int
}

func (n *an) build(rules []*rule.Rule[rune, runes.Pos]) ebnf.Pattern[rune, runes.Pos] {
	switch n.kind {
	case 0:
		return leafNode(n.ch).p
	case 1:
		return rules[n.ref]
	case 2:
		ps := make([]ebnf.Pattern[rune, runes.Pos], len(n.kids))
		for i, k := range n.kids {
			ps[i] = k.build(rules)
		}

		return concR(ps...)
	case 3:
		ps := make([]ebnf.Pattern[rune, runes.Pos], len(n.kids))
		for i, k := range n.kids {
			ps[i] = k.build(rules)
		}

		return altR(ps...)
	}

	return repR(n.kids[0].build(rules), n.min, n.max)
}

// text makes a text the grammar can match.
func (n *an) text(rng *rand.Rand, bodies []*an, sb *strings.Builder) {
	switch n.kind {
	case 0:
		sb.WriteRune(n.ch)
	case 1:
		bodies[n.ref].text(rng, bodies, sb)
	case 2:
		for _, k := range n.kids {
			k.text(rng, bodies, sb)
		}
	case 3:
		n.kids[rng.Intn(len(n.kids))].text(rng, bodies, sb)
	default:
		count := n.min + rng.Intn(3)
		if n.max > 0 {
			count = min(count, n.max)
		}

		for range count {
			n.kids[0].text(rng, bodies, sb)
		}
	}
}

// randomAN makes a random pattern that can call the rules from first on: a rule only calls the ones after it, so the
// grammar ends.
func randomAN(rng *rand.Rand, depth, first, rules int) *an {
	leaf := func() *an {
		if first < rules && rng.Intn(2) == 0 {
			return &an{kind: 1, ref: first + rng.Intn(rules-first)}
		}

		return &an{kind: 0, ch: rune('a' + rng.Intn(3))}
	}

	if depth == 0 {
		return leaf()
	}

	kids := func(n int) []*an {
		out := make([]*an, n)
		for i := range out {
			out[i] = randomAN(rng, depth-1, first, rules)
		}

		return out
	}

	switch rng.Intn(6) {
	case 0:
		return leaf()
	case 1:
		return &an{kind: 3, kids: kids(2 + rng.Intn(2))}
	case 2, 3:
		return &an{kind: 2, kids: kids(2 + rng.Intn(2))}
	}

	lo := rng.Intn(3)
	hi := 0

	if rng.Intn(3) > 0 {
		hi = max(lo+rng.Intn(3), 1)
	}

	// something that matches nothing in a repetition is not what this is about
	return &an{kind: 4, kids: []*an{{kind: 2, kids: []*an{{kind: 0, ch: 'a' + rune(rng.Intn(3))}, randomAN(rng, depth-1, first, rules)}}}, min: lo, max: hi}
}

func TestViewsOfRandomGrammarsKeepTheirPromises(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles generated parsers")
	}

	rng := rand.New(rand.NewSource(31337))

	// the names of rules are the names of methods of views and of types of the file
	names := []string{"text", "kind", "span", "slice", "node", "tree", "parser", "parse", "rule", "top", "item", "list", "recovered", "syntax error"}

	files := map[string]string{}

	var (
		registry strings.Builder
		cases    []string
	)

	const grammars = 120

	for g := range grammars {
		count := 2 + rng.Intn(4)

		perm := rng.Perm(len(names))

		decl := make([]*rule.Rule[rune, runes.Pos], count)
		for i := range decl {
			decl[i] = rule.Declare[rune, runes.Pos](names[perm[i]])
		}

		bodies := make([]*an, count)
		for i := range bodies {
			bodies[i] = randomAN(rng, 3, i+1, count)
			decl[i].Define(bodies[i].build(decl))
		}

		utf8 := g%2 == 1
		name := fmt.Sprintf("A%d", g)

		src, err := codegen.Generate[rune, runes.Pos](decl[0], codegen.Config[rune, runes.Pos]{
			Package: "probe", Name: name, AST: true, UTF8: utf8,
			// the grammars have choices that are not deterministic, it is the views that are tested
			Commit: true,
		})
		if err != nil {
			t.Fatalf("grammar %d: %v", g, err)
		}

		files[strings.ToLower(name)+".go"] = string(src)

		parse := "ParseString"
		if utf8 {
			parse = "Parse"
		}

		fmt.Fprintf(&registry, "\t%d: func(s string) (bool, error) {\n\t\ttree, err := New%sParser().%s(s)\n\t\tif err != nil {\n\t\t\treturn false, nil\n\t\t}\n\t\tsizes, rules := make([]int, len(tree.Nodes)), make([]int, len(tree.Nodes))\n\t\tfor i, n := range tree.Nodes {\n\t\t\tsizes[i], rules[i] = int(n.Size), int(n.Rule)\n\t\t}\n\t\treturn true, checkTree(tree, sizes, rules)\n\t},\n", g, name, parse)

		// texts the grammar makes, and texts it probably does not
		for range 12 {
			var sb strings.Builder
			bodies[0].text(rng, bodies, &sb)
			cases = append(cases, fmt.Sprintf("%d:%s", g, sb.String()))
		}

		for range 3 {
			var sb strings.Builder
			for range rng.Intn(6) {
				sb.WriteByte(byte('a' + rng.Intn(3)))
			}

			cases = append(cases, fmt.Sprintf("%d:%s", g, sb.String()))
		}
	}

	if d := os.Getenv("AST_DUMP"); d != "" {
		for n, s := range files {
			_ = os.WriteFile(d+"/"+n, []byte(s), 0o644)
		}
	}

	files["cases.txt"] = strings.Join(cases, "\n")
	files["probe_test.go"] = strings.Replace(astHarness, "/*REGISTRY*/", registry.String(), 1)

	out := gentest.Run(t, files)
	t.Logf("%d grammars: %s", grammars, strings.TrimSpace(out))
}

const astHarness = `package probe

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

var checkers = map[int]func(string) (bool, error){
/*REGISTRY*/}

// the methods of a view that are not about the children
var plain = map[string]bool{"Span": true, "Slice": true, "Text": true, "Kind": true, "Recovered": true}

// checkTree checks the view of every node, and of the top level.
func checkTree(tree any, sizes, rules []int) error {
	tv := reflect.ValueOf(tree)

	for node := range sizes {
		var view reflect.Value

		for i := 0; i < tv.NumMethod(); i++ {
			if !strings.HasPrefix(tv.Type().Method(i).Name, "As") {
				continue
			}

			out := tv.Method(i).Call([]reflect.Value{reflect.ValueOf(node)})
			if out[1].Bool() {
				if view.IsValid() {
					return fmt.Errorf("node %d is of two rules", node)
				}

				view = out[0]
			}
		}

		if !view.IsValid() {
			return fmt.Errorf("node %d has no view", node)
		}

		if err := checkView(view, node, sizes, rules); err != nil {
			return fmt.Errorf("node %d (%s): %w", node, view.Type().Name(), err)
		}
	}

	top := tv.MethodByName("Top").Call(nil)[0]

	return checkView(top, -1, sizes, rules)
}

func checkView(view reflect.Value, node int, sizes, rules []int) error {
	if got := int(view.FieldByName("Node").Int()); got != node {
		return fmt.Errorf("the view is of node %d", got)
	}

	// the children of the node
	children := map[int]bool{}
	i, end := 0, len(sizes)
	if node >= 0 {
		i, end = node+1, node+sizes[node]
	}
	for i < end {
		children[i] = true
		i += sizes[i]
	}

	seen := map[int]string{}

	take := func(method string, child int, rule *int) error {
		if !children[child] {
			return fmt.Errorf("%s gives node %d, which is not a child", method, child)
		}
		if other, dup := seen[child]; dup {
			return fmt.Errorf("%s and %s both give node %d", method, other, child)
		}
		if *rule >= 0 && rules[child] != *rule {
			return fmt.Errorf("%s gives nodes of different rules", method)
		}

		*rule = rules[child]
		seen[child] = method

		return nil
	}

	for m := 0; m < view.NumMethod(); m++ {
		name := view.Type().Method(m).Name
		if plain[name] && (node >= 0 || name == "Kind") {
			continue
		}

		rule := -1
		out := view.Method(m).Call(nil)

		switch {
		case len(out) == 2: // once or not at all
			if out[1].Bool() {
				if err := take(name, int(out[0].FieldByName("Node").Int()), &rule); err != nil {
					return err
				}
			}
		case out[0].Kind() == reflect.Struct: // exactly once
			if err := take(name, int(out[0].FieldByName("Node").Int()), &rule); err != nil {
				return err
			}
		default: // many
			var problem error
			last := -1

			yield := reflect.MakeFunc(out[0].Type().In(0), func(args []reflect.Value) []reflect.Value {
				child := int(args[0].FieldByName("Node").Int())
				if child <= last {
					problem = fmt.Errorf("%s is not in order", name)
				}
				last = child

				if err := take(name, child, &rule); err != nil && problem == nil {
					problem = err
				}

				return []reflect.Value{reflect.ValueOf(problem == nil)}
			})
			out[0].Call([]reflect.Value{yield})

			if problem != nil {
				return problem
			}
		}
	}

	if len(seen) != len(children) {
		return fmt.Errorf("the methods give %d of the %d children", len(seen), len(children))
	}

	// a view that says what it is (one of several) has at most one child
	if k := view.MethodByName("Kind"); k.IsValid() {
		out := k.Call(nil)
		if len(children) > 1 {
			return fmt.Errorf("Kind, and %d children", len(children))
		}
		if out[1].Bool() != (len(children) == 1) {
			return fmt.Errorf("Kind says %v with %d children", out[1].Bool(), len(children))
		}
		for child := range children {
			if int(out[0].Uint()) != rules[child] {
				return fmt.Errorf("Kind is not the rule of the child")
			}
		}
	}

	return nil
}

func TestViews(t *testing.T) {
	data, err := os.ReadFile("cases.txt")
	if err != nil {
		t.Fatal(err)
	}

	parsed, bad := 0, 0

	for _, line := range strings.Split(string(data), "\n") {
		g, in, _ := strings.Cut(line, ":")
		n, _ := strconv.Atoi(g)

		ok, err := checkers[n](in)
		if ok {
			parsed++
		}

		if err != nil {
			t.Errorf("grammar %d on %q: %v", n, in, err)

			if bad++; bad >= 10 {
				t.Fatal("too many problems")
			}
		}
	}

	if parsed < 600 {
		t.Fatalf("only %d texts were parsed", parsed)
	}

	t.Logf("%d texts parsed and checked", parsed)
}
`
