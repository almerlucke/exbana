// Package ast is a parsed Covenant file as plain Go values, for what reads the tree more than once: the checker, and what will run
// the program. Build makes it from the tree the generated parser returns, which is flat and says everything in terms of tokens.
package ast

// Span is a piece of the source, in bytes: from Begin up to End.
type Span struct{ Begin, End int }

// Node is anything in the tree.
type Node interface{ At() Span }

// At makes every node that embeds a Span a Node.
func (s Span) At() Span { return s }

// File is a source file: what it uses, and what it declares.
type File struct {
	Span
	Uses  []*Use
	Items []*Item
}

// Use is "use geometry::shapes::Shape as S": Path are the names, and Alias is "" if there is no "as".
type Use struct {
	Span
	Path  []string
	Alias string
}

// Item is a declaration, public or not. Decl is one of *Fn, *Struct, *Enum, *Trait, *Impl, *Const, *Alias, *Group.
type Item struct {
	Span
	Pub  bool
	Decl Node
}

// Generic is a parameter of a function or a type: T, or U: Area + Show. A name that is used in a "with" clause stands for a set
// of effects and not for a type; that is for the checker to see.
type Generic struct {
	Span
	Name   string
	Bounds []*Path
}

// Fn is a function or a method. Body is nil for the signature of a method in a trait.
type Fn struct {
	Span
	Name     string
	Pub      bool // for a method in an impl
	Generics []*Generic
	Params   []*Param
	Effects  []*Path
	Ret      Type // nil if the function does not say, which is Unit
	Body     *Block
}

// Param is a parameter. Self is the parameter "self", which has no type of its own.
type Param struct {
	Span
	Name string
	Self bool
	Type Type
}

// Field is a field of a struct or of a variant.
type Field struct {
	Span
	Name string
	Type Type
}

// Struct is "struct Point { x: Float, y: Float }".
type Struct struct {
	Span
	Name     string
	Generics []*Generic
	Fields   []*Field
}

// Variant is a case of an enum, with the fields it carries, if any.
type Variant struct {
	Span
	Name   string
	Fields []*Field
}

// Enum is "enum Shape { Circle(radius: Float) Empty }".
type Enum struct {
	Span
	Name     string
	Generics []*Generic
	Variants []*Variant
}

// Trait is a set of signatures.
type Trait struct {
	Span
	Name     string
	Generics []*Generic
	Methods  []*Fn
}

// Impl is "impl Trait for Type { ... }", or "impl Type { ... }" and then Trait is nil.
type Impl struct {
	Span
	Trait   Type
	Type    Type
	Methods []*Fn
}

// Const is "const limit: Int = 100".
type Const struct {
	Span
	Name  string
	Type  Type
	Value Expr
}

// Alias is "type Ports = List<Port>".
type Alias struct {
	Span
	Name     string
	Generics []*Generic
	Type     Type
}

// Group is "effects Storage = fs::read + fs::write".
type Group struct {
	Span
	Name    string
	Effects []*Path
}

// Type is a type as it is written: *TypeName or *TypeFn.
type Type interface{ Node }

// TypeName is a name with the types it is given: Int, List<Int>, geometry::Shape.
type TypeName struct {
	Span
	Path []string
	Args []Type
}

// TypeFn is Fn(Int, Int) with fs::read -> Int.
type TypeFn struct {
	Span
	Params  []Type
	Effects []*Path
	Ret     Type // nil if there is none, which is Unit
}

// TypeDyn is dyn Area: any value of a type that has the trait.
type TypeDyn struct {
	Span
	Path []string
}

// Path is a name or names with :: between them, for a value, a type, an effect or a module.
type Path struct {
	Span
	Names []string
}

// Block is the body of a function and what is in braces: the promises, and then the statements.
type Block struct {
	Span
	Requires []Expr
	Examples []Expr
	Stmts    []Stmt
}

// Stmt is *Let, *Var, *Return, *For, *While, *ExprStmt or *Assign.
type Stmt interface{ Node }

