package project

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

// Streaming endpoints. An authored operation with response = "stream" is
// exposed only through an endpoint with transport = "websocket". Generated
// code owns the connection: the upgrade, origin check, size limits, ping/pong,
// idle timeout, backpressure, periodic gate rechecks and the close handshake.
// The authored body sees only a typed scope.Stream of declared models.

const (
	TransportWebSocket = "websocket"

	defaultStreamMessageBytes = 64 * 1024
	defaultStreamWriteSeconds = 10
	defaultStreamPingSeconds  = 20
	defaultStreamIdleSeconds  = 60
	defaultStreamRecheck      = 15
	defaultStreamConnections  = 256

	maxStreamMessageBytes = 1 << 20
	maxStreamConnections  = 100000
)

// StreamLimits are the optional resource bounds of one stream operation, set
// in [operations.stream]. Zero fields take the documented defaults.
type StreamLimits struct {
	MaxMessageBytes int `toml:"max_message_bytes,omitempty"`
	WriteSeconds    int `toml:"write_seconds,omitempty"`
	PingSeconds     int `toml:"ping_seconds,omitempty"`
	IdleSeconds     int `toml:"idle_seconds,omitempty"`
	RecheckSeconds  int `toml:"recheck_seconds,omitempty"`
	MaxConnections  int `toml:"max_connections,omitempty"`
}

// Resolved returns the limits with defaults applied.
func (l StreamLimits) Resolved() StreamLimits {
	fill := func(value *int, fallback int) {
		if *value == 0 {
			*value = fallback
		}
	}

	fill(&l.MaxMessageBytes, defaultStreamMessageBytes)
	fill(&l.WriteSeconds, defaultStreamWriteSeconds)
	fill(&l.PingSeconds, defaultStreamPingSeconds)
	fill(&l.IdleSeconds, defaultStreamIdleSeconds)
	fill(&l.RecheckSeconds, defaultStreamRecheck)
	fill(&l.MaxConnections, defaultStreamConnections)

	return l
}

// StreamSettings returns the resolved limits of a stream operation.
func (o Operation) StreamSettings() StreamLimits {
	if o.Stream == nil {
		return StreamLimits{}.Resolved()
	}

	return o.Stream.Resolved()
}

var originPattern = regexp.MustCompile(
	`^(?:\*\.)?[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?(?::[0-9]{1,5})?$`,
)

// websocketLibraries cannot be authored dependencies: authored code never
// holds a raw connection, and no library gives it a way to open one.
var websocketLibraries = []string{
	"github.com/coder/websocket",
	"nhooyr.io/websocket",
	"github.com/gorilla/websocket",
	"golang.org/x/net/websocket",
}

// HasStream tells the scope and paths templates to render the stream
// runtime and catalog entries.
func (d templateData) HasStream() bool {
	return d.Manifest.hasStreams()
}

func (m Manifest) hasStreams() bool {
	for _, operation := range m.Operations {
		if operation.Response == ResponseStream {
			return true
		}
	}

	return false
}

func (o Operation) isStream() bool {
	return o.Response == ResponseStream
}

func (l StreamLimits) validate() error {
	check := func(name string, value, low, high int) error {
		if value != 0 && (value < low || value > high) {
			return fmt.Errorf(
				"stream %s must be between %d and %d", name, low, high,
			)
		}

		return nil
	}

	for _, item := range []struct {
		name            string
		value, low, max int
	}{
		{"max_message_bytes", l.MaxMessageBytes, 1, maxStreamMessageBytes},
		{"write_seconds", l.WriteSeconds, 1, 60},
		{"ping_seconds", l.PingSeconds, 1, 300},
		{"idle_seconds", l.IdleSeconds, 2, 3600},
		{"recheck_seconds", l.RecheckSeconds, 1, 300},
		{"max_connections", l.MaxConnections, 1, maxStreamConnections},
	} {
		if err := check(item.name, item.value, item.low, item.max); err != nil {
			return err
		}
	}

	resolved := l.Resolved()
	if resolved.IdleSeconds <= resolved.PingSeconds {
		return fmt.Errorf("stream idle_seconds must exceed ping_seconds")
	}

	return nil
}

// validateStreamOperation checks one operation's stream declaration. It runs
// for candidate authored operations only.
func (o Operation) validateStreamOperation() error {
	if !o.isStream() {
		if o.Stream != nil {
			return fmt.Errorf("[operations.stream] requires response = " +
				"\"stream\"")
		}

		return nil
	}

	if o.Body != BodyAuthored || o.Scope != "" {
		return fmt.Errorf(
			"stream responses require an authored body without a system " +
				"scope",
		)
	}

	if o.Public || o.Collection {
		return fmt.Errorf("stream operations must be protected and " +
			"non-collection; the verified caller scopes every stream")
	}

	if o.Write && (o.Idempotency != "exempt" || o.ReplayReason == "") {
		return fmt.Errorf(
			"a stream that writes requires idempotency = \"exempt\" with " +
				"a replay_reason",
		)
	}

	if o.Stream != nil {
		return o.Stream.validate()
	}

	return nil
}

