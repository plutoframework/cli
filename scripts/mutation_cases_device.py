"""Mutation cases for [auth.device]."""

cases = [
    (
        "device scope names undeclared permission",
        "internal/project/manifest_authdevice.go",
        "if !permissions[permission] || seen[permission] {",
        "if seen[permission] {",
        "./internal/project#TestAuthDeviceValidation",
    ),
    (
        "device client names undeclared scope",
        "internal/project/manifest_authdevice.go",
        "if !scopes[scope] || seen[scope] {",
        "if seen[scope] {",
        "./internal/project#TestAuthDeviceValidation",
    ),
    (
        "device verification over plain http",
        "internal/project/manifest_authdevice.go",
        'if parsed.Scheme != "https" && (parsed.Scheme != "http" || !loopback) {',
        'if parsed.Scheme != "https" && (parsed.Scheme != "http" || loopback) {',
        "./internal/project#TestAuthDeviceValidation",
    ),
    (
        "device refresh outlives the grant",
        "internal/project/manifest_authdevice.go",
        "if d.Access > d.Refresh || d.Refresh > d.Max {",
        "if false {",
        "./internal/project#TestAuthDeviceValidation",
    ),
    (
        "device client claims interactive actor",
        "internal/project/manifest_authdevice.go",
        'if client.ActorType == "user" {',
        "if false {",
        "./internal/project#TestAuthDeviceValidation",
    ),
    (
        "device scope check removed from authored gate",
        "internal/project/templates/candidate_authored_gate.go.tmpl",
        "if err := caller.Permits({{q .Authored.Permission}}); err != nil {",
        "if false {",
        "./internal/project#TestCandidateDeviceGeneration",
    ),
    (
        "device scope check removed from operation gate",
        "internal/project/templates/candidate_gate.go.tmpl",
        "if err := caller.Permits({{q .Contract.Permission}}); err != nil {",
        "if false {",
        "./internal/project#TestCandidateDeviceGeneration",
    ),
    (
        "device delegate may provision tenants",
        "internal/project/templates/candidate_provision_gate.go.tmpl",
        "if _, delegated := identity.Delegation(); delegated {",
        "if false {",
        "./internal/project#TestCandidateDeviceGeneration",
    ),
    (
        "device grant importable by authored code",
        "internal/project/candidate_boundary.go",
        '"github.com/plutoframework/pluto-auth/device",\n\t\t) ||',
        '"disabled/device",\n\t\t) ||',
        "./internal/project#TestDeviceGrantIsCriticalForAuthoredCode",
    ),
    (
        "device scope check removed from stream gate",
        "internal/project/templates/candidate_stream_gate.go.tmpl",
        "if err := caller.Permits({{q .Authored.Permission}}); err != nil {",
        "if false {",
        "TestCandidateDeviceEndToEnd",
    ),
]
