package exbana

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// Kind is the kind of a pattern, as far as tools that analyse or compile a grammar are concerned.
type Kind int

const (
	// KindOpaque is a pattern that can not describe itself, for example one that was written from scratch. A tool can
	// only call it.
	KindOpaque Kind = iota

	// KindLiteral is a fixed sequence of elements.
	KindLiteral

	// KindClass is one element out of a class of elements.
	KindClass

	// KindEnd is the end of the stream, it consumes nothing.
	KindEnd

	// KindSeq is its parts, one after the other.
	KindSeq

	// KindAlt is one of its parts.
	KindAlt

	// KindRepeat is its one part, between Min and Max times.
	KindRepeat

	// KindExcept is its first part, unless its second part matches at the same place.
	KindExcept

	// KindRule is a named rule. In a Grammar it is the definition of the rule, its one part is the definition.
	KindRule

	// KindRef is a use of a rule, by name.
	KindRef
)

func (k Kind) String() string {
	names := [...]string{"opaque", "literal", "class", "end", "seq", "alt", "repeat", "except", "rule", "ref"}
	if int(k) < len(names) {
		return names[k]
	}

	return "kind(" + strconv.Itoa(int(k)) + ")"
}

// Shape describes a pattern one level deep: what kind it is, how it is configured and which patterns it is built from.
// Which fields are used depends on the Kind.
type Shape[T, P any] struct {
	Kind Kind

	// ID is the name of the pattern, and Text the text it prints as.
	ID, Text string

	// Values is the sequence of a KindLiteral.
	Values []T

	// Ranges are the inclusive ranges [low, high] of a KindClass, Negated means every element but those. A single
	// value is a range with the same low and high.
	Ranges  [][2]T
	Negated bool

	// Exact is true if a literal or class is matched with exactly the Values or Ranges: elements are compared with ==
	// and the ordering of the type. It is false if the pattern is decided by a function, which is then all that is
	// known about it: a class that has no Ranges takes one element, a literal takes len(Values) elements.
	Exact bool

	// Min and Max are the number of repetitions of a KindRepeat, zero Max means no limit.
	Min, Max int

	// Possessive: the repetition never gives matches back. Orthogonal: the alternation commits to the first part that
	// matches.
	Possessive, Orthogonal bool

	// LeftRecursive: the rule may start with a call to itself. Atomic: failures of the rule are reported for the rule
	// as a whole.
	LeftRecursive, Atomic bool

	// Parts are the patterns this one is built from. A rule has one, and none while it is undefined.
	Parts []Pattern[T, P]
}

// Structured is implemented by patterns that can describe themselves. All patterns of this library do, patterns that
// do not are seen as KindOpaque.
type Structured[T, P any] interface {
	Shape() Shape[T, P]
}

// ShapeOf returns the shape of a pattern, or an opaque shape for a pattern that can not describe itself.
func ShapeOf[T, P any](p Pattern[T, P]) Shape[T, P] {
	if s, ok := p.(Structured[T, P]); ok {
		return s.Shape()
	}

	return Shape[T, P]{Kind: KindOpaque, ID: IDOf(p)}
}

// Node is a pattern with everything it is built from, as found by Analyze.
type Node[T, P any] struct {
	Shape[T, P]

	// Pattern is the pattern the node describes, which is what to call for a KindOpaque node.
	Pattern Pattern[T, P]

	// Children are the nodes of the parts. A KindRef has none, it refers to a rule of the Grammar by its ID.
	Children []*Node[T, P]
}

// Grammar is a pattern analysed into a tree of nodes and a table of rules. A recursive grammar is a finite structure
// because uses of rules are KindRef nodes instead of copies of the rule.
type Grammar[T, P any] struct {
	// Root is the node of the pattern that was analysed. If that pattern is a rule it is a KindRef.
	Root *Node[T, P]

	// Rules are the definitions of the rules, by name, and Order the names in the order they were met.
	Rules map[string]*Node[T, P]
	Order []string
}

