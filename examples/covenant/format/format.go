// Package format writes Covenant source the one way it is written: the layout of the lines, the spaces, the blank lines, the
// parentheses an expression needs and no others. It is what "covenant fmt" does.
//
// The source is read to a tree, and the tree is written out again. What the tree does not have is kept: a comment is put where it
// was, at the front of what follows it or at the end of the line it was on, and a list that was on more than one line stays so.
// A line that is too long (more than 100 columns) is broken at a place where the grammar lets a line end, which is after an opening
// bracket, a comma or a binary operator.
//
// Source does not trust itself: it reads what it wrote, and gives it back only if that is the same tree, with the same comments.
// A program is never changed by the formatter, and if it ever would be, the answer is an error and not the new program.
package format

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	covenant "github.com/almerlucke/exbana/v3/examples/covenant"
	"github.com/almerlucke/exbana/v3/examples/covenant/ast"
	"github.com/almerlucke/exbana/v3/examples/covenant/token"
)

// ErrBug is what an error from Source wraps when the fault is the formatter's and not the source's: it wrote something that is not the
// program it read. The file is not to be changed then.
var ErrBug = errors.New("a bug in the formatter, and the file is as it was")

// Width is how many columns a line can have before it is broken, if it can be.
const Width = 100

// Source formats a file. If the source is not Covenant, the error says where.
func Source(src string) (string, error) {
	f, tree, out, err := write(src)
	if err != nil {
		return "", err
	}

	if err := check(f, tree, out); err != nil {
		return "", err
	}

	return out, nil
}

// write is Source without the check.
func write(src string) (*covenant.File, *ast.File, string, error) {
	f, err := covenant.Parse(src)
	if err != nil {
		return nil, nil, "", err
	}

	tree := f.AST()

	p := &printer{src: src, toks: f.Tokens, comments: f.Comments}
	p.starts = lineStarts(src)

	out := render(p.file(tree), Width)
	if len(f.Tokens) == 0 && len(f.Comments) == 0 {
		out = ""
	}

	return f, tree, out, nil
}

// check makes sure that what was written is the program that was read.
func check(f *covenant.File, tree *ast.File, out string) error {
	g, err := covenant.Parse(out)
	if err != nil {
		return fmt.Errorf("the formatter wrote source that does not parse (%v): %w", err, ErrBug)
	}

	if a, b := ast.Dump(tree), ast.Dump(g.AST()); a != b {
		return fmt.Errorf("the formatter changed the program: %w\nbefore: %s\nafter:  %s", ErrBug, a, b)
	}

	if !slices.Equal(commentTexts(f.Source, f.Comments), commentTexts(out, g.Comments)) {
		return fmt.Errorf("the formatter lost or changed a comment: %w", ErrBug)
	}

	return nil
}

func commentTexts(src string, cs []token.Comment) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = src[c.Begin:c.End]
	}

	sort.Strings(out)

	return out
}

func lineStarts(src string) []int {
	starts := []int{0}

	for i := range len(src) {
		if src[i] == '\n' {
			starts = append(starts, i+1)
		}
	}

	return starts
}

type printer struct {
	src      string
	toks     []token.Token
	comments []token.Comment
	next     int   // the comments before this one are in the output
	starts   []int // where the lines of the source start
}

func (p *printer) lineOf(off int) int {
	return sort.Search(len(p.starts), func(i int) bool { return p.starts[i] > off })
}

func (p *printer) sameLine(a, b int) bool { return !strings.Contains(p.src[a:b], "\n") }

// take gives the comments that start before a place and have not been written yet.
func (p *printer) take(before int) []token.Comment {
	var out []token.Comment

	for p.next < len(p.comments) && p.comments[p.next].Begin < before {
		out = append(out, p.comments[p.next])
		p.next++
	}

	return out
}

// trailing gives the comment that is on the line of a place, after it, if there is one.
func (p *printer) trailing(after, limit int) (token.Comment, bool) {
	if p.next < len(p.comments) {
		if c := p.comments[p.next]; c.Begin >= after && c.Begin < limit && p.sameLine(after, c.Begin) {
			p.next++

			return c, true
		}
	}

	return token.Comment{}, false
}

func (p *printer) comment(c token.Comment) text { return text(p.src[c.Begin:c.End]) }

