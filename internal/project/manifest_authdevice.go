package project

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"time"
)

// Device grant defaults and bounds. Every bound exists so a manifest cannot
// declare a long-lived, unrotated or effectively unlimited delegated token.
const (
	deviceCodeTTLDefault     = 10 * time.Minute
	devicePollDefault        = 5 * time.Second
	deviceAccessTTLDefault   = 15 * time.Minute
	deviceRefreshTTLDefault  = 30 * 24 * time.Hour
	deviceMaxLifetimeDefault = 90 * 24 * time.Hour
	deviceMaxClients         = 16
	deviceMaxScopes          = 32
)

var (
	deviceScopeName = regexp.MustCompile(`^[a-z][a-z0-9_.:-]{0,63}$`)
	deviceClientID  = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)
	deviceActorType = regexp.MustCompile(`^[a-z][a-z0-9_]{0,30}$`)
)

// AuthSection groups declarations about managed authentication.
type AuthSection struct {
	Device *AuthDevice `toml:"device,omitempty"`
}

// AuthDevice declares the OAuth 2.0 device authorization grant (RFC 8628)
// for delegated clients such as a CLI or an agent. The owner approves in a
// browser session; the client receives scoped, short-lived access tokens and
// rotating refresh tokens. A scope is a name for a set of declared operation
// permissions: a delegated request is allowed only when the operation's
// permission is in the token's scopes AND the owner's own live membership and
// roles grant it. Scopes can only narrow, never widen.
type AuthDevice struct {

	// VerificationURI is the browser page where the owner enters the code.
	VerificationURI string `toml:"verification_uri"`

	CodeTTL      string `toml:"code_ttl,omitempty"`
	PollInterval string `toml:"poll_interval,omitempty"`
	AccessTTL    string `toml:"access_ttl,omitempty"`
	RefreshTTL   string `toml:"refresh_ttl,omitempty"`
	MaxLifetime  string `toml:"max_lifetime,omitempty"`

	Scopes  []DeviceScope  `toml:"scopes"`
	Clients []DeviceClient `toml:"clients"`
}

// DeviceScope maps a scope name to declared operation permissions.
type DeviceScope struct {
	Name        string   `toml:"name"`
	Description string   `toml:"description,omitempty"`
	Permissions []string `toml:"permissions"`
}

// DeviceClient is a declared public client. ActorType is carried on every
// delegated identity (for example "agent") for authored code and audit.
type DeviceClient struct {
	ID        string   `toml:"id"`
	Name      string   `toml:"name"`
	ActorType string   `toml:"actor_type"`
	Scopes    []string `toml:"scopes"`
}

// DeviceDurations are the effective durations after defaults.
type DeviceDurations struct {
	Code, Poll, Access, Refresh, Max time.Duration
}

// Go renders a duration as a Go expression in whole milliseconds.
func (DeviceDurations) Go(d time.Duration) string {
	return fmt.Sprintf("%d * time.Millisecond", d.Milliseconds())
}

// DeviceOn reports whether the manifest declares the device grant.
func (m Manifest) DeviceOn() bool {
	return m.Auth != nil && m.Auth.Device != nil
}

// Durations resolves the declared durations; validation has checked them.
func (d AuthDevice) Durations() DeviceDurations {
	pick := func(text string, fallback time.Duration) time.Duration {
		if text == "" {
			return fallback
		}

		value, err := time.ParseDuration(text)
		if err != nil {
			return fallback
		}

		return value
	}

	return DeviceDurations{
		Code:    pick(d.CodeTTL, deviceCodeTTLDefault),
		Poll:    pick(d.PollInterval, devicePollDefault),
		Access:  pick(d.AccessTTL, deviceAccessTTLDefault),
		Refresh: pick(d.RefreshTTL, deviceRefreshTTLDefault),
		Max:     pick(d.MaxLifetime, deviceMaxLifetimeDefault),
	}
}

// ScopePermissions lists scopes with their permissions, sorted by name.
func (d AuthDevice) ScopePermissions() []DeviceScope {
	scopes := slices.Clone(d.Scopes)
	for i := range scopes {
		scopes[i].Permissions = slices.Clone(scopes[i].Permissions)
		sort.Strings(scopes[i].Permissions)
	}

	sort.Slice(scopes, func(i, j int) bool {
		return scopes[i].Name < scopes[j].Name
	})

	return scopes
}

// SortedClients lists clients by id with sorted scopes.
func (d AuthDevice) SortedClients() []DeviceClient {
	clients := slices.Clone(d.Clients)
	for i := range clients {
		clients[i].Scopes = slices.Clone(clients[i].Scopes)
		sort.Strings(clients[i].Scopes)
	}

	sort.Slice(clients, func(i, j int) bool {
		return clients[i].ID < clients[j].ID
	})

	return clients
}

