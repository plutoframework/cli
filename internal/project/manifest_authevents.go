package project

import (
	"fmt"
	"slices"
)

// Authentication event kinds the generated hooks record. The kind is the only
// thing the declared operation receives: the actor and the organization come
// from the verified session and its memberships, never from the request.
const (
	AuthEventLogin     = "login"
	AuthEventLogout    = "logout"
	AuthEventOIDCLogin = "oidc_login"

	// AuthEventMachineEnroll is recorded when a machine redeems its
	// enrollment token, attributed to the machine in its own tenant.
	AuthEventMachineEnroll = "machine_enroll"
)

// Auth event failure policies.
const (
	AuthEventsClosed     = "closed"
	AuthEventsBestEffort = "best_effort"
)

// maxAuthEventTenants bounds the organizations one event is recorded in.
const maxAuthEventTenants = 16

// AuthSection groups declarations about managed authentication.
type AuthSection struct {
	Events *AuthEvents `toml:"events,omitempty"`
	Device *AuthDevice `toml:"device,omitempty"`
}

// AuthEvents declares the authored operation the managed sign-in, OIDC
// sign-in and sign-out handlers record into. The operation is an ordinary
// protected authored write (for example appending to an audit group), exposed
// by no endpoint, job or agent, so only the generated hook can run it, once
// per organization the verified subject belongs to.
type AuthEvents struct {

	// Operation is the <group>.<Action> id of the authored write.
	Operation string `toml:"operation"`

	// OnError is "closed" (default) or "best_effort". Closed denies the
	// sign-in (the session is revoked) or the sign-out when the record cannot
	// be written; best effort logs the failure and proceeds.
	OnError string `toml:"on_error,omitempty"`
}

// AuthEventsOn reports whether the manifest declares auth event recording.
func (m Manifest) AuthEventsOn() bool {
	return m.Auth != nil && m.Auth.Events != nil
}

// Closed reports the effective failure policy.
func (e AuthEvents) Closed() bool {
	return e.OnError != AuthEventsBestEffort
}

// Kinds lists the event kinds the capabilities enable.
func (m Manifest) AuthEventKinds() []string {
	kinds := []string{AuthEventLogin, AuthEventLogout}
	if m.Capabilities.OIDC {
		kinds = append(kinds, AuthEventOIDCLogin)
	}

	return kinds
}

// MemberIndex is the generated subject-to-organizations read of the
// memberships table: a security-invoker view exposing only the tenant id,
// admitted by a SELECT policy keyed to a transaction-local subject setting
// that only generated hook code sets from a live, verified session.
type MemberIndex struct {
	Table   string
	Subject string
	Tenant  string
}

// Setting is the transaction-local subject the policy admits.
func (MemberIndex) Setting() string {
	return "app.auth_event_subject"
}

// PolicyName is the SELECT policy that admits the subject's own rows.
func (MemberIndex) PolicyName() string {
	return "pluto_auth_event_members"
}

// ViewName is the view exposing only the organization ids.
func (MemberIndex) ViewName() string {
	return "pluto_auth_event_members"
}

// MemberIndexOf returns the index when auth events are declared.
func (m Manifest) MemberIndexOf() (MemberIndex, bool) {
	if !m.AuthEventsOn() || m.Memberships == nil {
		return MemberIndex{}, false
	}

	table, ok := m.tableByID(m.Memberships.Table)
	if !ok {
		return MemberIndex{}, false
	}

	return MemberIndex{
		Table:   table.Name,
		Subject: m.Memberships.SubjectColumn,
		Tenant:  m.Memberships.TenantColumn,
	}, true
}

func (m Manifest) tableByID(id string) (Table, bool) {
	for _, table := range m.Tables {
		if table.ID() == id {
			return table, true
		}
	}

	return Table{}, false
}