// tokenAt is the number of the first token that starts at or after the place.
func (p *printer) tokenAt(off int) int {
	return sort.Search(len(p.toks), func(i int) bool { return p.toks[i].Begin >= off })
}

// tokenEndingAt is the number of the last token that ends at the place, or -1.
func (p *printer) tokenEndingAt(end int) int {
	i := sort.Search(len(p.toks), func(i int) bool { return p.toks[i].End > end }) - 1
	if i >= 0 && p.toks[i].End == end {
		return i
	}

	return -1
}

// firstOf is the number of the first token of a kind at or after a place, or -1.
func (p *printer) firstOf(kind token.Kind, off int) int {
	for i := p.tokenAt(off); i < len(p.toks); i++ {
		if p.toks[i].Kind == kind {
			return i
		}
	}

	return -1
}

func isOpen(k token.Kind) bool  { return k == token.LParen || k == token.LBracket || k == token.LBrace }
func isClose(k token.Kind) bool { return k == token.RParen || k == token.RBracket || k == token.RBrace }

// opener is the bracket that a closing bracket closes.
func (p *printer) opener(closer int) int {
	depth := 0

	for i := closer; i >= 0; i-- {
		switch k := p.toks[i].Kind; {
		case isClose(k):
			depth++
		case isOpen(k):
			if depth--; depth == 0 {
				return i
			}
		}
	}

	return -1
}

// closer is the bracket that closes an opening bracket.
func (p *printer) closer(open int) int {
	depth := 0

	for i := open; i < len(p.toks); i++ {
		switch k := p.toks[i].Kind; {
		case isOpen(k):
			depth++
		case isClose(k):
			if depth--; depth == 0 {
				return i
			}
		}
	}

	return -1
}

// broken says whether the brackets have a line end directly in them, which is how the source says the list is on several lines.
func (p *printer) broken(open, closer int) bool {
	if open < 0 || closer < 0 {
		return false
	}

	depth := 0

	for i := open + 1; i < closer; i++ {
		switch k := p.toks[i].Kind; {
		case isOpen(k):
			depth++
		case isClose(k):
			depth--
		case k == token.NL && depth == 0:
			return true
		}
	}

	return false
}

// An entry is a line of a sequence, which is a block, or a body, or the whole file.
type entry struct {
	begin, end int
	kind       kind
	build      func() doc
}

type kind int

const (
	plain kind = iota
	large      // a blank line on both sides of it
	use
)

// lines writes the entries one to a line, with the comments in between where they were, and the blank lines of the source (not
// more than one in a row, and none at the start or at the end). closeOff is where the sequence ends, for the comments that come
// after the last entry.
func (p *printer) lines(es []entry, openOff, closeOff int) cat {
	var (
		out       cat
		started   bool
		prevLine  int
		displaced bool // the last comment was before the start of the sequence, so the lines of the source say nothing about a gap
	)

	add := func(d doc, begin, endLine int, forced bool) {
		if started {
			out = append(out, hardline)

			if forced || !displaced && p.lineOf(begin)-prevLine > 1 {
				out = append(out, hardline)
			}
		}

		out = append(out, d)
		started, prevLine, displaced = true, endLine, false
	}

	var prev *entry

	for i := range es {
		e := &es[i]

		// a blank line is wanted between a large entry and the one next to it, and between the uses and the rest
		forced := prev != nil && (prev.kind == large || e.kind == large || (prev.kind == use) != (e.kind == use))

		lead := p.take(e.begin)
		d := e.build()
		lead = append(lead, p.take(e.end)...)

		for _, c := range lead {
			add(p.comment(c), c.Begin, p.lineOf(c.End), forced)
			forced, displaced = false, c.Begin < openOff
		}

		if c, ok := p.trailing(e.end, closeOff); ok {
			d = cat{d, suffix(p.comment(c))}
		}

		add(d, e.begin, p.lineOf(max(e.end-1, e.begin)), forced)

		prev = e
	}

	for _, c := range p.take(closeOff) {
		add(p.comment(c), c.Begin, p.lineOf(c.End), false)
	}

	return out
}

// file writes the uses and the declarations.
func (p *printer) file(f *ast.File) doc {
	var es []entry

	for _, u := range f.Uses {
		es = append(es, entry{u.Begin, u.End, use, func() doc { return p.use(u) }})
	}

	for _, it := range f.Items {
		es = append(es, entry{it.Begin, it.End, p.itemKind(it), func() doc { return p.item(it) }})
	}

	return p.lines(es, 0, len(p.src))
}

