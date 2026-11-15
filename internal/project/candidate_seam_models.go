package project

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"text/template"
)

// Generated wire surface for authored operations: declared [[models]] become
// structs, declared [[operations.query]] become a strict typed parser, and
// page/object responses become the response envelope. Everything here is
// derived from pluto.toml; authored code only consumes the generated types.

// SeamModelField is one declared model field ready for rendering.
type SeamModelField struct {
	Name    string
	GoName  string
	GoType  string
	Tag     string
	Comment string
}

// SeamModel is one declared model ready for rendering.
type SeamModel struct {
	Name   string
	Fields []SeamModelField
}

// SeamQueryParam is one declared query parameter ready for rendering.
type SeamQueryParam struct {
	Name   string
	GoName string
	GoType string
	Kind   string
	Max    int
	After  string
}

// SeamOperation is one authored operation ready for rendering.
type SeamOperation struct {
	Group      string
	Module     string
	Action     string
	Symbol     string
	Response   string
	Item       string
	Output     string
	Input      string
	Collection bool
	Endpoint   bool
	Query      []SeamQueryParam
	Strconv    bool
	Time       bool
	Pagination bool
}

type seamGroup struct {
	Group  string
	Models []SeamModel
	Query  bool
}

// isAuthored reports whether an operation has an authored body.
func (o Operation) isAuthored() bool {
	return o.Body == BodyAuthored
}

func seamTemplate(name string, data any) ([]byte, error) {
	source, err := templates.ReadFile("templates/" + name + ".tmpl")
	if err != nil {
		return nil, err
	}

	tmpl, err := template.New(name).
		Funcs(template.FuncMap{"q": strconv.Quote, "goName": seamGoName}).
		Parse(string(source))
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return nil, err
	}

	return out.Bytes(), nil
}

// seamGoName converts snake_case to an exported Go identifier, keeping "id"
// as ID.
func seamGoName(name string) string {
	var out strings.Builder

	for _, part := range strings.Split(name, "_") {
		if part == "id" {
			out.WriteString("ID")

			continue
		}

		out.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}

	return out.String()
}

func seamModelField(modelID, field string) (SeamModelField, error) {
	match := modelField.FindStringSubmatch(field)
	if match == nil {
		return SeamModelField{}, fmt.Errorf(
			"model %s field %q is invalid",
			modelID,
			field,
		)
	}

	out := SeamModelField{Name: match[1], GoName: seamGoName(match[1])}

	switch match[2] {
	case "int64", "string", "bool":
		out.GoType = match[2]
	case "time":
		out.GoType = "time.Time"
	case "json":
		out.GoType = "json.RawMessage"
		out.Tag = ` openapi:"json"`
	case "hex":
		out.GoType = "string"
		out.Tag = ` openapi:"hex"`
		out.Comment = "// lowercase hexadecimal"
	default:
		return SeamModelField{}, fmt.Errorf(
			"model %s field %q has unsupported type",
			modelID,
			field,
		)
	}

	return out, nil
}

func seamModels(m Manifest, group string) ([]SeamModel, error) {
	models := []SeamModel{}

	for _, model := range m.Models {
		modelGroup, name, _ := splitQualified(model.ID)
		if modelGroup != group {
			continue
		}

		out := SeamModel{Name: name}
		names := map[string]bool{}

		for _, field := range model.Fields {
			item, err := seamModelField(model.ID, field)
			if err != nil {
				return nil, err
			}

			if names[item.GoName] {
				return nil, fmt.Errorf(
					"model %s fields collide on Go name %s",
					model.ID,
					item.GoName,
				)
			}

			names[item.GoName] = true
			out.Fields = append(out.Fields, item)
		}

		models = append(models, out)
	}

	return models, nil
}

func seamQueryParams(o Operation) ([]SeamQueryParam, error) {
	params := []SeamQueryParam{}
	names := map[string]bool{}

	for _, param := range o.Query {
		if o.Collection && (param.Name == "limit" || param.Name == "cursor") {
			return nil, fmt.Errorf(
				"operation %s: query %s is reserved for pagination",
				o.ID(),
				param.Name,
			)
		}

		item := SeamQueryParam{
			Name:   param.Name,
			GoName: seamGoName(param.Name),
			Kind:   param.Type,
			Max:    param.Max,
			After:  param.After,
		}

		switch param.Type {
		case "string":
			item.GoType = "string"
		case "int64":
			item.GoType = "*int64"
		case "bool":
			item.GoType = "*bool"
		case "time":
			item.GoType = "time.Time"
		}

		if names[item.GoName] {
			return nil, fmt.Errorf(
				"operation %s: query names collide on Go name %s",
				o.ID(),
				item.GoName,
			)
		}

		names[item.GoName] = true
		params = append(params, item)
	}

	return params, nil
}

