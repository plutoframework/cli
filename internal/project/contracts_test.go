package project

import (
	"fmt"
	"strings"
	"testing"
)

func contractFixture() (
	[]ResourceContract,
	[]OperationContract,
	[]EffectContract,
) {
	resources := []ResourceContract{
		{
			Group:        "sales",
			Name:         "Invoice",
			Table:        "invoices",
			Namespace:    "sales-invoices",
			Version:      1,
			Visibility:   "owner",
			InitialState: "draft",
			States:       []string{"draft", "approved"},
			Transitions: []ResourceTransition{
				{
					From:  "draft",
					Event: "approve",
					To:    "approved",
					Guard: "owner",
				},
			},
			Normalize:       "trim",
			MaxValueBytes:   4096,
			PageLimit:       50,
			ExportLimit:     500,
			CacheTTLSeconds: 60,
		},
	}
	operations := []OperationContract{
		{
			ID:             "sales.Read",
			Resource:       "sales.Invoice",
			Kind:           "load",
			Permission:     "invoice:read",
			ResourcePolicy: "owner",
			Idempotency:    "none",
		},
		{
			ID:             "sales.List",
			Resource:       "sales.Invoice",
			Kind:           "list",
			Permission:     "invoice:read",
			ResourcePolicy: "owner",
			Idempotency:    "none",
		},
		{
			ID:             "sales.Export",
			Resource:       "sales.Invoice",
			Kind:           "export",
			Permission:     "invoice:export",
			ResourcePolicy: "owner",
			Idempotency:    "none",
		},
		{
			ID:             "sales.Create",
			Resource:       "sales.Invoice",
			Kind:           "create",
			Permission:     "invoice:create",
			ResourcePolicy: "create",
			Idempotency:    "required",
		},
		{
			ID:             "sales.Approve",
			Resource:       "sales.Invoice",
			Kind:           "transition",
			Permission:     "invoice:approve",
			ResourcePolicy: "owner",
			Event:          "approve",
			Idempotency:    "required",
			Effects:        []string{"approval-notice"},
		},
		{
			ID:             "delivery.Notify",
			Resource:       "sales.Invoice",
			Kind:           "effect",
			Permission:     "invoice:notify",
			ResourcePolicy: "tenant",
			Idempotency:    "exempt",
			ExemptionReason: "The provider deduplicates the stable delivery " +
				"identifier.",
		},
	}
	effects := []EffectContract{
		{
			Name:        "approval-notice",
			Operation:   "delivery.Notify",
			Kind:        "invoice.approved",
			MaxAttempts: 5,
		},
	}

	return resources, operations, effects
}

func TestContractsBoundGeneratedResourceSize(t *testing.T) {
	r, o, e := contractFixture()
	for len(o) < 24 {
		copy := o[0]
		copy.ID = fmt.Sprintf("sales.Read%d", len(o))
		o = append(o, copy)
	}

	if err := ValidateContracts(r, o, e); err != nil {
		t.Fatal(err)
	}

	copy := o[0]
	copy.ID = "sales.Overflow"

	o = append(o, copy)
	if err := ValidateContracts(
		r,
		o,
		e,
	); err == nil ||
		!strings.Contains(err.Error(), "24 generated operations") {
		t.Fatalf("unbounded generation accepted: %v", err)
	}

	r, o, _ = contractFixture()
	e = nil
	o[4].Effects = nil

	for i := 0; i < 24; i++ {
		name := fmt.Sprintf("notice%d", i)
		e = append(
			e,
			EffectContract{
				Name:        name,
				Kind:        name,
				Operation:   "delivery.Notify",
				MaxAttempts: 3,
			},
		)
		o[4].Effects = append(o[4].Effects, name)
	}

	if err := ValidateContracts(r, o, e); err != nil {
		t.Fatal(err)
	}

	e = append(
		e,
		EffectContract{
			Name:        "overflow",
			Kind:        "overflow",
			Operation:   "delivery.Notify",
			MaxAttempts: 3,
		},
	)

	o[4].Effects = append(o[4].Effects, "overflow")
	if err := ValidateContracts(
		r,
		o,
		e,
	); err == nil ||
		!strings.Contains(err.Error(), "24 generated effects") {
		t.Fatalf("unbounded effects accepted: %v", err)
	}
}

func TestContractsBoundEffectPayloadExpansion(t *testing.T) {
	r, o, e := contractFixture()

	r[0].MaxValueBytes = 8192
	if err := ValidateContracts(r, o, e); err != nil {
		t.Fatal(err)
	}

	r[0].MaxValueBytes++
	if err := ValidateContracts(
		r,
		o,
		e,
	); err == nil ||
		!strings.Contains(err.Error(), "serialized delivery") {
		t.Fatalf("unbounded escaped payload accepted: %v", err)
	}

	o[4].Effects = nil
	e = nil

	r[0].MaxValueBytes = 65536
	if err := ValidateContracts(r, o, e); err != nil {
		t.Fatal(err)
	}
}