// itemKind says what a declaration is for the blank lines around it. It is not how long the source of it was: that is not the same
// after it is written, and formatting a file again would change it.
func (p *printer) itemKind(it *ast.Item) kind {
	switch d := it.Decl.(type) {
	case *ast.Fn:
		if d.Body != nil {
			return large
		}
	case *ast.Impl, *ast.Trait, *ast.Enum:
		return large
	}

	return plain
}

func (p *printer) use(u *ast.Use) doc {
	d := "use " + strings.Join(u.Path, "::")
	if u.Alias != "" {
		d += " as " + u.Alias
	}

	return text(d)
}

func (p *printer) item(it *ast.Item) doc {
	pub := cat{}
	if it.Pub {
		pub = cat{text("pub ")}
	}

	switch d := it.Decl.(type) {
	case *ast.Fn:
		return cat{pub, p.fn(d)}
	case *ast.Struct:
		return cat{pub, p.structDecl(d)}
	case *ast.Enum:
		return cat{pub, p.enumDecl(d)}
	case *ast.Trait:
		return cat{pub, p.traitDecl(d)}
	case *ast.Impl:
		return cat{pub, p.implDecl(d)}
	case *ast.Const:
		return cat{pub, text("const " + d.Name + ": "), p.typ(d.Type), text(" = "), p.expr(d.Value, 0)}
	case *ast.Alias:
		return cat{pub, text("type " + d.Name), p.generics(d.Generics), text(" = "), p.typ(d.Type)}
	case *ast.Group:
		return cat{pub, text("effects " + d.Name + " = "), p.effectChain(d.Effects)}
	}

	return text("")
}

// effectChain is a + b + c, and a line may end after a plus.
func (p *printer) effectChain(paths []*ast.Path) doc {
	if len(paths) == 0 {
		return text("")
	}

	rest := cat{}
	for _, e := range paths[1:] {
		rest = append(rest, text(" +"), space, text(strings.Join(e.Names, "::")))
	}

	return newGroup(cat{text(strings.Join(paths[0].Names, "::")), indent{rest}})
}

func (p *printer) withEffects(paths []*ast.Path) doc {
	if len(paths) == 0 {
		return text("")
	}

	return cat{text(" with "), p.effectChain(paths)}
}

func (p *printer) generics(gs []*ast.Generic) doc {
	if len(gs) == 0 {
		return text("")
	}

	var items []doc

	for _, g := range gs {
		s := g.Name

		if len(g.Bounds) > 0 {
			var bs []string
			for _, b := range g.Bounds {
				bs = append(bs, strings.Join(b.Names, "::"))
			}

			s += ": " + strings.Join(bs, " + ")
		}

		items = append(items, text(s))
	}

	return newGroup(cat{text("<"), indent{cat{softline, join(cat{text(","), space}, items)}}, softline, text(">")})
}

func (p *printer) typ(t ast.Type) doc {
	switch t := t.(type) {
	case *ast.TypeName:
		d := cat{text(strings.Join(t.Path, "::"))}

		if len(t.Args) > 0 {
			var args []doc
			for _, a := range t.Args {
				args = append(args, p.typ(a))
			}

			d = append(d, newGroup(cat{text("<"), indent{cat{softline, join(cat{text(","), space}, args)}}, softline, text(">")}))
		}

		return d
	case *ast.TypeDyn:
		return text("dyn " + strings.Join(t.Path, "::"))
	case *ast.TypeFn:
		var params []doc
		for _, a := range t.Params {
			params = append(params, p.typ(a))
		}

		d := cat{text("Fn")}

		if len(params) == 0 {
			d = append(d, text("()"))
		} else {
			d = append(d, newGroup(cat{text("("), indent{cat{softline, join(cat{text(","), space}, params)}}, softline, text(")")}))
		}

		d = append(d, p.withEffects(t.Effects))

		if t.Ret != nil {
			d = append(d, text(" -> "), p.typ(t.Ret))
		}

		return d
	}

	return text("")
}