var (
	// ErrUndefinedRule is returned by Analyze for a rule that was declared and never defined.
	ErrUndefinedRule = errors.New("exbana: rule is not defined")

	// ErrDuplicateRule is returned by Analyze when two different rules have the same name.
	ErrDuplicateRule = errors.New("exbana: two different rules have the same name")

	// ErrUnnamedRecursion is returned by Analyze for a pattern that contains itself without a named rule in between,
	// which can not be described as a finite grammar.
	ErrUnnamedRecursion = errors.New("exbana: recursion without a named rule")
)

// Analyze describes a pattern and everything it is built from as a Grammar, using the Shape of each pattern. Rules are
// found by name; a rule used in many places is one entry of the table.
func Analyze[T, P any](p Pattern[T, P]) (*Grammar[T, P], error) {
	a := &analyzer[T, P]{
		g:     &Grammar[T, P]{Rules: map[string]*Node[T, P]{}},
		rules: map[string]Pattern[T, P]{},
		path:  map[Pattern[T, P]]bool{},
	}

	root, err := a.build(p)
	if err != nil {
		return nil, err
	}

	a.g.Root = root

	return a.g, nil
}

type analyzer[T, P any] struct {
	g     *Grammar[T, P]
	rules map[string]Pattern[T, P]
	path  map[Pattern[T, P]]bool // patterns that are being described, to find recursion without a rule
}

// hashable returns true if the pattern can be a map key
func hashable[T, P any](p Pattern[T, P]) bool {
	return reflect.TypeOf(p).Comparable()
}

func (a *analyzer[T, P]) build(p Pattern[T, P]) (*Node[T, P], error) {
	s := ShapeOf(p)

	if hashable(p) {
		if a.path[p] {
			return nil, fmt.Errorf("%w: %s", ErrUnnamedRecursion, describePath(p, s))
		}

		a.path[p] = true
		defer delete(a.path, p)
	}

	if s.Kind == KindRule {
		return a.buildRule(p, s)
	}

	n := &Node[T, P]{Shape: s, Pattern: p}

	for _, part := range s.Parts {
		child, err := a.build(part)
		if err != nil {
			return nil, err
		}

		n.Children = append(n.Children, child)
	}

	// the children are the nodes of the parts
	n.Parts = nil

	return n, nil
}

func describePath[T, P any](p Pattern[T, P], s Shape[T, P]) string {
	if s.ID != "" {
		return strconv.Quote(s.ID)
	}

	return s.Kind.String()
}

func (a *analyzer[T, P]) buildRule(p Pattern[T, P], s Shape[T, P]) (*Node[T, P], error) {
	if len(s.Parts) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrUndefinedRule, s.ID)
	}

	// a rule without a name can not be referred to, so it stands for its definition
	if s.ID == "" {
		return a.build(s.Parts[0])
	}

	ref := &Node[T, P]{Shape: Shape[T, P]{Kind: KindRef, ID: s.ID}, Pattern: p}

	if known, ok := a.rules[s.ID]; ok {
		if known != p {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateRule, s.ID)
		}

		return ref, nil
	}

	// register before describing the definition, which may use the rule itself
	a.rules[s.ID] = p
	a.g.Order = append(a.g.Order, s.ID)

	def := &Node[T, P]{Shape: s, Pattern: p}
	def.Parts = nil
	a.g.Rules[s.ID] = def

	delete(a.path, p)
	defer func() { a.path[p] = true }()

	body, err := a.build(s.Parts[0])
	if err != nil {
		return nil, err
	}

	def.Children = []*Node[T, P]{body}

	return ref, nil
}

// String formats the grammar as EBNF, one rule per line, after the root if that is not just a rule.
func (g *Grammar[T, P]) String() string {
	var sb strings.Builder

	if g.Root != nil && g.Root.Kind != KindRef {
		sb.WriteString(g.Root.String() + "\n")
	}

	for _, name := range g.Order {
		sb.WriteString(name + " = " + g.Rules[name].Children[0].String() + "\n")
	}

	return sb.String()
}

