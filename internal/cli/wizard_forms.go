package cli

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

func textField(id, label, help, value string) wizardField {
	return wizardField{id: id, label: label, help: help, value: value}
}

func choiceField(
	id, label, help, value string,
	choices ...wizardChoice,
) wizardField {
	return wizardField{
		id:      id,
		label:   label,
		help:    help,
		value:   value,
		choices: choices,
	}
}

func boolField(id, label, help, value string) wizardField {
	return choiceField(
		id,
		label,
		help,
		value,
		wizardChoice{"No", "false"},
		wizardChoice{"Yes", "true"},
	)
}

func seed(cmd *cobra.Command, name, fallback string) string {
	if flag := cmd.Flags().Lookup(name); flag != nil && flag.Changed {
		return flag.Value.String()
	}

	return fallback
}

func argAt(args []string, index int, fallback string) string {
	if len(args) > index {
		return args[index]
	}

	return fallback
}

func wizardForm(
	kind string,
	options Options,
	cmd *cobra.Command,
	args []string,
) (wizardModel, error) {
	model := wizardModel{kind: kind, width: 80, height: 24}
	if kind == "menu" || kind == "add" {
		return menuForm(kind, options), nil
	}

	if kind == "init" {
		return initForm(model, options, cmd, args), nil
	}

	_, manifest, err := project.Discover(options.Directory)
	if err != nil {
		return model, err
	}

	model.context = fmt.Sprintf(
		"%s / %s tenancy / cache: %s",
		manifest.Project.Name,
		manifest.Capabilities.Tenancy,
		manifest.Capabilities.Cache,
	)

	switch kind {
	case "endpoint":
		model = endpointForm(model, manifest, cmd, args)
	case "job":
		model = jobForm(model, manifest, cmd, args)
	default:
		model = maintenanceForm(model, cmd)
	}

	model.focus(0)

	return model, nil
}

func menuForm(kind string, options Options) wizardModel {
	choices := []wizardChoice{
		{"Initialize an API application", "init"},
		{"Add an API endpoint", "endpoint"},
		{"Add a background job", "job"},
		{"Format this project", "fmt"},
		{"Run project checks", "vet"},
		{"Generate OpenAPI", "openapi"},
		{"Install the optional Git hook", "hook"},
	}
	value := "init"
	context := "Opinionated Go APIs, ready for humans and agents."

	if _, m, err := project.Discover(options.Directory); err == nil {
		value = "endpoint"
		context = m.Project.Name + " / saved project settings will be reused"
	}

	if kind == "add" {
		choices = choices[1:3]
		value = "endpoint"
	}

	model := wizardModel{
		kind:    kind,
		title:   "What would you like to build?",
		context: context,
		width:   80,
		height:  24,
		menu:    true,
		fields: []wizardField{
			choiceField(
				"action",
				"Choose a command",
				"The next screen walks through all available options.",
				value,
				choices...),
		},
		build: func(v map[string]string) ([]string, error) { return []string{v["action"]}, nil },
	}
	model.focus(0)

	return model
}

