// Package token is the first stage of Covenant: it turns the source text into tokens, and the grammar (see the grammar package)
// is over the kinds of those tokens.
//
// Doing it in two stages is what lets the grammar be proven to need no backtracking. Keywords that start with the same
// letters (trait, type) are different tokens by the time the grammar sees them, and so are the places where one character
// decides what follows. Three things the lexer decides for the grammar:
//
//   - A line end is a token (NL), and the lexer drops the blank and comment lines: there is never more than one NL in a row,
//     none at the start, and always one at the end of the last line, even if the source has none.
//   - A name that is directly followed by a colon is one token, a Label: "x: Int", "radius: 2.0". That is how the grammar tells
//     a field, a parameter or a named argument from an expression with a name in it. A space in front of the colon makes it
//     two tokens, and the grammar has no place for that, so there is one way to write it.
//   - Numbers are checked here: see Lex.
package token

import (
	"fmt"
	"strings"
)

// Kind is the kind of a token. The grammar matches on kinds only; what a token says is in the source, from Begin to End.
type Kind int

// The kinds. The first is not a kind of token: 0 is never made, so that a zero value is easy to tell.
const (
	_ Kind = iota

	NL    // the end of a line
	Ident // a name
	Label // a name directly followed by a colon: "x:"
	Int
	Float
	Text // "a string"
	Underscore

	// punctuation
	LParen
	RParen
	LBrace
	RBrace
	LBracket
	RBracket
	Comma
	Colon
	ColonColon
	Dot
	Arrow    // ->
	FatArrow // =>
	Assign   // =
	Eq       // ==
	Ne       // !=
	Lt
	Le
	Gt
	Ge
	Plus
	Minus
	Star
	Slash
	Percent

	// keywords
	And
	As
	Const
	Effects
	Else
	Enum
	Example
	Fn
	For
	If
	Impl
	In
	Let
	Match
	Not
	Or
	Pub
	Requires
	Return
	Self
	Struct
	Trait
	True
	False
	Try
	Type
	Use
	Var
	While
	With
	FnType // Fn, the type of functions

	numKinds
)

var names = [numKinds]string{
	NL: "end of line", Ident: "name", Label: "name:", Int: "integer", Float: "float", Text: "text", Underscore: "_",
	LParen: "(", RParen: ")", LBrace: "{", RBrace: "}", LBracket: "[", RBracket: "]", Comma: ",", Colon: ":", ColonColon: "::",
	Dot: ".", Arrow: "->", FatArrow: "=>", Assign: "=", Eq: "==", Ne: "!=", Lt: "<", Le: "<=", Gt: ">", Ge: ">=",
	Plus: "+", Minus: "-", Star: "*", Slash: "/", Percent: "%",
}

// keywords are the words that are not names.
var keywords = map[string]Kind{
	"and": And, "as": As, "const": Const, "effects": Effects, "else": Else, "enum": Enum, "example": Example, "fn": Fn,
	"for": For, "if": If, "impl": Impl, "in": In, "let": Let, "match": Match, "not": Not, "or": Or, "pub": Pub,
	"requires": Requires, "return": Return, "self": Self, "struct": Struct, "trait": Trait, "true": True, "false": False,
	"try": Try, "type": Type, "use": Use, "var": Var, "while": While, "with": With, "Fn": FnType,
}

func init() {
	for word, kind := range keywords {
		names[kind] = word
	}
}

// Keywords returns the words that are not names.
func Keywords() []string {
	out := make([]string, 0, len(keywords))
	for word := range keywords {
		out = append(out, word)
	}

	return out
}

// String is what the kind is called in a message: the word or the sign for the ones that have one.
func (k Kind) String() string {
	if k > 0 && k < numKinds && names[k] != "" {
		return names[k]
	}

	return fmt.Sprintf("token(%d)", int(k))
}

// Token is a piece of the source: the bytes from Begin up to End.
type Token struct {
	Kind       Kind
	Begin, End int
}

// Error is a place in the source the lexer can not make a token of.
type Error struct {
	Pos int // the byte offset
	Msg string
}

func (e *Error) Error() string { return fmt.Sprintf("offset %d: %s", e.Pos, e.Msg) }