func TestContractsSupportDistinctExplicitResources(t *testing.T) {
	r, o, e := contractFixture()
	if err := ValidateContracts(r, o, e); err != nil {
		t.Fatal(err)
	}

	r = append(r, ResourceContract{
		Group:           "catalog",
		Name:            "Document",
		Table:           "documents",
		Namespace:       "catalog-documents",
		Version:         2,
		Visibility:      "tenant",
		InitialState:    "published",
		States:          []string{"published"},
		Normalize:       "none",
		MaxValueBytes:   1024,
		PageLimit:       25,
		ExportLimit:     100,
		CacheTTLSeconds: 0,
	})

	o = append(
		o,
		OperationContract{
			ID:             "catalog.Browse",
			Resource:       "catalog.Document",
			Kind:           "list",
			Permission:     "document:read",
			ResourcePolicy: "tenant",
			Idempotency:    "none",
		},
	)
	if err := ValidateContracts(r, o, e); err != nil {
		t.Fatal(err)
	}
}

func TestContractsRejectMissingAndContradictoryObligations(t *testing.T) {
	for _, test := range []struct {
		name, want string
		change     func(
			[]ResourceContract,
			[]OperationContract,
			[]EffectContract,
		)
	}{
		{"unsafe SQL", "safe SQL", func(
			r []ResourceContract,
			_ []OperationContract,
			_ []EffectContract,
		) {
			r[0].Table = "invoices;drop table tenants"
		}},
		{
			"namespace",
			"namespace",
			func(
				r []ResourceContract,
				_ []OperationContract,
				_ []EffectContract,
			) {
				r[0].Namespace = ""
			},
		},
		{
			"version",
			"version",
			func(
				r []ResourceContract,
				_ []OperationContract,
				_ []EffectContract,
			) {
				r[0].Version = 0
			},
		},
		{
			"normalization",
			"normalization",
			func(
				r []ResourceContract,
				_ []OperationContract,
				_ []EffectContract,
			) {
				r[0].Normalize = ""
			},
		},
		{
			"visibility",
			"visibility",
			func(
				r []ResourceContract,
				_ []OperationContract,
				_ []EffectContract,
			) {
				r[0].Visibility = ""
			},
		},
		{
			"unbounded page",
			"bounded",
			func(
				r []ResourceContract,
				_ []OperationContract,
				_ []EffectContract,
			) {
				r[0].PageLimit = 0
			},
		},
		{
			"unbounded export",
			"bounded",
			func(
				r []ResourceContract,
				_ []OperationContract,
				_ []EffectContract,
			) {
				r[0].ExportLimit = 10001
			},
		},
		{
			"unbounded cache",
			"bounded",
			func(
				r []ResourceContract,
				_ []OperationContract,
				_ []EffectContract,
			) {
				r[0].CacheTTLSeconds = 3601
			},
		},
		{
			"initial state",
			"initial state",
			func(
				r []ResourceContract,
				_ []OperationContract,
				_ []EffectContract,
			) {
				r[0].InitialState = "unknown"
			},
		},
		{"ambiguous transition", "nondeterministic", func(
			r []ResourceContract,
			_ []OperationContract,
			_ []EffectContract,
		) {
			r[0].Transitions = append(r[0].Transitions, ResourceTransition{
				From:  "draft",
				Event: "approve",
				To:    "draft",
			})
		}},
		{"unknown transition target", "undeclared", func(
			r []ResourceContract,
			_ []OperationContract,
			_ []EffectContract,
		) {
			r[0].Transitions[0].To = "missing"
		}},
		{
			"missing transition guard",
			"transition guard",
			func(
				r []ResourceContract,
				_ []OperationContract,
				_ []EffectContract,
			) {
				r[0].Transitions[0].Guard = ""
			},
		},
		{"unknown transition guard", "transition guard", func(
			r []ResourceContract,
			_ []OperationContract,
			_ []EffectContract,
		) {
			r[0].Transitions[0].Guard = "sometimes"
		}},
		{"inconsistent event guards", "consistent guard", func(
			r []ResourceContract,
			_ []OperationContract,
			_ []EffectContract,
		) {
			r[0].Transitions = append(r[0].Transitions, ResourceTransition{
				From:  "approved",
				Event: "approve",
				To:    "draft",
				Guard: "not_owner",
			})
		}},
		{
			"unknown resource",
			"undeclared resource",
			func(
				_ []ResourceContract,
				o []OperationContract,
				_ []EffectContract,
			) {
				o[0].Resource = "other.Missing"
			},
		},
		{
			"public resource",
			"protected",
			func(
				_ []ResourceContract,
				o []OperationContract,
				_ []EffectContract,
			) {
				o[0].Public = true
			},
		},
		{
			"missing permission",
			"permission",
			func(
				_ []ResourceContract,
				o []OperationContract,
				_ []EffectContract,
			) {
				o[0].Permission = ""
			},
		},
		{
			"missing policy",
			"resource policy",
			func(
				_ []ResourceContract,
				o []OperationContract,
				_ []EffectContract,
			) {
				o[0].ResourcePolicy = ""
			},
		},
		{
			"visibility bypass",
			"query visibility",
			func(
				_ []ResourceContract,
				o []OperationContract,
				_ []EffectContract,
			) {
				o[1].ResourcePolicy = "tenant"
			},
		},
		{
			"wrong creation policy",
			"create policy",
			func(
				_ []ResourceContract,
				o []OperationContract,
				_ []EffectContract,
			) {
				o[3].ResourcePolicy = "owner"
			},
		},
		{
			"unknown event",
			"transition event",
			func(
				_ []ResourceContract,
				o []OperationContract,
				_ []EffectContract,
			) {
				o[4].Event = "delete"
			},
		},
		{"contradictory owner guard", "contradicts", func(
			r []ResourceContract,
			_ []OperationContract,
			_ []EffectContract,
		) {
			r[0].Transitions[0].Guard = "not_owner"
		}},
		{
			"read event",
			"non-transition",
			func(
				_ []ResourceContract,
				o []OperationContract,
				_ []EffectContract,
			) {
				o[0].Event = "approve"
			},
		},
		{
			"missing replay",
			"replay semantics",
			func(
				_ []ResourceContract,
				o []OperationContract,
				_ []EffectContract,
			) {
				o[4].Idempotency = "none"
			},
		},
		{
			"empty exemption",
			"exemption reason",
			func(
				_ []ResourceContract,
				o []OperationContract,
				_ []EffectContract,
			) {
				o[5].ExemptionReason = ""
			},
		},
		{"reason without exemption", "without exemption", func(
			_ []ResourceContract,
			o []OperationContract,
			_ []EffectContract,
		) {
			o[3].ExemptionReason = "Ignored because there is no exemption."
		}},
		{"read delivery", "atomic mutation", func(
			_ []ResourceContract,
			o []OperationContract,
			_ []EffectContract,
		) {
			o[0].Effects = []string{"approval-notice"}
		}},
		{"undeclared delivery", "undeclared", func(
			_ []ResourceContract,
			o []OperationContract,
			_ []EffectContract,
		) {
			o[4].Effects = []string{"unknown"}
		}},
		{"delivery duplication", "duplicate effect", func(
			_ []ResourceContract,
			o []OperationContract,
			_ []EffectContract,
		) {
			o[4].Effects = append(o[4].Effects, "approval-notice")
		}},
		{
			"unbounded retries",
			"finite",
			func(
				_ []ResourceContract,
				_ []OperationContract,
				e []EffectContract,
			) {
				e[0].MaxAttempts = 0
			},
		},
		{"recursive effect", "effect operation", func(
			_ []ResourceContract,
			_ []OperationContract,
			e []EffectContract,
		) {
			e[0].Operation = "sales.Approve"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			r, o, e := contractFixture()
			test.change(r, o, e)

			if err := ValidateContracts(
				r,
				o,
				e,
			); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q rejection, got %v", test.want, err)
			}
		})
	}
}

