package project

import (
	"bytes"
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SafePath refuses traversal and every symlink component, including the root.
// It is a preflight, not a defense against hostile concurrent filesystem races.
func SafePath(root, relative string) (string, error) {
	if filepath.IsAbs(relative) || relative == "" ||
		filepath.Clean(relative) != relative ||
		relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe generated path %q", relative)
	}

	path := filepath.Join(root, relative)

	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	for current := absolute; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}

		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlink refused: %s", current)
		}

		if filepath.Dir(current) == current {
			break
		}
	}

	return path, nil
}

// Equivalent ignores comments and formatting, but compares every Go token.
func Equivalent(a, b []byte) bool {
	tokens := func(source []byte) string {
		files := token.NewFileSet()

		var lexer scanner.Scanner
		lexer.Init(files.AddFile("", files.Base(), len(source)), source, nil, 0)

		var result strings.Builder

		for {
			_, tok, literal := lexer.Scan()
			if tok == token.EOF {
				break
			}

			fmt.Fprintf(&result, "%d:%q;", tok, literal)
		}

		return result.String()
	}

	return tokens(a) == tokens(b)
}

// Apply checks all known conflicts before writing. Replacement is allowed only
// for files whose old generated contract still matches. On ordinary write errors
// it restores replaced files and removes created files; no user hook is executed.
func Apply(
	root string,
	files, previous map[string][]byte,
	dryRun bool,
) ([]string, error) {
	names := make([]string, 0, len(files))
	backups := map[string][]byte{}

	for name := range files {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		if strings.Count(string(files[name]), "\n") > 1000 {
			return nil, fmt.Errorf(
				"generated file %s exceeds 1000-line ceiling; split this group before adding endpoints",
				name,
			)
		}

		path, err := SafePath(root, name)
		if err != nil {
			return nil, err
		}

		data, err := os.ReadFile(path)
		if err == nil {
			old, owned := previous[name]
			if !owned ||
				!bytes.Equal(data, old) &&
					(!strings.HasSuffix(name, ".go") || !Equivalent(data, old)) {
				return nil, fmt.Errorf(
					"refusing to overwrite %s (existing or modified)",
					path,
				)
			}

			backups[name] = data
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}

	if dryRun {
		return names, nil
	}

	written := []string{}

	for _, name := range names {
		path := filepath.Join(root, name)

		err := os.MkdirAll(filepath.Dir(path), 0o755)
		if err == nil {
			mode := os.FileMode(0o644)
			if strings.HasPrefix(name, "scripts/") {
				mode = 0o755
			}

			err = os.WriteFile(path, files[name], mode)
		}

		if err != nil {
			for _, done := range append(written, name) {
				dest := filepath.Join(root, done)
				if backup, ok := backups[done]; ok {
					_ = os.WriteFile(dest, backup, 0o644)
				} else {
					_ = os.Remove(dest)
				}
			}

			return nil, err
		}

		written = append(written, name)
	}

	return names, nil
}
