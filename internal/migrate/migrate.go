// Package migrate moves an existing Pluto application between manifest schema
// and boundary profile states. Steps are ordered, deterministic and planned
// completely before any write. Authored code is changed only by provably safe
// mechanical rewrites; everything else becomes a manual item.
package migrate

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/plutoframework/cli/internal/project"
)

const (
	ProfilePublished = "published"
	ProfileCandidate = "candidate"
)

// State identifies a manifest shape. Steps move exactly one State forward.
type State struct {
	Schema  int
	Profile string
}

func (s State) String() string {
	return fmt.Sprintf("schema %d/%s", s.Schema, s.Profile)
}

// Step is one versioned migration. Plan records every intended change in the
// workspace; it never writes to disk. Plans must be idempotent: planning the
// same input twice yields the same result.
type Step struct {
	ID      string
	Summary string
	From    State
	To      State
	Plan    func(*Workspace) error
}

// Steps is the ordered migration registry. Append new steps; never reorder or
// rewrite released ones, so older applications keep a reproducible path.
func Steps() []Step {
	return []Step{
		publishedToCandidate(),
	}
}

// Options are explicit user inputs. Candidate sources are never inferred.
type Options struct {
	Sources   project.BoundaryProfile
	Contracts []byte
}

// Item is a manual action or advisory note anchored to a file position.
type Item struct {
	Path   string
	Line   int
	Reason string
	Action string
}

func (i Item) String() string {
	location := i.Path
	if i.Line > 0 {
		location += ":" + strconv.Itoa(i.Line)
	}

	return location + ": " + i.Reason + "\n    Action: " + i.Action
}

// Plan is the complete deterministic outcome of a migration chain.
type Plan struct {
	From     State
	To       State
	Steps    []Step
	Root     string
	Writes   map[string][]byte
	Deletes  map[string]bool
	Original map[string][]byte
	Manual   []Item
	Notes    []Item
	Blocked  bool
	Manifest project.Manifest
	Tidy     bool
}

