// Package grammar is the syntax of Covenant, a small language meant to be read by people and by agents alike, as a grammar over
// the tokens the token package makes, and the generated parser for it (see the parser package).
//
// The syntax is built on a few rules, and the grammar shows each of them:
//
//   - There is one way to write things down. The grammar has no optional spellings, no automatic semicolons and no trailing
//     commas.
//   - A line ends a statement, and nothing else does. A line can be broken only where the grammar says so: after an opening
//     bracket, after a comma or a binary operator, and before a closing bracket.
//   - Mixing and with or needs parentheses, a comparison does not chain, and there are no operator overloads.
//   - Every place a function can leave early starts with the word try or return.
//   - What a function can do (its effects) is in its signature, and so are its errors, in its result.
//
// The grammar needs no backtracking: every choice is made by looking at the next token (the tests prove it with the optimizer),
// which is why a parser can be generated for it. Two things are in the way of that, and the syntax is the way it is because of
// them:
//
//   - A value of a struct is made like a call with named arguments, Point(x: 1.0, y: 2.0), and not with braces behind the name,
//     because in "if ready {" the brace starts the block.
//   - A name followed by a colon is one token (a label), so that "x: 1" is told from "x" without looking two tokens ahead.
//   - A value is made into a "dyn Trait" with "as", and a type does not have to be named for that to be wanted: there is no
//     conversion that is not written.
//
// What the grammar leaves to the checker is what it can not say without looking further than a token: that the left side of an
// assignment is something that can be assigned to, that the labels of a call are the fields of what it makes, and which of the
// two types after "impl" is the trait.
package grammar

import (
	"fmt"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/codegen"
	"github.com/almerlucke/exbana/v3/examples/covenant/token"
	"github.com/almerlucke/exbana/v3/optimize"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	"github.com/almerlucke/exbana/v3/patterns/end"
	"github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/readers/slice"
)

// The parser reads the kinds of the tokens as ints, and its positions are the numbers of the tokens.
type pattern = ebnf.Pattern[int, int]

func conc(ps ...pattern) pattern { return concatenation.New[int, int](ps...) }
func alt(ps ...pattern) pattern  { return alternation.New[int, int](ps...) }
func many(p pattern) pattern     { return repetition.Any[int, int](p) }
func some(p pattern) pattern     { return repetition.OneOrMore[int, int](p) }
func maybe(p pattern) pattern    { return repetition.Optional[int, int](p) }

// tok is a token of the kind, and prints as what the kind is called.
func tok(k token.Kind) pattern {
	return entity.Equals[int, int](int(k)).Printed("`" + k.String() + "`")
}

func decl(name string) *rule.Rule[int, int] { return rule.Declare[int, int](name) }

func def(name string, p pattern) *rule.Rule[int, int] { return rule.New[int, int](name, p) }