// list writes the items between brackets, on one line if they fit and if the source had them so, and one to a line if not. With
// pad there is a space inside the brackets when it is on one line: { x: Int }. The brackets are tokens, open and closer.
func (p *printer) list(open, close string, pad bool, openTok, closeTok int, es []entry) doc {
	closeOff := len(p.src)
	if closeTok >= 0 {
		closeOff = p.toks[closeTok].Begin
	}

	edge := softline
	if pad {
		edge = space
	}

	var items cat

	for i, e := range es {
		var lead cat

		cs := p.take(e.begin)
		d := e.build()
		cs = append(cs, p.take(e.end)...)

		for _, c := range cs {
			lead = append(lead, p.comment(c), hardline)
		}

		item := cat{lead, d}

		last := i == len(es)-1
		if !last {
			item = append(item, text(","))
		}

		// a comment at the end of the line of the item: after the comma, if there is one
		limit := closeOff
		if !last {
			limit = es[i+1].begin
		}

		if c, ok := p.trailing(e.end, limit); ok {
			item = append(item, suffix(p.comment(c)))
		}

		if i > 0 {
			items = append(items, space)
		}

		items = append(items, item)
	}

	// comments after the last item
	var tail cat
	for _, c := range p.take(closeOff) {
		tail = append(tail, hardline, p.comment(c))
	}

	if len(items) == 0 && len(tail) == 0 {
		return text(open + close)
	}

	return sourceGroup(cat{text(open), indent{cat{edge, items, tail}}, edge, text(close)}, p.broken(openTok, closeTok))
}

// fnOpen finds the parentheses of the parameters, which are the first ones after where the function starts.
func (p *printer) parens(begin int) (int, int) {
	open := p.firstOf(token.LParen, begin)
	if open < 0 {
		return -1, -1
	}

	return open, p.closer(open)
}

func (p *printer) fn(f *ast.Fn) doc {
	var es []entry

	for _, a := range f.Params {
		es = append(es, entry{a.Begin, a.End, plain, func() doc {
			if a.Self {
				return text("self")
			}

			return cat{text(a.Name + ": "), p.typ(a.Type)}
		}})
	}

	open, closeTok := p.parens(f.Begin)

	d := cat{text("fn " + f.Name), p.generics(f.Generics), p.list("(", ")", false, open, closeTok, es), p.withEffects(f.Effects)}

	if f.Ret != nil {
		d = append(d, text(" -> "), p.typ(f.Ret))
	}

	if f.Body != nil {
		d = append(d, text(" "), p.block(f.Body))
	}

	return d
}

func (p *printer) fields(open string, f []*ast.Field, begin, end int) doc {
	var es []entry

	for _, a := range f {
		es = append(es, entry{a.Begin, a.End, plain, func() doc { return cat{text(a.Name + ": "), p.typ(a.Type)} }})
	}

	closeTok := p.tokenEndingAt(end)
	openTok := -1

	if closeTok >= 0 {
		openTok = p.opener(closeTok)
	}

	if open == "{" {
		return p.list("{", "}", true, openTok, closeTok, es)
	}

	return p.list("(", ")", false, openTok, closeTok, es)
}

func (p *printer) structDecl(s *ast.Struct) doc {
	return cat{text("struct " + s.Name), p.generics(s.Generics), text(" "), p.fields("{", s.Fields, s.Begin, s.End)}
}

// body writes the lines of a body between braces: a block, an enum, a trait, an impl or a match.
func (p *printer) body(braceOff, closeOff int, es []entry) doc {
	openOff := braceOff

	// a comment on the line of the brace stays there
	var head doc = text("")

	limit := closeOff
	if len(es) > 0 {
		limit = es[0].begin
	}

	if c, ok := p.trailing(braceOff+1, limit); ok {
		head = suffix(p.comment(c))
	}

	lines := p.lines(es, openOff, closeOff)
	if len(lines) == 0 {
		// nothing in it; but a comment on the line of the brace is the end of that line, and the brace that closes goes on the next
		if _, ok := head.(suffix); ok {
			return cat{text("{"), head, hardline, text("}")}
		}

		return cat{text("{}")}
	}

	return cat{text("{"), head, indent{cat{hardline, lines}}, hardline, text("}")}
}