// Paths lists every changed path in lexical order.
func (p *Plan) Paths() []string {
	names := []string{}
	for name := range p.Writes {
		names = append(names, name)
	}

	for name := range p.Deletes {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// Workspace is the in-memory overlay a step reads and edits.
type Workspace struct {
	Root     string
	Options  Options
	Manifest project.Manifest
	plan     *Plan
}

// Read returns planned content, falling back to the file on disk.
func (w *Workspace) Read(name string) ([]byte, bool, error) {
	if w.plan.Deletes[name] {
		return nil, false, nil
	}

	if data, ok := w.plan.Writes[name]; ok {
		return data, true, nil
	}

	return w.Original(name)
}

// Original returns the file as it is on disk, ignoring planned changes.
func (w *Workspace) Original(name string) ([]byte, bool, error) {
	if data, ok := w.plan.Original[name]; ok {
		return data, true, nil
	}

	path, err := project.SafePath(w.Root, name)
	if err != nil {
		return nil, false, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}

	if err != nil {
		return nil, false, err
	}

	w.plan.Original[name] = data

	return data, true, nil
}

// Write records content. Unchanged content is not recorded.
func (w *Workspace) Write(name string, data []byte) error {
	current, exists, err := w.Read(name)
	if err != nil {
		return err
	}

	if exists && bytes.Equal(current, data) {
		return nil
	}

	delete(w.plan.Deletes, name)
	w.plan.Writes[name] = data

	return nil
}

// Delete records removal of an existing file.
func (w *Workspace) Delete(name string) error {
	_, exists, err := w.Read(name)
	if err != nil || !exists {
		return err
	}

	delete(w.plan.Writes, name)
	w.plan.Deletes[name] = true

	return nil
}

// Manual records an item the user must resolve; migrate exits nonzero.
func (w *Workspace) Manual(item Item) {
	w.plan.Manual = append(w.plan.Manual, item)
}

// Note records advisory information that does not block completion.
func (w *Workspace) Note(item Item) {
	w.plan.Notes = append(w.plan.Notes, item)
}

// Block stops the chain: the current step and later steps write nothing.
func (w *Workspace) Block(item Item) {
	w.plan.Blocked = true
	w.Manual(item)
}

// RequireTidy asks the apply phase to reconcile go.sum with go mod tidy.
func (w *Workspace) RequireTidy() {
	w.plan.Tidy = true
}

// Detect reads the schema and profile without full validation, so manifests
// from older schemas remain recognizable to the steps that upgrade them.
func Detect(data []byte) (State, error) {
	var probe struct {
		Kind          string `toml:"kind"`
		SchemaVersion int    `toml:"schema_version"`
		Boundary      struct {
			Mode string `toml:"mode"`
		} `toml:"boundary"`
	}

	if err := toml.Unmarshal(data, &probe); err != nil {
		return State{}, fmt.Errorf("pluto.toml: %w", err)
	}

	if probe.Kind != "" {
		return State{}, fmt.Errorf(
			"pluto.toml kind %q has no application migrations",
			probe.Kind,
		)
	}

	if probe.SchemaVersion < 1 {
		return State{}, fmt.Errorf("pluto.toml: missing schema_version")
	}

	profile := probe.Boundary.Mode
	if profile == "" {
		profile = ProfilePublished
	}

	if profile != ProfilePublished && profile != ProfileCandidate {
		return State{}, fmt.Errorf(
			"pluto.toml: unrecognized boundary mode %q",
			profile,
		)
	}

	return State{Schema: probe.SchemaVersion, Profile: profile}, nil
}

// ParseTarget accepts a profile name or a schema number. An empty target keeps
// the current profile at the latest schema.
func ParseTarget(value string, current State) (State, error) {
	switch value {
	case "":
		return State{Schema: project.SchemaVersion, Profile: current.Profile},
			nil
	case ProfilePublished, ProfileCandidate:
		return State{Schema: project.SchemaVersion, Profile: value}, nil
	}

	schema, err := strconv.Atoi(strings.TrimPrefix(value, "schema:"))
	if err != nil || schema < 1 || schema > project.SchemaVersion {
		return State{}, fmt.Errorf(
			"--to must be published, candidate or a schema version 1-%d",
			project.SchemaVersion,
		)
	}

	return State{Schema: schema, Profile: current.Profile}, nil
}

// Chain finds the ordered steps from one state to another.
func Chain(steps []Step, from, to State) ([]Step, error) {
	chain := []Step{}
	current := from

	for current != to {
		found := false

		for _, step := range steps {
			if step.From == current {
				chain = append(chain, step)
				current = step.To
				found = true

				break
			}
		}

		if !found || len(chain) > len(steps) {
			return nil, fmt.Errorf(
				"no migration path from %s to %s",
				from,
				to,
			)
		}
	}

	return chain, nil
}

// Build plans the full chain for the application at root. Nothing is written.
func Build(root, target string, options Options) (*Plan, error) {
	return build(Steps(), root, target, options)
}

func build(
	steps []Step,
	root, target string,
	options Options,
) (*Plan, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	path, err := project.SafePath(root, "pluto.toml")
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("pluto.toml: %w", err)
	}

	from, err := Detect(data)
	if err != nil {
		return nil, err
	}

	to, err := ParseTarget(target, from)
	if err != nil {
		return nil, err
	}

	chain, err := Chain(steps, from, to)
	if err != nil {
		return nil, err
	}

	plan := &Plan{
		From:     from,
		To:       to,
		Steps:    chain,
		Root:     root,
		Writes:   map[string][]byte{},
		Deletes:  map[string]bool{},
		Original: map[string][]byte{},
	}
	workspace := &Workspace{Root: root, Options: options, plan: plan}

	if from.Schema == project.SchemaVersion {
		manifest, err := project.Decode(data)
		if err != nil {
			return nil, fmt.Errorf("pluto.toml is not recognized: %w", err)
		}

		workspace.Manifest = manifest
		plan.Manifest = manifest
	}

	for _, step := range chain {
		if err := step.Plan(workspace); err != nil {
			return nil, fmt.Errorf("%s: %w", step.ID, err)
		}

		if plan.Blocked {
			plan.Writes = map[string][]byte{}
			plan.Deletes = map[string]bool{}

			break
		}

		plan.Manifest = workspace.Manifest
	}

	sortItems(plan.Manual)
	sortItems(plan.Notes)

	return plan, nil
}

func sortItems(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Path != items[j].Path {
			return items[i].Path < items[j].Path
		}

		return items[i].Line < items[j].Line
	})
}
