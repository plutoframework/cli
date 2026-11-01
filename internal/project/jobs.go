package project

import (
	"fmt"
	"go/format"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

type Job struct {
	MaxAttempts int    `toml:"max_attempts,omitempty"`
	Operation   string `toml:"operation"`
	Group       string `toml:"group"`
	Action      string `toml:"action"`
	Kind        string `toml:"kind"`
}

func validateJobs(jobs []Job) error {
	identifiers := map[string]bool{}
	kinds := map[string]bool{}

	for _, job := range jobs {
		if !packageName.MatchString(job.Group) ||
			token.Lookup(job.Group).IsKeyword() ||
			!actionName.MatchString(job.Action) ||
			!metadataKey.MatchString(job.Kind) || len(job.Kind) > 128 {
			return fmt.Errorf(
				"job requires a Go group/action and lowercase kind (<=128 bytes)",
			)
		}

		identifier := job.Group + "." + job.Action
		if identifiers[identifier] || kinds[job.Kind] {
			return fmt.Errorf(
				"duplicate job identifier or kind: %s",
				identifier,
			)
		}

		identifiers[identifier], kinds[job.Kind] = true, true
	}

	return nil
}

func jobRegistry(m Manifest) ([]byte, error) {
	jobs := append([]Job(nil), m.Jobs...)
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Kind < jobs[j].Kind })

	groups := map[string]bool{}
	for _, job := range jobs {
		groups[job.Group] = true
	}

	names := []string{}
	for name := range groups {
		names = append(names, name)
	}

	sort.Strings(names)

	var source strings.Builder
	source.WriteString(
		"package background\nimport (\njobs \"github.com/plutoframework/pluto-jobs\"\n",
	)

	for _, name := range names {
		fmt.Fprintf(
			&source,
			"%sjobs %s\n",
			name,
			strconv.Quote(m.Project.Module+"/internal/jobcontracts/"+name),
		)
	}

	source.WriteString(
		")\nfunc Handlers(dependencies Dependencies) map[jobs.Kind]jobs.Handler {\nreturn map[jobs.Kind]jobs.Handler{\n",
	)

	for _, job := range jobs {
		fmt.Fprintf(
			&source,
			"%sjobs.%sKind: new%s(dependencies),\n",
			job.Group,
			job.Action,
			symbol(Endpoint{Group: job.Group, Action: job.Action}),
		)
	}

	source.WriteString("}\n}\n")

	return format.Source([]byte(source.String()))
}

func jobData(m Manifest, job Job) (templateData, error) {
	operation, ok := m.Operation(job.Operation)
	if !ok {
		return templateData{}, fmt.Errorf(
			"job requires declared operation %s",
			job.Operation,
		)
	}

	data := dataFor(m, Endpoint{Group: job.Group, Action: job.Action})
	data.OperationGroup, data.OperationAction = operation.Group, operation.Action
	data.JobKind = job.Kind

	return data, nil
}

func JobFiles(m Manifest, job Job) (map[string][]byte, error) {
	if m.Boundary.Mode == "candidate" {
		return map[string][]byte{}, nil
	}

	data, err := jobData(m, job)
	if err != nil {
		return nil, err
	}

	source, err := render("job_test.go", data)
	if err != nil {
		return nil, err
	}

	return map[string][]byte{
		"internal/jobs/" + job.Group + "/" + strings.ToLower(job.Action) + "_test.go": source,
	}, nil
}

func managedJobs(m Manifest) (map[string][]byte, error) {
	files := map[string][]byte{}

	for _, job := range m.Jobs {
		data, err := jobData(m, job)
		if err != nil {
			return nil, err
		}

		for _, item := range []struct{ path, template string }{
			{"internal/jobcontracts/" + job.Group + "/" + strings.ToLower(job.Action) + ".go", "job.go"},
			{"internal/jobs/" + job.Group + "/" + strings.ToLower(job.Action) + ".go", "job_handler.go"},
			{"internal/background/" + job.Group + "_" + strings.ToLower(job.Action) + ".go", "job_binding.go"},
		} {
			source, err := render(item.template, data)
			if err != nil {
				return nil, err
			}

			files[item.path] = source
		}
	}

	return files, nil
}

func jobDependencies(m Manifest) ([]byte, error) {
	var source strings.Builder
	source.WriteString("package background\n")

	if len(m.Jobs) > 0 {
		source.WriteString("import (\n")

		groups := map[string]bool{}
		operations := map[string]bool{}

		for _, job := range m.Jobs {
			groups[job.Group] = true
			op, _ := m.Operation(job.Operation)
			operations[op.Group] = true
		}

		names := []string{}
		for group := range groups {
			names = append(names, group)
		}

		sort.Strings(names)

		for _, group := range names {
			fmt.Fprintf(
				&source,
				"%sjobs %q\n",
				group,
				m.Project.Module+"/internal/jobs/"+group,
			)
		}

		names = nil
		for group := range operations {
			names = append(names, group)
		}

		sort.Strings(names)

		for _, group := range names {
			fmt.Fprintf(
				&source,
				"%soperations %q\n",
				group,
				m.Project.Module+"/internal/operations/"+group,
			)
		}

		source.WriteString(")\n")
	}

	source.WriteString("type Dependencies struct {\n")

	for _, job := range m.Jobs {
		op, _ := m.Operation(job.Operation)
		symbol := symbol(Endpoint{Group: job.Group, Action: job.Action})
		fmt.Fprintf(
			&source,
			"%sOperation %soperations.%sBoundary\n%sIdentity %sjobs.%sIdentity\n",
			symbol,
			op.Group,
			op.Action,
			symbol,
			job.Group,
			job.Action,
		)
	}

	source.WriteString("}\n")

	return format.Source([]byte(source.String()))
}

// Kind constants are managed contracts derived only from project metadata.
// Payloads, handler logic and constructor bindings stay authored.
func jobKinds(m Manifest) map[string][]byte {
	groups := map[string]*strings.Builder{}
	for _, job := range m.Jobs {
		builder := groups[job.Group]
		if builder == nil {
			builder = &strings.Builder{}
			fmt.Fprintf(
				builder,
				"package %s\nimport jobs %q\nconst (\n",
				job.Group,
				"github.com/plutoframework/pluto-jobs",
			)

			groups[job.Group] = builder
		}

		fmt.Fprintf(builder, "%sKind jobs.Kind = %q\n", job.Action, job.Kind)
	}

	files := map[string][]byte{}

	for group, source := range groups {
		source.WriteString(")\n")

		files["internal/jobcontracts/"+group+"/kinds_gen.go"] = []byte(
			source.String(),
		)
	}

	return files
}
