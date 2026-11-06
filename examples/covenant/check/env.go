package check

import (
	"github.com/almerlucke/exbana/v3/examples/covenant/ast"
	"github.com/almerlucke/exbana/v3/examples/covenant/token"
)

// Module is a part of the program: what it declares, and what it brought in with use. A module is the files of a directory, or a
// file on its own; they share one set of names, so what is private to a module is private to all its files.
type Module struct {
	Name    string
	Native  bool // the host's: its signatures have no bodies
	File    *ast.File
	Source  string       // the text of the files, one after the other: the positions in the tree are in it
	Files   []SourceFile // where each of the files is in Source
	Decls   map[string]*Decl
	Imports map[string]any
}

// SourceFile is a file of a module, and where it starts in the text of the module.
type SourceFile struct {
	Name string
	Base int
	Text string
}

// Locate is the file, and the line and column in it (both counted from 1), of a position in the text of the module.
func (m *Module) Locate(offset int) (file string, line, col int) {
	for i := len(m.Files) - 1; i >= 0; i-- {
		if f := m.Files[i]; offset >= f.Base {
			line, col = token.Position(f.Text, offset-f.Base)

			return f.Name, line, col
		}
	}

	line, col = token.Position(m.Source, offset)

	return "", line, col
}

// Decl is a name a module declares.
type Decl struct {
	Name   string
	Pub    bool
	Entity any // *Def, *FnDef, *ConstDef, *GroupDef, *TraitDef or *AliasDef
	Span   ast.Span
}

// FnDef is a function or a method.
type FnDef struct {
	Name    string
	Module  *Module
	Decl    *ast.Fn
	Pub     bool
	TParams []*Param // all the type parameters of the scheme: those of the impl or trait, and the function's own
	EParams []string // the effect parameters, as 'E
	Type    *Fn      // with the type of self first, if there is one
	HasSelf bool
	Impl    *Impl
	Trait   *TraitDef
	Owner   *Def // the type a method belongs to, or nil for a function of a module

	ts *tscope // the type parameters in scope in the signature and the body
}

// ConstDef is a constant.
type ConstDef struct {
	Name   string
	Module *Module
	Decl   *ast.Const
	Type   Type
	pub    bool
}

// GroupDef is a name for a set of effects.
type GroupDef struct {
	Name   string
	Module *Module
	Decl   *ast.Group
	names  []string
	state  int // 0 not yet, 1 busy, 2 done
}

// TraitDef is a set of method signatures, with the type they are for as the parameter Self.
type TraitDef struct {
	Name    string
	Module  *Module
	Decl    *ast.Trait
	Self    *Param
	Methods map[string]*FnDef
	Order   []string
}

// AliasDef is a name for a type.
type AliasDef struct {
	Name   string
	Module *Module
	Decl   *ast.Alias
	Params []*Param
	Type   Type
	state  int
}

// Impl says that a type has the methods of a trait, or has methods of its own (Trait is nil).
type Impl struct {
	Trait   *TraitDef
	Self    Type
	Params  []*Param
	Methods map[string]*FnDef
	List    []*FnDef // in the order they are written
	Module  *Module
	Decl    *ast.Impl
}

// VariantRef is a variant that can be written without its enum: Some, None, Ok, Err.
type VariantRef struct{ Variant *Variant }

// local is a name in a function: a parameter, a let, a var, a name a pattern binds.
type local struct {
	name    string
	ty      Type
	mutable bool
	depth   int // how many closures deep it was declared
}

type scope struct {
	parent *scope
	vars   map[string]*local
}

func (s *scope) find(name string) *local {
	for ; s != nil; s = s.parent {
		if l, ok := s.vars[name]; ok {
			return l
		}
	}

	return nil
}

// fnCtx is the function, method or closure whose body is being checked.
type fnCtx struct {
	ret     Type
	allowed *Eff            // what the function may do; nil in a closure, which does what it calls
	collect map[string]bool // the effects a closure has so far
	depth   int             // 0 for a function, 1 for a closure in it
	parent  *fnCtx
	pure    bool // in a promise
	decls   []declared
}

// declared is a name a let or a var made, to see at the end of the function whether its type was worked out.
type declared struct {
	at    ast.Node
	local *local
}
