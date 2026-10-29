package project

import (
	"fmt"
	"strings"
)

const AgentVersion = "v0.1.3"

type Agent struct {
	Group      string `toml:"group"`
	Action     string `toml:"action"`
	Permission string `toml:"permission"`
	Provider   string `toml:"provider"`
}

func ValidateAgents(agents []Agent) error {
	seen := map[string]bool{}

	for _, a := range agents {
		if a.Provider != "scripted" && a.Provider != "openai" {
			return fmt.Errorf("agent provider must be scripted or openai")
		}

		e := Endpoint{
			Group: a.Group, Action: a.Action,
			Name: "Agent", Description: "Typed read-only agent.",
			Method: "GET", Path: "/agent", Permission: a.Permission,
		}
		if err := e.Validate(false); err != nil {
			return err
		}

		if a.Permission == "" {
			return fmt.Errorf("agent requires explicit permission")
		}

		key := a.Group + "." + a.Action
		if seen[key] {
			return fmt.Errorf("duplicate agent %s", key)
		}

		seen[key] = true
	}

	return nil
}

func AgentFiles(m Manifest, a Agent) (map[string][]byte, error) {
	files := map[string][]byte{}

	data := dataFor(m, Endpoint{
		Group: a.Group, Action: a.Action,
		Permission: a.Permission,
	})

	data.AgentProvider = a.Provider
	for _, name := range []string{"agent.go", "agent_test.go"} {
		source, err := render(name, data)
		if err != nil {
			return nil, err
		}

		suffix := ".go"
		if name == "agent_test.go" {
			suffix = "_test.go"
		}

		files["internal/agents/"+a.Group+"/"+
			strings.ToLower(a.Action)+suffix] = source
	}

	return files, nil
}

// AgentGoSum is verified against the public checksum database, not a replace.
const AgentGoSum = `github.com/plutoframework/pluto-agent v0.1.3 h1:w+a5Zm2K+I6/+JCH7DvaHltmZleYgSpzlBSJJHZjky0=
github.com/plutoframework/pluto-agent v0.1.3/go.mod h1:kgb6jFXGwRU7Sr6SwIp9fQYUxdm08vgGDfzgGTbggB0=
`
