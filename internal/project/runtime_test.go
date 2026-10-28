package project

import "testing"

func TestRuntimePolicies(t *testing.T) {
	base := Endpoint{
		Group:       "items",
		Action:      "Create",
		Method:      "POST",
		Path:        "/api/items",
		Name:        "Create item",
		Description: "Create an authorized item.",
	}
	if err := base.Validate(false); err != nil || !base.RequiresIdempotency() {
		t.Fatal(base, err)
	}

	exempt := base

	exempt.Idempotency = "exempt"
	if err := exempt.Validate(
		false,
	); err != nil ||
		exempt.RequiresIdempotency() {
		t.Fatal(exempt, err)
	}

	public := base

	public.Public = true
	if err := public.Validate(
		false,
	); err != nil ||
		public.RequiresIdempotency() {
		t.Fatal("public cache enabled", public, err)
	}

	for _, change := range []func(*Endpoint){
		func(e *Endpoint) { e.Collection = true },
		func(e *Endpoint) { e.Public = true; e.Idempotency = "required" },
		func(e *Endpoint) { e.Method = "GET"; e.Idempotency = "required" },
		func(e *Endpoint) { e.Idempotency = "typo" },
	} {
		invalid := base
		change(&invalid)

		if err := invalid.Validate(false); err == nil {
			t.Fatal("invalid runtime policy accepted", invalid)
		}
	}

	list := base
	list.Method = "GET"

	list.Collection = true
	if err := list.Validate(false); err != nil || list.RequiresIdempotency() {
		t.Fatal(list, err)
	}
}
