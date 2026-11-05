// Command gen writes the parser for the Covenant grammar to parser/parser.go. Run it with go generate in the directory above.
package main

import (
	"log"
	"os"

	"github.com/almerlucke/exbana/v3/examples/covenant/grammar"
)

func main() {
	src, err := grammar.Source()
	if err != nil {
		log.Fatal(err)
	}

	if err := os.WriteFile("parser/parser.go", src, 0o644); err != nil {
		log.Fatal(err)
	}
}
