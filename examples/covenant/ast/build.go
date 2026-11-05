package ast

import (
	"strings"

	"github.com/almerlucke/exbana/v3/examples/covenant/parser"
	"github.com/almerlucke/exbana/v3/examples/covenant/token"
)

// Build makes the tree of a file from what the parser returned. The tree is the one for src, and tokens are the tokens the parser
// read: the positions in the parser's tree are the numbers of tokens, and in the result they are bytes of the source.
func Build(src string, tokens []token.Token, tree *parser.CovenantTree) *File {
	b := &builder{src: src, toks: tokens, t: tree}

	f := &File{Span: Span{0, len(src)}}

	top, ok := tree.First(-1, parser.CovenantRuleFile)
	if !ok {
		return f
	}

	for u := range tree.Find(top, parser.CovenantRuleUse) {
		f.Uses = append(f.Uses, b.use(u))
	}

	for it := range tree.Find(top, parser.CovenantRuleItem) {
		f.Items = append(f.Items, b.item(it))
	}

	return f
}

type builder struct {
	src  string
	toks []token.Token
	t    *parser.CovenantTree
}

type rule = parser.CovenantRule

const (
	rFn          = parser.CovenantRuleFn
	rName        = parser.CovenantRuleName
	rPath        = parser.CovenantRulePath
	rType        = parser.CovenantRuleType
	rExpr        = parser.CovenantRuleExpr
	rBlock       = parser.CovenantRuleBlock
	rPattern     = parser.CovenantRulePattern
	rLabel       = parser.CovenantRuleLabel
	rGenerics    = parser.CovenantRuleGenerics
	rGeneric     = parser.CovenantRuleGeneric
	rParam       = parser.CovenantRuleParam
	rEffect      = parser.CovenantRuleEffect
	rField       = parser.CovenantRuleField
	rFnType      = parser.CovenantRuleFnType
	rFnSig       = parser.CovenantRuleFnSig
	rClosureParm = parser.CovenantRuleClosureParam
	rIf          = parser.CovenantRuleIf
)

func (b *builder) span(n int) Span {
	begin, end := b.t.Span(n)
	if begin >= len(b.toks) {
		return Span{len(b.src), len(b.src)}
	}

	if end <= begin {
		return Span{b.toks[begin].Begin, b.toks[begin].Begin}
	}

	// the line end that closes a statement or a declaration is not part of it
	if end-1 > begin && b.toks[end-1].Kind == token.NL {
		end--
	}

	return Span{b.toks[begin].Begin, b.toks[end-1].End}
}

func (b *builder) kids(n int, r rule) []int {
	var out []int
	for c := range b.t.Find(n, r) {
		out = append(out, c)
	}

	return out
}

func (b *builder) first(n int, r rule) (int, bool) { return b.t.First(n, r) }

// firstKind is the kind of the first token of a node.
func (b *builder) firstKind(n int) token.Kind {
	begin, _ := b.t.Span(n)
	if begin < len(b.toks) {
		return b.toks[begin].Kind
	}

	return 0
}

func (b *builder) tokText(i int) string { return b.src[b.toks[i].Begin:b.toks[i].End] }

func (b *builder) text(n int) string { return b.src[b.span(n).Begin:b.span(n).End] }

// nameOf is the name in a node of the rule name or label: a label without its colon.
func (b *builder) nameOf(n int) string {
	return strings.TrimSuffix(b.text(n), ":")
}

func (b *builder) names(n int) []string {
	begin, end := b.t.Span(n)

	var out []string

	for i := begin; i < end; i++ {
		if k := b.toks[i].Kind; k == token.Ident || k == token.Self {
			out = append(out, b.tokText(i))
		}
	}

	return out
}

func (b *builder) path(n int) *Path {
	return &Path{b.span(n), b.names(n)}
}

func (b *builder) use(n int) *Use {
	u := &Use{Span: b.span(n)}

	if p, ok := b.first(n, rPath); ok {
		u.Path = b.names(p)
	}

	if a, ok := b.first(n, rName); ok {
		u.Alias = b.text(a)
	}

	return u
}

func (b *builder) generics(parent int) []*Generic {
	g, ok := b.first(parent, rGenerics)
	if !ok {
		return nil
	}

	var out []*Generic

	for _, c := range b.kids(g, rGeneric) {
		gen := &Generic{Span: b.span(c)}

		if l, ok := b.first(c, rLabel); ok {
			gen.Name = b.nameOf(l)

			for _, p := range b.kids(c, rPath) {
				gen.Bounds = append(gen.Bounds, b.path(p))
			}
		} else if nm, ok := b.first(c, rName); ok {
			gen.Name = b.text(nm)
		}

		out = append(out, gen)
	}

	return out
}