// String formats the node as EBNF.
func (n *Node[T, P]) String() string {
	join := func(sep string) string {
		parts := make([]string, len(n.Children))
		for i, c := range n.Children {
			parts[i] = c.String()
		}

		return strings.Join(parts, sep)
	}

	switch n.Kind {
	case KindRef:
		return n.ID
	case KindRule:
		return n.ID
	case KindLiteral:
		if n.Exact {
			return formatValues(n.Values)
		}

		return "<literal " + strconv.Itoa(len(n.Values)) + ">"
	case KindClass:
		if !n.Exact {
			if n.Text != "" {
				return n.Text
			}

			return "<class>"
		}

		return formatClass(n.Ranges, n.Negated)
	case KindEnd:
		return "$"
	case KindSeq:
		return "(" + join(", ") + ")"
	case KindAlt:
		return "(" + join(" | ") + ")"
	case KindExcept:
		return n.Children[0].String() + " - " + n.Children[1].String()
	case KindRepeat:
		suffix := ""
		switch {
		case n.Min == 0 && n.Max == 0:
			suffix = "*"
		case n.Min == 0 && n.Max == 1:
			suffix = "?"
		case n.Min == 1 && n.Max == 0:
			suffix = "+"
		case n.Max == 0:
			suffix = "{" + strconv.Itoa(n.Min) + ",}"
		case n.Min == n.Max:
			suffix = "{" + strconv.Itoa(n.Min) + "}"
		default:
			suffix = "{" + strconv.Itoa(n.Min) + "," + strconv.Itoa(n.Max) + "}"
		}

		if n.Possessive {
			suffix += "+"
		}

		return n.Children[0].String() + suffix
	}

	if n.ID != "" {
		return "<" + n.ID + ">"
	}

	return "<opaque>"
}

func formatValues[T any](values []T) string {
	if rs, ok := any(values).([]rune); ok {
		return strconv.Quote(string(rs))
	}

	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprint(v)
	}

	return "<" + strings.Join(parts, " ") + ">"
}

func formatClass[T any](ranges [][2]T, negated bool) string {
	var sb strings.Builder

	sb.WriteString("[")

	if negated {
		sb.WriteString("^")
	}

	for _, r := range ranges {
		lo, hi := formatElement(r[0]), formatElement(r[1])
		if lo == hi {
			sb.WriteString(lo)
		} else {
			sb.WriteString(lo + "-" + hi)
		}
	}

	sb.WriteString("]")

	return sb.String()
}

func formatElement[T any](v T) string {
	if r, ok := any(v).(rune); ok {
		q := strconv.QuoteRune(r)
		return q[1 : len(q)-1]
	}

	return fmt.Sprint(v)
}

// CanMatchNothing returns true if the node can match without consuming anything. A pattern the analysis can not look
// into is assumed to, which is the safe answer for the questions it is used for.
func (g *Grammar[T, P]) CanMatchNothing(n *Node[T, P]) bool {
	return g.nullable(n, g.nullableRules())
}

func (g *Grammar[T, P]) nullable(n *Node[T, P], rules map[string]bool) bool {
	switch n.Kind {
	case KindLiteral:
		return len(n.Values) == 0
	case KindClass:
		return false
	case KindEnd:
		return true
	case KindSeq:
		for _, c := range n.Children {
			if !g.nullable(c, rules) {
				return false
			}
		}

		return true
	case KindAlt:
		for _, c := range n.Children {
			if g.nullable(c, rules) {
				return true
			}
		}

		return false
	case KindRepeat:
		return n.Min == 0 || g.nullable(n.Children[0], rules)
	case KindExcept:
		return g.nullable(n.Children[0], rules)
	case KindRef:
		return rules[n.ID]
	}

	return true // opaque
}

// nullableRules finds the rules that can match nothing. Rules use each other, so it repeats until nothing changes; a rule
// only ever goes from "can not" to "can", which is why that ends.
func (g *Grammar[T, P]) nullableRules() map[string]bool {
	rules := map[string]bool{}

	for changed := true; changed; {
		changed = false

		for _, name := range g.Order {
			if !rules[name] && g.nullable(g.Rules[name].Children[0], rules) {
				rules[name] = true
				changed = true
			}
		}
	}

	return rules
}

// LeftCalls returns the rules that can be entered at the very position where the node starts, without anything having
// been consumed: the rules a parser would call before it has looked at the input. A rule that can call itself this way
// is left recursive.
func (g *Grammar[T, P]) LeftCalls(n *Node[T, P]) []string {
	calls := map[string]bool{}
	g.leftCalls(n, g.nullableRules(), calls)

	out := make([]string, 0, len(calls))
	for name := range calls {
		out = append(out, name)
	}

	slices.Sort(out)

	return out
}

