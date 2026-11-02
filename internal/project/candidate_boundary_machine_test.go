package project

import "testing"

func TestCandidateRawMachineTypeFlow(t *testing.T) {
	for _, source := range []string{
		`
func bad(m *state.Machine[string, string, bool]) {

	_ = m.Evaluate
}
`,
		`
func bad() {

	var m *state.Machine[string, string, bool]
	alias := m
	_ = alias.Evaluate
}
`,
		`
var m *state.Machine[string, string, bool]

func bad() {

	_ = m.Evaluate
}
`,
		`
type Service struct {
	machine *state.Machine[string, string, bool]
}

func (s *Service) bad() {

	_ = s.machine.Evaluate
}
`,
		`
func acquire() *state.Machine[string, string, bool] {

	return nil
}
func bad() {

	_ = acquire().Evaluate
}
`,
		`
type Alias = state.Machine[string, string, bool]

func bad(m *Alias) {

	_ = m.Evaluate
}
`,
		`var mint = state.New[string,string,bool]`,
		`
func bad() {

	type Alias = state.Machine[string, string, bool]
	var m *Alias
	_ = m.Evaluate
}
`,
		`
type Other struct{}

func (Other) Evaluate() {}
func bad(m *state.Machine[string, string, bool]) {
	{
		m := Other{}
		m.Evaluate()
	}
	_ = m.Evaluate
}
`,
	} {
		path := "internal/domain/sales/policy.go"
		root := candidateReviewFixture(
			t,
			map[string]string{
				path: `package sales

import "github.com/plutoframework/pluto-core/state"
` + source,
			},
		)

		diagnostics, _ := inspectCandidateBoundaries(
			root,
			candidateReviewModule,
			[]byte(candidateReviewMetadata(t)),
		)
		if !candidateReviewHas(diagnostics, "ARCH603", path) {
			t.Fatalf("raw evaluation escaped: %s: %+v", source, diagnostics)
		}
	}
}