func (b *builder) item(n int) *Item {
	it := &Item{Span: b.span(n), Pub: b.firstKind(n) == token.Pub}

	for c := range b.t.Children(n) {
		switch b.t.Rule(c) {
		case rFn:
			f := b.fn(c)
			f.Pub = false
			it.Decl = f
		case parser.CovenantRuleStruct:
			it.Decl = b.structDecl(c)
		case parser.CovenantRuleEnum:
			it.Decl = b.enumDecl(c)
		case parser.CovenantRuleTrait:
			it.Decl = b.traitDecl(c)
		case parser.CovenantRuleImpl:
			it.Decl = b.implDecl(c)
		case parser.CovenantRuleConst:
			it.Decl = b.constDecl(c)
		case parser.CovenantRuleAlias:
			it.Decl = b.aliasDecl(c)
		case parser.CovenantRuleEffectGroup:
			it.Decl = b.group(c)
		}
	}

	return it
}

// fn is a function, or a signature (rule fn_sig) which has no body.
func (b *builder) fn(n int) *Fn {
	f := &Fn{Span: b.span(n), Generics: b.generics(n)}

	if nm, ok := b.first(n, rName); ok {
		f.Name = b.text(nm)
	}

	for _, p := range b.kids(n, rParam) {
		par := &Param{Span: b.span(p)}

		if b.firstKind(p) == token.Self {
			par.Name, par.Self = "self", true
		} else {
			if l, ok := b.first(p, rLabel); ok {
				par.Name = b.nameOf(l)
			}

			if t, ok := b.first(p, rType); ok {
				par.Type = b.typ(t)
			}
		}

		f.Params = append(f.Params, par)
	}

	for _, e := range b.kids(n, rEffect) {
		if p, ok := b.first(e, rPath); ok {
			f.Effects = append(f.Effects, b.path(p))
		}
	}

	if t, ok := b.first(n, rType); ok {
		f.Ret = b.typ(t)
	}

	if bl, ok := b.first(n, rBlock); ok {
		f.Body = b.block(bl)
	}

	return f
}

func (b *builder) fields(n int) []*Field {
	var out []*Field

	for _, c := range b.kids(n, rField) {
		f := &Field{Span: b.span(c)}

		if l, ok := b.first(c, rLabel); ok {
			f.Name = b.nameOf(l)
		}

		if t, ok := b.first(c, rType); ok {
			f.Type = b.typ(t)
		}

		out = append(out, f)
	}

	return out
}

func (b *builder) nameChild(n int) string {
	if nm, ok := b.first(n, rName); ok {
		return b.text(nm)
	}

	return ""
}

func (b *builder) structDecl(n int) *Struct {
	return &Struct{Span: b.span(n), Name: b.nameChild(n), Generics: b.generics(n), Fields: b.fields(n)}
}

func (b *builder) enumDecl(n int) *Enum {
	e := &Enum{Span: b.span(n), Name: b.nameChild(n), Generics: b.generics(n)}

	for _, v := range b.kids(n, parser.CovenantRuleVariant) {
		e.Variants = append(e.Variants, &Variant{Span: b.span(v), Name: b.nameChild(v), Fields: b.fields(v)})
	}

	return e
}

func (b *builder) traitDecl(n int) *Trait {
	t := &Trait{Span: b.span(n), Name: b.nameChild(n), Generics: b.generics(n)}

	for _, s := range b.kids(n, rFnSig) {
		t.Methods = append(t.Methods, b.fn(s))
	}

	return t
}

func (b *builder) implDecl(n int) *Impl {
	im := &Impl{Span: b.span(n)}

	types := b.kids(n, rType)

	switch len(types) {
	case 1:
		im.Type = b.typ(types[0])
	case 2:
		im.Trait, im.Type = b.typ(types[0]), b.typ(types[1])
	}

	for _, f := range b.kids(n, rFn) {
		m := b.fn(f)

		// a method that is public has "pub" in front of it
		if begin, _ := b.t.Span(f); begin > 0 && b.toks[begin-1].Kind == token.Pub {
			m.Pub = true
		}

		im.Methods = append(im.Methods, m)
	}

	return im
}

