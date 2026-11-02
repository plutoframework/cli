package project

import (
	"fmt"
	"path/filepath"
	"strings"
)

// CandidateManagedPaths is derived from the exact same render plan used for
// integrity checking. Existing files never become trusted merely by existing.
func CandidateManagedPaths(m Manifest) (map[string]bool, error) {
	files, err := candidateManaged(m)
	if err != nil {
		return nil, err
	}

	paths := map[string]bool{}
	for path := range files {
		paths[filepath.ToSlash(path)] = true
	}

	return paths, nil
}

func candidateManaged(m Manifest) (map[string][]byte, error) {
	files, err := managedPublished(m)
	if err != nil {
		return nil, err
	}

	foundation, err := candidateFoundation(m)
	if err != nil {
		return nil, err
	}

	for path, source := range foundation {
		files[path] = source
	}

	resources := map[string]ResourceContract{}
	for _, resource := range m.Resources {
		resources[resource.ID()] = resource
		data := dataFor(m, Endpoint{})

		data.Resource = resource
		for _, contract := range m.OperationContracts {
			if contract.Resource == resource.ID() {
				data.ContractOperations = append(
					data.ContractOperations,
					contract,
				)
			}
		}

		for _, effect := range m.Effects {
			for _, contract := range data.ContractOperations {
				if effect.Operation == contract.ID {
					data.ContractEffects = append(data.ContractEffects, effect)
				}
			}
		}

		for _, item := range []struct {
			path, name string
		}{
			{
				"internal/domain/" + resource.Group + "/" + strings.ToLower(
					resource.Name,
				) + "_contract_gen.go",
				"candidate_domain.go",
			},
			{
				"internal/ports/" + resource.Group + "/" + strings.ToLower(
					resource.Name,
				) + "_ports_gen.go",
				"candidate_ports.go",
			},
			{
				"internal/storage/" + resource.Group + "/" + strings.ToLower(
					resource.Name,
				) + "_data_gen.go",
				"candidate_data.go",
			},
			{
				"internal/storage/" + resource.Group + "/" + strings.ToLower(
					resource.Name,
				) + "_reads_gen.go",
				"candidate_reads.go",
			},
			{
				"db/migrations/100_" + resource.Group + "_" + strings.ToLower(
					resource.Name,
				) + ".sql",
				"candidate_schema.sql",
			},
		} {
			source, err := render(item.name, data)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", item.path, err)
			}

			files[item.path] = source
		}
	}

	contracts := map[string]OperationContract{}
	for _, contract := range m.OperationContracts {
		contracts[contract.ID] = contract
	}

	for _, operation := range m.Operations {
		contract := contracts[operation.ID()]
		data := dataFor(m, operation.Endpoint())
		data.Resource = resources[contract.Resource]
		data.Contract = contract

		data.ContractEffects = m.Effects
		for _, item := range []struct {
			path, name string
		}{
			{
				"internal/models/" + operation.Group + "/" + strings.ToLower(
					operation.Action,
				) + ".go",
				"candidate_models.go",
			},
			{
				"internal/operations/" +
					operation.Group +
					"/" +
					strings.ToLower(

						operation.Action,
					) + "_gate_gen.go",

				"candidate_gate.go",
			},
			{
				"internal/operations/" +
					operation.Group +
					"/" +
					strings.ToLower(

						operation.Action,
					) + "_workflow_gen.go",

				"candidate_workflow.go",
			},
		} {
			source, err := render(item.name, data)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", item.path, err)
			}

			files[item.path] = source
		}

		if contract.Kind == "effect" {
			source, err := render("candidate_effect.go", data)
			if err != nil {
				return nil, err
			}

			path := "internal/infrastructure/" + data.Resource.Group + "/" +
				operation.Group + "_" + strings.ToLower(operation.Action) +
				"_delivery_gen.go"
			files[path] = source
		}
	}

	for _, entry := range m.Endpoints {
		view, err := m.OperationEndpoint(entry)
		if err != nil {
			return nil, err
		}

		data := dataFor(m, view)
		data.Contract = contracts[entry.Operation]
		data.Resource = resources[data.Contract.Resource]

		source, err := render("candidate_handler.go", data)
		if err != nil {
			return nil, err
		}

		files["internal/handlers/"+entry.Group+"/"+strings.ToLower(
			entry.Action,
		)+".go"] = source
	}

	for _, job := range m.Jobs {
		data, err := jobData(m, job)
		if err != nil {
			return nil, err
		}

		data.Contract = contracts[job.Operation]
		data.Resource = resources[data.Contract.Resource]
		data.MaxAttempts = job.MaxAttempts

		source, err := render("candidate_job.go", data)
		if err != nil {
			return nil, err
		}

		files["internal/jobs/"+job.Group+"/"+strings.ToLower(
			job.Action,
		)+".go"] = source
	}

	for _, entry := range m.Agents {
		data, err := agentData(m, entry)
		if err != nil {
			return nil, err
		}

		data.Contract = contracts[entry.Operation]
		data.Resource = resources[data.Contract.Resource]

		source, err := render("candidate_agent.go", data)
		if err != nil {
			return nil, err
		}

		files["internal/agents/"+entry.Group+"/"+strings.ToLower(
			entry.Action,
		)+".go"] = source
	}

	caller, err := render("candidate_caller.go", dataFor(m, Endpoint{}))
	if err != nil {
		return nil, err
	}

	files["internal/access/caller_gen.go"] = caller

	runtime, err := candidateRuntime(m)
	if err != nil {
		return nil, err
	}

	files["internal/app/candidate_operations_gen.go"] = runtime
	files["internal/app/routes_gen.go"] = []byte("package app\n")
	files["internal/app/app.go"] = []byte("package app\n")
	files["internal/app/worker.go"] = []byte("package app\n")

	files["internal/app/worker_runtime_gen.go"] = []byte("package app\n")
	for path := range files {
		if strings.HasPrefix(path, "internal/background/") {
			files[path] = []byte("package background\n")
		}
	}

	roles, err := candidateRoleSource(m)
	if err != nil {
		return nil, err
	}

	files["internal/access/roles.go"] = roles

	memberships, err := render(
		"candidate_memberships.go",
		dataFor(m, Endpoint{}),
	)
	if err != nil {
		return nil, err
	}

	files["internal/storage/identity/memberships_gen.go"] = memberships
	files["db/migrations/099_memberships.sql"] = []byte(
		`CREATE TABLE "pluto_resource_memberships" (` +
			`"tenant_id" text NOT NULL,` +
			`"actor_id" text NOT NULL,` +
			`"roles" text[] NOT NULL,` +
			`PRIMARY KEY("tenant_id","actor_id"));`,
	)

	for _, item := range []struct {
		path, name string
	}{
		{
			"cmd/api/main.go",
			"candidate_main.go",
		},
		{
			"cmd/worker/main.go",
			"candidate_worker_main.go",
		},
	} {
		source, err := render(item.name, dataFor(m, Endpoint{}))
		if err != nil {
			return nil, err
		}

		files[item.path] = source
	}

	for path, source := range files {
		if strings.HasSuffix(path, ".go") {
			formatted, err := formatGenerated(source, m.Project.Module)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}

			files[path] = append(
				[]byte("// Code generated by Pluto; DO NOT EDIT.\n"),
				formatted...)
		}
	}

	return files, nil
}