func validateAuthDevice(m Manifest) error {
	if !m.DeviceOn() {
		return nil
	}

	device := *m.Auth.Device

	if m.Boundary.Mode != "candidate" {
		return fmt.Errorf(
			"[auth.device] requires the candidate boundary profile")
	}

	if m.Capabilities.Tenancy != "multi" {
		return fmt.Errorf("[auth.device] requires multi tenancy: a token is " +
			"bound to one verified tenant")
	}

	if !m.Capabilities.LocalCredentials {
		return fmt.Errorf("[auth.device] requires --local-credentials: the " +
			"owner approves in a signed-in browser session")
	}

	if err := validateDeviceURI(device.VerificationURI); err != nil {
		return err
	}

	if err := validateDeviceDurations(device); err != nil {
		return err
	}

	permissions := map[string]bool{}

	for _, operation := range m.Operations {
		if operation.Permission != "" {
			permissions[operation.Permission] = true
		}
	}

	scopes, err := validateDeviceScopes(device, permissions)
	if err != nil {
		return err
	}

	return validateDeviceClients(device, scopes)
}

func validateDeviceURI(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || len(raw) > 500 {
		return fmt.Errorf(
			"[auth.device] verification_uri must be an absolute " +
				"URL without credentials, query or fragment",
		)
	}

	loopback := parsed.Hostname() == "localhost" ||
		parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "::1"
	if parsed.Scheme != "https" && (parsed.Scheme != "http" || !loopback) {
		return fmt.Errorf("[auth.device] verification_uri must use https " +
			"(http only for loopback development)")
	}

	return nil
}

func validateDeviceDurations(device AuthDevice) error {
	for name, text := range map[string]string{
		"code_ttl": device.CodeTTL, "poll_interval": device.PollInterval,
		"access_ttl": device.AccessTTL, "refresh_ttl": device.RefreshTTL,
		"max_lifetime": device.MaxLifetime,
	} {
		if text == "" {
			continue
		}

		if _, err := time.ParseDuration(text); err != nil {
			return fmt.Errorf("[auth.device] %s: %w", name, err)
		}
	}

	d := device.Durations()

	for _, bound := range []struct {
		name     string
		value    time.Duration
		min, max time.Duration
	}{
		{"code_ttl", d.Code, time.Minute, 30 * time.Minute},
		{"poll_interval", d.Poll, time.Second, time.Minute},
		{"access_ttl", d.Access, time.Minute, time.Hour},
		{"refresh_ttl", d.Refresh, time.Hour, 90 * 24 * time.Hour},
		{"max_lifetime", d.Max, time.Hour, 365 * 24 * time.Hour},
	} {
		if bound.value < bound.min || bound.value > bound.max {
			return fmt.Errorf("[auth.device] %s must be between %s and %s",
				bound.name, bound.min, bound.max)
		}
	}

	if d.Access > d.Refresh || d.Refresh > d.Max {
		return fmt.Errorf("[auth.device] access_ttl <= refresh_ttl <= " +
			"max_lifetime is required")
	}

	return nil
}

func validateDeviceScopes(
	device AuthDevice,
	permissions map[string]bool,
) (map[string]bool, error) {
	if len(device.Scopes) == 0 || len(device.Scopes) > deviceMaxScopes {
		return nil, fmt.Errorf("[auth.device] declare 1..%d scopes",
			deviceMaxScopes)
	}

	scopes := map[string]bool{}

	for _, scope := range device.Scopes {
		if !deviceScopeName.MatchString(scope.Name) || scopes[scope.Name] {
			return nil, fmt.Errorf(
				"[auth.device] scope %q is invalid or duplicated", scope.Name)
		}

		scopes[scope.Name] = true

		if len(scope.Permissions) == 0 {
			return nil, fmt.Errorf(
				"[auth.device] scope %s needs permissions", scope.Name)
		}

		seen := map[string]bool{}

		for _, permission := range scope.Permissions {
			if !permissions[permission] || seen[permission] {
				return nil, fmt.Errorf(
					"[auth.device] scope %s names %q, which "+
						"is not a declared operation permission (or repeats)",
					scope.Name,
					permission,
				)
			}

			seen[permission] = true
		}
	}

	return scopes, nil
}

func validateDeviceClients(
	device AuthDevice,
	scopes map[string]bool,
) error {
	if len(device.Clients) == 0 || len(device.Clients) > deviceMaxClients {
		return fmt.Errorf("[auth.device] declare 1..%d clients",
			deviceMaxClients)
	}

	clients := map[string]bool{}

	for _, client := range device.Clients {
		if !deviceClientID.MatchString(client.ID) || clients[client.ID] {
			return fmt.Errorf(
				"[auth.device] client %q is invalid or duplicated", client.ID)
		}

		clients[client.ID] = true

		if client.Name == "" || len(client.Name) > 100 ||
			!deviceActorType.MatchString(client.ActorType) {
			return fmt.Errorf("[auth.device] client %s needs a name (<=100 "+
				"bytes) and a lowercase actor_type", client.ID)
		}

		if client.ActorType == "user" {
			return fmt.Errorf("[auth.device] client %s: actor_type \"user\" "+
				"is reserved for interactive sessions", client.ID)
		}

		if len(client.Scopes) == 0 {
			return fmt.Errorf(
				"[auth.device] client %s needs scopes", client.ID)
		}

		seen := map[string]bool{}

		for _, scope := range client.Scopes {
			if !scopes[scope] || seen[scope] {
				return fmt.Errorf(
					"[auth.device] client %s names undeclared or "+
						"repeated scope %q",
					client.ID,
					scope,
				)
			}

			seen[scope] = true
		}
	}

	return nil
}