func initForm(
	model wizardModel,
	options Options,
	cmd *cobra.Command,
	args []string,
) wizardModel {
	model.title = "Initialize your API"
	model.context = "Postgres + sqlc / review all settings before files are written"
	model.fields = []wizardField{
		textField(
			"directory",
			"Directory",
			"Use a new directory, or . for the current directory.",
			argAt(args, 0, "."),
		),
		textField(
			"name",
			"Application name",
			"Lowercase kebab-case, for example mission-control.",
			seed(cmd, "name", ""),
		),
		textField(
			"module",
			"Go module",
			"Use your real module path, for example github.com/you/mission-control.",
			seed(cmd, "module", ""),
		),
		textField(
			"env-prefix",
			"Environment prefix",
			"Uppercase, ending with one underscore; isolates this app's settings.",
			seed(cmd, "env-prefix", ""),
		),
		boolField(
			"multi-tenant",
			"Multi-tenant application",
			"Requires verified tenant context on business routes.",
			seed(cmd, "multi-tenant", "false"),
		),
		boolField(
			"local-credentials",
			"Local password authentication",
			"Enrollment, verification and recovery; you supply delivery, limits and ingress.",
			seed(cmd, "local-credentials", "false"),
		),
		choiceField(
			"cache",
			"Data cache",
			"Optional data optimization; authentication and permissions remain live.",
			seed(cmd, "cache", "none"),
			wizardChoice{"None", "none"},
			wizardChoice{"Bounded in-process memory", "memory"},
			wizardChoice{
				"Valkey / Redis (adds optional client and development service)",
				"valkey",
			},
		),
		choiceField(
			"no-docker",
			"Docker development stack",
			"Include a Dockerfile and a full local Compose stack.",
			seed(
				cmd,
				"no-docker",
				"false",
			),
			wizardChoice{"Include", "false"},
			wizardChoice{"Omit", "true"},
		),
		choiceField(
			"no-mise",
			"mise tools and tasks",
			"Include pinned development tools and tasks.",
			seed(
				cmd,
				"no-mise",
				"false",
			),
			wizardChoice{"Include", "false"},
			wizardChoice{"Omit", "true"},
		),
		choiceField(
			"no-fnox",
			"fnox secret management",
			"Include secret-management scaffolding; fnox uses mise.",
			seed(
				cmd,
				"no-fnox",
				"false",
			),
			wizardChoice{"Include", "false"},
			wizardChoice{"Omit", "true"},
		),
		boolField(
			"hook",
			"Install Git hook",
			"Optional staged-check hook; existing hooks are never replaced.",
			seed(cmd, "hook", "false"),
		),
		boolField(
			"dry-run",
			"Preview files only",
			"Validate and list files without writing or initializing Git.",
			seed(cmd, "dry-run", "false"),
		),
	}
	model.fields[1].fallback = func(v map[string]string) string {
		target := v["directory"]
		if !filepath.IsAbs(target) {
			target = filepath.Join(options.Directory, target)
		}

		target, _ = filepath.Abs(target)

		return filepath.Base(target)
	}
	model.fields[2].fallback = func(v map[string]string) string { return "example.com/" + v["name"] }
	model.fields[3].fallback = func(v map[string]string) string {
		return strings.ToUpper(strings.ReplaceAll(v["name"], "-", "_")) + "_"
	}
	model.build = func(v map[string]string) ([]string, error) {
		tenancy := "single"
		if v["multi-tenant"] == "true" {
			tenancy = "multi"
		}

		m := project.Manifest{
			SchemaVersion: project.SchemaVersion,
			Project: project.Project{
				Name:      v["name"],
				Module:    v["module"],
				EnvPrefix: v["env-prefix"],
			},
			Capabilities: project.Capabilities{
				Tenancy:          tenancy,
				Cache:            v["cache"],
				LocalCredentials: v["local-credentials"] == "true",
			},
			Database: project.Database{
				Engine:  "postgres",
				Queries: "sqlc",
			},
		}
		if err := m.Validate(); err != nil {
			return nil, err
		}

		result := []string{"init"}
		for _, f := range model.fields[1:] {
			result = append(result, "--"+f.id+"="+v[f.id])
		}

		return append(result, "--", v["directory"]), nil
	}
	model.focus(0)

	return model
}

func endpointForm(
	model wizardModel,
	m project.Manifest,
	cmd *cobra.Command,
	args []string,
) wizardModel {
	model.title = "Add an API endpoint"
	model.fields = []wizardField{
		textField(
			"group",
			"Handler group",
			"Lowercase Go package name, for example purchases.",
			argAt(args, 0, ""),
		),
		textField(
			"action",
			"Operation name",
			"Exported Go name, for example Create, List or Get.",
			argAt(args, 1, "Create"),
		),
		choiceField(
			"method",
			"HTTP method",
			"Choose the transport verb.",
			seed(cmd, "method", "POST"),
			wizardChoice{"POST", "POST"},
			wizardChoice{"GET", "GET"},
			wizardChoice{"PUT", "PUT"},
			wizardChoice{"PATCH", "PATCH"},
			wizardChoice{"DELETE", "DELETE"},
			wizardChoice{"HEAD", "HEAD"},
			wizardChoice{"OPTIONS", "OPTIONS"},
		),
		textField(
			"path",
			"Route path",
			"Absolute Chi route; parameters use {id}.",
			seed(cmd, "path", ""),
		),
		textField(
			"name",
			"Display name",
			"Human-readable endpoint name, also used by OpenAPI.",
			seed(cmd, "name", ""),
		),
		textField(
			"description",
			"Description",
			"Describe the endpoint's purpose and behavior.",
			seed(cmd, "description", ""),
		),
		boolField(
			"public",
			"Public endpoint",
			"Default No requires authentication. Multi-tenant business routes stay authenticated.",
			seed(cmd, "public", "false"),
		),
		textField(
			"permission",
			"RBAC permission",
			"Optional capability, for example purchases:create; never grants roles automatically.",
			seed(cmd, "permission", ""),
		),
		textField(
			"feature",
			"Feature flag",
			"Optional default-false gate, for example purchases.export; blank means no flag.",
			seed(cmd, "feature", ""),
		),
		boolField(
			"feature-client-visible",
			"Expose feature state to clients",
			"Only meaningful with a feature flag; uses the authenticated feature API.",
			seed(cmd, "feature-client-visible", "false"),
		),
		choiceField(
			"collection",
			"Paginated collection",
			"Auto enables pagination for GET List; choose explicitly for other names.",
			seed(cmd, "collection", "auto"),
			wizardChoice{"Auto (GET List)", "auto"},
			wizardChoice{"Yes", "true"},
			wizardChoice{"No", "false"},
		),
		choiceField(
			"idempotency",
			"Idempotency policy",
			"Auto requires keys on authenticated business writes; exempt only deliberately.",
			seed(cmd, "idempotency", "auto"),
			wizardChoice{"Auto", "auto"},
			wizardChoice{"Required", "required"},
			wizardChoice{"Exempt", "exempt"},
		),
		boolField(
			"dry-run",
			"Preview files only",
			"Validate and list proposed files without writing.",
			seed(cmd, "dry-run", "false"),
		),
	}
	model.fields[3].fallback = func(v map[string]string) string { return "/api/" + v["group"] }
	model.fields[4].fallback = func(v map[string]string) string { return v["action"] + " " + v["group"] }
	model.build = func(v map[string]string) ([]string, error) {
		collection := v["collection"] == "true" ||
			(v["collection"] == "auto" && v["method"] == "GET" && v["action"] == "List")
		endpoint := project.Endpoint{
			Group:         v["group"],
			Action:        v["action"],
			Method:        v["method"],
			Path:          v["path"],
			Name:          v["name"],
			Description:   v["description"],
			Public:        v["public"] == "true",
			Permission:    v["permission"],
			Feature:       v["feature"],
			ClientVisible: v["feature-client-visible"] == "true",
			Collection:    collection,
			Idempotency:   v["idempotency"],
		}
		candidate := m

		candidate.Endpoints = append(
			append([]project.Endpoint(nil), m.Endpoints...),
			endpoint,
		)
		if err := candidate.Validate(); err != nil {
			return nil, err
		}

		result := []string{"add", "endpoint", endpoint.Group, endpoint.Action}

		for _, f := range model.fields[2:] {
			value := v[f.id]
			if f.id == "collection" {
				value = strconv.FormatBool(collection)
			}

			result = append(result, "--"+f.id+"="+value)
		}

		return result, nil
	}

	return model
}

