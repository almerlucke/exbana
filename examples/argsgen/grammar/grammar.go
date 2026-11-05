// Package grammar is the command line of a small package manager as a grammar over the arguments, and the generated parser
// for it. The elements are strings, one argument each, so there is no lexer: the leaves are tests on an argument.
//
//	pkg [-v | --verbose | --config file]... install [--save | -D | --version v]... name... | remove name... | list [--json]
//
// Most leaves are declarative (an argument that equals "install"), which the generator and the optimizer can see into. One is not:
// an argument is something that does not start with a dash, which only its function knows, so the generator is told what
// that is as Go code (see Class) and the optimizer cannot prove what it would have to about the choices around it.
package grammar

import (
	"strconv"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/codegen"
	"github.com/almerlucke/exbana/v3/optimize"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	"github.com/almerlucke/exbana/v3/patterns/end"
	"github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/patterns/rule"
)

type pattern = ebnf.Pattern[string, int]

func conc(ps ...pattern) pattern { return concatenation.New[string, int](ps...) }
func alt(ps ...pattern) pattern  { return alternation.New[string, int](ps...) }
func many(p pattern) pattern     { return repetition.Any[string, int](p) }
func some(p pattern) pattern     { return repetition.OneOrMore[string, int](p) }
func maybe(p pattern) pattern    { return repetition.Optional[string, int](p) }

// arg is an argument that is exactly s.
func arg(s string) pattern {
	return entity.Equals[string, int](s).Printed(strconv.Quote(s))
}

// word is an argument that does not start with a dash. The grammar only knows it as a function.
func word() pattern {
	return entity.New[string, int](func(s string) bool { return s != "" && s[0] != '-' }).Named("argument")
}

// CLI returns the grammar. Every call makes a new one, because optimizing changes the patterns.
//
//	cli      = { global } command end
//	global   = "-v" | "--verbose" | "--config" value
//	command  = install | remove | list
//	install  = "install" { instflag } package { package }
//	instflag = "--save" | "-D" | "--version" value
//	remove   = "remove" package { package }
//	list     = "list" [ "--json" ]
//	package  = value = argument
func CLI() pattern {
	value := rule.New[string, int]("value", word())
	pkg := rule.New[string, int]("package", word())

	global := rule.New[string, int]("global", alt(arg("-v"), arg("--verbose"), conc(arg("--config"), value)))
	instflag := rule.New[string, int]("instflag", alt(arg("--save"), arg("-D"), conc(arg("--version"), value)))

	install := rule.New[string, int]("install", conc(arg("install"), many(instflag), pkg, many(pkg)))
	remove := rule.New[string, int]("remove", conc(arg("remove"), pkg, many(pkg)))
	list := rule.New[string, int]("list", conc(arg("list"), maybe(arg("--json"))))

	command := rule.New[string, int]("command", alt(install, remove, list))

	return rule.New[string, int]("cli", conc(many(global), command, end.New[string, int]().Printed("end of the arguments")))
}

// Literals are the arguments the grammar compares with, numbered for the optimizer: it can only reason about integers.
var literals = []string{"-v", "--verbose", "--config", "install", "--save", "-D", "--version", "remove", "list", "--json"}

func number(s string) int64 {
	for i, l := range literals {
		if l == s {
			return int64(i)
		}
	}

	return -1
}

// Class says, as Go code over the argument x, what the leaves that are functions accept.
func Class(n *ebnf.Node[string, int]) (string, bool) {
	if n.ID == "argument" {
		return `x != "" && !strings.HasPrefix(x, "-")`, true
	}

	return "", false
}

// Analysis is what the optimizer finds out about the grammar.
func Analysis() (*optimize.Result[string, int], error) {
	return optimize.AnalyzeFunc[string, int](CLI(), optimize.Options{Closed: true}, number)
}

// Source returns the source of the parser for the grammar, in package parser.
//
// The optimizer proves six of the eight places that could backtrack. The other two are the ones where an argument could
// start with the same thing as a flag, which it cannot rule out because it cannot look into the function that says what an
// argument is: we know an argument never starts with a dash, so the flags after "install" and the branches of command
// never compete with it. Force takes those on our word, and the tests compare the parser with the backtracking grammar
// to check that word.
func Source() ([]byte, error) {
	g := CLI()

	res, err := optimize.AnalyzeFunc[string, int](g, optimize.Options{Closed: true}, number)
	if err != nil {
		return nil, err
	}

	res.Apply()
	res.Force()

	return codegen.Generate[string, int](g, codegen.Config[string, int]{
		Package: "parser", Name: "Pkg", AST: true, Class: Class, Imports: []string{"strings"},
	})
}
