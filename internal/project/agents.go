package project

import (
	"fmt"
	"strings"
)

const AgentVersion = "v0.1.3"

type Agent struct {
	Approval   string `toml:"approval,omitempty"`
	Operation  string `toml:"operation"`
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

		key := a.Group + "." + a.Action
		if seen[key] {
			return fmt.Errorf("duplicate agent %s", key)
		}

		seen[key] = true
	}

	return nil
}

func agentData(m Manifest, a Agent) (templateData, error) {
	operation, ok := m.Operation(a.Operation)
	if !ok {
		return templateData{}, fmt.Errorf(
			"agent references undeclared operation %s",
			a.Operation,
		)
	}

	data := dataFor(
		m,
		Endpoint{
			Group:      a.Group,
			Action:     a.Action,
			Permission: operation.Permission,
		},
	)
	data.OperationGroup = operation.Group
	data.OperationAction = operation.Action
	data.AgentProvider = a.Provider

	return data, nil
}

func AgentFiles(m Manifest, a Agent) (map[string][]byte, error) {
	if m.Boundary.Mode == "candidate" {
		return map[string][]byte{}, nil
	}

	data, err := agentData(m, a)
	if err != nil {
		return nil, err
	}

	files := map[string][]byte{}

	for _, item := range []struct {
		path, template string
	}{
		{
			"internal/agents/" + a.Group + "/" + strings.ToLower(
				a.Action,
			) + "_test.go",
			"agent_test.go",
		},
		{
			"internal/app/" + a.Group + "_" + strings.ToLower(
				a.Action,
			) + "_provider.go",
			"agent_provider.go",
		},
	} {
		source, err := render(item.template, data)
		if err != nil {
			return nil, err
		}

		files[item.path] = source
	}

	return files, nil
}

func managedAgents(m Manifest) (map[string][]byte, error) {
	files := map[string][]byte{}

	for _, a := range m.Agents {
		data, err := agentData(m, a)
		if err != nil {
			return nil, err
		}

		source, err := render("agent.go", data)
		if err != nil {
			return nil, err
		}

		files["internal/agents/"+a.Group+"/"+strings.ToLower(
			a.Action,
		)+".go"] = source
	}

	return files, nil
}

// AgentGoSum is verified against the public checksum database, not a replace.
const AgentGoSum = `github.com/plutoframework/pluto-agent v0.1.3 h1:w+a5Zm2K+I6/+JCH7DvaHltmZleYgSpzlBSJJHZjky0=
github.com/plutoframework/pluto-agent v0.1.3/go.mod h1:kgb6jFXGwRU7Sr6SwIp9fQYUxdm08vgGDfzgGTbggB0=
`
