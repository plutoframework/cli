package project

import (
	"fmt"
	"strings"
)

// ValidateManifestContracts joins independent authority, domain/data behavior
// and
// transport declarations before generating any files. It does not establish
// runtime identity or prove arbitrary SQL: managed code and conformance do
// that.
func ValidateManifestContracts(m Manifest) error {
	if err := ValidateBoundaryProfile(
		m.Boundary,
		m.Capabilities.Tenancy,
	); err != nil {
		return err
	}

	if err := validateContractFilenames(m); err != nil {
		return err
	}

	operations := map[string]Operation{}
	permissions := []string{}

	for _, operation := range m.Operations {
		if err := operation.Validate(
			m.Capabilities.Tenancy == "multi",
		); err != nil {
			return err
		}

		operations[operation.ID()] = operation
		if operation.Permission != "" {
			permissions = append(permissions, operation.Permission)
		}
	}

	if err := validateJobs(m.Jobs); err != nil {
		return err
	}

	if err := ValidateAgents(m.Agents); err != nil {
		return err
	}

	for _, endpoint := range m.Endpoints {
		operation, ok := operations[endpoint.Operation]
		if !ok {
			return fmt.Errorf(
				"HTTP entry references undeclared operation %s",
				endpoint.Operation,
			)
		}

		if endpoint.Permission != "" &&
			endpoint.Permission != operation.Permission ||
			endpoint.Public && !operation.Public ||
			endpoint.Collection && !operation.Collection {
			return fmt.Errorf(
				"HTTP entry cannot override operation authority or "+
					"collection contract: %s",
				endpoint.Operation,
			)
		}

		if endpoint.Idempotency != "" && endpoint.Idempotency != "auto" {
			mode := operation.Idempotency
			if !operation.Write {
				mode = "exempt"
			}

			if endpoint.Idempotency != mode {
				return fmt.Errorf(
					"HTTP entry cannot override operation replay contract: %s",
					endpoint.Operation,
				)
			}
		}

		resolved, err := m.OperationEndpoint(endpoint)
		if err != nil {
			return err
		}

		if err := resolved.Validate(
			m.Capabilities.Tenancy == "multi",
		); err != nil {
			return err
		}
	}

	for _, job := range m.Jobs {
		operation, ok := operations[job.Operation]
		if !ok || operation.Public {
			return fmt.Errorf(
				"job requires a protected declared operation: %s",
				job.Operation,
			)
		}
	}

	for _, agent := range m.Agents {
		operation, ok := operations[agent.Operation]
		if !ok || operation.Public || operation.Permission == "" {
			return fmt.Errorf(
				"tool requires a protected declared operation: %s",
				agent.Operation,
			)
		}

		if agent.Permission != "" && agent.Permission != operation.Permission {
			return fmt.Errorf(
				"tool cannot override operation permission: %s",
				agent.Operation,
			)
		}
	}

	if m.Boundary.Mode != "candidate" {
		if len(
			m.Resources,
		)+len(
			m.OperationContracts,
		)+len(
			m.Effects,
		)+len(
			m.Roles,
		) != 0 {
			return fmt.Errorf(
				"published compatibility profile cannot enforce candidate " +
					"resource, role or behavior contracts",
			)
		}

		for _, agent := range m.Agents {
			operation := operations[agent.Operation]
			if operation.Write || operation.Collection ||
				agent.Approval != "" && agent.Approval != "none" {
				return fmt.Errorf(
					"published compatibility tools support protected reads " +
						"only",
				)
			}
		}

		return nil
	}

	if m.Capabilities.LocalCredentials || m.Capabilities.OIDC {
		return fmt.Errorf(
			"candidate profile currently requires session-only identity " +
				"ingress; local credentials and OIDC need managed " +
				"credential and delivery contracts before adoption",
		)
	}

	if _, err := CompileRoleContracts(m.Roles, permissions); err != nil {
		return err
	}

	if len(m.Operations) == 0 {
		if len(
			m.Resources,
		)+len(
			m.OperationContracts,
		)+len(
			m.Effects,
		)+len(
			m.Endpoints,
		)+len(
			m.Jobs,
		)+len(
			m.Agents,
		) != 0 {
			return fmt.Errorf(
				"candidate declarations require independent operations",
			)
		}

		return nil
	}

	if err := ValidateContracts(
		m.Resources,
		m.OperationContracts,
		m.Effects,
	); err != nil {
		return err
	}

	contracts := map[string]OperationContract{}

	for _, contract := range m.OperationContracts {
		operation, ok := operations[contract.ID]
		if !ok {
			return fmt.Errorf(
				"behavior contract references undeclared operation %s",
				contract.ID,
			)
		}

		write := contract.Kind == "create" || contract.Kind == "transition" ||
			contract.Kind == "effect"
		if operation.Permission != contract.Permission ||
			operation.Public != contract.Public ||
			operation.Write != write ||
			operation.Collection != (contract.Kind == "list") ||
			operation.Idempotency != contract.Idempotency ||
			operation.ReplayReason != contract.ExemptionReason {
			return fmt.Errorf(
				"operation authority and behavior contract disagree: %s",
				contract.ID,
			)
		}

		contracts[contract.ID] = contract
	}

	for id := range operations {
		if _, ok := contracts[id]; !ok {
			return fmt.Errorf(
				"candidate operation %s requires exactly one typed behavior "+
					"contract",
				id,
			)
		}
	}

	for _, endpoint := range m.Endpoints {
		contract := contracts[endpoint.Operation]
		if endpoint.Feature != "" || endpoint.ClientVisible {
			return fmt.Errorf(
				"candidate feature flags require an operation-owned shared " +
					"gate; route-only flags are unsupported",
			)
		}

		if contract.Kind == "effect" {
			return fmt.Errorf(
				"effect operations require managed worker delivery, not HTTP " +
					"entries",
			)
		}

		if err := validateResourceSource(endpoint, contract); err != nil {
			return err
		}
	}

	effects := map[string]EffectContract{}
	effectTargets := map[string]bool{}

	for _, effect := range m.Effects {
		effects[effect.Kind] = effect
		effectTargets[effect.Operation] = true
	}

	for id, contract := range contracts {
		if contract.Kind == "effect" && !effectTargets[id] {
			return fmt.Errorf(
				"effect operation %s requires a declared transactional "+
					"producer and delivery binding",
				id,
			)
		}
	}

	bound := map[string]bool{}

	for _, job := range m.Jobs {
		if job.MaxAttempts < 1 || job.MaxAttempts > 100 {
			return fmt.Errorf(
				"candidate job %s requires explicit bounded max_attempts",
				job.Kind,
			)
		}

		contract := contracts[job.Operation]
		if contract.Kind == "list" || contract.Kind == "export" {
			return fmt.Errorf(
				"candidate worker collections require a separately supported " +
					"delivery contract",
			)
		}

		effect, known := effects[job.Kind]
		if known {
			if job.Operation != effect.Operation ||
				job.MaxAttempts != effect.MaxAttempts {
				return fmt.Errorf(
					"job %s disagrees with declared effect target or retry "+
						"budget",
					job.Kind,
				)
			}

			bound[job.Kind] = true
		} else if contract.Kind == "effect" {
			return fmt.Errorf(
				"worker effect %s has no declared delivery kind",
				job.Operation,
			)
		}
	}

	for kind := range effects {
		if !bound[kind] {
			return fmt.Errorf(
				"declared effect %s requires its matching managed job binding",
				kind,
			)
		}
	}

	for _, agent := range m.Agents {
		contract := contracts[agent.Operation]
		switch contract.Kind {
		case "load":
			if agent.Approval != "" && agent.Approval != "none" {
				return fmt.Errorf(
					"read tool declares an unsupported approval mode",
				)
			}
		case "create", "transition":
			if agent.Approval != "host" || contract.Idempotency != "required" {
				return fmt.Errorf(
					"write tool requires host approval and required " +
						"operation idempotency",
				)
			}
		default:
			return fmt.Errorf(
				"candidate tools support resource loads or approved create/" +
					"transition operations",
			)
		}
	}

	return nil
}

