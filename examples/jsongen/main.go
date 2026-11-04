// Command jsongen parses JSON with a parser that was generated from a grammar, and says what is in it.
//
//	go run ./examples/jsongen '{"a": [1, 2, {"b": null}]}'
//
// The parser is in the parser package. It is generated from the grammar in the grammar package, and does not use the
// library: regenerate it with go generate after changing the grammar.
package main

//go:generate go run ./gen

import (
	"fmt"
	"os"
	"sort"

	"github.com/almerlucke/exbana/v3/examples/jsongen/parser"
)

func main() {
	input := `{"name": "exbana", "tags": ["parser", "generator"], "stars": 5, "archived": false, "parent": null}`
	if len(os.Args) > 1 {
		input = os.Args[1]
	}

	tree, err := parser.NewJSONParser().ParseString(input)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Print(Summary(tree))
}

// Summary counts the rules in the tree, and lists the keys of the top level object.
func Summary(tree *parser.JSONTree) string {
	counts := map[string]int{}
	for _, n := range tree.Nodes {
		counts[n.Rule.String()]++
	}

	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}

	sort.Strings(names)

	out := fmt.Sprintf("%d nodes\n", len(tree.Nodes))
	for _, name := range names {
		out += fmt.Sprintf("  %-8s %d\n", name, counts[name])
	}

	return out
}
