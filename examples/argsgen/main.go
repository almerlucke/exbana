// Command argsgen parses its own command line with a parser that was generated from a grammar over the arguments. The elements
// the parser reads are strings, one per argument, so there is no lexer and no text: the tree has argument numbers where a
// parser of text has offsets, and the typed views give the arguments by what they mean.
//
//	go run ./examples/argsgen install --save --version 1.2 left-pad lodash
//	go run ./examples/argsgen -v --config pkg.toml list --json
//
// The parser is in the parser package; regenerate it with go generate after changing the grammar.
package main

//go:generate go run ./gen

import (
	"fmt"
	"os"
	"strings"

	"github.com/almerlucke/exbana/v3/examples/argsgen/parser"
)

// Invocation is what a command line asks for.
type Invocation struct {
	Verbose  bool
	Config   string
	Command  string // install, remove or list
	Packages []string
	Save     bool
	Dev      bool
	Version  string
	JSON     bool
}

// Parse reads the arguments (without the name of the program).
func Parse(args []string) (*Invocation, error) {
	tree, err := parser.NewPkgParser().Parse(args)
	if err != nil {
		return nil, explain(args, err)
	}

	cli := tree.Top().Cli()
	inv := &Invocation{}

	for g := range cli.Globals() {
		// the arguments the node matched: the flag, and for --config the file after it
		a := g.Slice()

		switch a[0] {
		case "-v", "--verbose":
			inv.Verbose = true
		case "--config":
			inv.Config = a[1]
		}
	}

	command := cli.Command()

	switch kind, _ := command.Kind(); kind {
	case parser.PkgRuleInstall:
		install, _ := command.Install()
		inv.Command = "install"

		for f := range install.Instflags() {
			switch a := f.Slice(); a[0] {
			case "--save":
				inv.Save = true
			case "-D":
				inv.Dev = true
			case "--version":
				inv.Version = a[1]
			}
		}

		inv.Packages = packages(install.Packages())
	case parser.PkgRuleRemove:
		remove, _ := command.Remove()
		inv.Command = "remove"
		inv.Packages = packages(remove.Packages())
	case parser.PkgRuleList:
		list, _ := command.List()
		inv.Command = "list"
		inv.JSON = len(list.Slice()) == 2
	}

	return inv, nil
}

func packages(seq func(func(parser.PkgPackage) bool)) []string {
	var out []string

	seq(func(p parser.PkgPackage) bool {
		out = append(out, p.Slice()[0])
		return true
	})

	return out
}

// explain says where in the command line the parser got stuck, and what it wanted there.
func explain(args []string, err error) error {
	e, ok := err.(*parser.PkgSyntaxError)
	if !ok {
		return err
	}

	var sb strings.Builder

	if e.AtEnd {
		sb.WriteString("the command line ends too soon")
	} else {
		fmt.Fprintf(&sb, "argument %d, %q, was not expected", e.Pos+1, args[e.Pos])
	}

	if len(e.Expected) > 0 {
		sb.WriteString(": expected " + strings.Join(e.Expected, ", "))
	}

	return fmt.Errorf("%s", sb.String())
}

func main() {
	inv, err := Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	fmt.Printf("%+v\n", *inv)
}
