package codegen

import (
	"fmt"
	"strings"

	ebnf "github.com/almerlucke/exbana/v3"
)

// The typed AST (Config.AST) is a view type for every rule. A view is a tree and a node of it, and has a method for every
// rule that can be a child of that node, made from how often the grammar lets that rule match inside this one:
//
//	exactly once        func (v Member) Value() Value
//	once or not at all  func (v Member) Comma() (Comma, bool)
//	any other number    func (v Object) Members() iter.Seq[Member]
//
// The views do not copy anything. They are as cheap as the node numbers they hold, and they only read the tree when asked.

// manyTimes stands for any number of times, and caps the counts.
const manyTimes = 1 << 12

// card says how many times something matches: at least min, and at most max.
type card struct{ min, max int }

func (a card) plus(b card) card {
	return card{min(a.min+b.min, manyTimes), min(a.max+b.max, manyTimes)}
}

// childSet is the rules that can match directly inside a pattern, in the order the pattern first mentions them, with how
// many times each of them matches. total is how many rules match inside, of any kind.
type childSet struct {
	order []string
	cards map[string]card
	total card
}

func newChildSet() childSet { return childSet{cards: map[string]card{}} }

func (s *childSet) set(name string, c card) {
	if _, ok := s.cards[name]; !ok {
		s.order = append(s.order, name)
	}

	s.cards[name] = c
}

// seqSets is the children of patterns that match one after the other.
func seqSets(parts ...childSet) childSet {
	out := newChildSet()

	for _, p := range parts {
		for _, name := range p.order {
			c := p.cards[name]
			if have, ok := out.cards[name]; ok {
				c = have.plus(c)
			}

			out.set(name, c)
		}

		out.total = out.total.plus(p.total)
	}

	return out
}

// altSets is the children of patterns of which one matches.
func altSets(parts ...childSet) childSet {
	out := newChildSet()

	for _, p := range parts {
		for _, name := range p.order {
			out.set(name, card{})
		}
	}

	for i, p := range parts {
		for _, name := range out.order {
			c := p.cards[name] // not mentioned is zero times
			have := out.cards[name]

			if i == 0 {
				have = c
			} else {
				have = card{min(have.min, c.min), max(have.max, c.max)}
			}

			out.cards[name] = have
		}

		if i == 0 {
			out.total = p.total
		} else {
			out.total = card{min(out.total.min, p.total.min), max(out.total.max, p.total.max)}
		}
	}

	return out
}

// repeatSet is the children of a pattern that matches between lo and hi times, where a hi of 0 means as often as it can.
func repeatSet(s childSet, lo, hi int) childSet {
	scale := func(c card) card {
		out := card{min: min(c.min*lo, manyTimes)}

		switch {
		case c.max == 0:
		case hi == 0:
			out.max = manyTimes
		default:
			out.max = min(c.max*hi, manyTimes)
		}

		return out
	}

	out := newChildSet()
	for _, name := range s.order {
		out.set(name, scale(s.cards[name]))
	}

	out.total = scale(s.total)

	return out
}

// childrenOf works out which rules match inside the pattern.
func (c *compiler[T, P]) childrenOf(n *ebnf.Node[T, P]) childSet {
	out := newChildSet()

	switch n.Kind {
	case ebnf.KindRef:
		out.set(n.ID, card{1, 1})
		out.total = card{1, 1}
	case ebnf.KindSeq:
		parts := make([]childSet, len(n.Children))
		for i, ch := range n.Children {
			parts[i] = c.childrenOf(ch)
		}

		return seqSets(parts...)
	case ebnf.KindAlt:
		parts := make([]childSet, len(n.Children))
		for i, ch := range n.Children {
			parts[i] = c.childrenOf(ch)
		}

		return altSets(parts...)
	case ebnf.KindRepeat:
		return repeatSet(c.childrenOf(n.Children[0]), n.Min, n.Max)
	case ebnf.KindExcept:
		// what the exception matches is only looked at, it leaves nothing in the tree
		return c.childrenOf(n.Children[0])
	}

	return out
}

