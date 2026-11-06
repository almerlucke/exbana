package check

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/almerlucke/exbana/v3/examples/covenant/token"
)

// A directory is a module: all the .cov files in it are one set of names. A directory inside it is another module, named after
// where it is: the module of the directory myapp has the module myapp::util in the directory myapp/util. There is no line in a
// file that says what module it is in, so what a file says can not differ from where it is.
//
// A .cov file that is given on its own is a module of one file, and its name is the name of the file.
//
// The name of a module comes from where it is, and that can be wrong: a project that is checked out as myapp-main, a library that is
// kept in a directory with another name than the one it is used by. So whoever loads a program can name it, as a mount: name=dir
// is the directory dir as the module name, and name=file.cov is the file as it. Nothing is in the files for that.

// validName says whether a name of a directory or a file can be the name of a module: it is a name, and not a keyword.
func validName(s string) bool {
	if s == "" {
		return false
	}

	for i, r := range s {
		switch {
		case r == '_', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}

	for _, k := range token.Keywords() {
		if s == k {
			return false
		}
	}

	return true
}

func moduleName(what, name string) error {
	if !validName(name) {
		return fmt.Errorf("%s is not a name that a module can have (%s): rename it to letters, digits and underscores, not starting with a digit", name, what)
	}

	return nil
}

// mountName checks a name that is given for a module: it is names with :: between them, as the name of a module in a directory in
// another is.
func mountName(arg, name string) error {
	for _, part := range strings.Split(name, "::") {
		if !validName(part) {
			return fmt.Errorf("%s is not a name that a module can have (in %s): it is names with :: between them, each of letters, digits and underscores, not starting with a digit and not a keyword", name, arg)
		}
	}

	return nil
}

// SplitMount is the name and the path in an argument that may be name=path. If the argument is a path that is there, it is that,
// whatever is in it; and if it has no name, the name is "".
func SplitMount(arg string) (name, path string) {
	if _, err := os.Stat(arg); err == nil {
		return "", arg
	}

	if i := strings.Index(arg, "="); i > 0 {
		if _, err := os.Stat(arg[i+1:]); err == nil {
			return arg[:i], arg[i+1:]
		}
	}

	return "", arg
}

// LoadFiles reads .cov files that are given on their own: each of them is a module named after the file, or by the name in
// name=file.cov.
func LoadFiles(paths []string) (map[string][]Source, error) {
	modules := map[string][]Source{}

	for _, arg := range paths {
		name, p := SplitMount(arg)

		if name == "" {
			name = strings.TrimSuffix(filepath.Base(p), ".cov")
			if err := moduleName(p, name); err != nil {
				return nil, err
			}
		} else if err := mountName(arg, name); err != nil {
			return nil, err
		}

		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}

		if _, dup := modules[name]; dup {
			return nil, fmt.Errorf("two files are the module %s: %s", name, p)
		}

		modules[name] = []Source{{Name: p, Text: string(data)}}
	}

	return modules, nil
}

// LoadDir reads the modules of the directory tree at root: the directory is a module, named after it, and so is every directory
// in it that has .cov files, named after where it is. It gives the modules, and the name of the module of root itself. Directories
// and files that start with a dot or an underscore are left out.
func LoadDir(root string) (modules map[string][]Source, name string, err error) {
	return LoadDirAs(root, "")
}

// LoadDirAs is LoadDir for a directory that is given a name, which is the name of its module, and the start of the names of the
// modules in it. The name of the directory is not looked at then. An empty name is the name of the directory.
func LoadDirAs(root, as string) (modules map[string][]Source, name string, err error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, "", err
	}

	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return nil, "", fmt.Errorf("%s is not a directory", root)
	}

	if as != "" {
		name = as
		if err := mountName(as+"="+root, name); err != nil {
			return nil, "", err
		}
	} else {
		name = filepath.Base(abs)
		if err := moduleName(root, name); err != nil {
			return nil, "", err
		}
	}

	modules = map[string][]Source{}

	var walk func(dir, shown, module string) error

	walk = func(dir, shown, module string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}

		for _, e := range entries {
			n := e.Name()
			if strings.HasPrefix(n, ".") || strings.HasPrefix(n, "_") {
				continue
			}

			switch {
			case e.IsDir():
				if err := moduleName(filepath.Join(shown, n), n); err != nil {
					return err
				}

				if err := walk(filepath.Join(dir, n), filepath.Join(shown, n), module+"::"+n); err != nil {
					return err
				}
			case strings.HasSuffix(n, ".cov") && isFile(filepath.Join(dir, n)):
				data, err := os.ReadFile(filepath.Join(dir, n))
				if err != nil {
					return err
				}

				modules[module] = append(modules[module], Source{Name: filepath.Join(shown, n), Text: string(data)})
			}
		}

		return nil
	}

	if err := walk(abs, filepath.Clean(root), name); err != nil {
		return nil, "", err
	}

	if len(modules) == 0 {
		return nil, "", fmt.Errorf("there are no .cov files in %s", root)
	}

	return modules, name, nil
}

func isFile(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.Mode().IsRegular()
}

// Merge adds modules to others, and says if one of them is a module twice.
func Merge(into, from map[string][]Source) error {
	names := make([]string, 0, len(from))
	for n := range from {
		names = append(names, n)
	}

	sort.Strings(names)

	for _, n := range names {
		if _, dup := into[n]; dup {
			return fmt.Errorf("the module %s is there twice: %s and %s", n, into[n][0].Name, from[n][0].Name)
		}

		into[n] = from[n]
	}

	return nil
}
