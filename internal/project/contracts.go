package project

import (
	"fmt"
	"regexp"
	"strings"
)

// ResourceContract describes the supported generated scoped-record adapter.
// Application migrations remain explicit; this is not a schema inference API.
type ResourceContract struct {
	Group           string               `toml:"group"`
	Name            string               `toml:"name"`
	Table           string               `toml:"table"`
	Namespace       string               `toml:"namespace"`
	Version         int                  `toml:"version"`
	Visibility      string               `toml:"visibility"`
	InitialState    string               `toml:"initial_state"`
	States          []string             `toml:"states"`
	Transitions     []ResourceTransition `toml:"transitions"`
	Normalize       string               `toml:"normalize"`
	MaxValueBytes   int                  `toml:"max_value_bytes"`
	PageLimit       int                  `toml:"page_limit"`
	ExportLimit     int                  `toml:"export_limit"`
	CacheTTLSeconds int                  `toml:"cache_ttl_seconds"`
}

type ResourceTransition struct {
	From  string `toml:"from"`
	Event string `toml:"event"`
	To    string `toml:"to"`
	Guard string `toml:"guard"`
}

func (r ResourceContract) ID() string {
	return r.Group + "." + r.Name
}

// OperationContract declares required behavior independently of entry
// transport.
// CLI integration also checks equality with the owning authority declaration.
type OperationContract struct {
	ID              string   `toml:"id"`
	Resource        string   `toml:"resource"`
	Kind            string   `toml:"kind"`
	Permission      string   `toml:"permission"`
	Public          bool     `toml:"public"`
	ResourcePolicy  string   `toml:"resource_policy"`
	Event           string   `toml:"event"`
	Idempotency     string   `toml:"idempotency"`
	ExemptionReason string   `toml:"exemption_reason"`
	Effects         []string `toml:"effects"`
}

type EffectContract struct {
	Name        string `toml:"name"`
	Operation   string `toml:"operation"`
	Kind        string `toml:"kind"`
	MaxAttempts int    `toml:"max_attempts"`
}

