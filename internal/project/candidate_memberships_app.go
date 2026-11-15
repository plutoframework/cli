package project

import (
	"fmt"
	"strconv"
	"strings"
)

// candidateAppMemberships renders the membership verifier and role resolver
// over the app-owned table declared in [memberships]. The tenant is bound per
// transaction through app.tenant_id so FORCE row-level security applies to
// the lookup.
func candidateAppMemberships(m Manifest) ([]byte, error) {
	if m.Memberships == nil {
		return nil, fmt.Errorf("memberships are not declared")
	}

	var table *Table

	for index := range m.Tables {
		if m.Tables[index].ID() == m.Memberships.Table {
			table = &m.Tables[index]
		}
	}

	if table == nil {
		return nil, fmt.Errorf(
			"memberships table %q must be a declared table",
			m.Memberships.Table,
		)
	}

	query := "SELECT " + quoteIdent(m.Memberships.RoleColumn) +
		" FROM " + quoteIdent(table.Name) +
		" WHERE " + quoteIdent(m.Memberships.SubjectColumn) + " = $1" +
		" AND " + quoteIdent(m.Memberships.TenantColumn) + " = $2"

	return []byte(strings.ReplaceAll(
		candidateAppMembershipsSource,
		"{{MODULE}}",
		m.Project.Module,
	) + "\nconst membershipQuery = " + strconv.Quote(query) + "\n"), nil
}

func quoteIdent(name string) string {
	return `"` + name + `"`
}

const candidateAppMembershipsSource = `package identity

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	rbac "github.com/plutoframework/pluto-rbac"
	"{{MODULE}}/internal/domain"
)

// Memberships resolves the subject's role in the tenant from the app-owned
// table declared in [memberships]. Generated; edit pluto.toml instead.
// The runtime database role must not own the table so row-level security
// applies.
type Memberships struct {
	pool *pgxpool.Pool
}

func NewMemberships(
	pool *pgxpool.Pool,
) *Memberships {
	return &Memberships{pool: pool}
}

func (m *Memberships) Resolve(
	ctx context.Context,
	subject, tenant string,
) (domain.Principal, error) {
	if m == nil || m.pool == nil || subject == "" || tenant == "" {
		return domain.Principal{}, domain.ErrUnauthenticated
	}

	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return domain.Principal{}, domain.ErrForbidden
	}

	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(
		ctx,
		"SELECT set_config('app.tenant_id', $1, true)",
		tenant,
	); err != nil {
		return domain.Principal{}, domain.ErrForbidden
	}

	var role string
	if err := tx.QueryRow(
		ctx,
		membershipQuery,
		subject,
		tenant,
	).Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Principal{}, domain.ErrForbidden
		}

		return domain.Principal{}, domain.ErrForbidden
	}

	if role == "" {
		return domain.Principal{}, domain.ErrForbidden
	}

	return domain.Principal{
		ActorID:  subject,
		TenantID: tenant,
		Roles:    []rbac.RoleID{rbac.RoleID(role)},
	}, nil
}

func (m *Memberships) VerifyMembership(
	ctx context.Context,
	subject, tenant string,
) error {
	_, err := m.Resolve(ctx, subject, tenant)
	return err
}
`

var candidateMembershipsSchema = []byte(
	`CREATE TABLE "pluto_resource_memberships" (` +
		`"tenant_id" text NOT NULL,` +
		`"actor_id" text NOT NULL,` +
		`"roles" text[] NOT NULL,` +
		`PRIMARY KEY("tenant_id","actor_id"));`,
)