func validateContractFilenames(m Manifest) error {
	seen := map[string]bool{}
	operationSymbols := map[string]string{}
	add := func(kind, group, name string) error {
		key := kind + "/" + group + "/" + strings.ToLower(name)
		if seen[key] {
			return fmt.Errorf(
				"generated filename collision for %s %s.%s",
				kind,
				group,
				name,
			)
		}

		seen[key] = true

		return nil
	}

	for _, operation := range m.Operations {
		names := []string{
			operation.Action,
			operation.Action + "Boundary",
			"New" + operation.Action,
		}
		if m.Boundary.Mode == "candidate" {
			names = append(names, operation.Action+"Intent")
		} else {
			names = append(names, "New"+operation.Action+"WithDependencies")
		}

		for _, name := range names {
			key := operation.Group + "." + name
			if owner := operationSymbols[key]; owner != "" {
				return fmt.Errorf(
					"generated Go identifier collision %s between %s and %s",
					key,
					owner,
					operation.ID(),
				)
			}

			operationSymbols[key] = operation.ID()
		}

		if err := add(
			"operation",
			operation.Group,
			operation.Action,
		); err != nil {
			return err
		}
	}

	for _, resource := range m.Resources {
		if err := add("resource", resource.Group, resource.Name); err != nil {
			return err
		}
	}

	for _, endpoint := range m.Endpoints {
		if err := add(
			"HTTP entry",
			endpoint.Group,
			endpoint.Action,
		); err != nil {
			return err
		}
	}

	for _, job := range m.Jobs {
		if err := add("worker entry", job.Group, job.Action); err != nil {
			return err
		}
	}

	for _, agent := range m.Agents {
		if err := add("tool entry", agent.Group, agent.Action); err != nil {
			return err
		}
	}

	return nil
}

