// Command covenant checks and runs Covenant programs, and offers their functions as tools.
//
//	covenant check    app.cov [more.cov ...]                        the types and effects of the program
//	covenant test     app.cov [more.cov ...]                        the same, and then the examples of its functions
//	covenant run      app.cov [more.cov ...] [args...]              run main, with the arguments
//	covenant run      app.cov [more.cov ...] app::name [arguments]  call another function
//	covenant describe app.cov [more.cov ...]                        the public functions as JSON for an agent: the manifest
//	covenant call     app.cov [more.cov ...] app::name '{"a": 1}'   call a public function with arguments in JSON, and get JSON
//	covenant dis      app.cov [more.cov ...] app::name              the bytecode of a function
//	covenant fmt      app.cov                                       the source, written the one way it is written (-w to rewrite the files)
//
// # Modules
//
// A program is a directory, or some files:
//
//	covenant run myapp/ args...         the directory myapp is the module myapp: all the .cov files in it are one set of names
//	covenant run app.cov util.cov args  each file on its own is a module, named after the file, without .cov
//	covenant run app=./checkout/ args   the directory is the module app, whatever it is called
//
// A directory in a directory is another module, named after where it is: myapp/util is myapp::util. Another module is used with
// "use myapp::util", and what is private in a module is private to all its files and to none outside it. -lib DIR adds the
// modules of another directory tree, to use, and -lib name=DIR gives it the name it is used by. A function is named with its
// module, myapp::name; main is the main of the module of the directory, or of the first file.
//
// The name is not in the files, so it can not differ from where they are; name=dir (and name=file.cov) is for when where they are is
// not the name they are used by: a checkout called myapp-main, a library that is kept in a directory with another name.
//
// # main
//
// A program is run from its main function, which is the one of the first file:
//
//	fn main(args: List<Text>) with fs::read -> Int
//
// It may have the parameter args, or none, and give nothing, an Int (the exit code), or a Result of those: an Err is written
// to the standard error, and the exit code is 1. To tell the arguments from a function to call, put -- before them.
//
// # Permissions
//
// A program is only allowed what it is given, and it is given nothing unless it is asked for here. The signature of main says
// what it needs, and the checker has proven that nothing it calls needs more; the flags say what it gets:
//
//	-allow-read=DIR,...     fs::read, for files under these directories
//	-allow-write=DIR,...    fs::write, for files under these directories
//	-allow-net=HOST,...     net::fetch, for these hosts (example.com, example.com:8080, *.example.com)
//	-allow-env=NAME,...     env::read, for these environment variables
//	-allow-run=PROGRAM,...  process::run, for these programs
//	-allow-stdin -allow-time -allow-random     io::read, time, random
//	-allow-private-net      lets net reach loopback and private addresses, which it does not do otherwise
//	-allow-signature        grants the effects that have nothing to scope: stdin, time and random
//	-allow-all              grants everything, anywhere
//
// io::print is always granted. If main needs more than it is given, nothing is run, and the message says which flags to give.
//
// run and test walk the tree by default; with -bytecode they run the bytecode, which gives the same and is faster.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/almerlucke/exbana/v3/examples/covenant/check"
	"github.com/almerlucke/exbana/v3/examples/covenant/run"
	"github.com/almerlucke/exbana/v3/examples/covenant/std"
	"github.com/almerlucke/exbana/v3/examples/covenant/tool"
)

// stdin is what programs read; the tests change it.
var stdin io.Reader = os.Stdin

// list is a flag with a list of values in it, separated by commas.
type list []string

func (l *list) String() string { return strings.Join(*l, ",") }

func (l *list) Set(s string) error {
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			*l = append(*l, v)
		}
	}

	return nil
}

// permissions is the flags that say what a program gets.
type permissions struct {
	read, write, net, env, run list
	stdin, time, random        bool
	privateNet, signature, all bool
}

