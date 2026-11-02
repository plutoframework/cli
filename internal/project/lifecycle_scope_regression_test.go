package project

import (
	"os"
	"path/filepath"
	"testing"
)

const lifecycleScopeDomain = `package domain

import state "github.com/plutoframework/pluto-core/state"

type Phase string
type Event string
type Workflow = state.Machine[Phase, Event, bool]
type Forward = Workflow
type CycleA = CycleB
type CycleB = CycleA

type Cursor struct{}

func (*Cursor) Next() {}
`

func TestLifecycleLexicalBindings(t *testing.T) {
	cases := []struct {
		name, declaration string
		want              int
	}{
		{
			"imported alias expression",
			"var next = (*domain.Workflow).Next",
			1,
		},
		{
			"imported alias chain",
			"var next = (*domain.Forward).Next",
			1,
		},
		{
			"import alias shadowed by value",
			`type Holder struct {
	Workflow *domain.Cursor
}

func Run(domain Holder) {
	domain.Workflow.Next()
}`,
			0,
		},
		{
			"nonmachine expression",
			"var next = (*domain.Cursor).Next",
			0,
		},
		{
			"alias cycle terminates",
			"var next = (*domain.CycleA).Next",
			0,
		},
		{
			"named result",
			`func Build() (m *domain.Workflow) {
	_ = m.Next
	return
}`,
			1,
		},
		{
			"block shadow restored",
			`func Run(cursor *domain.Cursor, m *domain.Workflow) {
	{
		cursor := m
		_ = cursor
	}
	cursor.Next()
}`,
			0,
		},
		{
			"machine restored after cursor shadow",
			`func Run(m *domain.Workflow, cursor *domain.Cursor) {
	{
		m := cursor
		m.Next()
	}
	_ = m.Next
}`,
			1,
		},
		{
			"if initializer scope restored",
			`func Run(cursor *domain.Cursor, m *domain.Workflow) {
	if cursor := m; cursor != nil {
		_ = cursor.Next
	}
	cursor.Next()
}`,
			1,
		},
		{
			"switch initializer scope restored",
			`func Run(cursor *domain.Cursor, m *domain.Workflow) {
	switch cursor := m; true {
	case true:
		_ = cursor.Next
	}
	cursor.Next()
}`,
			1,
		},
		{
			"case shadow stays in case",
			`func Run(cursor *domain.Cursor, m *domain.Workflow) {
	switch true {
	case true:
		cursor := m
		_ = cursor.Next
	default:
		cursor.Next()
	}
	cursor.Next()
}`,
			1,
		},
		{
			"loop initializer scope restored",
			`func Run(cursor *domain.Cursor, m *domain.Workflow) {
	for cursor := m; cursor != nil; {
		_ = cursor.Next
		break
	}
	cursor.Next()
}`,
			1,
		},
		{
			"range variable shadows machine",
			`func Run(m *domain.Workflow, cursors []*domain.Cursor) {
	for _, m := range cursors {
		m.Next()
	}
	_ = m.Next
}`,
			1,
		},
		{
			"initializer sees outer binding",
			`func Run(m *domain.Workflow) {
	{
		var m = m.Next
		_ = m
	}
}`,
			1,
		},
		{
			"literal parameter shadows capture",
			`func Run(m *domain.Workflow) {
	_ = func(m *domain.Cursor) {
		m.Next()
	}
	_ = m.Next
}`,
			1,
		},
		{
			"literal named result",
			`var build = func() (m *domain.Workflow) {
	_ = m.Next
	return
}`,
			1,
		},
		{
			"literal inherits capture",
			`func Run(m *domain.Workflow) {
	_ = func() {
		_ = m.Next
	}
}`,
			1,
		},
		{
			"unknown local shadows package variable",
			`var m *domain.Workflow

func Run() {
	m := 1
	_ = m.Next
}`,
			0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := lifecycleFixture(t, map[string]string{
				"internal/domain/order.go": lifecycleScopeDomain,
				"internal/operations/run.go": `package operations

import "example.test/app/internal/domain"

` + tc.declaration + "\n",
			})
			diagnostics, _ := inspectLifecycleBoundaries(
				root,
				"example.test/app",
			)
			got := 0

			for _, diagnostic := range diagnostics {
				if diagnostic.ID == "ARCH302" {
					got++
				}
			}

			if got != tc.want {
				t.Fatalf("ARCH302 count = %d, want %d: %+v", got, tc.want,
					diagnostics)
			}
		})
	}
}

func TestLifecycleInventoryIncludesExplicitRootBasename(t *testing.T) {
	for _, name := range []string{"dist", "testdata"} {
		t.Run(name, func(t *testing.T) {
			root := lifecycleFixture(t, map[string]string{
				"internal/domain/order.go": lifecycleScopeDomain,
				"internal/operations/run.go": `package operations

import "example.test/app/internal/domain"

var next = (*domain.Workflow).Next
`,
			})

			target := filepath.Join(t.TempDir(), name)
			if err := os.Rename(root, target); err != nil {
				t.Fatal(err)
			}

			diagnostics, _ := inspectLifecycleBoundaries(
				target,
				"example.test/app",
			)
			if len(diagnostics) != 1 || diagnostics[0].ID != "ARCH302" {
				t.Fatalf(
					"root basename hid lifecycle boundary: %+v",
					diagnostics,
				)
			}
		})
	}
}
