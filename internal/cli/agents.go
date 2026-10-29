package cli

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

func addAgentCommand(options Options) *cobra.Command {
	var (
		permission string
		provider   string
		dry        bool
	)

	command := &cobra.Command{
		Use:   "agent GROUP ACTION",
		Short: "Add an opt-in typed read-only agent and tool skeleton",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, m, err := project.Discover(options.Directory)
			if err != nil {
				return err
			}

			if err := project.Contracts(root, m); err != nil {
				return err
			}

			old := m

			a := project.Agent{
				Group:      args[0],
				Action:     args[1],
				Permission: permission,
				Provider:   provider,
			}
			m.SchemaVersion = project.SchemaVersion

			m.Agents = append(append([]project.Agent(nil), m.Agents...), a)
			if err := m.Validate(); err != nil {
				return err
			}

			files, err := project.AgentFiles(m, a)
			if err != nil {
				return err
			}

			managed, err := project.Managed(m)
			if err != nil {
				return err
			}

			for path, source := range managed {
				files[path] = source
			}

			previous, err := project.Managed(old)
			if err != nil {
				return err
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

			files["pluto.toml"], err = project.Encode(m)
			if err != nil {
				return err
			}

			module := string(previous["go.mod"])
			pin := regexp.MustCompile(
				`(?m)^\s*(?:require\s+)?github\.com/plutoframework/pluto-agent\s+(\S+)`,
			)

			match := pin.FindStringSubmatch(module)
			if len(match) > 0 && match[1] != project.AgentVersion {
				return fmt.Errorf(
					"existing agent version %s requires explicit upgrade to %s",
					match[1],
					project.AgentVersion,
				)
			}

			if len(match) == 0 {
				module += "\nrequire github.com/plutoframework/pluto-agent " + project.AgentVersion + "\n"
			}

			files["go.mod"] = []byte(module)

			sums := string(previous["go.sum"])
			for _, line := range strings.Split(strings.TrimSpace(project.AgentGoSum), "\n") {
				if !strings.Contains(sums, line+"\n") {
					sums += line + "\n"
				}
			}

			files["go.sum"] = []byte(sums)

			paths, err := project.Apply(root, files, previous, dry)
			if err != nil {
				return err
			}

			for _, path := range paths {
				cmd.Println(path)
			}

			if !dry {
				cmd.Println(
					"Agent added. Run go mod tidy; inject verified dependencies and resource operations before enabling a route.",
				)
			}

			return nil
		},
	}
	command.Flags().
		StringVar(&permission, "permission", "", "Required explicit RBAC capability")
	command.Flags().
		StringVar(&provider, "provider", "scripted", "Provider skeleton: scripted or openai (runtime opt-in required)")
	command.Flags().BoolVar(&dry, "dry-run", false, "Preflight without writing")

	return command
}