// childrenOfRule is childrenOf for the body of a rule. A rule that is generated as a loop matches a base, then its tails any
// number of times, and the rule itself is what the loop stands for and not a child.
func (c *compiler[T, P]) childrenOfRule(name string) childSet {
	lr, ok := c.lr[name]
	if !ok {
		return c.childrenOf(c.g.Rules[name].Children[0])
	}

	bases := make([]childSet, len(lr.bases))
	for i, b := range lr.bases {
		bases[i] = c.childrenOf(b)
	}

	tails := make([]childSet, len(lr.tails))
	for i, t := range lr.tails {
		rest := make([]childSet, len(t.Children)-1)
		for j, part := range t.Children[1:] {
			rest[j] = c.childrenOf(part)
		}

		tails[i] = seqSets(rest...)
	}

	return seqSets(altSets(bases...), repeatSet(altSets(tails...), 0, 0))
}

// plural is the name of something there are many of.
func plural(s string) string {
	switch {
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "x"), strings.HasSuffix(s, "z"), strings.HasSuffix(s, "ch"), strings.HasSuffix(s, "sh"):
		return s + "es"
	case len(s) > 1 && strings.HasSuffix(s, "y") && !strings.ContainsAny(s[len(s)-2:len(s)-1], "aeiouAEIOU"):
		return s[:len(s)-1] + "ies"
	}

	return s + "s"
}