func (p *printer) enumDecl(e *ast.Enum) doc {
	var es []entry

	for _, v := range e.Variants {
		es = append(es, entry{v.Begin, v.End, plain, func() doc {
			if len(v.Fields) == 0 {
				return text(v.Name)
			}

			return cat{text(v.Name), p.fields("(", v.Fields, v.Begin, v.End)}
		}})
	}

	brace := p.firstOf(token.LBrace, e.Begin)

	return cat{text("enum " + e.Name), p.generics(e.Generics), text(" "), p.body(p.toks[brace].Begin, e.End-1, es)}
}

func (p *printer) traitDecl(t *ast.Trait) doc {
	var es []entry

	for _, m := range t.Methods {
		es = append(es, entry{m.Begin, m.End, plain, func() doc { return p.fn(m) }})
	}

	brace := p.firstOf(token.LBrace, t.Begin)

	return cat{text("trait " + t.Name), p.generics(t.Generics), text(" "), p.body(p.toks[brace].Begin, t.End-1, es)}
}

func (p *printer) implDecl(im *ast.Impl) doc {
	var es []entry

	for _, m := range im.Methods {
		begin := m.Begin
		if m.Pub {
			begin = p.toks[p.tokenAt(m.Begin)-1].Begin
		}

		kind := large
		if m.Body == nil {
			kind = plain
		}

		es = append(es, entry{begin, m.End, kind, func() doc {
			if m.Pub {
				return cat{text("pub "), p.fn(m)}
			}

			return p.fn(m)
		}})
	}

	head := cat{text("impl ")}
	if im.Trait != nil {
		head = append(head, p.typ(im.Trait), text(" for "))
	}

	head = append(head, p.typ(im.Type), text(" "))

	brace := p.firstOf(token.LBrace, im.Begin)

	return cat{head, p.body(p.toks[brace].Begin, im.End-1, es)}
}

// block writes a block between braces: the promises, and then the statements.
func (p *printer) block(b *ast.Block) doc {
	var es []entry

	add := func(x ast.Expr, kw string) {
		begin := p.clauseStart(x.At().Begin)
		es = append(es, entry{begin, x.At().End, plain, func() doc { return cat{text(kw + " "), p.expr(x, 0)} }})
	}

	for _, x := range b.Requires {
		add(x, "requires")
	}

	for _, x := range b.Examples {
		add(x, "example")
	}

	for _, s := range b.Stmts {
		es = append(es, entry{s.At().Begin, s.At().End, plain, func() doc { return p.stmt(s) }})
	}

	sort.SliceStable(es, func(i, j int) bool { return es[i].begin < es[j].begin })

	return p.body(b.Begin, b.End-1, es)
}

// clauseStart is where the word requires or example is, in front of an expression.
func (p *printer) clauseStart(exprBegin int) int {
	for i := p.tokenAt(exprBegin) - 1; i >= 0; i-- {
		if k := p.toks[i].Kind; k == token.Requires || k == token.Example {
			return p.toks[i].Begin
		}
	}

	return exprBegin
}

func (p *printer) stmt(s ast.Stmt) doc {
	switch s := s.(type) {
	case *ast.Let:
		d := cat{text("let ")}

		if s.Pattern != nil {
			d = append(d, p.pattern(s.Pattern))
		} else {
			d = append(d, text(s.Name))

			if s.Type != nil {
				d = append(d, text(": "), p.typ(s.Type))
			}
		}

		return append(d, text(" = "), p.expr(s.Value, 0))
	case *ast.Var:
		d := cat{text("var " + s.Name)}

		if s.Type != nil {
			d = append(d, text(": "), p.typ(s.Type))
		}

		return append(d, text(" = "), p.expr(s.Value, 0))
	case *ast.Return:
		if s.Value == nil {
			return text("return")
		}

		return cat{text("return "), p.expr(s.Value, 0)}
	case *ast.For:
		var pats []doc
		for _, pt := range s.Patterns {
			pats = append(pats, p.pattern(pt))
		}

		return cat{text("for "), join(text(", "), pats), text(" in "), p.expr(s.Iter, 1), text(" "), p.block(s.Body)}
	case *ast.While:
		return cat{text("while "), p.expr(s.Cond, 1), text(" "), p.block(s.Body)}
	case *ast.Assign:
		return cat{p.expr(s.Target, 0), text(" = "), p.expr(s.Value, 0)}
	case *ast.ExprStmt:
		return p.expr(s.X, 0)
	}

	return text("")
}

