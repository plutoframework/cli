package project

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"path"
	"sort"
	"strings"
)

// LockPath is the managed lock: for each CLI-owned file, the digest of the
// content the CLI last wrote. It is committed with the application. Without
// it a template change in any CLI release would make every earlier render
// look like a hand edit; with it, a file that still matches what the CLI
// wrote is known to be pristine, whatever CLI version wrote it.
const LockPath = ".pluto/managed.lock"

// CLIVersion is stamped into the lock header. It is informational only;
// decisions use digests.
var CLIVersion string

// Lock maps a managed path to the digest of the last CLI-written content.
type Lock map[string]string

// SourceDigest hashes content the way the CLI compares it: Go by tokens and
// comment text, so gofmt-only changes keep a file pristine (the CLI formats
// after writing), everything else byte for byte.
func SourceDigest(name string, data []byte) string {
	sum := sha256.New()

	if path.Ext(name) == ".go" {
		if tokens, ok := GoTokens(data); ok {
			sum.Write([]byte("go-tokens\n" + tokens))

			return hex.EncodeToString(sum.Sum(nil))
		}
	}

	sum.Write([]byte("bytes\n"))
	sum.Write(data)

	return hex.EncodeToString(sum.Sum(nil))
}

// Matches reports whether data is exactly what the CLI last wrote to name.
func (l Lock) Matches(name string, data []byte) bool {
	digest, ok := l[name]

	return ok && digest == SourceDigest(name, data)
}

// ParseLock reads the lock format: comment lines, then "<sha256> <path>".
func ParseLock(data []byte) (Lock, error) {
	lock := Lock{}
	scan := bufio.NewScanner(bytes.NewReader(data))

	for number := 1; scan.Scan(); number++ {
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		digest, name, ok := strings.Cut(line, " ")
		if !ok || len(digest) != sha256.Size*2 || name == "" {
			return nil, fmt.Errorf(
				"%s:%d: malformed lock entry; rerun pluto generate",
				LockPath,
				number,
			)
		}

		lock[name] = digest
	}

	return lock, scan.Err()
}

// Render writes the lock deterministically: sorted, no timestamps.
func (l Lock) Render() []byte {
	var out strings.Builder

	out.WriteString("# Managed by pluto; do not edit. Digests of the " +
		"content the CLI last wrote\n")
	out.WriteString("# to each managed file. pluto generate overwrites a " +
		"managed file only while\n")
	out.WriteString("# it still matches its digest; commit this file.\n")

	if CLIVersion != "" {
		out.WriteString("# pluto " + CLIVersion + "\n")
	}

	names := make([]string, 0, len(l))
	for name := range l {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		out.WriteString(l[name] + " " + name + "\n")
	}

	return []byte(out.String())
}

// ReadLock loads the lock; a missing file is an empty lock and ok is false.
func ReadLock(root string) (lock Lock, ok bool, err error) {
	file, err := SafePath(root, LockPath)
	if err != nil {
		return nil, false, err
	}

	data, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return Lock{}, false, nil
	}

	if err != nil {
		return nil, false, err
	}

	lock, err = ParseLock(data)

	return lock, err == nil, err
}

// recordLock notes freshly written managed files in the lock. Called by
// Apply after a successful write, so every command that renders managed files
// keeps the lock current. Authored files written by the same command (stubs,
// lifecycles) are never recorded: the lock must not make them replaceable.
func recordLock(root string, files map[string][]byte) error {
	lock, _, err := ReadLock(root)
	if err != nil {
		return err
	}

	manifest, err := lockManifest(root, files)
	if err != nil {
		return err
	}

	managed, err := Managed(manifest)
	if err != nil {
		return err
	}

	documents, err := Documents(manifest)
	if err != nil {
		return err
	}

	for name, data := range files {
		_, isManaged := managed[name]
		_, isDocument := documents[name]

		if isManaged || isDocument {
			lock[name] = SourceDigest(name, data)
		}
	}

	file := root + "/" + LockPath
	if err := os.MkdirAll(path.Dir(file), 0o755); err != nil {
		return err
	}

	return os.WriteFile(file, lock.Render(), 0o644)
}

// lockManifest is the manifest the written files belong to: the one being
// written, else the one on disk.
func lockManifest(root string, files map[string][]byte) (Manifest, error) {
	if data, ok := files["pluto.toml"]; ok {
		return Decode(data)
	}

	_, manifest, err := Discover(root)

	return manifest, err
}

// GoTokens lists tokens with comment text. Automatic semicolons and trailing
// commas before a closing bracket are dropped: formatters add or remove them.
func GoTokens(source []byte) (string, bool) {
	files := token.NewFileSet()
	failed := false

	var lexer scanner.Scanner

	lexer.Init(
		files.AddFile("", files.Base(), len(source)),
		source,
		func(token.Position, string) {
			failed = true
		},
		scanner.ScanComments,
	)

	type item struct {
		tok     token.Token
		literal string
	}

	items := []item{}

	for {
		_, tok, literal := lexer.Scan()
		if tok == token.EOF {
			break
		}

		if tok == token.SEMICOLON && literal == "\n" {
			continue
		}

		if tok == token.COMMENT {
			literal = strings.TrimSpace(literal)
		}

		items = append(items, item{tok, literal})
	}

	var result strings.Builder

	for i, current := range items {
		if current.tok == token.COMMA && i+1 < len(items) {
			switch items[i+1].tok {
			case token.RPAREN, token.RBRACE, token.RBRACK:
				continue
			}
		}

		fmt.Fprintf(&result, "%d:%q;", current.tok, current.literal)
	}

	return result.String(), !failed
}