// Position is the line and column, both counted from 1, of a byte offset in the source. The column counts characters.
func Position(src string, offset int) (line, col int) {
	offset = max(0, min(offset, len(src)))
	line, col = 1, 1

	for _, r := range src[:offset] {
		if r == '\n' {
			line, col = line+1, 1
		} else {
			col++
		}
	}

	return line, col
}

// Kinds returns the kinds of the tokens, which is what the parser reads.
func Kinds(tokens []Token) []int {
	out := make([]int, len(tokens))
	for i, t := range tokens {
		out[i] = int(t.Kind)
	}

	return out
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' }
func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isWord(c byte) bool   { return isLetter(c) || isDigit(c) }
func isHex(c byte) bool    { return isDigit(c) || c >= 'a' && c <= 'f' }

// Lex makes the tokens of the source.
//
// A number is written one way: 0, 42, 1_000_000, 0xff, 0b1010_0101, 1.5, 1e9, 2.5e-3. An underscore sits between two digits,
// there are no leading zeros, and the letters in a number are lower case. 1. and .5 are not numbers, and neither is a number
// that goes on as a word (12abc). 1.e3 is the integer 1 and a member e3.
//
// What is not part of the language is an error: a semicolon, a character that is not ASCII outside a text, a text that
// is not closed or that has an escape other than \" \\ \n \t \r.
func Lex(src string) ([]Token, error) {
	l := &lexer{src: src}

	for l.pos < len(src) {
		if err := l.next(); err != nil {
			return nil, err
		}
	}

	// the last line ends, whether the source says so or not
	if n := len(l.out); n > 0 && l.out[n-1].Kind != NL {
		l.emit(NL, len(src), len(src))
	}

	return l.out, nil
}

type lexer struct {
	src string
	pos int
	out []Token
}

func (l *lexer) emit(kind Kind, begin, end int) {
	l.out = append(l.out, Token{kind, begin, end})
}

func (l *lexer) fail(pos int, format string, args ...any) error {
	return &Error{pos, fmt.Sprintf(format, args...)}
}

// last is the kind of the token before, or 0.
func (l *lexer) last() Kind {
	if n := len(l.out); n > 0 {
		return l.out[n-1].Kind
	}

	return 0
}

func (l *lexer) at(i int) byte {
	if i < len(l.src) {
		return l.src[i]
	}

	return 0
}

func (l *lexer) next() error {
	src, i := l.src, l.pos
	c := src[i]

	switch {
	case c == ' ' || c == '\t':
		l.pos++
	case c == '\n' || c == '\r' && l.at(i+1) == '\n':
		end := i + 1
		if c == '\r' {
			end++
		}

		// blank lines are not lines, and a file does not start with a line end
		if k := l.last(); k != NL && k != 0 {
			l.emit(NL, i, end)
		}

		l.pos = end
	case c == '/' && l.at(i+1) == '/':
		for l.pos < len(src) && src[l.pos] != '\n' {
			l.pos++
		}
	case isLetter(c):
		return l.word()
	case isDigit(c):
		return l.number()
	case c == '"':
		return l.text()
	default:
		return l.punctuation()
	}

	return nil
}

func (l *lexer) word() error {
	src, begin := l.src, l.pos

	for l.pos < len(src) && isWord(src[l.pos]) {
		l.pos++
	}

	word := src[begin:l.pos]

	if word == "_" {
		l.emit(Underscore, begin, l.pos)
		return nil
	}

	if kind, ok := keywords[word]; ok {
		l.emit(kind, begin, l.pos)
		return nil
	}

	// a name and a colon, not a double colon, and not a name that is behind a dot or a double colon: a member or a path
	// that is followed by a colon is part of an expression, as in {Color::Red: 1}
	if l.at(l.pos) == ':' && l.at(l.pos+1) != ':' {
		if k := l.last(); k != Dot && k != ColonColon {
			l.pos++
			l.emit(Label, begin, l.pos)

			return nil
		}
	}

	l.emit(Ident, begin, l.pos)

	return nil
}

// digits reads digits with single underscores between them, and says whether it read any.
func (l *lexer) digits(ok func(byte) bool) (bool, error) {
	start := l.pos

	for l.pos < len(l.src) && ok(l.src[l.pos]) {
		l.pos++

		if l.at(l.pos) == '_' {
			if !ok(l.at(l.pos + 1)) {
				return false, l.fail(l.pos, "an underscore in a number has to be between two digits")
			}

			l.pos++
		}
	}

	return l.pos > start, nil
}

func (l *lexer) number() error {
	src, begin := l.src, l.pos
	kind := Int

	switch {
	case src[begin] == '0' && l.at(begin+1) == 'x':
		l.pos += 2
		if ok, err := l.digits(isHex); err != nil || !ok {
			return orFail(err, l.fail(begin, "0x needs hex digits, in lower case"))
		}
	case src[begin] == '0' && l.at(begin+1) == 'b':
		l.pos += 2
		if ok, err := l.digits(func(c byte) bool { return c == '0' || c == '1' }); err != nil || !ok {
			return orFail(err, l.fail(begin, "0b needs binary digits"))
		}
	default:
		if src[begin] == '0' {
			l.pos++

			if isDigit(l.at(l.pos)) {
				return l.fail(begin, "a number does not start with a zero")
			}
		} else if _, err := l.digits(isDigit); err != nil {
			return err
		}

		// a dot is a fraction only if a digit follows: 1.max is the member max of 1
		if l.at(l.pos) == '.' && isDigit(l.at(l.pos+1)) {
			kind = Float
			l.pos++

			if _, err := l.digits(isDigit); err != nil {
				return err
			}
		}

		if l.at(l.pos) == 'e' {
			kind = Float
			l.pos++

			if c := l.at(l.pos); c == '+' || c == '-' {
				l.pos++
			}

			if ok, err := l.digits(isDigit); err != nil || !ok {
				return orFail(err, l.fail(begin, "an exponent needs digits"))
			}
		}
	}

	if isWord(l.at(l.pos)) {
		return l.fail(begin, "a number can not go on as a word: %q", src[begin:l.pos+1])
	}

	l.emit(kind, begin, l.pos)

	return nil
}

func orFail(err, otherwise error) error {
	if err != nil {
		return err
	}

	return otherwise
}

func (l *lexer) text() error {
	src, begin := l.src, l.pos
	l.pos++

	for l.pos < len(src) {
		switch c := src[l.pos]; c {
		case '"':
			l.pos++
			l.emit(Text, begin, l.pos)

			return nil
		case '\n':
			return l.fail(begin, "a text ends on the line it starts")
		case '\\':
			if !strings.ContainsRune(`"\ntr`, rune(l.at(l.pos+1))) || l.pos+1 >= len(src) {
				return l.fail(l.pos, "an escape is one of \\\" \\\\ \\n \\t \\r")
			}

			l.pos += 2
		default:
			l.pos++
		}
	}

	return l.fail(begin, "a text is not closed")
}

// two and one are the signs of two characters and of one.
var two = map[string]Kind{"::": ColonColon, "->": Arrow, "=>": FatArrow, "==": Eq, "!=": Ne, "<=": Le, ">=": Ge}

var one = map[byte]Kind{
	'(': LParen, ')': RParen, '{': LBrace, '}': RBrace, '[': LBracket, ']': RBracket, ',': Comma, ':': Colon, '.': Dot,
	'=': Assign, '<': Lt, '>': Gt, '+': Plus, '-': Minus, '*': Star, '/': Slash, '%': Percent,
}

func (l *lexer) punctuation() error {
	src, i := l.src, l.pos

	if i+1 < len(src) {
		if kind, ok := two[src[i:i+2]]; ok {
			l.pos += 2
			l.emit(kind, i, l.pos)

			return nil
		}
	}

	if kind, ok := one[src[i]]; ok {
		l.pos++
		l.emit(kind, i, l.pos)

		return nil
	}

	switch c := src[i]; {
	case c == ';':
		return l.fail(i, "there are no semicolons: a line ends a statement")
	case c == '!':
		return l.fail(i, "! is only part of !=: use not")
	case c >= 0x80:
		return l.fail(i, "only ASCII outside a text")
	default:
		return l.fail(i, "unexpected character %q", c)
	}
}