func (p *printer) pattern(pt ast.Pattern) doc {
	switch pt := pt.(type) {
	case *ast.PatWild:
		return text("_")
	case *ast.PatLit:
		return text(pt.Lit.Text)
	case *ast.PatName:
		d := cat{text(strings.Join(pt.Path, "::"))}

		if pt.HasArgs {
			var es []entry
			for _, a := range pt.Args {
				es = append(es, entry{a.At().Begin, a.At().End, plain, func() doc { return p.pattern(a) }})
			}

			closeTok := p.tokenEndingAt(pt.End)
			openTok := -1

			if closeTok >= 0 {
				openTok = p.opener(closeTok)
			}

			d = append(d, p.list("(", ")", false, openTok, closeTok, es))
		}

		return d
	}

	return text("")
}

// The strength of what holds an expression together, from what holds the least to what holds the most: a closure, and, or, not,
// a comparison, a sum, a product, a sign in front, and then what comes behind: a call, a member, a cast. A part of an expression
// that holds less than the place it is in needs parentheses.
func prec(e ast.Expr) int {
	switch e := e.(type) {
	case *ast.Closure:
		return 0
	case *ast.Binary:
		switch e.Op {
		case "and", "or":
			return 1
		case "==", "!=", "<", "<=", ">", ">=":
			return 3
		case "+", "-":
			return 4
		}

		return 5
	case *ast.Unary:
		if e.Op == "not" {
			return 2
		}

		return 6
	case *ast.Call, *ast.Member, *ast.Update, *ast.Cast:
		return 7
	}

	return 8
}

// expr writes an expression that is in a place that needs at least the strength min.
func (p *printer) expr(e ast.Expr, min int) doc {
	d := p.bare(e)
	if prec(e) < min {
		return cat{text("("), d, text(")")}
	}

	return d
}

func (p *printer) bare(e ast.Expr) doc {
	switch e := e.(type) {
	case *ast.Lit:
		return text(e.Text)
	case *ast.Name:
		return text(strings.Join(e.Path, "::"))
	case *ast.List:
		var es []entry
		for _, x := range e.Elems {
			es = append(es, entry{x.At().Begin, x.At().End, plain, func() doc { return p.expr(x, 0) }})
		}

		closeTok := p.tokenEndingAt(e.End)

		return p.list("[", "]", false, p.opener(closeTok), closeTok, es)
	case *ast.Map:
		if len(e.Entries) == 0 {
			// the empty map has a colon in it, and the comments that are in it come before the colon
			cs := p.take(e.End)
			if len(cs) == 0 {
				return text("{:}")
			}

			var lines cat
			for _, c := range cs {
				lines = append(lines, hardline, p.comment(c))
			}

			return cat{text("{"), indent{cat{lines, hardline, text(":")}}, hardline, text("}")}
		}

		var es []entry
		for _, en := range e.Entries {
			es = append(es, entry{en.Begin, en.End, plain, func() doc { return cat{p.key(en.Key), text(": "), p.expr(en.Value, 0)} }})
		}

		closeTok := p.tokenEndingAt(e.End)

		return p.list("{", "}", false, p.opener(closeTok), closeTok, es)
	case *ast.Call:
		var es []entry

		for _, a := range e.Args {
			es = append(es, entry{a.Begin, a.End, plain, func() doc {
				if a.Label != "" {
					return cat{text(a.Label + ": "), p.expr(a.Value, 0)}
				}

				return p.expr(a.Value, 0)
			}})
		}

		fun := p.expr(e.Fun, 7)
		closeTok := p.tokenEndingAt(e.End)

		return cat{fun, p.list("(", ")", false, p.opener(closeTok), closeTok, es)}
	case *ast.Member:
		return cat{p.expr(e.X, 7), text("." + e.Name)}
	case *ast.Cast:
		return cat{p.expr(e.X, 7), text(" as dyn " + strings.Join(e.Type.Path, "::"))}
	case *ast.Update:
		x := p.expr(e.X, 7)

		var es []entry
		for _, f := range e.Fields {
			es = append(es, entry{f.Begin, f.End, plain, func() doc { return cat{text(f.Name + ": "), p.expr(f.Value, 0)} }})
		}

		closeTok := p.tokenEndingAt(e.End)

		return cat{x, text(" with "), p.list("{", "}", true, p.opener(closeTok), closeTok, es)}
	case *ast.Unary:
		switch e.Op {
		case "not":
			return cat{text("not "), p.expr(e.X, 3)}
		case "try":
			return cat{text("try "), p.expr(e.X, 7)}
		}

		return cat{text("-"), p.expr(e.X, 7)}
	case *ast.Binary:
		return p.binary(e)
	case *ast.Closure:
		return p.closure(e)
	case *ast.Match:
		var es []entry
		for _, a := range e.Arms {
			es = append(es, entry{a.Begin, a.End, plain, func() doc { return cat{p.pattern(a.Pattern), text(" => "), p.expr(a.Value, 0)} }})
		}

		x := p.expr(e.X, 1)
		brace := p.firstOf(token.LBrace, e.X.At().End)

		return hug{cat{text("match "), x, text(" "), p.body(p.toks[brace].Begin, e.End-1, es)}}
	case *ast.If:
		return hug{p.ifExpr(e)}
	}

	return text("")
}

