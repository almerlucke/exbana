package optimize

import (
	"fmt"

	ebnf "github.com/almerlucke/exbana/v3"
)

// Integer is the element types the analysis can reason about: it computes with sets of elements, and sets of integers
// can be stored as ranges. Runes, bytes and integer tokens are all fine.
type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint8 | ~uint16 | ~uint32
}

type analysis[T, P any] struct {
	g     *ebnf.Grammar[T, P]
	opts  Options
	runes bool

	// conv maps an element to an integer. Without it nothing is known about what a leaf accepts.
	conv func(T) int64

	// the left recursive rules and how they split into base branches and tails
	lr map[string]lrSplit[T, P]

	// what can be said about every node and rule on its own: whether it can match nothing, and which elements it can
	// start with
	ruleNullable map[string]bool
	ruleFirst    map[string]set
	nodeNullable map[*ebnf.Node[T, P]]bool
	nodeFirst    map[*ebnf.Node[T, P]]set

	// what can come after a node: the elements that can follow it, in every place it is used
	follow     map[string]set
	nodeFollow map[*ebnf.Node[T, P]]set

	// whether a rule has only one way to match that can lead to a match of the whole
	ruleDet map[string]bool
}

type lrSplit[T, P any] struct {
	bases, tails []*ebnf.Node[T, P]
	err          error
}

func newAnalysis[T, P any](g *ebnf.Grammar[T, P], opts Options, conv func(T) int64) *analysis[T, P] {
	var zero T

	_, isRune := any(zero).(rune)

	a := &analysis[T, P]{
		g: g, opts: opts, runes: isRune, conv: conv, lr: map[string]lrSplit[T, P]{},
		ruleNullable: map[string]bool{}, ruleFirst: map[string]set{},
		nodeNullable: map[*ebnf.Node[T, P]]bool{}, nodeFirst: map[*ebnf.Node[T, P]]set{},
		follow: map[string]set{}, nodeFollow: map[*ebnf.Node[T, P]]set{},
		ruleDet: map[string]bool{},
	}

	for _, name := range g.Order {
		if g.Rules[name].LeftRecursive {
			bases, tails, err := g.SplitLeftRecursive(name)
			a.lr[name] = lrSplit[T, P]{bases, tails, err}
		}
	}

	a.computeFirst()
	a.computeFollow()
	a.computeDeterminism()

	return a
}

func (a *analysis[T, P]) classSet(n *ebnf.Node[T, P]) set {
	if a.conv == nil {
		return all
	}

	iv := make([]interval, len(n.Ranges))
	for i, r := range n.Ranges {
		iv[i] = interval{a.conv(r[0]), a.conv(r[1])}
	}

	s := normalize(iv)
	if n.Negated {
		return complement(s)
	}

	return s
}

// nullableFirst works out whether the node can match nothing and with which elements it can start, from its parts.
func (a *analysis[T, P]) nullableFirst(n *ebnf.Node[T, P]) (bool, set) {
	var (
		null  bool
		first set
	)

	switch n.Kind {
	case ebnf.KindLiteral:
		switch {
		case len(n.Values) == 0:
			null = true
		case n.Exact && a.conv != nil:
			v := a.conv(n.Values[0])
			first = set{{v, v}}
		default:
			first = all // compared with a function nobody can look into
		}
	case ebnf.KindClass:
		if n.Exact {
			first = a.classSet(n)
		} else {
			first = all
		}
	case ebnf.KindEnd:
		// only matches where the stream ends, where no element can follow. Treating it as able to match nothing is the
		// safe way to look at it: whatever comes after it then counts as following what comes before it.
		null = true
	case ebnf.KindSeq:
		null = true

		for _, c := range n.Children {
			cn, cf := a.nullableFirst(c)
			if null {
				first = union(first, cf)
			}

			null = null && cn
		}
	case ebnf.KindAlt:
		for _, c := range n.Children {
			cn, cf := a.nullableFirst(c)
			null = null || cn
			first = union(first, cf)
		}
	case ebnf.KindRepeat:
		cn, cf := a.nullableFirst(n.Children[0])
		null, first = n.Min == 0 || cn, cf
	case ebnf.KindExcept:
		// what the exception removes is not subtracted: a superset is the safe answer
		cn, cf := a.nullableFirst(n.Children[0])
		a.nullableFirst(n.Children[1])
		null, first = cn, cf
	case ebnf.KindRef:
		null, first = a.ruleNullable[n.ID], a.ruleFirst[n.ID]
	default: // opaque
		null, first = true, all
	}

	a.nodeNullable[n], a.nodeFirst[n] = null, first

	return null, first
}

