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
	Group  string `toml:"group"`
	Action string `toml:"action"`
	Kind   string `toml:"kind"`
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
			strconv.Quote(m.Project.Module+"/internal/jobs/"+name),
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

func JobFiles(m Manifest, job Job) (map[string][]byte, error) {
	files := map[string][]byte{}
	data := dataFor(m, Endpoint{Group: job.Group, Action: job.Action})
	data.JobKind = job.Kind

	for _, name := range []string{"job.go", "job_test.go"} {
		source, err := render(name, data)
		if err != nil {
			return nil, err
		}

		suffix := ".go"
		if name == "job_test.go" {
			suffix = "_test.go"
		}

		path := "internal/jobs/" + job.Group + "/" + strings.ToLower(
			job.Action,
		) + suffix
		files[path] = source
	}

	binding, err := render("job_binding.go", data)
	if err != nil {
		return nil, err
	}

	files["internal/background/"+job.Group+"_"+strings.ToLower(job.Action)+".go"] = binding

	return files, nil
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

		files["internal/jobs/"+group+"/kinds_gen.go"] = []byte(source.String())
	}

	return files
}