func (p *permissions) register(fs *flag.FlagSet) {
	fs.Var(&p.read, "allow-read", "directories that can be read")
	fs.Var(&p.write, "allow-write", "directories that can be written")
	fs.Var(&p.net, "allow-net", "hosts that can be reached")
	fs.Var(&p.env, "allow-env", "environment variables that can be read")
	fs.Var(&p.run, "allow-run", "programs that can be run")
	fs.BoolVar(&p.stdin, "allow-stdin", false, "allow reading the standard input")
	fs.BoolVar(&p.time, "allow-time", false, "allow the clock")
	fs.BoolVar(&p.random, "allow-random", false, "allow random numbers")
	fs.BoolVar(&p.privateNet, "allow-private-net", false, "allow reaching loopback and private addresses")
	fs.BoolVar(&p.signature, "allow-signature", false, "grant the effects that have nothing to scope")
	fs.BoolVar(&p.all, "allow-all", false, "grant everything, anywhere")
}

func (p *permissions) policy(out, errs io.Writer) std.Policy {
	pol := std.Policy{
		ReadRoots: p.read, WriteRoots: p.write, NetHosts: p.net, EnvVars: p.env, Programs: p.run,
		AllowStdin: p.stdin || p.signature || p.all, AllowTime: p.time || p.signature || p.all, AllowRandom: p.random || p.signature || p.all,
		AllowPrivateNet: p.privateNet || p.all, Stdin: stdin, Stdout: out, Stderr: errs,
	}

	if p.all {
		pol.ReadRoots, pol.WriteRoots, pol.NetHosts, pol.EnvVars, pol.Programs = []string{"*"}, []string{"*"}, []string{"*"}, []string{"*"}, []string{"*"}
	}

	return pol
}

// the flag that gives an effect
var grantFlags = map[string]string{
	"fs::read": "-allow-read=DIR", "fs::write": "-allow-write=DIR", "net::fetch": "-allow-net=HOST", "env::read": "-allow-env=NAME",
	"process::run": "-allow-run=PROGRAM", "io::read": "-allow-stdin", "time": "-allow-time", "random": "-allow-random",
}

// notGranted says what a function needs that the policy does not grant, as a message that says what to give, or "" if it is all granted.
func notGranted(name string, fd *check.FnDef, pol std.Policy) string {
	granted := map[string]bool{}
	for _, e := range pol.Granted() {
		granted[e] = true
	}

	var lines []string

	for _, e := range fd.Type.Eff.All() {
		if !granted[e] {
			flag := grantFlags[e]
			if flag == "" {
				flag = "(the host has to grant it)"
			}

			lines = append(lines, fmt.Sprintf("  %-14s %s", e, flag))
		}
	}

	if len(lines) == 0 {
		return ""
	}

	sort.Strings(lines)

	return name + " needs permissions that were not granted:\n" + strings.Join(lines, "\n") +
		"\nThe signature says what it needs, and the flags say what it gets. -allow-signature grants stdin, time and random."
}

func main() { os.Exit(command(os.Args[1:], os.Stdout, os.Stderr)) }