// computeFirst finds, for every rule, whether it can match nothing and its first elements. Rules use each other, so
// it repeats until nothing changes; both only ever grow, which is why that ends.
func (a *analysis[T, P]) computeFirst() {
	for changed := true; changed; {
		changed = false

		for _, name := range a.g.Order {
			null, first := a.nullableFirst(a.g.Rules[name].Children[0])

			if null != a.ruleNullable[name] || !equal(first, a.ruleFirst[name]) {
				a.ruleNullable[name], a.ruleFirst[name] = null, first
				changed = true
			}
		}
	}

	a.nullableFirst(a.g.Root)
}

// walkFollow gives every node below n the elements that can follow it, given those that can follow n.
func (a *analysis[T, P]) walkFollow(n *ebnf.Node[T, P], f set) (changed bool) {
	a.nodeFollow[n] = f

	switch n.Kind {
	case ebnf.KindSeq:
		changed = a.walkSeq(n.Children, f)
	case ebnf.KindAlt:
		for _, c := range n.Children {
			changed = a.walkFollow(c, f) || changed
		}
	case ebnf.KindRepeat:
		// after an item comes another item, or what follows the repetition; an optional item is not repeated
		c := n.Children[0]
		if n.Max == 1 {
			changed = a.walkFollow(c, f) || changed
		} else {
			changed = a.walkFollow(c, union(a.nodeFirst[c], f)) || changed
		}
	case ebnf.KindExcept:
		changed = a.walkFollow(n.Children[0], f) || changed
		// the exception is only looked at, what follows it is not known
		changed = a.walkFollow(n.Children[1], all) || changed
	case ebnf.KindRef:
		if grown := union(a.follow[n.ID], f); !equal(grown, a.follow[n.ID]) {
			a.follow[n.ID] = grown
			changed = true
		}
	}

	return changed
}

// walkSeq gives the parts of a sequence what can follow each of them: the follow of a part is what the next part starts with,
// and if that part can match nothing, what comes after it.
func (a *analysis[T, P]) walkSeq(parts []*ebnf.Node[T, P], f set) (changed bool) {
	fol := f

	for i := len(parts) - 1; i >= 0; i-- {
		c := parts[i]
		changed = a.walkFollow(c, fol) || changed

		if c.Kind == ebnf.KindEnd {
			// the end of the stream: nothing is consumed after it, and where it does not match (before an element)
			// whatever comes after it is not reached either
			fol = empty
			continue
		}

		fol = union(a.nodeFirst[c], ternary(a.nodeNullable[c], fol, empty))
	}

	return changed
}

// seqInfo is whether a series of parts can match nothing and with which elements it can start.
func (a *analysis[T, P]) seqInfo(parts []*ebnf.Node[T, P]) (null bool, first set) {
	null = true

	for _, c := range parts {
		if null {
			first = union(first, a.nodeFirst[c])
		}

		null = null && a.nodeNullable[c]
	}

	return null, first
}

