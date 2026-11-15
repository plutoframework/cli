package project

import (
	"strings"
	"testing"
)

func TestOperationOwnedFeatureReachesEntries(t *testing.T) {
	m := manifestContractFixture(t)
	m.Operations[approveIndex(t, m)].Feature = "sales.approve"
	m.Operations[approveIndex(t, m)].ClientVisible = true

	if err := ValidateManifestContracts(m); err != nil {
		t.Fatal(err)
	}

	entry, err := m.OperationEndpoint(m.Endpoints[0])
	if err != nil {
		t.Fatal(err)
	}

	if entry.Feature != "sales.approve" || !entry.ClientVisible {
		t.Fatalf("entry did not inherit the operation flag: %+v", entry)
	}

	// A loaded manifest restates the flag on its entries; that must survive
	// another validation round-trip.
	m.Endpoints[0] = entry
	if err := ValidateManifestContracts(m); err != nil {
		t.Fatal(err)
	}
}

func TestEntryCannotChangeOperationFeature(t *testing.T) {
	m := manifestContractFixture(t)
	m.Operations[approveIndex(t, m)].Feature = "sales.approve"
	m.Endpoints[0].Feature = "other.flag"

	err := ValidateManifestContracts(m)
	if err == nil || !strings.Contains(err.Error(), "declared on the") {
		t.Fatalf("expected entry override rejection: %v", err)
	}
}

func TestOperationFeatureKeyAndVisibilityValidate(t *testing.T) {
	m := manifestContractFixture(t)
	m.Operations[approveIndex(t, m)].Feature = "Bad Key"

	if err := ValidateManifestContracts(m); err == nil {
		t.Fatal("expected invalid feature key rejection")
	}

	m = manifestContractFixture(t)
	m.Operations[approveIndex(t, m)].ClientVisible = true

	if err := ValidateManifestContracts(m); err == nil {
		t.Fatal("expected client-visible without feature rejection")
	}
}

func approveIndex(t *testing.T, m Manifest) int {
	t.Helper()

	for i, operation := range m.Operations {
		if operation.ID() == m.Endpoints[0].Operation {
			return i
		}
	}

	t.Fatal("fixture endpoint has no operation")

	return 0
}