// validateStreams checks the endpoint side: a stream operation is reachable
// only through exactly one websocket endpoint, and a websocket endpoint
// carries only a stream operation.
func validateStreams(m Manifest) error {
	exposed := map[string]int{}

	for _, entry := range m.Endpoints {
		operation, ok := m.Operation(entry.Operation)
		if !ok {
			continue
		}

		websocket := entry.Transport == TransportWebSocket

		switch {
		case entry.Transport != "" && !websocket:
			return fmt.Errorf(
				"endpoint %s.%s transport must be %q or omitted",
				entry.Group, entry.Action, TransportWebSocket,
			)
		case websocket && !operation.isStream():
			return fmt.Errorf(
				"endpoint %s.%s: transport = \"websocket\" requires an "+
					"operation with response = \"stream\"",
				entry.Group, entry.Action,
			)
		case !websocket && operation.isStream():
			return fmt.Errorf(
				"endpoint %s.%s: stream operation %s must be exposed with "+
					"transport = \"websocket\"",
				entry.Group, entry.Action, operation.ID(),
			)
		case !websocket && len(entry.Origins) != 0:
			return fmt.Errorf(
				"endpoint %s.%s: origins require transport = \"websocket\"",
				entry.Group, entry.Action,
			)
		}

		if websocket {
			exposed[operation.ID()]++
		}
	}

	for _, operation := range m.Operations {
		if operation.isStream() && exposed[operation.ID()] != 1 {
			return fmt.Errorf(
				"stream operation %s requires exactly one websocket endpoint",
				operation.ID(),
			)
		}
	}

	return nil
}

// validateEndpointOrigins checks the browser origin allowlist of a websocket
// endpoint. Without origins only same-host browsers connect; clients that
// send no Origin header (service clients) are never browsers and pass.
func (e Endpoint) validateWebSocket() error {
	if e.Transport == "" {
		return nil
	}

	if e.Transport != TransportWebSocket {
		return fmt.Errorf("transport must be %q or omitted", TransportWebSocket)
	}

	if e.Method != "GET" || e.Collection || e.Public {
		return fmt.Errorf(
			"a websocket endpoint requires method GET and a protected, " +
				"non-collection operation",
		)
	}

	if len(e.Origins) > 16 {
		return fmt.Errorf("origins allows at most 16 host patterns")
	}

	seen := map[string]bool{}

	for _, origin := range e.Origins {
		if !originPattern.MatchString(origin) || seen[origin] ||
			strings.HasPrefix(origin, "*") && !strings.HasPrefix(origin, "*.") {
			return fmt.Errorf(
				"origins entries must be distinct host patterns such as "+
					"app.example.com or *.example.com, got %q",
				origin,
			)
		}

		seen[origin] = true
	}

	return nil
}

func rejectWebSocketDependencies(authored *Authored) error {
	if authored == nil {
		return nil
	}

	for _, dependency := range authored.Dependencies {
		for _, library := range websocketLibraries {
			if dependency.Module == library ||
				strings.HasPrefix(dependency.Module, library+"/") {
				return fmt.Errorf(
					"authored dependency %s is a websocket library; "+
						"authored code never holds a connection, declare a "+
						"stream operation instead",
					dependency.Module,
				)
			}
		}
	}

	return nil
}

const (
	websocketModule  = "github.com/coder/websocket"
	websocketVersion = "v1.8.14"

	websocketGoSum = "github.com/coder/websocket " + websocketVersion +
		" h1:9L0p0iKiNOibykf283eHkKUHHrpG7f65OE3BhhO7v9g=\n" +
		"github.com/coder/websocket " + websocketVersion +
		"/go.mod h1:NX3SzP+inril6yawo5CQXx8+fk145lPDC6pumgx0mVg=\n"
)

// EnsureStreamModule adds the pinned WebSocket library to go.mod and its
// checksums to go.sum when the manifest declares a stream and the application
// does not require it yet. The library is imported only by generated code; an
// existing requirement, whatever its version, is left to the application.
func EnsureStreamModule(m Manifest, goMod, goSum []byte) ([]byte, []byte) {
	if !m.hasStreams() || strings.Contains(string(goMod), websocketModule+" ") {
		return goMod, goSum
	}

	goMod = append(
		bytes.TrimRight(goMod, "\n"),
		[]byte("\n\nrequire "+websocketModule+" "+websocketVersion+"\n")...,
	)

	if !strings.Contains(string(goSum), websocketModule+" "+websocketVersion+" ") {
		goSum = append(goSum, []byte(websocketGoSum)...)
	}

	return goMod, goSum
}