// Covenant returns the grammar. Every call makes a new one, because optimizing changes the patterns.
//
//	file    = { use } { item } end
//	use     = "use" path [ "as" name ] nl
//	item    = [ "pub" ] ( fn | struct | enum | trait | impl | const | type | effects )
//	fn      = "fn" name [ generics ] "(" [ params ] ")" [ "with" effect { "+" effect } ] [ "->" type ] block nl
//	block   = "{" [ nl ] { clause } { stmt } "}"
//	stmt    = let | var | "return" [ expr ] nl | for | while | expr [ "=" expr ] nl
//
// A function body begins with its promises, the requires and example clauses, and goes on with its statements. nl is a line
// end; where the grammar says [ nl ] a line may end, and a list goes on after it.
func Covenant() pattern {
	nl := tok(token.NL)
	nls := maybe(nl) // a line may end here

	// a list of none or more, separated by commas, and lines may end after the commas and before the closer: this is the only
	// shape lists have, and there is no comma behind the last one
	list := func(open token.Kind, item pattern, closer token.Kind) pattern {
		return conc(tok(open), nls, alt(tok(closer), conc(item, many(conc(tok(token.Comma), nls, item)), nls, tok(closer))))
	}

	name := def("name", tok(token.Ident))
	label := def("label", tok(token.Label))

	// a name, a path of names, or a path that starts with self
	path := def("path", conc(alt(name, tok(token.Self)), many(conc(tok(token.ColonColon), name))))

	number := def("number", alt(tok(token.Int), tok(token.Float)))
	text := def("text", tok(token.Text))
	boolean := def("bool", alt(tok(token.True), tok(token.False)))

	// what a function may do, between its parameters and its result: names of effects, and of groups of them
	effect := def("effect", path)
	effects := conc(tok(token.With), effect, many(conc(tok(token.Plus), nls, effect)))

	// types:  Name  geometry::Shape  List<Int>  Result<Port, ParseError>  Fn(Path) with fs::read -> Text
	typ := decl("type")
	typeArgs := conc(tok(token.Lt), nls, typ, many(conc(tok(token.Comma), nls, typ)), nls, tok(token.Gt))
	fnType := def("fn_type", conc(tok(token.FnType), list(token.LParen, typ, token.RParen), maybe(effects), maybe(conc(tok(token.Arrow), typ))))
	// dyn Area: any value of a type that has the trait Area
	dynType := def("dyn_type", conc(tok(token.Dyn), path))
	typ.Define(alt(fnType, dynType, conc(path, maybe(typeArgs))))

	// generics:  <T, U: Area + Show>
	generic := def("generic", alt(conc(label, path, many(conc(tok(token.Plus), path))), name))
	generics := def("generics", conc(tok(token.Lt), nls, generic, many(conc(tok(token.Comma), nls, generic)), nls, tok(token.Gt)))

	// patterns
	pat := decl("pattern")
	pat.Define(alt(
		tok(token.Underscore), number, text, boolean,
		conc(path, maybe(list(token.LParen, pat, token.RParen))),
	))

	// expressions
	expr := decl("expr")
	block := decl("block")

	// an argument is an expression, or a label and one: the label names the field or the parameter it is for
	arg := def("arg", alt(conc(label, expr), expr))
	call := def("call", list(token.LParen, arg, token.RParen))
	member := def("member", conc(tok(token.Dot), name))

	fieldInit := def("field_init", conc(label, expr))
	update := def("update", conc(tok(token.With), list(token.LBrace, fieldInit, token.RBrace)))

	listValue := def("list", list(token.LBracket, expr, token.RBracket))

	// a map:  {"a": 1, "b": 2}  and the empty map  {:}
	entry := def("entry", alt(conc(label, expr), conc(expr, tok(token.Colon), expr)))
	mapValue := def("map", conc(tok(token.LBrace), nls,
		alt(tok(token.Colon), conc(entry, many(conc(tok(token.Comma), nls, entry)))),
		nls, tok(token.RBrace)))

	paren := def("paren", conc(tok(token.LParen), nls, expr, nls, tok(token.RParen)))

	arm := def("arm", conc(pat, tok(token.FatArrow), expr, nl))
	matchExpr := def("match", conc(tok(token.Match), expr, tok(token.LBrace), nls, some(arm), tok(token.RBrace)))

	ifExpr := decl("if")
	ifExpr.Define(conc(tok(token.If), expr, block, maybe(conc(tok(token.Else), alt(ifExpr, block)))))

	// a closure:  fn(x) => x * 2   fn(a: Int, b: Int) -> Int { ... }
	// It is a whole expression and not a part of one, as its body goes on as far as it can: "fn(x) => x + 1" is a closure of
	// x + 1. To call one, or to add to it, it goes between parentheses.
	closureParam := def("closure_param", alt(conc(label, typ), name))
	closure := def("closure", conc(tok(token.Fn), list(token.LParen, closureParam, token.RParen), maybe(conc(tok(token.Arrow), typ)),
		alt(conc(tok(token.FatArrow), expr), block)))

	primary := def("primary", alt(number, text, boolean, listValue, mapValue, paren, matchExpr, ifExpr, path))
	// a value made into one of any type that has a trait, which is said where it is done:  Circle(radius: 1.0) as dyn Area
	cast := def("cast", conc(tok(token.As), dynType))
	postfix := def("postfix", conc(primary, many(alt(call, member, update, cast))))
	unary := def("unary", conc(maybe(alt(tok(token.Minus), tok(token.Try))), postfix))

	mulOp := def("mul_op", alt(tok(token.Star), tok(token.Slash), tok(token.Percent)))
	addOp := def("add_op", alt(tok(token.Plus), tok(token.Minus)))
	cmpOp := def("cmp_op", alt(tok(token.Eq), tok(token.Ne), tok(token.Le), tok(token.Ge), tok(token.Lt), tok(token.Gt)))

	// a line may break after a binary operator
	product := def("product", conc(unary, many(conc(mulOp, nls, unary))))
	sum := def("sum", conc(product, many(conc(addOp, nls, product))))
	compare := def("compare", conc(sum, maybe(conc(cmpOp, nls, sum))))
	negation := def("negation", conc(maybe(tok(token.Not)), compare))
	// and or or, never both in a row: the one who mixes them says what is meant with parentheses
	logic := def("logic", conc(negation, maybe(alt(
		some(conc(tok(token.And), nls, negation)),
		some(conc(tok(token.Or), nls, negation)),
	))))
	expr.Define(alt(closure, logic))

	// statements
	letStmt := def("let", conc(tok(token.Let), alt(conc(label, typ), pat), tok(token.Assign), expr, nl))
	varStmt := def("var", conc(tok(token.Var), alt(conc(label, typ), name), tok(token.Assign), expr, nl))
	returnStmt := def("return", conc(tok(token.Return), maybe(expr), nl))
	forStmt := def("for", conc(tok(token.For), pat, many(conc(tok(token.Comma), pat)), tok(token.In), expr, block, nl))
	whileStmt := def("while", conc(tok(token.While), expr, block, nl))
	// an assignment starts like an expression: the grammar can not tell, and the checker sees if the left side can be assigned to
	assign := def("assign", conc(tok(token.Assign), expr))
	exprStmt := def("expr_stmt", conc(expr, maybe(assign), nl))
	stmt := def("stmt", alt(letStmt, varStmt, returnStmt, forStmt, whileStmt, exprStmt))

	// promises of a function
	requires := def("requires", conc(tok(token.Requires), expr, nl))
	example := def("example", conc(tok(token.Example), expr, nl))

	block.Define(conc(tok(token.LBrace), nls, many(alt(requires, example)), many(stmt), tok(token.RBrace)))

	// declarations
	param := def("param", alt(tok(token.Self), conc(label, typ)))
	fnSig := conc(tok(token.Fn), name, maybe(generics), list(token.LParen, param, token.RParen), maybe(effects), maybe(conc(tok(token.Arrow), typ)))
	fnDecl := def("fn", conc(fnSig, block, nl))
	fnSigDecl := def("fn_sig", conc(fnSig, nl))

	field := def("field", conc(label, typ))
	structDecl := def("struct", conc(tok(token.Struct), name, maybe(generics), list(token.LBrace, field, token.RBrace), nl))

	variant := def("variant", conc(name, maybe(list(token.LParen, field, token.RParen)), nl))
	enumDecl := def("enum", conc(tok(token.Enum), name, maybe(generics), tok(token.LBrace), nls, some(variant), tok(token.RBrace), nl))

	traitDecl := def("trait", conc(tok(token.Trait), name, maybe(generics), tok(token.LBrace), nls, many(fnSigDecl), tok(token.RBrace), nl))
	// with "for" the first type is the trait, and without it the type has these methods of its own
	implDecl := def("impl", conc(tok(token.Impl), typ, maybe(conc(tok(token.For), typ)), tok(token.LBrace), nls,
		many(conc(maybe(tok(token.Pub)), fnDecl)), tok(token.RBrace), nl))

	constDecl := def("const", conc(tok(token.Const), label, typ, tok(token.Assign), expr, nl))
	aliasDecl := def("alias", conc(tok(token.Type), name, maybe(generics), tok(token.Assign), typ, nl))

	// a group of effects: it can name effects and other groups, and is used after "with" like an effect
	groupDecl := def("effect_group", conc(tok(token.Effects), name, tok(token.Assign), effect, many(conc(tok(token.Plus), nls, effect)), nl))

	item := def("item", conc(maybe(tok(token.Pub)), alt(fnDecl, structDecl, enumDecl, traitDecl, implDecl, constDecl, aliasDecl, groupDecl)))

	use := def("use", conc(tok(token.Use), path, maybe(conc(tok(token.As), name)), nl))

	return def("file", conc(many(use), many(item), end.New[int, int]().Printed("end of input")))
}