// Let is "let name: Type = value" (Name is set), or "let pattern = value".
type Let struct {
	Span
	Name    string
	Type    Type
	Pattern Pattern
	Value   Expr
}

// Var is "var name: Type = value".
type Var struct {
	Span
	Name  string
	Type  Type
	Value Expr
}

// Return is "return value", or "return".
type Return struct {
	Span
	Value Expr
}

// For is "for a, b in iter { ... }".
type For struct {
	Span
	Patterns []Pattern
	Iter     Expr
	Body     *Block
}

// While is "while cond { ... }".
type While struct {
	Span
	Cond Expr
	Body *Block
}

// ExprStmt is an expression on a line of its own.
type ExprStmt struct {
	Span
	X Expr
}

// Assign is "target = value". The grammar takes any expression for the target; the checker says if it can be assigned to.
type Assign struct {
	Span
	Target Expr
	Value  Expr
}

// Expr is an expression.
type Expr interface{ Node }

// LitKind says what a literal is.
type LitKind int

// The kinds of literals.
const (
	LitInt LitKind = iota
	LitFloat
	LitText
	LitBool
)

// Lit is a literal. Text is as it is written, with the quotes for a text.
type Lit struct {
	Span
	Kind LitKind
	Text string
}

// Name is a name or names with :: between them in an expression: a variable, a function, Shape::Circle, self.
type Name struct {
	Span
	Path []string
}

// List is "[a, b]".
type List struct {
	Span
	Elems []Expr
}

// Entry is a key and a value in a map.
type Entry struct {
	Span
	Key, Value Expr
}

// Map is "{k: v}", and "{:}" with no entries.
type Map struct {
	Span
	Entries []*Entry
}

// Arg is an argument of a call, with the label it was given, or "".
type Arg struct {
	Span
	Label string
	Value Expr
}

// Call is a call of a function, a method, or a constructor of a struct or a variant.
type Call struct {
	Span
	Fun  Expr
	Args []*Arg
}

// Member is "x.name", a field or the start of a method call.
type Member struct {
	Span
	X    Expr
	Name string
}

// Cast is "x as dyn Area": the value, made into one that is known by a trait.
type Cast struct {
	Span
	X    Expr
	Type *TypeDyn
}

// FieldInit is a field in "x with { field: value }".
type FieldInit struct {
	Span
	Name  string
	Value Expr
}

// Update is "x with { field: value }": a copy of x with other fields.
type Update struct {
	Span
	X      Expr
	Fields []*FieldInit
}

// Unary is "-x", "not x" or "try x".
type Unary struct {
	Span
	Op string
	X  Expr
}

// Binary is "a + b". Op is the sign or the word.
type Binary struct {
	Span
	Op   string
	L, R Expr
}

// ClosureParam is a parameter of a closure, with a type if it was written.
type ClosureParam struct {
	Span
	Name string
	Type Type
}

// Closure is "fn(x) => x + 1", with Value set, or "fn(x) { ... }", with Body.
type Closure struct {
	Span
	Params []*ClosureParam
	Ret    Type
	Value  Expr
	Body   *Block
}

// Arm is a case of a match.
type Arm struct {
	Span
	Pattern Pattern
	Value   Expr
}

// Match is "match x { pattern => value }".
type Match struct {
	Span
	X    Expr
	Arms []*Arm
}

// If is "if cond { ... } else ...". Else is nil, an *If, or a *Block.
type If struct {
	Span
	Cond Expr
	Then *Block
	Else Node
}

// Pattern is *PatWild, *PatLit or *PatName.
type Pattern interface{ Node }

// PatWild is "_".
type PatWild struct{ Span }

// PatLit is a number, a text or a bool.
type PatLit struct {
	Span
	Lit *Lit
}

// PatName is a name that binds a variable, or names a variant when it has Args or is a name of one: that is for the checker
// to see. HasArgs is true if there are parentheses, even with nothing inside.
type PatName struct {
	Span
	Path    []string
	HasArgs bool
	Args    []Pattern
}
