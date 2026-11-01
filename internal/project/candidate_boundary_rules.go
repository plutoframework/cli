package project

import "strings"

func candidateSourceRules(
	module string,
	declaration candidateDeclaration,
) candidateRules {
	rules := candidateRules{
		managed:        map[string]bool{},
		capabilities:   map[string]map[string]bool{},
		protectedTypes: map[string]bool{},
		privateNames:   map[string]map[string]string{},
		domainFiles:    map[string]bool{},
	}
	rules.globalPrivate = map[string]map[string]bool{}
	rules.transportNamespace = module + "/internal/transport."
	factory := "internal/app/candidate_operations_gen.go"

	caller := "internal/access/caller_gen.go"
	for _, path := range []string{
		factory,
		caller,
		"internal/app/routes_gen.go",
		"internal/app/worker_runtime_gen.go",
		"internal/background/dependencies.go",
		"internal/background/handlers_gen.go",
		"internal/access/authorization_gen.go",
		"internal/access/permissions_gen.go",
		"internal/storage/postgres/postgres.go",
		"internal/storage/postgres/db/schema.sql.go",
		"internal/transport/sessions.go",
		"internal/transport/credentials.go",
	} {
		rules.managed[path] = true
	}

	capability := func(namespace, name string, owners ...string) {
		key := namespace + "." + name
		if rules.capabilities[key] == nil {
			rules.capabilities[key] = map[string]bool{}
		}

		for _, path := range owners {
			rules.capabilities[key][path] = true
		}
	}
	reserve := func(directory, name, owner string) {
		if rules.privateNames[directory] == nil {
			rules.privateNames[directory] = map[string]string{}
		}

		rules.privateNames[directory][name] = owner
	}
	reserveGlobal := func(name, owner string) {
		if rules.globalPrivate[name] == nil {
			rules.globalPrivate[name] = map[string]bool{}
		}

		rules.globalPrivate[name][owner] = true
	}

	for _, resource := range declaration.Resources {
		if !validOperationID(resource.ID()) {
			continue
		}

		domain := "internal/domain/" + resource.Group + "/" + strings.ToLower(
			resource.Name,
		) + "_contract_gen.go"
		data := "internal/storage/" + resource.Group + "/" + strings.ToLower(
			resource.Name,
		) + "_data_gen.go"
		reads := "internal/storage/" + resource.Group + "/" + strings.ToLower(
			resource.Name,
		) + "_reads_gen.go"

		ports := "internal/ports/" + resource.Group + "/" + strings.ToLower(
			resource.Name,
		) + "_ports_gen.go"
		for _, path := range []string{domain, data, reads, ports} {
			rules.managed[path] = true
		}

		rules.domainFiles[domain] = true
		domainNamespace := module + "/internal/domain/" + resource.Group
		storageNamespace := module + "/internal/storage/" + resource.Group
		capability(domainNamespace, "Rehydrate"+resource.Name, data, reads)
		capability(domainNamespace, "New"+resource.Name+"Machine", factory)
		capability(storageNamespace, "New"+resource.Name+"Data", factory)
		capability(
			storageNamespace,
			"New"+resource.Name+"ResourcePolicy",
			factory,
		)

		for _, name := range []string{
			resource.Name + "Record",
			resource.Name + "Command",
		} {
			rules.protectedTypes[domainNamespace+"."+name] = true
		}

		rules.protectedTypes[storageNamespace+"."+resource.Name+"Data"] = true
		portType := module +
			"/internal/ports/" +
			resource.Group +
			"." +
			resource.Name
		rules.protectedTypes[portType+"Data"] = true

		lower := strings.ToLower(resource.Name[:1]) + resource.Name[1:]
		for _, field := range []string{
			"ID",
			"Tenant",
			"Owner",
			"State",
			"Version",
			"Value",
			"Valid",
		} {
			reserveGlobal(lower+"Record"+field, domain)
		}

		for _, field := range []string{
			"Kind",
			"ID",
			"Key",
			"Value",
			"Limit",
			"Cursor",
			"Valid",
		} {
			reserveGlobal(lower+"Command"+field, domain)
		}

		// Constructors cannot be laundered through an authored helper or alias.
		// The
		// exact managed files remain responsible for the semantics of their
		// calls.
		commandOwners := []string{domain, data, reads}
		evaluatorOwners := []string{}

		for _, contract := range declaration.Contracts {
			if contract.Resource != resource.ID() {
				continue
			}

			parts := strings.Split(contract.ID, ".")
			if len(parts) != 2 {
				continue
			}

			gate := "internal/operations/" + parts[0] + "/" + strings.ToLower(
				parts[1],
			) + "_gate_gen.go"
			workflow := "internal/operations/" +
				parts[0] +
				"/" +
				strings.ToLower(

					parts[1],
				) + "_workflow_gen.go"
			commandOwners = append(commandOwners, gate, workflow)
			evaluatorOwners = append(evaluatorOwners, workflow)
		}

		for _, job := range declaration.Jobs {
			commandOwners = append(
				commandOwners,
				"internal/jobcontracts/"+job.Group+"/"+strings.ToLower(
					job.Action,
				)+".go",
			)
		}

		capability(
			domainNamespace,
			"New"+resource.Name+"Command",
			commandOwners...)
		capability(
			domainNamespace,
			"Evaluate"+resource.Name,
			evaluatorOwners...)
	}

	for _, operation := range declaration.Operations {
		if !validOperationID(operation.ID()) {
			continue
		}

		gate := "internal/operations/" +
			operation.Group +
			"/" +
			strings.ToLower(

				operation.Action,
			) + "_gate_gen.go"
		workflow := "internal/operations/" +
			operation.Group +
			"/" +
			strings.ToLower(

				operation.Action,
			) + "_workflow_gen.go"
		rules.managed[gate] = true
		rules.managed[workflow] = true
		namespace := module + "/internal/operations/" + operation.Group
		capability(namespace, "New"+operation.Action, factory)
		capability(
			namespace,
			"New"+operation.Action+"WithDependencies",
			factory,
		)

		rules.protectedTypes[namespace+"."+operation.Action] = true
		rules.protectedTypes[namespace+
			"."+
			operation.Action+
			"Dependencies"] = true
		rules.protectedTypes[namespace+"."+operation.Action+"Boundary"] = true
		rules.protectedTypes[namespace+"."+operation.Action+"Intent"] = true

		lower := strings.ToLower(operation.Action[:1]) + operation.Action[1:]
		for _, suffix := range []string{
			"Gate",
			"Data",
			"Machine",
			"IntentOwner",
			"IntentCaller",
			"IntentCommand",
		} {
			reserve("internal/operations/"+operation.Group, lower+suffix, gate)
		}
	}

	for _, endpoint := range declaration.Endpoints {
		rules.managed["internal/handlers/"+endpoint.Group+"/"+strings.ToLower(
			endpoint.Action,
		)+".go"] = true
		rules.managed["internal/handlers/"+
			endpoint.Group+
			"/register_gen.go"] = true
	}

	for _, job := range declaration.Jobs {
		for _, path := range []string{
			"internal/jobs/" + job.Group + "/" + strings.ToLower(
				job.Action,
			) + ".go",
			"internal/jobcontracts/" + job.Group + "/" + strings.ToLower(
				job.Action,
			) + ".go",
			"internal/background/" + job.Group + "_" + strings.ToLower(
				job.Action,
			) + ".go",
		} {
			rules.managed[path] = true
		}
	}

	for _, agent := range declaration.Agents {
		if !validOperationID(agent.Group + "." + agent.Action) {
			continue
		}

		path := "internal/agents/" + agent.Group + "/" + strings.ToLower(
			agent.Action,
		) + ".go"
		directory := "internal/agents/" + agent.Group
		rules.managed[path] = true

		lower := strings.ToLower(agent.Action[:1]) + agent.Action[1:]
		for _, suffix := range []string{
			"Context",
			"Operation",
			"Intent",
			"Receipt",
		} {
			reserve(directory, lower+suffix, path)
		}

		for _, field := range []string{"applied", "output"} {
			reserve(directory, field, path)
		}

		rules.protectedTypes[module+"/"+directory+"."+lower+"Receipt"] = true
		rules.protectedTypes[module+
			"/"+
			directory+
			"."+
			agent.Action+
			"Dependencie"+

			"s"] = true
	}

	for _, contract := range declaration.Contracts {
		if contract.Kind != "effect" {
			continue
		}

		parts := strings.Split(contract.ID, ".")
		if len(parts) != 2 {
			continue
		}

		for _, resource := range declaration.Resources {
			if resource.ID() != contract.Resource {
				continue
			}

			capability(
				module+"/internal/infrastructure/"+resource.Group,
				"New"+parts[1]+"Delivery",
				factory,
			)
		}
	}

	capability(module+"/internal/storage/identity", "NewMemberships", factory)

	for _, field := range []string{
		"candidateRuntimeManager",
		"candidateRuntimeHandler",
		"candidateRuntimePool",
		"candidateRuntimeWorkers",
	} {
		reserveGlobal(field, factory)
	}

	for _, field := range []string{
		"callerIdentity",
		"callerTenant",
		"callerWorkID",
		"callerWorkKind",
	} {
		reserveGlobal(field, caller)
	}

	rules.managed["internal/storage/identity/memberships_gen.go"] = true
	rules.protectedTypes[module+"/internal/access.Caller"] = true
	rules.protectedTypes["github.com/plutoframework/pluto-auth.Identity"] = true
	rules.protectedTypes["github.com/plutoframework/pluto-rbac/"+
		"identity.Execution"] = true
	rules.protectedTypes["github.com/plutoframework/"+
		"pluto-rbac.Execution"] = true

	capability(module+"/internal/transport", "BoundaryManager", factory)
	capability("github.com/plutoframework/pluto-rbac/identity", "New", factory)
	capability(
		"github.com/plutoframework/pluto-auth",
		"New",
		factory,
		"internal/transport/sessions.go",
		"internal/transport/credentials.go",
	)

	return rules
}
