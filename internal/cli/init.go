package cli

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

func initCommand(options Options) *cobra.Command {
	var (
		name, module, prefix                       string
		localCredentials                           bool
		cacheBackend                               string
		multi, noMise, noFnox, noDocker, hook, dry bool
	)

	command := &cobra.Command{
		Use:     "init [directory]",
		Short:   "Create a runnable API without overwriting existing files",
		Long:    "Create a runnable API with Docker, mise and fnox development files by default.\nUse --no-docker, --no-mise or --no-fnox to omit each preset independently.",
		Example: "  pluto init my-api --module example.com/my-api\n  pluto init . --name my-api --env-prefix MY_API_ --multi-tenant\n  pluto init minimal-api --no-docker --no-mise --no-fnox",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := options.Directory
			if len(args) > 0 {
				target = args[0]
				if !filepath.IsAbs(target) {
					target = filepath.Join(options.Directory, target)
				}
			}

			target, err := filepath.Abs(target)
			if err != nil {
				return err
			}

			if name == "" {
				name = filepath.Base(target)
			}

			if module == "" {
				module = "example.com/" + name
			}

			if prefix == "" {
				prefix = strings.ToUpper(
					strings.ReplaceAll(name, "-", "_"),
				) + "_"
			}

			tenancy := "single"
			if multi {
				tenancy = "multi"
			}

			m := project.Manifest{
				SchemaVersion: project.SchemaVersion,
				Project: project.Project{
					Name:      name,
					Module:    module,
					EnvPrefix: prefix,
				},
				Capabilities: project.Capabilities{
					Cache:            cacheBackend,
					Tenancy:          tenancy,
					LocalCredentials: localCredentials,
				},
				Database: project.Database{
					Engine:  "postgres",
					Queries: "sqlc",
				},
				Development: project.Development{
					Mise:   !noMise,
					Fnox:   !noFnox,
					Docker: !noDocker,
				},
			}
			if err := m.Validate(); err != nil {
				return err
			}

			if _, err := exec.LookPath("git"); err != nil {
				return fmt.Errorf("git is required: %w", err)
			}

			if _, err := exec.LookPath("go"); err != nil {
				return fmt.Errorf("go 1.25.5+ is required: %w", err)
			}

			files, err := project.InitialFiles(m)
			if err != nil {
				return err
			}

			if hook {
				if err := project.PreflightHook(
					cmd.Context(),
					target,
				); err != nil {
					return err
				}
			}

			names, err := project.Apply(target, files, nil, dry)
			if err != nil {
				return err
			}

			if dry {
				for _, path := range names {
					cmd.Println(path)
				}

				return nil
			}

			if err := project.GitInit(cmd.Context(), target); err != nil {
				return err
			}

			if err := project.Run(
				cmd.Context(),
				target,
				cmd.OutOrStdout(),
				cmd.ErrOrStderr(),
				"go",
				"run",
				"./cmd/openapi",
			); err != nil {
				return fmt.Errorf(
					"project files written; OpenAPI generation failed: %w; run pluto openapi after repairing the environment",
					err,
				)
			}

			if hook {
				if err := project.InstallHook(
					cmd.Context(),
					target,
				); err != nil {
					return err
				}
			}

			cmd.Printf(
				"Created %s in %s (%s tenancy).\n",
				name,
				target,
				tenancy,
			)
			cmd.Println(
				"Next: go run ./cmd/openapi; pluto fmt; pluto vet; go run ./cmd/api",
			)

			return nil
		},
	}
	flags := command.Flags()
	flags.StringVar(
		&cacheBackend,
		"cache",
		"none",
		"Data cache: none, memory or valkey (saved in TOML)",
	)
	flags.StringVar(
		&name,
		"name",
		"",
		"Lowercase application name (default directory name)",
	)
	flags.StringVar(
		&module,
		"module",
		"",
		"Go module path (default example.com/<name>, provisional)",
	)
	flags.StringVar(
		&prefix,
		"env-prefix",
		"",
		"Uppercase environment prefix ending in underscore",
	)
	flags.BoolVar(
		&localCredentials,
		"local-credentials",
		false,
		"Scaffold password lifecycle with required delivery/limit/ingress seams (saved in TOML)",
	)
	flags.BoolVar(
		&multi,
		"multi-tenant",
		false,
		"Require verified tenant context (saved in pluto.toml)",
	)
	flags.BoolVar(
		&noMise,
		"no-mise",
		false,
		"Omit mise tool pins and developer tasks",
	)
	flags.BoolVar(
		&noFnox,
		"no-fnox",
		false,
		"Omit fnox configuration and mise integration",
	)
	flags.BoolVar(
		&noDocker,
		"no-docker",
		false,
		"Omit Dockerfile, Compose and Docker ignore files",
	)
	flags.BoolVar(
		&hook,
		"hook",
		false,
		"Install pre-commit staged-policy vet hook (opt-in)",
	)
	flags.BoolVar(
		&dry,
		"dry-run",
		false,
		"Preflight and list files without writing or initializing Git",
	)

	return command
}