func seamOperation(m Manifest, o Operation) (SeamOperation, error) {
	out := SeamOperation{
		Group:      o.Group,
		Module:     m.Project.Module,
		Action:     o.Action,
		Symbol:     strings.ToUpper(o.Group[:1]) + o.Group[1:] + o.Action,
		Response:   o.Response,
		Collection: o.Collection,
	}

	for _, endpoint := range m.Endpoints {
		if endpoint.Operation == o.ID() {
			out.Endpoint = true
		}
	}

	for field, ref := range map[string]string{
		"item": o.Item, "output": o.Output, "input": o.Input,
	} {
		if ref == "" {
			continue
		}

		group, name, _ := splitQualified(ref)
		if group != o.Group {
			return out, fmt.Errorf(
				"operation %s: %s model %s must belong to group %s",
				o.ID(),
				field,
				ref,
				o.Group,
			)
		}

		switch field {
		case "item":
			out.Item = name
		case "output":
			out.Output = name
		case "input":
			out.Input = name
		}
	}

	// A provision endpoint decodes a JSON body into its declared input model;
	// it has no query parser.
	if o.Scope == ScopeProvision || o.Scope == ScopeEnroll ||
		o.isMachine() && o.Write {
		out.Endpoint = false
	}

	if out.Endpoint && !o.isStream() && (o.Write || out.Input != "") {
		return out, fmt.Errorf(
			"operation %s: authored endpoints must be reads without input "+
				"models; expose writes through generated bodies",
			o.ID(),
		)
	}

	if out.Endpoint && o.Response == "" {
		return out, fmt.Errorf(
			"operation %s: an authored endpoint requires a response",
			o.ID(),
		)
	}

	query, err := seamQueryParams(o)
	if err != nil {
		return out, err
	}

	out.Query = query

	for _, param := range query {
		out.Strconv = out.Strconv || param.Kind == "int64" ||
			param.Kind == "bool"
		out.Time = out.Time || param.Kind == "time"
	}

	// Values formats time parameters, so time is needed with an endpoint.
	out.Pagination = o.Response == ResponsePage ||
		(out.Endpoint && o.Collection)

	return out, nil
}

// candidateSeamModels renders the models-package files for authored
// operations: one models_gen.go per group, query_gen.go for groups with
// authored endpoints, and one request/response file per authored operation.
func candidateSeamModels(m Manifest) (map[string][]byte, error) {
	files := map[string][]byte{}
	groups := map[string]*seamGroup{}

	order := []string{}

	for _, operation := range m.Operations {
		if !operation.usesSeamWire() {
			continue
		}

		spec, err := seamOperation(m, operation)
		if err != nil {
			return nil, err
		}

		group, ok := groups[operation.Group]
		if !ok {
			models, err := seamModels(m, operation.Group)
			if err != nil {
				return nil, err
			}

			group = &seamGroup{Group: operation.Group, Models: models}
			groups[operation.Group] = group
			order = append(order, operation.Group)
		}

		if spec.Endpoint {
			group.Query = true
		}

		source, err := seamTemplate("candidate_seam_request.go", spec)
		if err != nil {
			return nil, err
		}

		files["internal/models/"+operation.Group+"/"+strings.ToLower(
			operation.Action,
		)+".go"] = source
	}

	sort.Strings(order)

	for _, name := range order {
		group := groups[name]

		source, err := seamTemplate("candidate_seam_models.go", group)
		if err != nil {
			return nil, err
		}

		files["internal/models/"+name+"/models_gen.go"] = source

		if group.Query {
			source, err := seamTemplate("candidate_seam_query.go", group)
			if err != nil {
				return nil, err
			}

			files["internal/models/"+name+"/query_gen.go"] = source
		}
	}

	return files, nil
}

// seamHandler renders the HTTP handler for an authored operation endpoint.
func seamHandler(m Manifest, operation Operation) ([]byte, error) {
	spec, err := seamOperation(m, operation)
	if err != nil {
		return nil, err
	}

	return seamTemplate("candidate_seam_handler.go", spec)
}

// seamPathQuery renders the paths.Endpoint Query field for an authored read.
func seamPathQuery(o Operation) string {
	if len(o.Query) == 0 {
		return ""
	}

	var out strings.Builder

	out.WriteString("Query:[]QueryParam{\n")

	for _, param := range o.Query {
		fmt.Fprintf(
			&out,
			"{Name:%q,Type:%q,Max:%d,After:%q},\n",
			param.Name,
			param.Type,
			param.Max,
			param.After,
		)
	}

	out.WriteString("},\n")

	return out.String()
}