// walkRule gives what can follow to the nodes of a rule. A left recursive rule that is a base followed by tails is walked as
// the loop it can be: after the base, and after every tail, come more tails, or what follows the rule.
func (a *analysis[T, P]) walkRule(name string) bool {
	body := a.g.Rules[name].Children[0]
	f := a.follow[name]

	sp, ok := a.lr[name]
	if !ok || sp.err != nil || len(sp.tails) == 0 {
		return a.walkFollow(body, f)
	}

	a.nodeFollow[body] = f

	var tailsFirst set

	for _, t := range sp.tails {
		_, first := a.seqInfo(t.Children[1:])
		tailsFirst = union(tailsFirst, first)
	}

	inner := union(tailsFirst, f)
	changed := false

	for _, b := range sp.bases {
		changed = a.walkFollow(b, inner) || changed
	}

	for _, t := range sp.tails {
		// the first part is the rule itself, which is not a call: it is the loop
		a.nodeFollow[t] = inner
		a.nodeFollow[t.Children[0]] = inner
		changed = a.walkSeq(t.Children[1:], inner) || changed
	}

	return changed
}

func ternary[V any](cond bool, yes, no V) V {
	if cond {
		return yes
	}

	return no
}

// computeFollow finds, for every rule, what can follow it wherever it is used. The pattern that was analysed is followed by
// anything unless the options say it is the whole input.
func (a *analysis[T, P]) computeFollow() {
	rootFollow := all
	if a.opts.Closed {
		rootFollow = empty
	}

	for changed := true; changed; {
		changed = a.walkFollow(a.g.Root, rootFollow)

		for _, name := range a.g.Order {
			changed = a.walkRule(name) || changed
		}
	}
}

// computeDeterminism finds the rules that have only one way to match that can lead to a match of the whole. A rule that
// uses itself is assumed to have one way until a way is found that it has not. A left recursive rule offers all of its matches,
// which comes down to one way if it can be evaluated as a loop, see lrLoopSafe.
func (a *analysis[T, P]) computeDeterminism() {
	for _, name := range a.g.Order {
		a.ruleDet[name] = true
	}

	for changed := true; changed; {
		changed = false

		for _, name := range a.g.Order {
			if !a.ruleDet[name] {
				continue
			}

			var det bool

			if sp, ok := a.lr[name]; ok && len(sp.tails) > 0 || ok && sp.err != nil {
				det, _ = a.lrLoopSafe(name)
			} else {
				det = a.det(a.g.Rules[name].Children[0])
			}

			if !det {
				a.ruleDet[name] = false
				changed = true
			}
		}
	}
}

// det: the node has only one way to match that can lead to a match of the whole, so that trying its other ways, if it has
// them, is never of any use.
func (a *analysis[T, P]) det(n *ebnf.Node[T, P]) bool {
	switch n.Kind {
	case ebnf.KindLiteral, ebnf.KindClass, ebnf.KindEnd:
		return true
	case ebnf.KindSeq:
		for _, c := range n.Children {
			if !a.det(c) {
				return false
			}
		}

		return true
	case ebnf.KindAlt:
		if n.Orthogonal {
			return true
		}

		safe, _ := a.orthogonalSafe(n)

		return safe
	case ebnf.KindRepeat:
		if n.Possessive {
			return true
		}

		safe, _ := a.possessiveSafe(n)

		return safe
	case ebnf.KindExcept:
		return a.det(n.Children[0])
	case ebnf.KindRef:
		return a.ruleDet[n.ID]
	}

	return false // opaque
}

const (
	opaqueHint = " (a pattern the analysis can not look into: use entity.Between, In, vector.Text and the like)"
	openHint   = " (the grammar may be followed by anything: end it with an end pattern, or use Options.Closed if it is the whole input)"
)

// overlapReason says why two sets of elements, which must not have any in common, do: the hint says what to do about it when
// one of them is "anything".
func (a *analysis[T, P]) overlapReason(what string, x, y set, yIsFollow bool) string {
	common := intersect(x, y)
	r := fmt.Sprintf("%s can both start with %s", what, common.format(a.runes))

	switch {
	case x.isAll():
		r += opaqueHint
	case y.isAll() && yIsFollow:
		r += openHint
	case y.isAll():
		r += opaqueHint
	}

	return r
}