func (g *Grammar[T, P]) leftCalls(n *Node[T, P], rules map[string]bool, calls map[string]bool) {
	switch n.Kind {
	case KindRef:
		calls[n.ID] = true
	case KindSeq:
		for _, c := range n.Children {
			g.leftCalls(c, rules, calls)

			if !g.nullable(c, rules) {
				break
			}
		}
	case KindAlt:
		for _, c := range n.Children {
			g.leftCalls(c, rules, calls)
		}
	case KindRepeat:
		g.leftCalls(n.Children[0], rules, calls)
	case KindExcept:
		// the exception is looked at from the same position as the pattern
		g.leftCalls(n.Children[0], rules, calls)
		g.leftCalls(n.Children[1], rules, calls)
	}
}

// SplitLeftRecursive splits the definition of a rule that is directly left recursive,
//
//	r = base1 | base2 | r tail1 | r tail2
//
// into its base branches and its tail branches. A tail is a sequence whose first part is a use of the rule itself; the
// rest of the sequence, after that first part, is what the tail adds. If the rule has no branch that starts with itself
// there are no tails and nothing is left recursive. It reports a rule that is left recursive in another way: through
// another rule, or through a base branch, or with no base branch at all.
func (g *Grammar[T, P]) SplitLeftRecursive(rule string) (bases, tails []*Node[T, P], err error) {
	def, ok := g.Rules[rule]
	if !ok {
		return nil, nil, fmt.Errorf("there is no rule %q", rule)
	}

	body := def.Children[0]
	branches := []*Node[T, P]{body}

	if body.Kind == KindAlt {
		branches = body.Children
	}

	for _, b := range branches {
		switch {
		case b.Kind == KindRef && b.ID == rule:
			return nil, nil, fmt.Errorf("a branch of %s is only %s", rule, rule)
		case b.Kind == KindSeq && len(b.Children) > 0 && b.Children[0].Kind == KindRef && b.Children[0].ID == rule:
			if len(b.Children) == 1 {
				return nil, nil, fmt.Errorf("a branch of %s is only %s", rule, rule)
			}

			tails = append(tails, b)
		default:
			bases = append(bases, b)
		}
	}

	if len(tails) == 0 {
		return bases, nil, nil
	}

	if len(bases) == 0 {
		return nil, nil, fmt.Errorf("every branch of %s starts with %s, there is nothing for it to start from", rule, rule)
	}

	// a base branch must not get back to the rule at the same position, that is left recursion the loop does not cover
	for _, b := range bases {
		if slices.Contains(g.LeftCalls(b), rule) {
			return nil, nil, fmt.Errorf("%s calls itself at the same position through more than a first part of a branch", rule)
		}
	}

	return bases, tails, nil
}

// LeftCycles returns the rules that can enter themselves at the same position, directly or through other rules: a parser
// for them would call itself without consuming anything and never stop. A rule that is left recursive in the way
// SplitLeftRecursive understands (a directly recursive tail) is not counted, its tails come after its base branches.
func (g *Grammar[T, P]) LeftCycles() []string {
	rules := g.nullableRules()

	edges := map[string][]string{}

	for _, name := range g.Order {
		calls := map[string]bool{}

		if bases, tails, err := g.SplitLeftRecursive(name); err == nil && len(tails) > 0 {
			for _, b := range bases {
				g.leftCalls(b, rules, calls)
			}
		} else {
			g.leftCalls(g.Rules[name].Children[0], rules, calls)
		}

		for callee := range calls {
			edges[name] = append(edges[name], callee)
		}
	}

	var cyclic []string

	for _, name := range g.Order {
		// can the rule get back to itself
		seen := map[string]bool{}
		stack := append([]string(nil), edges[name]...)

		for len(stack) > 0 {
			next := stack[len(stack)-1]
			stack = stack[:len(stack)-1]

			if next == name {
				cyclic = append(cyclic, name)
				break
			}

			if !seen[next] {
				seen[next] = true
				stack = append(stack, edges[next]...)
			}
		}
	}

	return cyclic
}