func command(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: covenant check|test|run|describe|call|dis [flags] files... [what to run]\n       covenant fmt [-w] [-l] [-check] files...")
		return 2
	}

	name := args[0]

	if name == "fmt" {
		return formatCommand(args[1:], stdout, stderr)
	}

	switch name {
	case "check", "test", "run", "dis", "describe", "call":
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", name)
		return 2
	}

	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)

	steps := fs.Int64("max-steps", 0, "end the call after this many steps (0 is the default, -1 is no limit)")
	timeout := fs.Duration("timeout", 0, "end the call after this long")
	bytecode := fs.Bool("bytecode", false, "run the bytecode and not the tree")

	var libs list

	fs.Var(&libs, "lib", "a directory of modules to add to the program")

	var perms permissions
	perms.register(fs)

	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}

	// the program is a directory, or the .cov files at the start; the rest is for the program
	args = fs.Args()

	var (
		modules map[string][]check.Source
		first   string
		rest    []string
		err     error
	)

	switch {
	case len(args) > 0 && isDirArg(args[0]):
		as, path := check.SplitMount(args[0])

		if modules, first, err = check.LoadDirAs(path, as); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}

		rest = args[1:]
	default:
		n := 0
		for n < len(args) && strings.HasSuffix(args[n], ".cov") {
			n++
		}

		if n == 0 {
			fmt.Fprintln(stderr, "no program: give a directory, or the .cov files")
			return 2
		}

		if modules, err = check.LoadFiles(args[:n]); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}

		if as, path := check.SplitMount(args[0]); as != "" {
			first = as
		} else {
			first = strings.TrimSuffix(filepath.Base(path), ".cov")
		}

		rest = args[n:]
	}

	for _, lib := range libs {
		as, path := check.SplitMount(lib)

		more, _, err := check.LoadDirAs(path, as)
		if err == nil {
			err = check.Merge(modules, more)
		}

		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}

	res := check.CheckModules(check.Config{Native: std.Natives()}, modules)
	if !res.OK() {
		for _, d := range res.Diagnostics {
			fmt.Fprintln(stderr, d)
		}

		return 1
	}

	switch name {
	case "check":
		fmt.Fprintf(stdout, "%d modules: no problems\n", len(modules))

		return 0
	case "describe":
		return describe(res, stdout, stderr)
	}

	pol := perms.policy(stdout, stderr)
	host := std.New(pol)

	prog, err := run.New(res, run.Config{Host: host.Host(), Grant: pol.Granted(), MaxSteps: *steps, Bytecode: *bytecode})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	host.Bind(prog)

	ctx := context.Background()

	if *timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}

	switch name {
	case "test":
		return test(ctx, prog, stdout, stderr)
	case "dis":
		return dis(prog, rest, stdout, stderr)
	case "call":
		return callTool(ctx, prog, res, pol, rest, stdout, stderr)
	}

	return runProgram(ctx, prog, res, pol, first, rest, stdout, stderr)
}

// splitTarget is the module and the function in "myapp::util::double": the function is what comes after the last ::, since a module
// can have :: in its name.
func splitTarget(s string) (module, fn string, ok bool) {
	i := strings.LastIndex(s, "::")
	if i < 0 {
		return "", "", false
	}

	return s[:i], s[i+2:], true
}

// isDirArg says whether an argument is a directory, or a name for one: name=dir.
func isDirArg(arg string) bool {
	_, path := check.SplitMount(arg)

	return isDir(path)
}

func isDir(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.IsDir()
}

func describe(res *check.Result, stdout, stderr io.Writer) int {
	m, err := tool.Describe(res)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	out, err := m.JSON()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	fmt.Fprintln(stdout, string(out))

	return 0
}

// runProgram runs main, with the rest of the command line as its arguments, or the function that is named.
func runProgram(ctx context.Context, prog *run.Program, res *check.Result, pol std.Policy, first string, rest []string, stdout, stderr io.Writer) int {
	if len(rest) > 0 && rest[0] != "--" && strings.Contains(rest[0], "::") {
		return callFunction(ctx, prog, res, pol, rest, stdout, stderr)
	}

	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}

	var fd *check.FnDef

	ok := false

	if m := res.Modules[first]; m != nil {
		if d, there := m.Decls["main"]; there {
			fd, ok = d.Entity.(*check.FnDef)
		}
	}

	if !ok {
		fmt.Fprintf(stderr, "module %s has no main: write one, or say what to call, as %s::name\n", first, first)
		return 2
	}

	if ret := check.Resolve(fd.Type.Ret).String(); !validMainResult(ret) {
		fmt.Fprintf(stderr, "main gives %s: it has to give nothing, an Int (the exit code), or a Result of one of them\n", ret)
		return 2
	}

	var args []run.Value

	switch n := len(fd.Type.Params); {
	case n == 1 && check.Resolve(fd.Type.Params[0]).String() == "List<Text>":
		items := make([]run.Value, len(rest))
		for i, a := range rest {
			items[i] = a
		}

		args = []run.Value{run.NewList(items)}
	case n == 0:
		if len(rest) > 0 {
			fmt.Fprintln(stderr, "main takes no arguments, and some are given")
			return 2
		}
	default:
		fmt.Fprintln(stderr, "main has no parameters, or one: args: List<Text>")
		return 2
	}

	if msg := notGranted(first+"::main", fd, pol); msg != "" {
		fmt.Fprintln(stderr, msg)
		return 1
	}

	v, err := prog.Call(ctx, first, "main", args...)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	return exitCode(v, stderr)
}

