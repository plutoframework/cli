package sqlcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// checkWithSQLC analyses every query set and verifies sqlc diff.
func checkWithSQLC(
	ctx context.Context,
	cfg Config,
	sqlc string,
) ([]Diagnostic, error) {
	tables := tableMap(cfg)

	var diagnostics []Diagnostic

	for _, set := range cfg.Sets {
		files, err := expandSet(cfg.Root, set)
		if err != nil || len(files) == 0 {
			continue
		}

		found, err := analyzeSet(ctx, cfg, sqlc, set, files, tables)
		if err != nil {
			return nil, err
		}

		diagnostics = append(diagnostics, found...)
	}

	if cfg.SQLCConfig != "" {
		diagnostics = append(
			diagnostics,
			checkDiff(ctx, cfg, sqlc)...,
		)
	}

	return diagnostics, nil
}

// planPayload is the subset of the sqlc plugin request the check reads.
type planPayload struct {
	Queries []struct {
		Name    string `json:"name"`
		Text    string `json:"text"`
		Columns []struct {
			Table *struct {
				Name string `json:"name"`
			} `json:"table"`
		} `json:"columns"`
		Params []struct {
			Number int `json:"number"`
			Column *struct {
				NotNull     bool `json:"not_null"`
				IsSQLCSlice bool `json:"is_sqlc_slice"`
				Table       *struct {
					Name string `json:"name"`
				} `json:"table"`
			} `json:"column"`
		} `json:"params"`
	} `json:"queries"`
}

func analyzeSet(
	ctx context.Context,
	cfg Config,
	sqlc string,
	set QuerySet,
	files []string,
	tables map[string]Table,
) ([]Diagnostic, error) {
	payload, output, err := captureCatalog(ctx, cfg, sqlc, set, files)
	if err != nil {
		return nil, err
	}

	if payload == nil {
		return []Diagnostic{newDiagnostic(
			files[0],
			1,
			fmt.Sprintf(
				"sqlc could not analyse query set %q: %s",
				set.Group,
				output,
			),
			"Fix the query or schema error reported by sqlc.",
		)}, nil
	}

	locations := queryLocations(cfg.Root, files)

	env := queryEnv{
		tables:  tables,
		allowed: map[string]bool{},
		set:     set.Group,
	}
	for _, name := range set.Tables {
		env.allowed[tableKey(name)] = true
	}

	var diagnostics []Diagnostic

	for _, raw := range payload.Queries {
		q := query{Name: raw.Name, Text: raw.Text}

		location, known := locations[raw.Name]
		if known {
			q.File, q.Line = location.file, location.line
		} else {
			q.File, q.Line = files[0], 1
		}

		for _, column := range raw.Columns {
			if column.Table != nil {
				q.Tables = append(q.Tables, column.Table.Name)
			}
		}

		for _, param := range raw.Params {
			item := queryParam{Number: param.Number}
			if param.Column != nil {
				item.NotNull = param.Column.NotNull

				item.Slice = param.Column.IsSQLCSlice
				if param.Column.Table != nil {
					q.Tables = append(q.Tables, param.Column.Table.Name)
				}
			}

			q.Params = append(q.Params, item)
		}

		diagnostics = append(diagnostics, analyzeQuery(q, env)...)
	}

	return diagnostics, nil
}

type location struct {
	file string
	line int
}

// queryLocations maps sqlc query names to their `-- name:` comment line.
func queryLocations(root string, files []string) map[string]location {
	locations := map[string]location{}

	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			continue
		}

		for index, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 3 && fields[0] == "--" &&
				fields[1] == "name:" {
				locations[fields[2]] = location{file, index + 1}
			}
		}
	}

	return locations
}

type sqlcConfigFile struct {
	Version string          `json:"version"`
	Plugins []sqlcPlugin    `json:"plugins"`
	SQL     []sqlcSQLConfig `json:"sql"`
}

type sqlcPlugin struct {
	Name    string `json:"name"`
	Process struct {
		Cmd    string `json:"cmd"`
		Format string `json:"format"`
	} `json:"process"`
}

type sqlcSQLConfig struct {
	Engine  string        `json:"engine"`
	Schema  []string      `json:"schema"`
	Queries []string      `json:"queries"`
	Codegen []sqlcCodegen `json:"codegen"`
}

type sqlcCodegen struct {
	Plugin string `json:"plugin"`
	Out    string `json:"out"`
}

// captureCatalog runs sqlc generate with a capture-only process plugin so the
// request holds sqlc's own parse of the queries. A nil payload means sqlc
// rejected the input; the second value then carries its message.
func captureCatalog(
	ctx context.Context,
	cfg Config,
	sqlc string,
	set QuerySet,
	files []string,
) (*planPayload, string, error) {
	dir, err := os.MkdirTemp("", "pluto-sqlcheck-")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(dir)

	capture := filepath.Join(dir, "request.json")
	plugin := filepath.Join(dir, "capture.sh")

	script := "#!/bin/sh\ncat > " + shellQuote(capture) + "\nprintf '{}'\n"
	if err := os.WriteFile(plugin, []byte(script), 0o700); err != nil {
		return nil, "", err
	}

	var schemas, queries []string

	// sqlc resolves paths relative to the config file, so make them so.
	relative := func(path string) string {
		rel, err := filepath.Rel(dir, filepath.Join(cfg.Root, path))
		if err != nil {
			return filepath.Join(cfg.Root, path)
		}

		return rel
	}
	for _, schema := range cfg.SchemaDirs {
		schemas = append(schemas, relative(schema))
	}

	for _, file := range files {
		queries = append(queries, relative(file))
	}

	generated := sqlcConfigFile{Version: "2"}
	entry := sqlcPlugin{Name: "pluto-capture"}
	entry.Process.Cmd = plugin
	entry.Process.Format = "json"
	generated.Plugins = []sqlcPlugin{entry}
	generated.SQL = []sqlcSQLConfig{{
		Engine:  "postgresql",
		Schema:  schemas,
		Queries: queries,
		Codegen: []sqlcCodegen{{Plugin: "pluto-capture", Out: "out"}},
	}}

	encoded, err := json.Marshal(generated)
	if err != nil {
		return nil, "", err
	}

	configPath := filepath.Join(dir, "sqlc.json")
	if err := os.WriteFile(configPath, encoded, 0o600); err != nil {
		return nil, "", err
	}

	output, runErr := runSQLC(ctx, cfg.Root, sqlc, "generate", "-f", configPath)

	data, readErr := os.ReadFile(capture)
	if runErr != nil || readErr != nil {
		return nil, output, nil
	}

	var payload planPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, "unreadable sqlc catalog: " + err.Error(), nil
	}

	return &payload, "", nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func runSQLC(
	ctx context.Context,
	root string,
	sqlc string,
	args ...string,
) (string, error) {
	command := exec.CommandContext(ctx, sqlc, args...)
	command.Dir = root

	var output bytes.Buffer

	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()

	return strings.TrimSpace(output.String()), err
}

// checkDiff requires the committed sqlc output to match a fresh generate.
func checkDiff(
	ctx context.Context,
	cfg Config,
	sqlc string,
) []Diagnostic {
	output, err := runSQLC(ctx, cfg.Root, sqlc, "diff", "-f", cfg.SQLCConfig)
	if err == nil {
		return nil
	}

	if len(output) > 800 {
		output = output[:800] + "..."
	}

	return []Diagnostic{newDiagnostic(
		cfg.SQLCConfig,
		1,
		"sqlc diff is not clean: "+output,
		"Run sqlc generate and commit the result; never hand-edit "+
			"generated query code.",
	)}
}
