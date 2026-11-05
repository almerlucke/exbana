// Command covenant checks and runs Covenant programs.
//
//	covenant check app.cov [more.cov ...]      the types and effects of the program
//	covenant test  app.cov [more.cov ...]      the same, and then the examples of its functions
//	covenant run   app.cov [more.cov ...] app::main [arguments]   call a function
//
// Every file is a module, named by the name of the file without .cov: geometry.cov is the module geometry, which another file
// uses with "use geometry". A function is named with its module, app::main. The arguments are numbers, true and false, and
// otherwise text.
//
// The program can print: the module io has print(text), with the effect io::print, and a program that runs is granted that.
// Other effects can be granted with -grant, but there is nothing in this command to do them: they only let a function that has
// them be called.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/almerlucke/exbana/v3/examples/covenant/check"
	"github.com/almerlucke/exbana/v3/examples/covenant/run"
)

const ioModule = `
trait Functions {
    fn print(text: Text) with io::print
}
`

func main() { os.Exit(command(os.Args[1:], os.Stdout, os.Stderr)) }

func command(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: covenant check|test|run files... [module::function arguments...]")
		return 2
	}

	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)

	grant := fs.String("grant", "", "effects to grant, separated by commas")
	steps := fs.Int64("max-steps", 0, "end the call after this many steps (0 is the default, -1 is no limit)")
	timeout := fs.Duration("timeout", 0, "end the call after this long")

	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}

	var files, rest []string

	for i, a := range fs.Args() {
		if strings.HasSuffix(a, ".cov") {
			files = append(files, a)
		} else {
			rest = fs.Args()[i:]
			break
		}
	}

	if len(files) == 0 {
		fmt.Fprintln(stderr, "no files: give the .cov files of the program")
		return 2
	}

	sources := map[string]string{}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}

		sources[strings.TrimSuffix(filepath.Base(f), ".cov")] = string(data)
	}

	res := check.Check(check.Config{Native: map[string]string{"io": ioModule}}, sources)
	if !res.OK() {
		for _, d := range res.Diagnostics {
			fmt.Fprintln(stderr, d)
		}

		return 1
	}

	switch args[0] {
	case "check":
		fmt.Fprintf(stdout, "%d modules: no problems\n", len(sources))

		return 0
	case "test", "run":
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		return 2
	}

	cfg := run.Config{
		MaxSteps: *steps,
		Grant:    append([]string{"io::print"}, splitList(*grant)...),
		Host: map[string]run.HostFunc{
			"io::print": func(ctx context.Context, a []run.Value) (run.Value, error) {
				fmt.Fprintln(stdout, a[0].(string))
				return run.Unit{}, nil
			},
		},
	}

	prog, err := run.New(res, cfg)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	ctx := context.Background()

	if *timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}

	if args[0] == "test" {
		return test(ctx, prog, stdout, stderr)
	}

	return call(ctx, prog, rest, stdout, stderr)
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}

	return strings.Split(s, ",")
}

func test(ctx context.Context, prog *run.Program, stdout, stderr io.Writer) int {
	failed := 0

	results := prog.Examples(ctx)
	for _, r := range results {
		if r.Err != nil {
			failed++

			fmt.Fprintf(stderr, "%s:%d: example in %s fails: %s\n    %s\n", r.Module, r.Line, r.Function, r.Err, r.Text)
		}
	}

	fmt.Fprintf(stdout, "%d examples, %d fail\n", len(results), failed)

	if failed > 0 {
		return 1
	}

	return 0
}

func call(ctx context.Context, prog *run.Program, rest []string, stdout, stderr io.Writer) int {
	if len(rest) == 0 {
		fmt.Fprintln(stderr, "what to run? give module::function after the files")
		return 2
	}

	module, fn, ok := strings.Cut(rest[0], "::")
	if !ok {
		fmt.Fprintf(stderr, "%q is not module::function\n", rest[0])
		return 2
	}

	var args []run.Value

	for _, a := range rest[1:] {
		args = append(args, argument(a))
	}

	v, err := prog.Call(ctx, module, fn, args...)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	if _, isUnit := v.(run.Unit); !isUnit {
		fmt.Fprintln(stdout, run.Format(v))
	}

	return 0
}

// argument is the value an argument of the command line stands for.
func argument(s string) run.Value {
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}

	if f, err := strconv.ParseFloat(s, 64); err == nil && strings.ContainsAny(s, ".eE") {
		return f
	}

	switch s {
	case "true":
		return true
	case "false":
		return false
	}

	return s
}
