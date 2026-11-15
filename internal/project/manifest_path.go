package project

import (
	"fmt"
	"regexp"
	"strings"
)

// Typed path parameters of authored HTTP entries. The endpoint path in
// pluto.toml names the {segments}; [[operations.path]] types them. The
// generated handler validates each before the gate runs, binds them into the
// request model, and the verified-tenant gate is bound to the resource key
// "<resource>:<value>" of the one parameter named by resource_source =
// "path:<name>". A value that is well formed but names a row the tenant
// cannot see is a typed not-found (404) returned by the authored adapter.

const (
	pathTypeUUID   = "uuid"
	pathTypeString = "string"
	pathTypeInt64  = "int64"

	maxPathString = 256
)

// pathName matches the route parameter syntax of endpoint paths.
var pathName = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)

// isPathOperation reports an authored operation on the verified user path,
// the only kind that can bind path parameters: not provision, enroll,
// machine, stream or per-tenant.
func (o Operation) isPathOperation() bool {
	return o.Body == BodyAuthored && o.Scope == "" && !o.isMachine() &&
		!o.isStream()
}

func (o Operation) validatePath() error {
	if len(o.Path) == 0 {
		return nil
	}

	if !o.isPathOperation() {
		return fmt.Errorf(
			"path parameters require an authored operation on the " +
				"verified user path",
		)
	}

	if o.Public {
		return fmt.Errorf("path parameters require a protected operation")
	}

	if o.Collection || o.Response != ResponseObject {
		return fmt.Errorf(
			"path parameters require response = \"object\" and no " +
				"collection",
		)
	}

	seen := map[string]bool{}
	goNames := map[string]bool{}

	for _, param := range o.Query {
		goNames[seamGoName(param.Name)] = true
	}

	for _, param := range o.Path {
		if !pathName.MatchString(param.Name) {
			return fmt.Errorf(
				"path name %q must start lowercase and use letters and "+
					"digits",
				param.Name,
			)
		}

		if seen[param.Name] {
			return fmt.Errorf("duplicate path parameter %s", param.Name)
		}

		seen[param.Name] = true

		goName := pathGoName(param.Name)
		if goNames[goName] || goName == "Body" {
			return fmt.Errorf(
				"path %s collides on Go name %s",
				param.Name,
				goName,
			)
		}

		goNames[goName] = true

		if err := param.validate(); err != nil {
			return err
		}
	}

	return nil
}

func (p OperationPath) validate() error {
	switch p.Type {
	case pathTypeUUID:
		if p.Max != 0 {
			return fmt.Errorf("path %s cannot declare max", p.Name)
		}
	case pathTypeString:
		if p.Max < 1 || p.Max > maxPathString {
			return fmt.Errorf(
				"path %s requires max between 1 and %d",
				p.Name,
				maxPathString,
			)
		}
	case pathTypeInt64:
		if p.Max < 0 {
			return fmt.Errorf("path %s max cannot be negative", p.Name)
		}
	default:
		return fmt.Errorf("path %s type must be uuid, string or int64", p.Name)
	}

	return nil
}

// pathGoName is the exported request field of a path parameter.
func pathGoName(name string) string {
	if name == "id" {
		return "ID"
	}

	return strings.ToUpper(name[:1]) + name[1:]
}

// pathSegments lists the {parameters} of an endpoint path in order.
func pathSegments(path string) []string {
	params := []string{}

	for _, part := range strings.Split(path, "/") {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			params = append(params, strings.Trim(part, "{}"))
		}
	}

	return params
}

// validatePathBindings ties each authored endpoint's {segments} to the typed
// declarations and to the resource key of its gate.
func (m Manifest) validatePathBindings() error {
	exposed := map[string]bool{}

	for _, entry := range m.Endpoints {
		operation, ok := m.Operation(entry.Operation)
		if !ok || !operation.isPathOperation() {
			continue
		}

		exposed[operation.ID()] = true

		if err := bindEndpointPath(entry, operation); err != nil {
			return fmt.Errorf("endpoint %s: %w", entry.Operation, err)
		}
	}

	for _, operation := range m.Operations {
		if len(operation.Path) != 0 && !exposed[operation.ID()] {
			return fmt.Errorf(
				"operation %s declares path parameters but has no HTTP "+
					"endpoint",
				operation.ID(),
			)
		}
	}

	return nil
}

func bindEndpointPath(entry Endpoint, operation Operation) error {
	segments := pathSegments(entry.Path)
	declared := map[string]bool{}

	for _, param := range operation.Path {
		declared[param.Name] = true
	}

	named := map[string]bool{}

	for _, name := range segments {
		named[name] = true

		if !declared[name] {
			return fmt.Errorf(
				"path parameter {%s} needs a [[operations.path]] "+
					"declaration on %s",
				name,
				operation.ID(),
			)
		}
	}

	for _, param := range operation.Path {
		if !named[param.Name] {
			return fmt.Errorf(
				"declared path %s is not a {segment} of %s",
				param.Name,
				entry.Path,
			)
		}
	}

	if len(segments) == 0 {
		return nil
	}

	key, found := strings.CutPrefix(entry.ResourceSource, "path:")
	if !found || !named[key] {
		return fmt.Errorf(
			"resource_source must be path:<name> naming one of the path " +
				"parameters",
		)
	}

	return nil
}

// pathKey is the declared path parameter the gate's resource key carries,
// or "" without path parameters.
func (m Manifest) pathKey(operation Operation) string {
	for _, entry := range m.Endpoints {
		if entry.Operation != operation.ID() {
			continue
		}

		if key, ok := strings.CutPrefix(entry.ResourceSource, "path:"); ok {
			return key
		}
	}

	return ""
}

// hasPathOperations reports authored operations that bind path parameters.
func (m Manifest) hasPathOperations() bool {
	for _, operation := range m.Operations {
		if len(operation.Path) != 0 {
			return true
		}
	}

	return false
}
