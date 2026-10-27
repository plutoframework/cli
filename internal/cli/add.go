package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

func addCommand(options Options) *cobra.Command {
	add := &cobra.Command{
		Use:   "add",
		Short: "Add application code using saved project choices",
	}

	var (
		endpoint project.Endpoint
		dry      bool
	)

	command := &cobra.Command{
		Use:     "endpoint GROUP ACTION",
		Short:   "Add an endpoint, models, operation, HTTP adapter and tests",
		Example: "  pluto add endpoint widgets Create --method POST --path /api/widgets --name 'Create widget' --description 'Create an authorized widget.'",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, m, err := project.Discover(options.Directory)
			if err != nil {
				return err
			}

			if err := project.Contracts(root, m); err != nil {
				return err
			}

			if _, err := project.SafePath(
				root,
				"openapi/openapi.json",
			); err != nil {
				return err
			}

			old := m
			endpoint.Group, endpoint.Action = args[0], args[1]

			m.Endpoints = append(
				append([]project.Endpoint(nil), m.Endpoints...),
				endpoint,
			)
			if err := m.Validate(); err != nil {
				return err
			}

			files, err := project.Managed(m)
			if err != nil {
				return err
			}

			authored, err := project.EndpointFiles(m, endpoint)
			if err != nil {
				return err
			}

			for path, data := range authored {
				files[path] = data
			}

			previous, err := project.Managed(old)
			if err != nil {
				return err
			}

			previous["pluto.toml"], err = os.ReadFile(
				filepath.Join(root, "pluto.toml"),
			)
			if err != nil {
				return err
			}

			files["pluto.toml"], err = project.Encode(m)
			if err != nil {
				return err
			}

			paths, err := project.Apply(root, files, previous, dry)
			if err != nil {
				return err
			}

			if dry {
				for _, path := range paths {
					cmd.Println(path)
				}

				return nil
			}

			if err := project.Run(
				cmd.Context(),
				root,
				cmd.OutOrStdout(),
				cmd.ErrOrStderr(),
				"go",
				"run",
				"./cmd/openapi",
			); err != nil {
				return fmt.Errorf(
					"endpoint written; OpenAPI regeneration failed: %w; repair and run pluto openapi",
					err,
				)
			}

			cmd.Printf(
				"Added %s.%s; operation intentionally returns not implemented. Run pluto fmt and pluto vet.\n",
				endpoint.Group,
				endpoint.Action,
			)

			return nil
		},
	}
	flags := command.Flags()
	flags.StringVar(
		&endpoint.Method,
		"method",
		"POST",
		"HTTP method (uppercase)",
	)
	flags.StringVar(
		&endpoint.Path,
		"path",
		"",
		"Absolute Chi route; parameters use {id}",
	)
	flags.StringVar(
		&endpoint.Name,
		"name",
		"",
		"Human-readable endpoint name (required)",
	)
	flags.StringVar(
		&endpoint.Description,
		"description",
		"",
		"Human-readable description (required)",
	)
	flags.BoolVar(
		&endpoint.Public,
		"public",
		false,
		"Explicitly allow unauthenticated calls (single tenancy only)",
	)
	flags.StringVar(
		&endpoint.Permission,
		"permission",
		"",
		"Independent RBAC capability checked before operation",
	)
	flags.StringVar(
		&endpoint.Feature,
		"feature",
		"",
		"Declare a default-false feature gate",
	)
	flags.BoolVar(
		&endpoint.ClientVisible,
		"feature-client-visible",
		false,
		"Expose this flag via authenticated /api/features",
	)
	flags.BoolVar(
		&dry,
		"dry-run",
		false,
		"Preflight and list files without writing",
	)
	add.AddCommand(command)

	return add
}
