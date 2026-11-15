package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/plutoframework/cli/internal/project"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
)

type operationBlueprint struct {
	Roles              []project.RoleContract      `toml:"roles"`
	Resources          []project.ResourceContract  `toml:"resources"`
	Operations         []project.Operation         `toml:"operations"`
	OperationContracts []project.OperationContract `toml:"operation_contracts"`
	Effects            []project.EffectContract    `toml:"effects"`
	Endpoints          []project.Endpoint          `toml:"endpoints"`
	Jobs               []project.Job               `toml:"jobs"`
	Agents             []project.Agent             `toml:"agents"`
	Tables             []project.Table             `toml:"tables"`
	Queries            []project.QuerySet          `toml:"queries"`
	Ports              []project.Port              `toml:"ports"`
	Models             []project.Model             `toml:"models"`
	Authored           *project.Authored           `toml:"authored"`
	Memberships        *project.Memberships        `toml:"memberships"`
	Lookups            []project.Lookup            `toml:"lookups"`
	Principals         []project.Principal         `toml:"principals"`
	Database           *blueprintDatabase          `toml:"database"`
}

// blueprintDatabase declares the runtime role for a blueprint. Engine and
// query settings stay owned by pluto.toml.
type blueprintDatabase struct {
	RuntimeRole string `toml:"runtime_role"`
}

func readOperationBlueprint(path string) (operationBlueprint, error) {
	var blueprint operationBlueprint

	info, err := os.Stat(path)
	if err != nil {
		return blueprint, err
	}

	if !info.Mode().IsRegular() {
		return blueprint, fmt.Errorf("blueprint must be a regular TOML file")
	}

	file, err := os.Open(path)
	if err != nil {
		return blueprint, err
	}
	defer file.Close()

	const maximum = 2 << 20

	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return blueprint, err
	}

	if len(data) > maximum {
		return blueprint, fmt.Errorf("blueprint exceeds 2 MiB input bound")
	}

	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&blueprint); err != nil {
		return blueprint, fmt.Errorf("invalid blueprint: %w", err)
	}

	if len(
		blueprint.Roles,
	)+len(
		blueprint.Resources,
	)+len(
		blueprint.Operations,
	)+len(
		blueprint.OperationContracts,
	)+len(
		blueprint.Effects,
	)+len(
		blueprint.Endpoints,
	)+len(
		blueprint.Jobs,
	)+len(
		blueprint.Agents,
	)+len(
		blueprint.Tables,
	)+len(
		blueprint.Queries,
	)+len(
		blueprint.Ports,
	)+len(
		blueprint.Models,
	)+len(
		blueprint.Lookups,
	)+len(
		blueprint.Principals,
	) == 0 && blueprint.Authored == nil && blueprint.Memberships == nil &&
		blueprint.Database == nil {
		return blueprint, fmt.Errorf(
			"blueprint must declare application contracts",
		)
	}

	return blueprint, nil
}