func validateResourceSource(
	endpoint Endpoint,
	contract OperationContract,
) error {
	params := []string{}

	for _, part := range strings.Split(endpoint.Path, "/") {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			params = append(
				params,
				strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}"),
			)
		}
	}

	if contract.Kind == "list" || contract.Kind == "export" {
		if endpoint.ResourceSource != "collection" || len(params) != 0 {
			return fmt.Errorf(
				"collection entry requires collection resource_source and no " +
					"record path parameters",
			)
		}

		return nil
	}

	switch {
	case endpoint.ResourceSource == "body":
		if (endpoint.Method != "POST" &&
			endpoint.Method != "PUT" &&
			endpoint.Method != "PATCH") ||

			len(params) != 0 {
			return fmt.Errorf(
				"body resource_source requires POST, PUT or PATCH and no " +
					"unused path parameters",
			)
		}
	case strings.HasPrefix(endpoint.ResourceSource, "path:"):
		name := strings.TrimPrefix(endpoint.ResourceSource, "path:")
		if len(params) != 1 || params[0] != name ||
			!contractLabel.MatchString(name) {
			return fmt.Errorf(
				"path resource_source must identify the one declared record " +
					"path parameter",
			)
		}
	case strings.HasPrefix(endpoint.ResourceSource, "query:"):
		name := strings.TrimPrefix(endpoint.ResourceSource, "query:")
		if !contractLabel.MatchString(name) || len(params) != 0 {
			return fmt.Errorf(
				"query resource_source requires a safe explicit label and no " +
					"unused path parameters",
			)
		}
	default:
		return fmt.Errorf(
			"record entry requires explicit body, path:<param> or " +
				"query:<param> resource_source",
		)
	}

	return nil
}