func TestContractsRejectCollisionsAndOrphanedEffects(t *testing.T) {
	for _, kind := range []string{
		"empty",
		"resource",
		"table",
		"namespace",
		"operation",
		"effect",
		"orphan",
	} {
		t.Run(kind, func(t *testing.T) {
			r, o, e := contractFixture()

			switch kind {
			case "empty":
				r = nil
			case "resource":
				r = append(r, r[0])
			case "table":
				copy := r[0]
				copy.Name = "Other"
				copy.Namespace = "other"
				r = append(r, copy)
			case "namespace":
				copy := r[0]
				copy.Name = "Other"
				copy.Table = "other"
				r = append(r, copy)
			case "operation":
				o = append(o, o[0])
			case "effect":
				e = append(e, e[0])
			case "orphan":
				o[4].Effects = nil
			}

			if err := ValidateContracts(r, o, e); err == nil {
				t.Fatal("unsupported contract accepted")
			}
		})
	}
}

func TestContractsReserveFrameworkTables(t *testing.T) {
	for _, table := range []string{
		"pluto_jobs",
		"pluto_idempotency",
		"pluto_resource_audit",
		"pluto_future",
	} {
		r, o, e := contractFixture()

		r[0].Table = table
		if err := ValidateContracts(
			r,
			o,
			e,
		); err == nil ||
			!strings.Contains(err.Error(), "reserved framework namespace") {
			t.Fatalf("reserved table %s accepted: %v", table, err)
		}
	}
}

func TestContractsShareCanonicalAuthorityRules(t *testing.T) {
	r, o, e := contractFixture()

	o[0].Permission = "!!!!!!"
	if err := ValidateContracts(
		r,
		o,
		e,
	); err == nil ||
		!strings.Contains(err.Error(), "invalid permission") {
		t.Fatalf("invalid permission accepted: %v", err)
	}

	r, o, e = contractFixture()

	r[0].Group = "context"
	if err := ValidateContracts(r, o, e); err == nil {
		t.Fatal("reserved owner accepted")
	}

	r, o, e = contractFixture()

	r[0].PageLimit = 101
	if err := ValidateContracts(
		r,
		o,
		e,
	); err == nil ||
		!strings.Contains(err.Error(), "bounded") {
		t.Fatalf("unsupported Core page limit accepted: %v", err)
	}
}
