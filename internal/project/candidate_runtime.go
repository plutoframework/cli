package project

import (
	"bytes"
	"fmt"
	"go/format"
	"sort"
	"strconv"
	"strings"
)

func candidateRuntime(m Manifest) ([]byte, error) {
	var source bytes.Buffer

	q := strconv.Quote

	fmt.Fprintf(
		&source,
		"package app\nimport(\n\"context\";\"errors\";\"log/slog\";\"net/http\"\n\"github.com/go-chi/chi/v5\"\n\"github.com/jackc/pgx/v5/pgxpool\"\nauth \"github.com/plutoframework/pluto-auth\"\nauthpostgres \"github.com/plutoframework/pluto-auth/stores/postgres\"\n\"github.com/plutoframework/pluto-core/cache\"\nidentitygate \"github.com/plutoframework/pluto-rbac/identity\"\njobs \"github.com/plutoframework/pluto-jobs\"\njobspostgres \"github.com/plutoframework/pluto-jobs/stores/postgres\"\n",
	)

	for _, name := range []string{"access", "config", "transport", "features", "storage/identity", "paths"} {
		fmt.Fprintf(&source, "%s\n", q(m.Project.Module+"/internal/"+name))
	}

	groups := map[string]bool{}
	resources := map[string]ResourceContract{}
	contracts := map[string]OperationContract{}

	usedResources := map[string]bool{}
	for _, contract := range m.OperationContracts {
		usedResources[contract.Resource] = true
	}

	for _, resource := range m.Resources {
		resources[resource.ID()] = resource
		if usedResources[resource.ID()] {
			groups[resource.Group] = true
		}
	}

	sorted := []string{}
	for group := range groups {
		sorted = append(sorted, group)
	}

	sort.Strings(sorted)

	for _, group := range sorted {
		fmt.Fprintf(
			&source,
			"%sdomain %s\n%sdata %s\n",
			group,
			q(m.Project.Module+"/internal/domain/"+group),
			group,
			q(m.Project.Module+"/internal/storage/"+group),
		)
	}

	opGroups := map[string]bool{}
	effectGroups := map[string]bool{}
	handlerGroups := map[string]bool{}
	jobGroups := map[string]bool{}

	for _, operation := range m.Operations {
		opGroups[operation.Group] = true
	}

	for _, contract := range m.OperationContracts {
		contracts[contract.ID] = contract
		if contract.Kind == "effect" {
			effectGroups[resources[contract.Resource].Group] = true
		}
	}

	for _, entry := range m.Endpoints {
		handlerGroups[entry.Group] = true
	}

	for _, job := range m.Jobs {
		jobGroups[job.Group] = true
	}

	for _, item := range []struct {
		set         map[string]bool
		alias, path string
	}{{opGroups, "operations", "operations"}, {effectGroups, "effects", "infrastructure"}, {handlerGroups, "handlers", "handlers"}, {jobGroups, "jobs", "jobs"}} {
		names := []string{}
		for group := range item.set {
			names = append(names, group)
		}

		sort.Strings(names)

		for _, group := range names {
			fmt.Fprintf(
				&source,
				"%s%s %s\n",
				group,
				item.alias,
				q(m.Project.Module+"/internal/"+item.path+"/"+group),
			)
		}
	}

	source.WriteString(")\ntype CandidateOperations struct{\n")

	for _, operation := range m.Operations {
		fmt.Fprintf(
			&source,
			"%s %soperations.%sBoundary\n",
			symbol(operation.Endpoint()),
			operation.Group,
			operation.Action,
		)
	}

	source.WriteString(
		"}\ntype CandidateRuntime struct{Operations CandidateOperations;candidateRuntimeManager *auth.Manager;candidateRuntimeHandler http.Handler;candidateRuntimePool *pgxpool.Pool}\n",
	)
	source.WriteString(
		"func(r *CandidateRuntime)Handler()http.Handler{return r.candidateRuntimeHandler}\nfunc(r *CandidateRuntime)WithSession(ctx context.Context,credential,tenant string,run func(context.Context,access.Caller)error)error{if r==nil{return errors.New(\"nil runtime\")};return access.WithSession(ctx,r.candidateRuntimeManager,credential,tenant,run)}\n",
	)
	source.WriteString(
		"func NewCandidateRuntime(cfg config.Config,pool *pgxpool.Pool,logger *slog.Logger,store cache.Store,environment string,destinations map[string]string)(*CandidateRuntime,error){\nif pool==nil||logger==nil||environment==\"\"{return nil,errors.New(\"invalid candidate runtime\")}\nif store==nil{store=cache.Disabled{}}\npolicy,err:=access.NewPolicy();if err!=nil{return nil,err}\nmemberships:=identity.NewMemberships(pool)\nsessions,err:=authpostgres.New(pool);if err!=nil{return nil,err}\nauthentication,err:=transport.NewAuthentication(cfg.Auth,sessions,memberships);if err!=nil{return nil,err}\nauthorizer,err:=access.NewAuthorizer(policy,memberships);if err!=nil{return nil,err}\n_ = authorizer;_ = destinations\nruntime:=&CandidateRuntime{candidateRuntimeManager:authentication.Manager,candidateRuntimePool:pool}\n",
	)

	for _, resource := range m.Resources {
		if !usedResources[resource.ID()] {
			continue
		}

		fmt.Fprintf(
			&source,
			"machine%s,err:=%sdomain.New%sMachine();if err!=nil{return nil,err}\n",
			resource.Group+resource.Name,
			resource.Group,
			resource.Name,
		)
	}

	for index, operation := range m.Operations {
		contract := contracts[operation.ID()]
		resource := resources[contract.Resource]
		suffix := fmt.Sprint(index)
		fmt.Fprintf(
			&source,
			"policy%s,err:=%sdata.New%sResourcePolicy(pool,%s);if err!=nil{return nil,err}\nrequirement%s,err:=authorizer.Require(%s);if err!=nil{return nil,err}\nresource%s,err:=requirement%s.WithResourcePolicy(policy%s);if err!=nil{return nil,err}\ngate%s,err:=identitygate.New(authentication.Manager,resource%s,memberships);if err!=nil{return nil,err}\n",
			suffix,
			resource.Group,
			resource.Name,
			q(operation.ID()),
			suffix,
			q(operation.Permission),
			suffix,
			suffix,
			suffix,
			suffix,
			suffix,
		)

		if contract.Kind == "effect" {
			fmt.Fprintf(
				&source,
				"data%s,err:=%seffects.New%sDelivery(pool,gate%s,destinations[%s]);if err!=nil{return nil,err}\n",
				suffix,
				resource.Group,
				symbol(operation.Endpoint()),
				suffix,
				q(operation.ID()),
			)
		} else {
			fmt.Fprintf(
				&source,
				"data%s,err:=%sdata.New%sData(pool,gate%s,machine%s,store,%s,environment,%s);if err!=nil{return nil,err}\n",
				suffix,
				resource.Group,
				resource.Name,
				suffix,
				resource.Group+resource.Name,
				q(m.Project.Name),
				q(operation.ID()),
			)
		}

		fmt.Fprintf(
			&source,
			"runtime.Operations.%s=%soperations.New%s(gate%s,machine%s,data%s)\n",
			symbol(operation.Endpoint()),
			operation.Group,
			operation.Action,
			suffix,
			resource.Group+resource.Name,
			suffix,
		)
	}

	source.WriteString(
		"router:=chi.NewRouter();router.Use(transport.SecurityHeaders);router.Use(transport.Recover(logger))\nreg:=transport.Registrar{Router:router,Auth:authentication.Verifier,Sessions:authentication.HTTP,Features:features.New(cfg.Features),Logger:logger,Policy:policy}\n",
	)
	source.WriteString(
		"transport.RegisterSessions(reg,authentication)\nreg.Handle(paths.Health,func(w http.ResponseWriter,r *http.Request){transport.JSON(w,200,paths.HealthResponse{Data:\"ok\"})})\n",
	)

	for _, entry := range m.Endpoints {
		operation, _ := m.Operation(entry.Operation)
		fmt.Fprintf(
			&source,
			"%shandlers.Register%s(reg,runtime.Operations.%s,cfg.Server.BodyLimit)\n",
			entry.Group,
			entry.Action,
			symbol(operation.Endpoint()),
		)
	}

	source.WriteString(
		"_ = reg\nruntime.candidateRuntimeHandler=router;return runtime,nil\n}\n",
	)
	source.WriteString("type CandidateWorkerCredentials struct{\n")

	for _, job := range m.Jobs {
		fmt.Fprintf(
			&source,
			"%s string\n",
			symbol(Endpoint{Group: job.Group, Action: job.Action}),
		)
	}

	source.WriteString(
		"}\nfunc(r *CandidateRuntime)Worker(credentials CandidateWorkerCredentials)(*jobs.Worker,error){\nif r==nil{return nil,errors.New(\"nil runtime\")}\nhandlers:=map[jobs.Kind]jobs.Handler{}\n",
	)

	for index, job := range m.Jobs {
		operation, _ := m.Operation(job.Operation)
		fmt.Fprintf(
			&source,
			"handler%d,err:=%sjobs.New%sHandler(r.Operations.%s,r.candidateRuntimeManager,credentials.%s);if err!=nil{return nil,err};handlers[%s]=handler%d\n",
			index,
			job.Group,
			job.Action,
			symbol(operation.Endpoint()),
			symbol(Endpoint{Group: job.Group, Action: job.Action}),
			q(job.Kind),
			index,
		)
	}

	source.WriteString(
		"store,err:=jobspostgres.New(r.candidateRuntimePool);if err!=nil{return nil,err};return jobs.NewWorker(store,handlers,jobs.DefaultConfig())\n}\n",
	)

	// Keep imported candidate framework modules available for bootstrap projects.
	source.WriteString("var _ = identitygate.New\nvar _ = stringsPlaceholder\n")

	output := strings.ReplaceAll(
		source.String(),
		"var _ = stringsPlaceholder\n",
		"",
	)

	return format.Source([]byte(output))
}