var (
	contractSQLName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	contractLabel   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)
	contractKey     = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,127}$`)
)

func validOperationID(id string) bool {
	parts := strings.Split(id, ".")
	if len(parts) != 2 {
		return false
	}

	return contractAuthority(parts[0], parts[1], "") == nil
}

// Reuse the canonical generated name and permission rules. The placeholder
// route is validation input only and is never emitted as an HTTP entry.
func contractAuthority(group, action, permission string) error {
	return (Endpoint{
		Group:       group,
		Action:      action,
		Name:        "Contract",
		Description: "Declared operation contract.",
		Method:      "GET",
		Path:        "/contract",
		Permission:  permission,
	}).Validate(
		false,
	)
}

func validateResource(r ResourceContract) error {
	if strings.HasPrefix(r.Table, "pluto_") {
		return fmt.Errorf(
			"resource %s table uses the reserved framework namespace",
			r.ID(),
		)
	}

	if err := contractAuthority(r.Group, r.Name, ""); err != nil {
		return fmt.Errorf("resource %s: %w", r.ID(), err)
	}

	if !contractSQLName.MatchString(r.Table) ||
		!contractKey.MatchString(r.Namespace) ||
		r.Version < 1 ||
		r.Version > 1_000_000 {
		return fmt.Errorf(
			"resource %s requires valid owner/name, safe SQL table, "+
				"namespace and positive bounded version",
			r.ID(),
		)
	}

	if r.Visibility != "owner" && r.Visibility != "tenant" {
		return fmt.Errorf(
			"resource %s requires explicit owner or tenant visibility",
			r.ID(),
		)
	}

	if r.Normalize != "trim" && r.Normalize != "none" {
		return fmt.Errorf(
			"resource %s requires explicit trim or none normalization",
			r.ID(),
		)
	}

	if r.MaxValueBytes < 1 || r.MaxValueBytes > 65536 || r.PageLimit < 1 ||
		r.PageLimit > 100 ||
		r.ExportLimit < r.PageLimit ||
		r.ExportLimit > 10000 ||
		r.CacheTTLSeconds < 0 ||
		r.CacheTTLSeconds > 3600 {
		return fmt.Errorf(
			"resource %s requires bounded value/page/export/cache limits",
			r.ID(),
		)
	}

	if len(r.States) == 0 || len(r.States) > 64 {
		return fmt.Errorf("resource %s requires 1..64 lifecycle states", r.ID())
	}

	states := map[string]bool{}
	for _, state := range r.States {
		if !contractLabel.MatchString(state) || states[state] {
			return fmt.Errorf(
				"resource %s has invalid or duplicate lifecycle state",
				r.ID(),
			)
		}

		states[state] = true
	}

	if !states[r.InitialState] {
		return fmt.Errorf("resource %s initial state is undeclared", r.ID())
	}

	if len(r.Transitions) > 256 {
		return fmt.Errorf("resource %s has too many transitions", r.ID())
	}

	transitions := map[string]bool{}
	guards := map[string]string{}

	for _, transition := range r.Transitions {
		key := transition.From + "/" + transition.Event
		if !states[transition.From] || !states[transition.To] ||
			!contractLabel.MatchString(transition.Event) ||
			transitions[key] {
			return fmt.Errorf(
				"resource %s has an undeclared or nondeterministic transition",
				r.ID(),
			)
		}

		if transition.Guard != "none" && transition.Guard != "owner" &&
			transition.Guard != "not_owner" {
			return fmt.Errorf(
				"resource %s requires explicit none, owner or not_owner "+
					"transition guard",
				r.ID(),
			)
		}

		if guard, exists := guards[transition.Event]; exists &&
			guard != transition.Guard {
			return fmt.Errorf(
				"resource %s requires one consistent guard per event for "+
					"replay authorization",
				r.ID(),
			)
		}

		guards[transition.Event] = transition.Guard
		transitions[key] = true
	}

	return nil
}

// ValidateContracts rejects unsupported or incomplete behavior before
// generation.
// Bounds and references are checked here; exact managed implementations, typed
// ports and real data tests establish the corresponding runtime guarantees.
func ValidateContracts(
	resources []ResourceContract,
	operations []OperationContract,
	effects []EffectContract,
) error {
	if len(resources) == 0 || len(operations) == 0 {
		return fmt.Errorf(
			"candidate data contracts require resources and operations",
		)
	}

	if len(resources) > 100 || len(operations) > 1000 || len(effects) > 1000 {
		return fmt.Errorf("contract inventory exceeds supported bounds")
	}

	resourceByID := map[string]ResourceContract{}
	tables := map[string]bool{}
	namespaces := map[string]bool{}

	for _, resource := range resources {
		if err := validateResource(resource); err != nil {
			return err
		}

		_, duplicate := resourceByID[resource.ID()]
		if duplicate || tables[resource.Table] ||
			namespaces[resource.Namespace] {
			return fmt.Errorf(
				"duplicate resource identity, table or cache namespace: %s",
				resource.ID(),
			)
		}

		resourceByID[resource.ID()] = resource
		tables[resource.Table] = true
		namespaces[resource.Namespace] = true
	}

	operationByID := map[string]OperationContract{}
	operationCounts := map[string]int{}

	for _, operation := range operations {
		if !validOperationID(operation.ID) {
			return fmt.Errorf("invalid contract operation ID %q", operation.ID)
		}

		if _, duplicate := operationByID[operation.ID]; duplicate {
			return fmt.Errorf("duplicate contract operation %s", operation.ID)
		}

		resource, known := resourceByID[operation.Resource]
		if !known {
			return fmt.Errorf(
				"operation %s references an undeclared resource",
				operation.ID,
			)
		}

		operationCounts[operation.Resource]++
		if operationCounts[operation.Resource] > 24 {
			return fmt.Errorf(
				"resource %s exceeds 24 generated operations",
				operation.Resource,
			)
		}

		if operation.Public || strings.TrimSpace(operation.Permission) == "" {
			return fmt.Errorf(
				"resource operation %s requires protected explicit permission",
				operation.ID,
			)
		}

		parts := strings.Split(operation.ID, ".")
		if err := contractAuthority(
			parts[0],
			parts[1],
			operation.Permission,
		); err != nil {
			return fmt.Errorf("operation %s authority: %w", operation.ID, err)
		}

		write := operation.Kind == "create" || operation.Kind == "transition"
		switch operation.Kind {
		case "load", "list", "export", "create", "transition", "effect":
		default:
			return fmt.Errorf(
				"operation %s has an unsupported contract kind",
				operation.ID,
			)
		}

		if operation.Kind == "create" {
			if operation.ResourcePolicy != "create" {
				return fmt.Errorf(
					"create operation %s requires explicit create policy",
					operation.ID,
				)
			}
		} else if operation.ResourcePolicy != "owner" &&
			operation.ResourcePolicy != "tenant" {
			return fmt.Errorf(
				"operation %s requires explicit owner or tenant resource "+
					"policy",
				operation.ID,
			)
		}

		if (operation.Kind == "list" || operation.Kind == "export") &&
			operation.ResourcePolicy != resource.Visibility {
			return fmt.Errorf(
				"collection operation %s policy must match query visibility",
				operation.ID,
			)
		}

		if operation.Kind == "transition" {
			found := false

			for _, transition := range resource.Transitions {
				if transition.Event == operation.Event {
					if operation.ResourcePolicy == "owner" &&
						transition.Guard == "not_owner" {
						return fmt.Errorf(
							"operation %s owner policy contradicts its not_"+
								"owner transition guard",
							operation.ID,
						)
					}

					found = true
				}
			}

			if !found {
				return fmt.Errorf(
					"operation %s references an undeclared transition event",
					operation.ID,
				)
			}
		} else if operation.Event != "" {
			return fmt.Errorf(
				"non-transition operation %s cannot select a lifecycle event",
				operation.ID,
			)
		}

		if write || operation.Kind == "effect" {
			if operation.Idempotency != "required" &&
				operation.Idempotency != "exempt" {
				return fmt.Errorf(
					"write/effect operation %s requires replay semantics or "+
						"an explicit exemption",
					operation.ID,
				)
			}
		} else if operation.Idempotency != "none" {
			return fmt.Errorf(
				"read operation %s must declare no mutation replay",
				operation.ID,
			)
		}

		if operation.Idempotency == "exempt" {
			reason := strings.TrimSpace(operation.ExemptionReason)
			if len(reason) < 12 || len(reason) > 512 ||
				reason != operation.ExemptionReason {
				return fmt.Errorf(
					"operation %s requires a specific bounded exemption "+
						"reason",
					operation.ID,
				)
			}
		} else if operation.ExemptionReason != "" {
			return fmt.Errorf(
				"operation %s has an exemption reason without exemption",
				operation.ID,
			)
		}

		if !write && len(operation.Effects) > 0 {
			return fmt.Errorf(
				"only an atomic mutation may enqueue declared effects: %s",
				operation.ID,
			)
		}

		if len(operation.Effects) > 0 && resource.MaxValueBytes > 8192 {
			return fmt.Errorf(
				"effect-producing resource %s requires max_value_bytes at "+
					"most 8192 to bound serialized delivery payloads",
				operation.Resource,
			)
		}

		operationByID[operation.ID] = operation
	}

	effectByName := map[string]EffectContract{}
	kinds := map[string]bool{}
	effectCounts := map[string]int{}

	for _, effect := range effects {
		if !contractKey.MatchString(effect.Name) ||
			!contractKey.MatchString(effect.Kind) ||
			effect.MaxAttempts < 1 ||
			effect.MaxAttempts > 100 {
			return fmt.Errorf(
				"effect %s requires a typed kind and finite 1..100 attempts",
				effect.Name,
			)
		}

		_, duplicate := effectByName[effect.Name]
		if duplicate || kinds[effect.Kind] {
			return fmt.Errorf("duplicate effect name or kind: %s", effect.Name)
		}

		target, ok := operationByID[effect.Operation]
		if !ok || target.Kind != "effect" {
			return fmt.Errorf(
				"effect %s must reference a declared effect operation",
				effect.Name,
			)
		}

		effectCounts[target.Resource]++
		if effectCounts[target.Resource] > 24 {
			return fmt.Errorf(
				"resource %s exceeds 24 generated effects",
				target.Resource,
			)
		}

		effectByName[effect.Name] = effect
		kinds[effect.Kind] = true
	}

	used := map[string]bool{}

	for _, operation := range operations {
		seen := map[string]bool{}

		for _, name := range operation.Effects {
			effect, ok := effectByName[name]
			if !ok || seen[name] {
				return fmt.Errorf(
					"operation %s has an undeclared or duplicate effect",
					operation.ID,
				)
			}

			if operationByID[effect.Operation].Resource != operation.Resource {
				return fmt.Errorf(
					"effect %s crosses resource ownership without a "+
						"supported mapping",
					name,
				)
			}

			seen[name] = true
			used[name] = true
		}
	}

	for name := range effectByName {
		if !used[name] {
			return fmt.Errorf("effect %s has no transactional producer", name)
		}
	}

	return nil
}
