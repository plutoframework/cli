package cli

import (
	"fmt"
	"path/filepath"

	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

// splitFlags opt a frontend into its own repository. Without --api or
// --openapi, pluto frontend init keeps the frontend inside the application.
type splitFlags struct {
	api, openapi, name, envPrefix, devTarget string
	localCredentials, oidc                   bool
}

func (s *splitFlags) register(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(
		&s.api,
		"api",
		"",
		"Separate repository: path to the Go application checkout; fills "+
			"the settings below",
	)
	flags.StringVar(
		&s.openapi,
		"openapi",
		"",
		"Separate repository: openapi.json path or HTTPS URL",
	)
	flags.StringVar(&s.name, "name", "", "Separate repository: project name")
	flags.StringVar(
		&s.envPrefix,
		"env-prefix",
		"",
		"Separate repository: backend environment prefix, e.g. MY_API_",
	)
	flags.BoolVar(
		&s.localCredentials,
		"local-credentials",
		false,
		"Separate repository: backend supports local credentials",
	)
	flags.BoolVar(
		&s.oidc,
		"oidc",
		false,
		"Separate repository: backend supports OIDC sign-in",
	)
	flags.StringVar(
		&s.devTarget,
		"dev-target",
		"",
		"Separate repository: Vite proxy target, e.g. http://127.0.0.1:8080",
	)
}

func (s *splitFlags) requested() bool {
	return s.api != "" || s.openapi != ""
}

// resolve builds the frontend-only manifest settings. --api reads the backend
// checkout once; explicit flags override what it provides.
func (s *splitFlags) resolve(
	cmd *cobra.Command,
	root string,
) (string, project.FrontendAPI, error) {
	var (
		api  project.FrontendAPI
		name string
	)

	if s.api != "" {
		start := s.api
		if !filepath.IsAbs(start) {
			start = filepath.Join(root, start)
		}

		backend, m, err := project.Discover(start)
		if err != nil {
			return "", api, fmt.Errorf("--api: %w", err)
		}

		api = project.BackendSettings(m)
		name = m.Project.Name

		contract, err := filepath.Rel(
			root,
			filepath.Join(backend, "openapi", "openapi.json"),
		)
		if err != nil {
			return "", api, err
		}

		api.OpenAPI = filepath.ToSlash(contract)
	}

	changed := cmd.Flags().Changed

	if s.openapi != "" {
		api.OpenAPI = s.openapi
	}

	if changed("name") {
		name = s.name
	}

	if changed("env-prefix") {
		api.EnvPrefix = s.envPrefix
	}

	if changed("local-credentials") {
		api.LocalCredentials = s.localCredentials
	}

	if changed("oidc") {
		api.OIDC = s.oidc
	}

	api.DevTarget = s.devTarget

	if name == "" {
		return "", api, fmt.Errorf(
			"--name is required without --api",
		)
	}

	if api.EnvPrefix == "" {
		return "", api, fmt.Errorf(
			"--env-prefix is required without --api",
		)
	}

	return name, api, nil
}