// possessiveSafe: giving back repetitions of the item can never help, so a repetition that never gives anything back
// matches exactly what the one that does.
func (a *analysis[T, P]) possessiveSafe(n *ebnf.Node[T, P]) (bool, string) {
	item := n.Children[0]

	switch {
	case a.nodeNullable[item]:
		return false, "the repeated item can match nothing" + ternary(a.nodeFirst[item].isAll(), opaqueHint, "")
	case !a.det(item):
		return false, "the repeated item can match in more than one way"
	}

	follow := a.nodeFollow[n]
	if !intersect(a.nodeFirst[item], follow).isEmpty() {
		return false, a.overlapReason("the repeated item and what follows it", a.nodeFirst[item], follow, true)
	}

	return true, fmt.Sprintf("the item starts with %s, what follows never does", a.nodeFirst[item].format(a.runes))
}

// orthogonalSafe: at most one branch can match, and it has only one useful way to, so committing to the first branch that
// matches is the same as trying them all.
func (a *analysis[T, P]) orthogonalSafe(n *ebnf.Node[T, P]) (bool, string) {
	for i, c := range n.Children {
		if a.nodeNullable[c] {
			return false, fmt.Sprintf("branch %d can match nothing", i+1) + ternary(a.nodeFirst[c].isAll(), opaqueHint, "")
		}
	}

	for i := range n.Children {
		for j := i + 1; j < len(n.Children); j++ {
			if x, y := a.nodeFirst[n.Children[i]], a.nodeFirst[n.Children[j]]; !intersect(x, y).isEmpty() {
				return false, a.overlapReason(fmt.Sprintf("branches %d and %d", i+1, j+1), x, y, false)
			}
		}
	}

	for i, c := range n.Children {
		if !a.det(c) {
			return false, fmt.Sprintf("branch %d can match in more than one way", i+1)
		}
	}

	return true, "the branches start with different elements, so at most one can match"
}

// lrLoopSafe: a left recursive rule, a base followed by tails, can be evaluated as a loop that takes a base branch and then tails
// for as long as they match, and that is what the rule matches when it is asked for its longest match: at most one base
// branch and at most one tail can match, each in only one way, and what a tail starts with can not also be what follows the
// rule (or giving a tail back could help).
func (a *analysis[T, P]) lrLoopSafe(name string) (bool, string) {
	sp := a.lr[name]
	if sp.err != nil {
		return false, sp.err.Error()
	}

	for i, b := range sp.bases {
		if a.nodeNullable[b] {
			return false, fmt.Sprintf("base branch %d can match nothing", i+1) + ternary(a.nodeFirst[b].isAll(), opaqueHint, "")
		}

		if !a.det(b) {
			return false, fmt.Sprintf("base branch %d can match in more than one way", i+1)
		}
	}

	for i := range sp.bases {
		for j := i + 1; j < len(sp.bases); j++ {
			if x, y := a.nodeFirst[sp.bases[i]], a.nodeFirst[sp.bases[j]]; !intersect(x, y).isEmpty() {
				return false, a.overlapReason(fmt.Sprintf("base branches %d and %d", i+1, j+1), x, y, false)
			}
		}
	}

	var (
		tailsFirst set
		firsts     []set
	)

	for i, t := range sp.tails {
		null, first := a.seqInfo(t.Children[1:])
		if null {
			return false, fmt.Sprintf("tail %d can match nothing, the loop would not end", i+1) + ternary(first.isAll(), opaqueHint, "")
		}

		for _, c := range t.Children[1:] {
			if !a.det(c) {
				return false, fmt.Sprintf("tail %d can match in more than one way", i+1)
			}
		}

		firsts = append(firsts, first)
		tailsFirst = union(tailsFirst, first)
	}

	for i := range firsts {
		for j := i + 1; j < len(firsts); j++ {
			if !intersect(firsts[i], firsts[j]).isEmpty() {
				return false, a.overlapReason(fmt.Sprintf("tails %d and %d", i+1, j+1), firsts[i], firsts[j], false)
			}
		}
	}

	if follow := a.follow[name]; !intersect(tailsFirst, follow).isEmpty() {
		return false, a.overlapReason("what a tail starts with and what follows the rule", tailsFirst, follow, true)
	}

	return true, fmt.Sprintf("a base branch then tails: the tails start with %s, what follows the rule never does", tailsFirst.format(a.runes))
}