func validateAuthEvents(m Manifest) error {
	if !m.AuthEventsOn() {
		if m.Auth != nil {
			return fmt.Errorf("[auth] declares nothing; remove it")
		}

		return nil
	}

	events := m.Auth.Events

	switch {
	case m.Boundary.Mode != "candidate":
		return fmt.Errorf("[auth.events] requires the candidate profile")
	case !m.Capabilities.LocalCredentials:
		return fmt.Errorf("[auth.events] requires local credentials")
	case m.Memberships == nil:
		return fmt.Errorf(
			"[auth.events] requires [memberships]: the organizations of " +
				"a signed-in subject come from its membership table",
		)
	}

	if events.OnError != "" && events.OnError != AuthEventsClosed &&
		events.OnError != AuthEventsBestEffort {
		return fmt.Errorf(
			"[auth.events] on_error must be %q or %q",
			AuthEventsClosed,
			AuthEventsBestEffort,
		)
	}

	operation, ok := m.Operation(events.Operation)
	if !ok {
		return fmt.Errorf(
			"[auth.events] operation %q is not declared",
			events.Operation,
		)
	}

	if err := authEventOperation(m, operation); err != nil {
		return fmt.Errorf("[auth.events] operation %s: %w", operation.ID(), err)
	}

	return validateAuthEventRoles(m, operation)
}

func authEventOperation(m Manifest, operation Operation) error {
	if operation.Body != BodyAuthored || !operation.Write ||
		operation.Public || operation.Scope != "" ||
		operation.Permission == "" || operation.Collection {
		return fmt.Errorf(
			"must be a protected authored write (body = \"authored\", " +
				"write = true, not public, a permission, no scope)",
		)
	}

	if operation.Output != "" || operation.Item != "" ||
		operation.Response != "" {
		return fmt.Errorf("must return no response")
	}

	for _, model := range m.Models {
		if model.ID != operation.Input {
			continue
		}

		if !slices.Equal(model.Fields, []string{"event:string"}) {
			return fmt.Errorf(
				"input model %s must have exactly one field, event:string",
				model.ID,
			)
		}

		return authEventUnreachable(m, operation)
	}

	return fmt.Errorf("requires an input model with the field event:string")
}

// authEventUnreachable keeps the operation reachable only from the generated
// hook: a public endpoint would let any member forge an audit event, and a
// job or agent would run it with chosen input.
func authEventUnreachable(m Manifest, operation Operation) error {
	for _, endpoint := range m.Endpoints {
		if endpoint.Operation == operation.ID() {
			return fmt.Errorf("must not be exposed by an endpoint")
		}
	}

	for _, job := range m.Jobs {
		if job.Operation == operation.ID() {
			return fmt.Errorf("must not be run by a job")
		}
	}

	for _, agent := range m.Agents {
		if agent.Operation == operation.ID() {
			return fmt.Errorf("must not be an agent tool")
		}
	}

	return nil
}

// validateAuthEventRoles requires every declared role to hold the operation's
// permission. A member whose role lacked it would be denied by the gate, and
// a fail-closed policy would then lock that member out of signing in.
func validateAuthEventRoles(m Manifest, operation Operation) error {
	byID := map[string]RoleContract{}
	for _, role := range m.Roles {
		byID[role.ID] = role
	}

	var holds func(id string, seen map[string]bool) bool

	holds = func(id string, seen map[string]bool) bool {
		role, ok := byID[id]
		if !ok || seen[id] {
			return false
		}

		seen[id] = true

		if slices.Contains(role.Permissions, operation.Permission) {
			return true
		}

		for _, parent := range role.Inherits {
			if holds(parent, seen) {
				return true
			}
		}

		return false
	}

	for _, role := range m.Roles {
		if !holds(role.ID, map[string]bool{}) {
			return fmt.Errorf(
				"role %s lacks permission %s that the hook needs for every "+
					"member",
				role.ID,
				operation.Permission,
			)
		}
	}

	return nil
}

// AuthEventOperation returns the declared event operation.
func (m Manifest) AuthEventOperation() (Operation, bool) {
	if !m.AuthEventsOn() {
		return Operation{}, false
	}

	return m.Operation(m.Auth.Events.Operation)
}

// IsAuthEventOperation reports the operation the generated hooks run; with
// machine principals declared it also runs for a machine enrollment.
func (m Manifest) IsAuthEventOperation(operation Operation) bool {
	event, ok := m.AuthEventOperation()

	return ok && event.ID() == operation.ID()
}