func (b *builder) constDecl(n int) *Const {
	c := &Const{Span: b.span(n)}

	if l, ok := b.first(n, rLabel); ok {
		c.Name = b.nameOf(l)
	}

	if t, ok := b.first(n, rType); ok {
		c.Type = b.typ(t)
	}

	if e, ok := b.first(n, rExpr); ok {
		c.Value = b.expr(e)
	}

	return c
}

func (b *builder) aliasDecl(n int) *Alias {
	a := &Alias{Span: b.span(n), Name: b.nameChild(n), Generics: b.generics(n)}

	if t, ok := b.first(n, rType); ok {
		a.Type = b.typ(t)
	}

	return a
}

func (b *builder) group(n int) *Group {
	g := &Group{Span: b.span(n), Name: b.nameChild(n)}

	for _, e := range b.kids(n, rEffect) {
		if p, ok := b.first(e, rPath); ok {
			g.Effects = append(g.Effects, b.path(p))
		}
	}

	return g
}

func (b *builder) typ(n int) Type {
	if ft, ok := b.first(n, rFnType); ok {
		return b.fnType(ft)
	}

	t := &TypeName{Span: b.span(n)}

	if p, ok := b.first(n, rPath); ok {
		t.Path = b.names(p)
	}

	for _, a := range b.kids(n, rType) {
		t.Args = append(t.Args, b.typ(a))
	}

	return t
}

func (b *builder) fnType(n int) Type {
	t := &TypeFn{Span: b.span(n)}

	// the parameters are inside the parentheses, and the result comes after them
	begin, end := b.t.Span(n)
	rparen, depth := end, 0

	for i := begin + 1; i < end && rparen == end; i++ {
		switch b.toks[i].Kind {
		case token.LParen:
			depth++
		case token.RParen:
			if depth--; depth == 0 {
				rparen = i
			}
		}
	}

	for _, c := range b.kids(n, rType) {
		if cb, _ := b.t.Span(c); cb < rparen {
			t.Params = append(t.Params, b.typ(c))
		} else {
			t.Ret = b.typ(c)
		}
	}

	for _, e := range b.kids(n, rEffect) {
		if p, ok := b.first(e, rPath); ok {
			t.Effects = append(t.Effects, b.path(p))
		}
	}

	return t
}

func (b *builder) block(n int) *Block {
	bl := &Block{Span: b.span(n)}

	for c := range b.t.Children(n) {
		switch b.t.Rule(c) {
		case parser.CovenantRuleRequires:
			if e, ok := b.first(c, rExpr); ok {
				bl.Requires = append(bl.Requires, b.expr(e))
			}
		case parser.CovenantRuleExample:
			if e, ok := b.first(c, rExpr); ok {
				bl.Examples = append(bl.Examples, b.expr(e))
			}
		case parser.CovenantRuleStmt:
			for s := range b.t.Children(c) {
				bl.Stmts = append(bl.Stmts, b.stmt(s))
			}
		}
	}

	return bl
}

func (b *builder) stmt(n int) Stmt {
	sp := b.span(n)

	switch b.t.Rule(n) {
	case parser.CovenantRuleLet:
		l := &Let{Span: sp}

		if lb, ok := b.first(n, rLabel); ok {
			l.Name = b.nameOf(lb)

			if t, ok := b.first(n, rType); ok {
				l.Type = b.typ(t)
			}
		} else if p, ok := b.first(n, rPattern); ok {
			l.Pattern = b.pattern(p)
		}

		if e, ok := b.first(n, rExpr); ok {
			l.Value = b.expr(e)
		}

		return l
	case parser.CovenantRuleVar:
		v := &Var{Span: sp}

		if lb, ok := b.first(n, rLabel); ok {
			v.Name = b.nameOf(lb)

			if t, ok := b.first(n, rType); ok {
				v.Type = b.typ(t)
			}
		} else {
			v.Name = b.nameChild(n)
		}

		if e, ok := b.first(n, rExpr); ok {
			v.Value = b.expr(e)
		}

		return v
	case parser.CovenantRuleReturn:
		r := &Return{Span: sp}

		if e, ok := b.first(n, rExpr); ok {
			r.Value = b.expr(e)
		}

		return r
	case parser.CovenantRuleFor:
		f := &For{Span: sp}

		for _, p := range b.kids(n, rPattern) {
			f.Patterns = append(f.Patterns, b.pattern(p))
		}

		if e, ok := b.first(n, rExpr); ok {
			f.Iter = b.expr(e)
		}

		if bl, ok := b.first(n, rBlock); ok {
			f.Body = b.block(bl)
		}

		return f
	case parser.CovenantRuleWhile:
		w := &While{Span: sp}

		if e, ok := b.first(n, rExpr); ok {
			w.Cond = b.expr(e)
		}

		if bl, ok := b.first(n, rBlock); ok {
			w.Body = b.block(bl)
		}

		return w
	}

	// an expression on a line, and maybe "= value"
	x, _ := b.first(n, rExpr)
	target := b.expr(x)

	if a, ok := b.first(n, parser.CovenantRuleAssign); ok {
		v, _ := b.first(a, rExpr)

		return &Assign{Span: sp, Target: target, Value: b.expr(v)}
	}

	return &ExprStmt{Span: sp, X: target}
}