// emitViews writes the view types.
func (c *compiler[T, P]) emitViews() string {
	var b strings.Builder

	tree := c.cfg.Name + "Tree"
	rule := c.cfg.Name + "Rule"

	view := func(typ, doc string, constName string, set childSet, rules bool, recovers bool, positions map[string][]string) {
		taken := map[string]bool{"Tree": true, "Node": true, "Kind": true}
		if rules {
			for _, n := range []string{"Span", "Slice", "Text", "Recovered"} {
				taken[n] = true
			}
		}

		// the names the configuration gave come first: the methods that follow from the grammar make way for them
		for _, names := range positions {
			for _, n := range names {
				taken[n] = true
			}
		}

		free := func(base string) string {
			name := base
			if taken[name] {
				name = base + "Rule"
			}

			for i := 2; taken[name]; i++ {
				name = fmt.Sprintf("%sRule%d", base, i)
			}

			taken[name] = true

			return name
		}

		fmt.Fprintf(&b, "// %s\ntype %s struct {\n\tTree *%s\n\n\t// Node is the number of the node in the tree, or -1 for the top level.\n\tNode int\n}\n\n", doc, typ, tree)

		if rules {
			fmt.Fprintf(&b, "// As%s is the node seen as a %s. ok is false if the node is not of that rule.\n", typ[len(c.cfg.Name):], typ)
			fmt.Fprintf(&b, "func (t *%s) As%s(node int) (%s, bool) {\n\tif node < 0 || node >= len(t.Nodes) || t.Nodes[node].Rule != %s {\n\t\treturn %s{}, false\n\t}\n\n\treturn %s{t, node}, true\n}\n\n", tree, typ[len(c.cfg.Name):], typ, constName, typ, typ)

			fmt.Fprintf(&b, "// Span is where the node starts and ends in the input.\nfunc (v %s) Span() (begin, end int) {\n\treturn v.Tree.Span(v.Node)\n}\n\n", typ)
			fmt.Fprintf(&b, "// Slice is the part of the input the node matched.\nfunc (v %s) Slice() %s {\n\treturn v.Tree.Slice(v.Node)\n}\n\n", typ, c.sliceType())
		}

		for _, name := range set.order {
			cd := set.cards[name]
			child := c.viewType[name]
			ident := c.ruleIdent[name]
			cc := c.ruleConst[name]

			// a rule that recovers from errors can be cut short, and then it does not have everything its rule says
			if recovers && cd.min > 0 {
				cd.min = 0
			}

			for i, pname := range positions[name] {
				if cd.min > i {
					fmt.Fprintf(&b, "// %s is the %s inside that comes number %d.\nfunc (v %s) %s() %s {\n\tnode, _ := v.Tree.Nth(v.Node, %s, %d)\n\n\treturn %s{v.Tree, node}\n}\n\n", pname, name, i+1, typ, pname, child, cc, i, child)
				} else {
					fmt.Fprintf(&b, "// %s is the %s inside that comes number %d, if there is one.\nfunc (v %s) %s() (%s, bool) {\n\tnode, ok := v.Tree.Nth(v.Node, %s, %d)\n\n\treturn %s{v.Tree, node}, ok\n}\n\n", pname, name, i+1, typ, pname, child, cc, i, child)
				}
			}

			switch {
			case cd.max == 1 && cd.min == 1:
				m := free(ident)
				fmt.Fprintf(&b, "// %s is the %s inside.\nfunc (v %s) %s() %s {\n\tnode, _ := v.Tree.First(v.Node, %s)\n\n\treturn %s{v.Tree, node}\n}\n\n", m, name, typ, m, child, cc, child)
			case cd.max == 1:
				m := free(ident)
				fmt.Fprintf(&b, "// %s is the %s inside, if there is one.\nfunc (v %s) %s() (%s, bool) {\n\tnode, ok := v.Tree.First(v.Node, %s)\n\n\treturn %s{v.Tree, node}, ok\n}\n\n", m, name, typ, m, child, cc, child)
			default:
				m := free(plural(ident))
				fmt.Fprintf(&b, "// %s are the %s nodes inside, in order.\nfunc (v %s) %s() iter.Seq[%s] {\n\treturn func(yield func(%s) bool) {\n\t\tfor node := range v.Tree.Find(v.Node, %s) {\n\t\t\tif !yield(%s{v.Tree, node}) {\n\t\t\t\treturn\n\t\t\t}\n\t\t}\n\t}\n}\n\n", m, name, typ, m, child, child, cc, child)
			}
		}

		// a rule that is one of several rules
		if len(set.order) >= 2 && set.total.max == 1 {
			fmt.Fprintf(&b, "// Kind is the rule of what is inside: this rule is one of several.\nfunc (v %s) Kind() (%s, bool) {\n\tfor node := range v.Tree.Children(v.Node) {\n\t\treturn v.Tree.Nodes[node].Rule, true\n\t}\n\n\treturn 0, false\n}\n\n", typ, rule)
		}

		if recovers {
			fmt.Fprintf(&b, "// Recovered is true if the parser skipped part of the node after a syntax error.\nfunc (v %s) Recovered() bool {\n\t_, ok := v.Tree.First(v.Node, %s)\n\n\treturn ok\n}\n\n", typ, c.errorConst())
		}

		if rules && c.isText {
			fmt.Fprintf(&b, "// Text is the part of the input the node matched, as a string.\nfunc (v %s) Text() string {\n\treturn v.Tree.Text(v.Node)\n}\n\n", typ)
		}
	}

	for _, name := range c.g.Order {
		_, recovers := c.cfg.Recover[name]

		view(c.viewType[name], fmt.Sprintf("%s is a node of the rule %q, seen through the grammar: the rules that match inside it are its methods.", c.viewType[name], name),
			c.ruleConst[name], c.childrenOfRule(name), true, recovers, c.cfg.Positions[name])
	}

	view(c.topType, fmt.Sprintf("%s is the top level of a %s: what the grammar matched, seen through the grammar.", c.topType, tree),
		"", c.childrenOf(c.g.Root), false, false, nil)

	fmt.Fprintf(&b, "// Top is the top level of the tree.\nfunc (t *%s) Top() %s {\n\treturn %s{t, -1}\n}\n", tree, c.topType, c.topType)

	return b.String()
}