func jobForm(
	model wizardModel,
	m project.Manifest,
	cmd *cobra.Command,
	args []string,
) wizardModel {
	model.title = "Add a background job"
	model.fields = []wizardField{
		textField(
			"group",
			"Job group",
			"Lowercase Go package name, for example invoices.",
			argAt(args, 0, ""),
		),
		textField(
			"action",
			"Handler name",
			"Exported Go name, for example Send.",
			argAt(args, 1, "Send"),
		),
		textField(
			"kind",
			"Delivery kind",
			"Stable lowercase identifier, for example invoice.send.",
			seed(cmd, "kind", ""),
		),
		boolField(
			"dry-run",
			"Preview files only",
			"Validate and list proposed files without writing.",
			seed(cmd, "dry-run", "false"),
		),
	}
	model.fields[2].fallback = func(v map[string]string) string { return v["group"] + "." + strings.ToLower(v["action"]) }
	model.build = func(v map[string]string) ([]string, error) {
		candidate := m

		candidate.Jobs = append(
			append([]project.Job(nil), m.Jobs...),
			project.Job{
				Group:  v["group"],
				Action: v["action"],
				Kind:   v["kind"],
			},
		)
		if err := candidate.Validate(); err != nil {
			return nil, err
		}

		return []string{
			"add",
			"job",
			v["group"],
			v["action"],
			"--kind=" + v["kind"],
			"--dry-run=" + v["dry-run"],
		}, nil
	}

	return model
}

func maintenanceForm(model wizardModel, cmd *cobra.Command) wizardModel {
	model.title = "Review project command"
	flag := ""

	switch model.kind {
	case "vet":
		flag = "staged"
		model.fields = []wizardField{
			boolField(
				flag,
				"Require fully staged project",
				"Checks the exact staged working tree before vet.",
				seed(cmd, flag, "false"),
			),
		}
	case "openapi":
		flag = "check"
		model.fields = []wizardField{
			boolField(
				flag,
				"Check freshness only",
				"Yes validates without writing; No regenerates OpenAPI.",
				seed(cmd, flag, "false"),
			),
		}
	default:
		label := "Apply canonical Go formatting"
		if model.kind == "hook" {
			label = "Install optional staged-check hook"
		}

		model.fields = []wizardField{
			choiceField(
				"action",
				"Action",
				label,
				model.kind,
				wizardChoice{label, model.kind},
			),
		}
	}

	model.build = func(v map[string]string) ([]string, error) {
		if model.kind == "hook" {
			return []string{"hook", "install"}, nil
		}

		result := []string{model.kind}
		if flag != "" {
			result = append(result, "--"+flag+"="+v[flag])
		}

		return result, nil
	}

	return model
}
