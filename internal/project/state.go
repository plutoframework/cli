package project

// StateFiles creates application-owned lifecycle rules. They are intentionally
// absent from Managed: Go code, not a TOML transition DSL, is authoritative.
func StateFiles(m Manifest, group string) (map[string][]byte, error) {
	endpoint := Endpoint{
		Group:       group,
		Action:      "Submit",
		Name:        "Lifecycle",
		Description: "Application business lifecycle.",
		Method:      "GET",
		Path:        "/lifecycle",
	}
	if err := endpoint.Validate(false); err != nil {
		return nil, err
	}

	files := map[string][]byte{}

	for _, name := range []string{"lifecycle.go", "lifecycle_test.go"} {
		source, err := render(name, dataFor(m, endpoint))
		if err != nil {
			return nil, err
		}

		files["internal/domain/"+group+"/"+name] = source
	}

	return files, nil
}
