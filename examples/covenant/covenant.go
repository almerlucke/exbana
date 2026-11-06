// Package covenant reads Covenant source: the lexer (see the token package) makes tokens, and the parser that was generated from
// the grammar (see the grammar and parser packages) makes a tree of them.
package covenant

//go:generate go run ./gen

import (
	"fmt"
	"strings"

	"github.com/almerlucke/exbana/v3/examples/covenant/ast"
	"github.com/almerlucke/exbana/v3/examples/covenant/parser"
	"github.com/almerlucke/exbana/v3/examples/covenant/token"
)

// File is a parsed source file: the tokens and the tree of them. The positions in the tree are the numbers of tokens, and
// Tokens says where in the source each of them is.
type File struct {
	Source   string
	Tokens   []token.Token
	Comments []token.Comment // the comments are not tokens: this is where they are, in order
	Tree     *parser.CovenantTree
}

// AST is the file as plain values: see the ast package.
func (f *File) AST() *ast.File { return ast.Build(f.Source, f.Tokens, f.Tree) }

// ASTAt is AST for a file that is one of the files of a module: the positions in it are as if it started at base in the text of
// the module.
func (f *File) ASTAt(base int) *ast.File { return ast.BuildAt(base, f.Source, f.Tokens, f.Tree) }

// Error is a place where the source is not Covenant, with where it is and what would have been right there.
type Error struct {
	Line, Col int
	Msg       string
}

func (e *Error) Error() string { return fmt.Sprintf("%d:%d: %s", e.Line, e.Col, e.Msg) }

// Parse reads the source.
func Parse(src string) (*File, error) {
	tokens, comments, err := token.LexComments(src)
	if err != nil {
		if lex, ok := err.(*token.Error); ok {
			line, col := token.Position(src, lex.Pos)

			return nil, &Error{line, col, lex.Msg}
		}

		return nil, err
	}

	tree, err := parser.NewCovenantParser().Parse(token.Kinds(tokens))
	if err != nil {
		syn, ok := err.(*parser.CovenantSyntaxError)
		if !ok {
			return nil, err
		}

		// the error is at a token: its place in the source is where the token starts, or the end of the source
		at := len(src)
		if syn.Pos < len(tokens) {
			at = tokens[syn.Pos].Begin
		}

		line, col := token.Position(src, at)
		found := "end of input"

		if syn.Pos < len(tokens) {
			t := tokens[syn.Pos]
			found = fmt.Sprintf("%q", src[t.Begin:t.End])

			if t.Kind == token.NL {
				found = "end of line"
			}
		}

		return nil, &Error{line, col, fmt.Sprintf("unexpected %s, expected %s", found, strings.Join(syn.Expected, ", "))}
	}

	return &File{src, tokens, comments, tree}, nil
}