func (b *builder) pattern(n int) Pattern {
	sp := b.span(n)

	if b.firstKind(n) == token.Underscore {
		return &PatWild{sp}
	}

	for c := range b.t.Children(n) {
		switch b.t.Rule(c) {
		case parser.CovenantRuleNumber, parser.CovenantRuleText, parser.CovenantRuleBool:
			return &PatLit{sp, b.expr(c).(*Lit)}
		case rPath:
			p := &PatName{Span: sp, Path: b.names(c)}

			_, pend := b.t.Span(c)
			_, end := b.t.Span(n)
			p.HasArgs = pend < end

			for _, a := range b.kids(n, rPattern) {
				p.Args = append(p.Args, b.pattern(a))
			}

			return p
		}
	}

	return &PatWild{sp}
}

// binary folds the operands and the operators between them, which are children in order, to the left.
func (b *builder) binary(n int, operand rule, op func(c int) string) Expr {
	var acc Expr
	pending := ""

	for c := range b.t.Children(n) {
		if b.t.Rule(c) == operand {
			e := b.expr(c)
			if acc == nil {
				acc = e
			} else {
				acc = &Binary{Span{acc.At().Begin, e.At().End}, pending, acc, e}
			}

			continue
		}

		pending = op(c)
	}

	return acc
}

