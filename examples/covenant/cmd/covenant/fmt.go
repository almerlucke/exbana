package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/almerlucke/exbana/v3/examples/covenant/format"
)

// formatCommand is "covenant fmt": it writes Covenant source the one way it is written.
//
//	covenant fmt app.cov            the formatted source, on the standard output
//	covenant fmt -                  the same for the source on the standard input, which is what an editor uses
//	covenant fmt -w app.cov dir/    rewrite the files, and the .cov files in the directories, that are not formatted
//	covenant fmt -l dir/            list the files that are not formatted
//	covenant fmt -check dir/        the same, and the exit code is 1 if there are any (for a build to ask)
func formatCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("fmt", flag.ContinueOnError)
	flags.SetOutput(stderr)

	write := flags.Bool("w", false, "rewrite the files that are not formatted")
	list := flags.Bool("l", false, "list the files that are not formatted")
	check := flags.Bool("check", false, "list the files that are not formatted, and exit with 1 if there are any")

	if err := flags.Parse(args); err != nil {
		return 2
	}

	paths := flags.Args()
	if len(paths) == 0 {
		fmt.Fprintln(stderr, "usage: covenant fmt [-w] [-l] [-check] files, directories or - ...")
		return 2
	}

	// without a flag the result is written to the output, which is for one source
	if !*write && !*list && !*check && (len(paths) > 1 || isDir(paths[0])) {
		fmt.Fprintln(stderr, "to format more than one file, or a directory, give -w, -l or -check")
		return 2
	}

	code := 0

	each := func(name string, src []byte, rewrite func([]byte) error) {
		out, err := format.Source(string(src))
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)

			code = 1

			return
		}

		same := out == string(src)

		switch {
		case *list || *check:
			if !same {
				fmt.Fprintln(stdout, name)

				if *check {
					code = 1
				}
			}
		case !same && *write:
			if err := rewrite([]byte(out)); err != nil {
				fmt.Fprintf(stderr, "%s: %v\n", name, err)

				code = 1
			}
		}

		if !*write && !*list && !*check {
			io.WriteString(stdout, out)
		}
	}

	for _, path := range paths {
		if path == "-" {
			src, err := io.ReadAll(stdin)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}

			each("<stdin>", src, func(out []byte) error {
				_, err := stdout.Write(out)
				return err
			})

			continue
		}

		files, err := covFiles(path)
		if err != nil {
			fmt.Fprintln(stderr, err)

			code = 1

			continue
		}

		for _, file := range files {
			src, err := os.ReadFile(file)
			if err != nil {
				fmt.Fprintln(stderr, err)

				code = 1

				continue
			}

			each(file, src, func(out []byte) error { return os.WriteFile(file, out, 0o644) })
		}
	}

	return code
}

// covFiles is the file, or the .cov files in a directory and the ones below it. The names that start with a dot or an underscore
// are left out, as they are when a directory is loaded as a program.
func covFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		return []string{path}, nil
	}

	var out []string

	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if name := d.Name(); p != path && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
			if d.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		if !d.IsDir() && strings.HasSuffix(p, ".cov") {
			out = append(out, p)
		}

		return nil
	})

	return out, err
}