// key is the key of an entry of a map. The colon after it must not be the end of a label: "a: 1" is a label and a value, and so is "x % a: 1"
// if a name ends the key, which is not the key x % a. So a key that is not a name, a literal or something that ends in a bracket or in
// a member is between parentheses.
func (p *printer) key(k ast.Expr) doc {
	switch k := k.(type) {
	case *ast.Lit, *ast.Name, *ast.Member, *ast.Call, *ast.List, *ast.Map, *ast.Update:
		return p.expr(k, 1)
	}

	return cat{text("("), p.bare(k), text(")")}
}

func (p *printer) ifExpr(e *ast.If) doc {
	d := cat{text("if "), p.expr(e.Cond, 1), text(" "), p.block(e.Then)}

	switch el := e.Else.(type) {
	case *ast.If:
		d = append(d, text(" else "), p.ifExpr(el))
	case *ast.Block:
		d = append(d, text(" else "), p.block(el))
	}

	return d
}

func (p *printer) closure(c *ast.Closure) doc {
	var es []entry

	for _, a := range c.Params {
		es = append(es, entry{a.Begin, a.End, plain, func() doc {
			if a.Type != nil {
				return cat{text(a.Name + ": "), p.typ(a.Type)}
			}

			return text(a.Name)
		}})
	}

	open, closeTok := p.parens(c.Begin)

	d := cat{text("fn"), p.list("(", ")", false, open, closeTok, es)}

	if c.Ret != nil {
		d = append(d, text(" -> "), p.typ(c.Ret))
	}

	if c.Body != nil {
		return hug{append(d, text(" "), p.block(c.Body))}
	}

	return append(d, text(" => "), p.expr(c.Value, 0))
}

// binary writes a row of operators of one strength as a group: if it does not fit, the lines end after the operators.
func (p *printer) binary(e *ast.Binary) doc {
	level := prec(e)

	var (
		operands []ast.Expr
		ops      []string
	)

	// the row goes to the left; a comparison is never a row, as it does not chain
	operands, ops = append(operands, e.R), append(ops, e.Op)
	cur := e.L

	for level != 3 {
		b, ok := cur.(*ast.Binary)
		if !ok || prec(b) != level || (level == 1 && b.Op != e.Op) {
			break
		}

		operands = append(operands, b.R)
		ops = append(ops, b.Op)
		cur = b.L
	}

	operands = append(operands, cur)
	slices.Reverse(operands)
	slices.Reverse(ops)

	// the operands of a comparison are sums or stronger, and those of and and or are not or stronger; those of the others are as strong
	// as the operator, and on the right stronger, as the row goes to the left
	first, rest := level, level+1

	switch level {
	case 1:
		first, rest = 2, 2
	case 3:
		first, rest = 4, 4
	}

	d := cat{p.expr(operands[0], first)}

	var tail cat

	for i, op := range ops {
		tail = append(tail, text(" "+op), space, p.expr(operands[i+1], rest))
	}

	// a line the source ended after an operator stays ended
	broken := false

	for i := 0; i+1 < len(operands) && !broken; i++ {
		for _, t := range p.toks[p.tokenAt(operands[i].At().End):] {
			if t.Begin >= operands[i+1].At().Begin {
				break
			}

			broken = broken || t.Kind == token.NL
		}
	}

	return chainGroup(cat{d, indent{tail}}, broken)
}