func (b *builder) expr(n int) Expr {
	sp := b.span(n)

	switch b.t.Rule(n) {
	case rExpr, parser.CovenantRulePrimary, parser.CovenantRuleParen:
		for c := range b.t.Children(n) {
			return b.expr(c)
		}
	case parser.CovenantRuleClosure:
		return b.closure(n)
	case parser.CovenantRuleLogic:
		// the operator is the word between two operands
		var acc Expr

		prevEnd := 0

		for c := range b.t.Children(n) {
			e := b.expr(c)
			cb, ce := b.t.Span(c)

			if acc == nil {
				acc = e
			} else {
				op := token.And
				for i := prevEnd; i < cb; i++ {
					if b.toks[i].Kind == token.Or {
						op = token.Or
					}
				}

				acc = &Binary{Span{acc.At().Begin, e.At().End}, op.String(), acc, e}
			}

			prevEnd = ce
		}

		return acc
	case parser.CovenantRuleNegation:
		c, _ := b.first(n, parser.CovenantRuleCompare)
		if b.firstKind(n) == token.Not {
			return &Unary{sp, "not", b.expr(c)}
		}

		return b.expr(c)
	case parser.CovenantRuleCompare, parser.CovenantRuleSum, parser.CovenantRuleProduct:
		operand, opRule := parser.CovenantRuleSum, parser.CovenantRuleCmpOp

		switch b.t.Rule(n) {
		case parser.CovenantRuleSum:
			operand, opRule = parser.CovenantRuleProduct, parser.CovenantRuleAddOp
		case parser.CovenantRuleProduct:
			operand, opRule = parser.CovenantRuleUnary, parser.CovenantRuleMulOp
		}

		return b.binary(n, operand, func(c int) string {
			if b.t.Rule(c) == opRule {
				return b.text(c)
			}

			return ""
		})
	case parser.CovenantRuleUnary:
		c, _ := b.first(n, parser.CovenantRulePostfix)
		switch b.firstKind(n) {
		case token.Minus:
			return &Unary{sp, "-", b.expr(c)}
		case token.Try:
			return &Unary{sp, "try", b.expr(c)}
		}

		return b.expr(c)
	case parser.CovenantRulePostfix:
		return b.postfix(n)
	case parser.CovenantRuleNumber:
		kind := LitInt
		if b.firstKind(n) == token.Float {
			kind = LitFloat
		}

		return &Lit{sp, kind, b.text(n)}
	case parser.CovenantRuleText:
		return &Lit{sp, LitText, b.text(n)}
	case parser.CovenantRuleBool:
		return &Lit{sp, LitBool, b.text(n)}
	case rPath:
		return &Name{sp, b.names(n)}
	case parser.CovenantRuleList:
		l := &List{Span: sp}
		for _, e := range b.kids(n, rExpr) {
			l.Elems = append(l.Elems, b.expr(e))
		}

		return l
	case parser.CovenantRuleMap:
		m := &Map{Span: sp}

		for _, e := range b.kids(n, parser.CovenantRuleEntry) {
			en := &Entry{Span: b.span(e)}

			if l, ok := b.first(e, rLabel); ok {
				en.Key = &Name{b.span(l), []string{b.nameOf(l)}}

				v, _ := b.first(e, rExpr)
				en.Value = b.expr(v)
			} else {
				k, _ := b.t.Nth(e, rExpr, 0)
				v, _ := b.t.Nth(e, rExpr, 1)
				en.Key, en.Value = b.expr(k), b.expr(v)
			}

			m.Entries = append(m.Entries, en)
		}

		return m
	case parser.CovenantRuleMatch:
		m := &Match{Span: sp}

		x, _ := b.first(n, rExpr)
		m.X = b.expr(x)

		for _, a := range b.kids(n, parser.CovenantRuleArm) {
			p, _ := b.first(a, rPattern)
			v, _ := b.first(a, rExpr)
			m.Arms = append(m.Arms, &Arm{b.span(a), b.pattern(p), b.expr(v)})
		}

		return m
	case rIf:
		i := &If{Span: sp}

		c, _ := b.first(n, rExpr)
		i.Cond = b.expr(c)

		blocks := b.kids(n, rBlock)
		i.Then = b.block(blocks[0])

		if len(blocks) > 1 {
			i.Else = b.block(blocks[1])
		} else if e, ok := b.first(n, rIf); ok {
			i.Else = b.expr(e)
		}

		return i
	}

	return &Name{sp, nil}
}

func (b *builder) postfix(n int) Expr {
	var acc Expr

	for c := range b.t.Children(n) {
		sp := b.span(c)

		switch b.t.Rule(c) {
		case parser.CovenantRuleCall:
			call := &Call{Span: Span{acc.At().Begin, sp.End}, Fun: acc}

			for _, a := range b.kids(c, parser.CovenantRuleArg) {
				arg := &Arg{Span: b.span(a)}

				if l, ok := b.first(a, rLabel); ok {
					arg.Label = b.nameOf(l)
				}

				v, _ := b.first(a, rExpr)
				arg.Value = b.expr(v)
				call.Args = append(call.Args, arg)
			}

			acc = call
		case parser.CovenantRuleMember:
			acc = &Member{Span{acc.At().Begin, sp.End}, acc, b.nameChild(c)}
		case parser.CovenantRuleUpdate:
			u := &Update{Span: Span{acc.At().Begin, sp.End}, X: acc}

			for _, f := range b.kids(c, parser.CovenantRuleFieldInit) {
				l, _ := b.first(f, rLabel)
				v, _ := b.first(f, rExpr)
				u.Fields = append(u.Fields, &FieldInit{b.span(f), b.nameOf(l), b.expr(v)})
			}

			acc = u
		default:
			acc = b.expr(c)
		}
	}

	return acc
}

func (b *builder) closure(n int) Expr {
	c := &Closure{Span: b.span(n)}

	for _, p := range b.kids(n, rClosureParm) {
		cp := &ClosureParam{Span: b.span(p)}

		if l, ok := b.first(p, rLabel); ok {
			cp.Name = b.nameOf(l)

			if t, ok := b.first(p, rType); ok {
				cp.Type = b.typ(t)
			}
		} else {
			cp.Name = b.nameChild(p)
		}

		c.Params = append(c.Params, cp)
	}

	if t, ok := b.first(n, rType); ok {
		c.Ret = b.typ(t)
	}

	if bl, ok := b.first(n, rBlock); ok {
		c.Body = b.block(bl)
	} else if e, ok := b.first(n, rExpr); ok {
		c.Value = b.expr(e)
	}

	return c
}
