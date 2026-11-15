package sqlcheck

// tableState is what the migrations, applied in order, leave behind for one
// table.
type tableState struct {
	File     string
	Line     int
	Columns  map[string]bool
	Enabled  bool
	Forced   bool
	Policies map[string]*policyState
	Triggers map[string]*triggerState
}

type policyState struct {
	File        string
	Line        int
	Command     string
	Restrictive bool
	HasUsing    bool
	HasCheck    bool
	UsingOK     bool
	CheckOK     bool

	// LookupOK is set when the policy is the exact declared lookup policy:
	// FOR SELECT, permissive, USING (<key> = current_setting(<setting>)) and
	// no WITH CHECK.
	LookupOK bool
}

type triggerState struct {
	File     string
	Line     int
	Before   bool
	Events   map[string]bool
	Function string
	Disabled bool
}

// migrationState accumulates state across migration files.
type migrationState struct {
	tables map[string]*tableState

	// functions maps a function name to whether its body always raises.
	functions map[string]bool
	setting   string

	// columns maps declared table names to their tenant column.
	columns map[string]string

	// lookups are the declared lookups by policy name.
	lookups map[string]Lookup
}

func newMigrationState(setting string) *migrationState {
	return &migrationState{
		tables:    map[string]*tableState{},
		functions: map[string]bool{},
		setting:   setting,
		columns:   map[string]string{},
		lookups:   map[string]Lookup{},
	}
}

func (s *migrationState) table(name string) *tableState {
	return s.tables[tableKey(name)]
}

func (s *migrationState) create(name, file string, line int) *tableState {
	state := &tableState{
		File:     file,
		Line:     line,
		Columns:  map[string]bool{},
		Policies: map[string]*policyState{},
		Triggers: map[string]*triggerState{},
	}
	s.tables[tableKey(name)] = state

	return state
}