func validMainResult(ret string) bool {
	return ret == "Unit" || ret == "Int" || strings.HasPrefix(ret, "Result<Unit,") || strings.HasPrefix(ret, "Result<Int,")
}

// exitCode is what a program that has ended says to the shell: nothing is 0, an Int is itself, an Ok is what is in it, and an Err is
// 1 and is written to the standard error.
func exitCode(v run.Value, stderr io.Writer) int {
	switch v := v.(type) {
	case int64:
		return int(v & 0xff)
	case *run.Enum:
		if v.Variant.Name == "Err" {
			if text, ok := v.Fields[0].(string); ok {
				fmt.Fprintln(stderr, "error:", text)
			} else {
				fmt.Fprintln(stderr, "error:", run.Format(v.Fields[0]))
			}

			return 1
		}

		return exitCode(v.Fields[0], stderr)
	}

	return 0
}

func test(ctx context.Context, prog *run.Program, stdout, stderr io.Writer) int {
	failed := 0

	results := prog.Examples(ctx)
	for _, r := range results {
		if r.Err != nil {
			failed++

			fmt.Fprintf(stderr, "%s:%d: example in %s fails: %s\n    %s\n", r.File, r.Line, r.Function, r.Err, r.Text)
		}
	}

	fmt.Fprintf(stdout, "%d examples, %d fail\n", len(results), failed)

	if failed > 0 {
		return 1
	}

	return 0
}

func dis(prog *run.Program, rest []string, stdout, stderr io.Writer) int {
	if len(rest) != 1 {
		fmt.Fprintln(stderr, "which function? give module::function after the files")
		return 2
	}

	module, fn, ok := splitTarget(rest[0])
	if !ok {
		fmt.Fprintf(stderr, "%q is not module::function\n", rest[0])
		return 2
	}

	out, err := prog.Disassemble(module, fn)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	fmt.Fprint(stdout, out)

	return 0
}

// callFunction calls a function that is named, with arguments from the command line, and writes what it gives.
func callFunction(ctx context.Context, prog *run.Program, res *check.Result, pol std.Policy, rest []string, stdout, stderr io.Writer) int {
	module, fn, ok := splitTarget(rest[0])
	if !ok {
		fmt.Fprintf(stderr, "%q is not module::function\n", rest[0])
		return 2
	}

	if fd := findFn(res, module, fn); fd != nil {
		if msg := notGranted(rest[0], fd, pol); msg != "" {
			fmt.Fprintln(stderr, msg)
			return 1
		}
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

func findFn(res *check.Result, module, fn string) *check.FnDef {
	m := res.Modules[module]
	if m == nil {
		return nil
	}

	d, ok := m.Decls[fn]
	if !ok {
		return nil
	}

	fd, _ := d.Entity.(*check.FnDef)

	return fd
}

// callTool calls a public function with arguments in JSON, and writes the result as JSON.
func callTool(ctx context.Context, prog *run.Program, res *check.Result, pol std.Policy, rest []string, stdout, stderr io.Writer) int {
	if len(rest) == 0 || len(rest) > 2 {
		fmt.Fprintln(stderr, `what to call? give module::function and the arguments as JSON: app::name '{"a": 1}'`)
		return 2
	}

	module, fn, ok := splitTarget(rest[0])
	if !ok {
		fmt.Fprintf(stderr, "%q is not module::function\n", rest[0])
		return 2
	}

	args := "{}"
	if len(rest) == 2 {
		args = rest[1]
	}

	if args == "-" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}

		args = string(data)
	}

	if fd := findFn(res, module, fn); fd != nil {
		if msg := notGranted(rest[0], fd, pol); msg != "" {
			fmt.Fprintln(stderr, msg)
			return 1
		}
	}

	out, err := tool.Call(ctx, prog, res, module, fn, []byte(args))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	fmt.Fprintln(stdout, string(out))

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
