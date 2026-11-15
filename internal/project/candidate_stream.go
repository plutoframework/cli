package project

import (
	"fmt"
	"strings"
)

// streamHandlerData is the template view of one websocket endpoint.
type streamHandlerData struct {
	SeamOperation
	Origins []string
}

// streamHandler renders the generated upgrade handler of a stream endpoint.
func streamHandler(
	m Manifest,
	operation Operation,
	entry Endpoint,
) ([]byte, error) {
	spec, err := seamOperation(m, operation)
	if err != nil {
		return nil, err
	}

	return seamTemplate("candidate_stream_handler.go", streamHandlerData{
		SeamOperation: spec,
		Origins:       entry.Origins,
	})
}

// streamPathSpec renders the paths.Endpoint Stream field of a websocket
// endpoint: the declared message models and the generated limits that the
// OpenAPI document publishes.
func streamPathSpec(
	m Manifest,
	operation Operation,
	entry Endpoint,
) (string, error) {
	_, item, ok := splitQualified(operation.Item)
	if !ok {
		return "", fmt.Errorf(
			"operation %s: stream requires an item model",
			operation.ID(),
		)
	}

	limits := operation.StreamSettings()

	var out strings.Builder

	fmt.Fprintf(&out, "Stream:&StreamSpec{\nServer:%s.%s{},\n", operation.Group, item)

	if operation.Input != "" {
		_, input, _ := splitQualified(operation.Input)
		fmt.Fprintf(&out, "Client:%s.%s{},\n", operation.Group, input)
	}

	fmt.Fprintf(
		&out,
		"MaxMessageBytes:%d,\n",
		limits.MaxMessageBytes,
	)

	if len(entry.Origins) > 0 {
		out.WriteString("Origins:[]string{")

		for _, origin := range entry.Origins {
			fmt.Fprintf(&out, "%q,", origin)
		}

		out.WriteString("},\n")
	}

	out.WriteString("},\n")

	return out.String(), nil
}
