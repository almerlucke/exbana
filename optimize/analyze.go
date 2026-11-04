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

type analysis[T Integer, P any] struct {
	g     *ebnf.Grammar[T, P]
	opts  Options
	runes bool

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

func newAnalysis[T Integer, P any](g *ebnf.Grammar[T, P], opts Options) *analysis[T, P] {
	var zero T

	_, isRune := any(zero).(rune)

	a := &analysis[T, P]{
		g: g, opts: opts, runes: isRune,
		ruleNullable: map[string]bool{}, ruleFirst: map[string]set{},
		nodeNullable: map[*ebnf.Node[T, P]]bool{}, nodeFirst: map[*ebnf.Node[T, P]]set{},
		follow: map[string]set{}, nodeFollow: map[*ebnf.Node[T, P]]set{},
		ruleDet: map[string]bool{},
	}

	a.computeFirst()
	a.computeFollow()
	a.computeDeterminism()

	return a
}

func classSet[T Integer, P any](n *ebnf.Node[T, P]) set {
	iv := make([]interval, len(n.Ranges))
	for i, r := range n.Ranges {
		iv[i] = interval{int64(r[0]), int64(r[1])}
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
		case n.Exact:
			v := int64(n.Values[0])
			first = set{{v, v}}
		default:
			first = all // compared with a function nobody can look into
		}
	case ebnf.KindClass:
		if n.Exact {
			first = classSet(n)
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
		// the follow of a part is what the next part starts with, and if that part can match nothing, what comes after it
		fol := f

		for i := len(n.Children) - 1; i >= 0; i-- {
			c := n.Children[i]
			changed = a.walkFollow(c, fol) || changed

			if c.Kind == ebnf.KindEnd {
				// the end of the stream: nothing is consumed after it, and where it does not match (before an element)
				// whatever comes after it is not reached either
				fol = empty
				continue
			}

			fol = union(a.nodeFirst[c], ternary(a.nodeNullable[c], fol, empty))
		}
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
			changed = a.walkFollow(a.g.Rules[name].Children[0], a.follow[name]) || changed
		}
	}
}

// computeDeterminism finds the rules that have only one way to match that can lead to a match of the whole. A rule that
// uses itself is assumed to have one way until a way is found that it has not; marked left recursive rules offer all of their
// matches and never have one.
func (a *analysis[T, P]) computeDeterminism() {
	for _, name := range a.g.Order {
		a.ruleDet[name] = !a.g.Rules[name].LeftRecursive
	}

	for changed := true; changed; {
		changed = false

		for _, name := range a.g.Order {
			if a.ruleDet[name] && !a.det(a.g.Rules[name].Children[0]) {
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
