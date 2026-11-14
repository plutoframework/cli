package project

import (
	"go/token"
	"strconv"
	"strings"
)

// candidateSeamPassword is the only Pluto package an authored credential
// integration may import: it supplies Sender, AttemptLimiter, Notification and
// PasswordPolicy types. Sessions, identities, stores, memberships and every
// application package stay unreachable, so the seam cannot issue authority.
const candidateSeamPassword = "github.com/plutoframework/pluto-auth/" +
	"methods/password"

func inspectCandidateCredentialSeam(
	file *candidateSource,
	module string,
	emit func(id string, pos token.Pos, message, repair string),
) {
	for _, spec := range file.tree.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}

		if candidateSeamImportAllowed(path, module) {
			continue
		}

		emit(
			"ARCH609",
			spec.Pos(),
			"authored credential integration reaches a restricted "+
				"capability: "+path,
			"Keep the seam to delivery, ingress, attempt limiting and "+
				"password policy. Use the standard library, third-party "+
				"delivery clients and pluto-auth/methods/password; the "+
				"managed runtime owns sessions, identities and data.",
		)
	}
}

func candidateSeamImportAllowed(path, module string) bool {
	switch {
	case path == "C" || path == "unsafe" || path == "reflect" ||
		path == "plugin":
		return false
	case path == module || strings.HasPrefix(path, module+"/"):
		return false
	case path == candidateSeamPassword:
		return true
	case path == "github.com/plutoframework" ||
		strings.HasPrefix(path, "github.com/plutoframework/"):
		return false
	case candidateDataPackage(path):
		return false
	}

	return true
}
