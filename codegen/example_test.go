package codegen_test

import (
	"fmt"
	"strings"

	"github.com/almerlucke/exbana/v3/codegen"
	"github.com/almerlucke/exbana/v3/optimize"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	"github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/patterns/vector"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

// Generate writes the source of a parser for a grammar. The grammar first has to be free of backtracking: optimize.Optimize
// takes it out wherever that is provably safe, and Generate says where it is not.
func ExampleGenerate() {
	digits := rule.New[rune, runes.Pos]("number", repetition.OneOrMore[rune, runes.Pos](entity.Between[rune, runes.Pos]('0', '9')))
	op := rule.New[rune, runes.Pos]("operator", alternation.New[rune, runes.Pos](vector.Text[runes.Pos]("+"), vector.Text[runes.Pos]("-")))
	sum := rule.New[rune, runes.Pos]("sum", concatenation.New[rune, runes.Pos](digits, repetition.Any[rune, runes.Pos](concatenation.New[rune, runes.Pos](op, digits))))

	// without this the repetitions and the alternation still backtrack, and Generate refuses
	if _, err := codegen.Generate[rune, runes.Pos](sum, codegen.Config[rune, runes.Pos]{Package: "calc", Name: "Calc"}); err != nil {
		fmt.Println("refused:", strings.SplitN(err.Error(), "\n", 2)[0])
	}

	if _, err := optimize.Optimize[rune, runes.Pos](sum, optimize.Options{Closed: true}); err != nil {
		fmt.Println(err)
		return
	}

	src, err := codegen.Generate[rune, runes.Pos](sum, codegen.Config[rune, runes.Pos]{Package: "calc", Name: "Calc"})
	if err != nil {
		fmt.Println(err)
		return
	}

	// the rules of the grammar are constants in the generated code, and the parser and tree are types
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(line, "\tCalcRule") || strings.HasPrefix(line, "type Calc") || strings.HasPrefix(line, "func NewCalc") {
			fmt.Println(line)
		}
	}

	// Output:
	// refused: codegen: 3 problems:
	// type CalcRule uint16
	// 	CalcRuleSum CalcRule = iota
	// 	CalcRuleNumber
	// 	CalcRuleOperator
	// type CalcNode struct {
	// type CalcTree struct {
	// type CalcSyntaxError struct {
	// type CalcParser struct {
	// func NewCalcParser() *CalcParser {
}