func addBlueprintCommand(options Options) *cobra.Command {
	var (
		file string
		dry  bool
	)

	command := &cobra.Command{
		Use: "blueprint --file PATH",
		Short: "Atomically add declared candidate resources, operations and " +
			"entries",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if file == "" {
				return fmt.Errorf("blueprint requires --file")
			}

			root, manifest, err := project.Discover(options.Directory)
			if err != nil {
				return err
			}

			if manifest.Boundary.Mode != "candidate" {
				return fmt.Errorf(
					"blueprints require an explicitly configured candidate " +
						"boundary profile",
				)
			}

			if err := project.Contracts(root, manifest); err != nil {
				return err
			}

			path := file
			if !filepath.IsAbs(path) {
				path = filepath.Join(root, path)
			}

			blueprint, err := readOperationBlueprint(path)
			if err != nil {
				return err
			}

			old := manifest
			manifest.Roles = append(
				append([]project.RoleContract(nil), manifest.Roles...),
				blueprint.Roles...)
			manifest.Resources = append(
				append([]project.ResourceContract(nil), manifest.Resources...),
				blueprint.Resources...)
			manifest.Operations = append(
				append([]project.Operation(nil), manifest.Operations...),
				blueprint.Operations...)
			manifest.OperationContracts = append(
				append(
					[]project.OperationContract(nil),
					manifest.OperationContracts...),
				blueprint.OperationContracts...)
			manifest.Effects = append(
				append([]project.EffectContract(nil), manifest.Effects...),
				blueprint.Effects...)
			manifest.Endpoints = append(
				append([]project.Endpoint(nil), manifest.Endpoints...),
				blueprint.Endpoints...)
			manifest.Jobs = append(
				append([]project.Job(nil), manifest.Jobs...),
				blueprint.Jobs...)

			manifest.Agents = append(
				append([]project.Agent(nil), manifest.Agents...),
				blueprint.Agents...)
			manifest.Tables = append(
				append([]project.Table(nil), manifest.Tables...),
				blueprint.Tables...)
			manifest.Queries = append(
				append([]project.QuerySet(nil), manifest.Queries...),
				blueprint.Queries...)
			manifest.Ports = append(
				append([]project.Port(nil), manifest.Ports...),
				blueprint.Ports...)
			manifest.Models = append(
				append([]project.Model(nil), manifest.Models...),
				blueprint.Models...)
			manifest.Lookups = append(
				append([]project.Lookup(nil), manifest.Lookups...),
				blueprint.Lookups...)
			manifest.Principals = append(
				append([]project.Principal(nil), manifest.Principals...),
				blueprint.Principals...)

			if blueprint.Authored != nil {
				if manifest.Authored != nil {
					return fmt.Errorf(
						"blueprint cannot replace existing [authored]",
					)
				}

				manifest.Authored = blueprint.Authored
			}

			if blueprint.Memberships != nil {
				if manifest.Memberships != nil {
					return fmt.Errorf(
						"blueprint cannot replace existing [memberships]",
					)
				}

				manifest.Memberships = blueprint.Memberships
			}

			if blueprint.Database != nil {
				if manifest.Database.RuntimeRole != "" {
					return fmt.Errorf(
						"blueprint cannot replace existing runtime_role",
					)
				}

				manifest.Database.RuntimeRole = blueprint.Database.RuntimeRole
			}

			if err := manifest.Validate(); err != nil {
				return err
			}

			files, err := project.Managed(manifest)
			if err != nil {
				return err
			}

			merge := func(generated map[string][]byte, err error) error {
				if err != nil {
					return err
				}

				for path, source := range generated {
					files[path] = source
				}

				return nil
			}
			for _, operation := range blueprint.Operations {
				if err := merge(
					project.OperationFiles(manifest, operation),
				); err != nil {
					return err
				}
			}

			for _, endpoint := range blueprint.Endpoints {
				if err := merge(
					project.EndpointFiles(manifest, endpoint),
				); err != nil {
					return err
				}
			}

			for _, job := range blueprint.Jobs {
				if err := merge(project.JobFiles(manifest, job)); err != nil {
					return err
				}
			}

			for _, agent := range blueprint.Agents {
				if err := merge(
					project.AgentFiles(manifest, agent),
				); err != nil {
					return err
				}
			}

			if err := merge(
				project.AuthoredAdapterStubs(manifest),
			); err != nil {
				return err
			}

			if len(blueprint.Tables) > 0 {
				if err := merge(
					project.ProposeTableMigrations(root, manifest),
				); err != nil {
					return err
				}
			}

			if blueprint.Database != nil || len(blueprint.Tables) > 0 {
				if err := merge(
					project.ProposeRuntimeRoleMigration(root, manifest, files),
				); err != nil {
					return err
				}
			}

			return applyBlueprintChange(cmd, root, manifest, old, files, dry)
		},
	}
	command.Flags().
		StringVar(
			&file,
			"file",
			"",
			"TOML blueprint path; relative paths use the project root",
		)
	command.Flags().
		BoolVar(
			&dry,
			"dry-run",
			false,
			"Validate the complete blueprint without writing",
		)

	return command
}

func applyBlueprintChange(
	cmd *cobra.Command,
	root string,
	manifest, old project.Manifest,
	files map[string][]byte,
	dry bool,
) error {
	previous, err := project.Managed(old)
	if err != nil {
		return err
	}

	// sqlc.yaml and .golangci.yml are the published tool files until a query
	// set is declared; the candidate render then extends them, so untouched
	// copies are replaceable.
	for _, name := range []string{"sqlc.yaml", ".golangci.yml"} {
		if _, owned := previous[name]; owned {
			continue
		}

		if tool, ok := project.ToolFiles(old)[name]; ok {
			previous[name] = tool
		}
	}

	for _, name := range []string{"pluto.toml", "go.mod", "go.sum"} {
		path, err := project.SafePath(root, name)
		if err != nil {
			return err
		}

		previous[name], err = os.ReadFile(path)
		if err != nil {
			return err
		}
	}

	files["pluto.toml"], err = project.Encode(manifest)
	if err != nil {
		return err
	}

	files["go.mod"], err = project.ConfigureBoundaryModules(
		manifest.Boundary,
		len(manifest.Agents) > 0,
		previous["go.mod"],
	)
	if err != nil {
		return err
	}

	if len(manifest.Agents) > 0 {
		files["go.sum"], err = blueprintChecksums(previous["go.sum"])
		if err != nil {
			return err
		}
	}

	paths, err := project.Apply(root, files, previous, dry)
	if err != nil {
		return err
	}

	for _, path := range paths {
		cmd.Println(path)
	}

	if !dry {
		cmd.Println(
			"Blueprint contracts created. Run go mod tidy, pluto fmt and " +
				"pluto vet; apply declared migrations explicitly.",
		)
	}

	return nil
}

func blueprintChecksums(current []byte) ([]byte, error) {
	existing := map[string]string{}

	for _, line := range strings.Split(string(current), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 {
			key := fields[0] + " " + fields[1]
			if prior, ok := existing[key]; ok && prior != fields[2] {
				return nil, fmt.Errorf(
					"conflicting existing checksums for %s",
					key,
				)
			}

			existing[key] = fields[2]
		}
	}

	output := string(current)

	for _, line := range strings.Split(
		strings.TrimSpace(
			project.AgentGoSum,
		),
		"\n",
	) {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("invalid embedded agent checksum metadata")
		}

		key := fields[0] + " " + fields[1]
		if prior, ok := existing[key]; ok {
			if prior != fields[2] {
				return nil, fmt.Errorf(
					"existing checksum for %s conflicts with the pinned "+
						"agent dependency",
					key,
				)
			}

			continue
		}

		if output != "" && !strings.HasSuffix(output, "\n") {
			output += "\n"
		}

		output += line + "\n"
		existing[key] = fields[2]
	}

	return []byte(output), nil
}