// Accepts says whether the grammar, as it is written (it tries every way there is, and is slow), matches all of the tokens. The
// generated parser has to say the same; the tests compare them.
func Accepts(kinds []int) bool {
	ok, _, err := ebnf.Parse(Covenant(), slice.New(kinds))
	if err != nil {
		panic(err)
	}

	return ok
}

// Analysis is what the optimizer finds out about the grammar.
func Analysis() (*optimize.Result[int, int], error) {
	return optimize.Optimize[int, int](Covenant(), optimize.Options{Closed: true})
}

// Positions names the children that a rule has more than once, by where they are.
var Positions = map[string]map[string][]string{
	"compare": {"sum": {"Left", "Right"}},
	"entry":   {"expr": {"Key", "Value"}},
	"impl":    {"type": {"First", "Second"}},
}

// Source returns the source of the parser for the grammar, in package parser. The generator (see gen) writes it to
// parser/parser.go, and the tests check that file is what this returns.
func Source() ([]byte, error) {
	g := Covenant()

	res, err := optimize.Optimize[int, int](g, optimize.Options{Closed: true})
	if err != nil {
		return nil, err
	}

	// the parser can only be generated for a grammar that does not backtrack: say what is in the way if it ever does
	if blocked := res.Blocked(); len(blocked) > 0 {
		return nil, fmt.Errorf("the grammar backtracks in %d places:\n%v", len(blocked), res)
	}

	return codegen.Generate[int, int](g, codegen.Config[int, int]{Package: "parser", Name: "Covenant", AST: true, Positions: Positions})
}
