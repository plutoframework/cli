package migrate

import (
	"fmt"
	"io"
	"strings"
)

// Very large inputs fall back to a whole-file replacement hunk so diffing
// stays bounded in memory.
const diffCellLimit = 4_000_000

// WriteDiff prints a unified diff of every planned change in path order.
func WriteDiff(out io.Writer, plan *Plan) error {
	for _, name := range plan.Paths() {
		before := plan.Original[name]
		after := plan.Writes[name]

		from, to := "a/"+name, "b/"+name
		if before == nil {
			from = "/dev/null"
		}

		if plan.Deletes[name] {
			to = "/dev/null"
		}

		if _, err := fmt.Fprintf(
			out,
			"--- %s\n+++ %s\n",
			from,
			to,
		); err != nil {
			return err
		}

		if _, err := io.WriteString(
			out,
			unified(splitLines(before), splitLines(after)),
		); err != nil {
			return err
		}
	}

	return nil
}

func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}

	lines := strings.SplitAfter(string(data), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}

type edit struct {
	kind byte
	line string
}

// unified renders hunks with three lines of context from an LCS edit script.
func unified(a, b []string) string {
	edits := script(a, b)

	var out strings.Builder

	const context = 3

	for start := 0; start < len(edits); {
		if edits[start].kind == ' ' {
			start++
			continue
		}

		first := max(start-context, 0)
		end := start

		for end < len(edits) {
			if edits[end].kind != ' ' {
				end++
				continue
			}

			run := end
			for run < len(edits) && edits[run].kind == ' ' {
				run++
			}

			if run == len(edits) || run-end > 2*context {
				end = min(end+context, len(edits))
				break
			}

			end = run
		}

		oldStart, newStart := 1, 1

		for _, item := range edits[:first] {
			if item.kind != '+' {
				oldStart++
			}

			if item.kind != '-' {
				newStart++
			}
		}

		oldCount, newCount := 0, 0

		for _, item := range edits[first:end] {
			if item.kind != '+' {
				oldCount++
			}

			if item.kind != '-' {
				newCount++
			}
		}

		if oldCount == 0 {
			oldStart--
		}

		if newCount == 0 {
			newStart--
		}

		fmt.Fprintf(
			&out,
			"@@ -%d,%d +%d,%d @@\n",
			oldStart,
			oldCount,
			newStart,
			newCount,
		)

		for _, item := range edits[first:end] {
			out.WriteByte(item.kind)
			out.WriteString(item.line)

			if !strings.HasSuffix(item.line, "\n") {
				out.WriteString("\n\\ No newline at end of file\n")
			}
		}

		start = end
	}

	return out.String()
}

func script(a, b []string) []edit {
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}

	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix &&
		a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}

	edits := []edit{}
	for _, line := range a[:prefix] {
		edits = append(edits, edit{' ', line})
	}

	left, right := a[prefix:len(a)-suffix], b[prefix:len(b)-suffix]
	edits = append(edits, middle(left, right)...)

	for _, line := range a[len(a)-suffix:] {
		edits = append(edits, edit{' ', line})
	}

	return edits
}

func middle(a, b []string) []edit {
	edits := []edit{}

	if (len(a)+1)*(len(b)+1) > diffCellLimit {
		for _, line := range a {
			edits = append(edits, edit{'-', line})
		}

		for _, line := range b {
			edits = append(edits, edit{'+', line})
		}

		return edits
	}

	width := len(b) + 1
	lengths := make([]int, (len(a)+1)*width)

	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lengths[i*width+j] = lengths[(i+1)*width+j+1] + 1
			} else {
				lengths[i*width+j] = max(
					lengths[(i+1)*width+j],
					lengths[i*width+j+1],
				)
			}
		}
	}

	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			edits = append(edits, edit{' ', a[i]})
			i++
			j++
		case lengths[(i+1)*width+j] >= lengths[i*width+j+1]:
			edits = append(edits, edit{'-', a[i]})
			i++
		default:
			edits = append(edits, edit{'+', b[j]})
			j++
		}
	}

	for ; i < len(a); i++ {
		edits = append(edits, edit{'-', a[i]})
	}

	for ; j < len(b); j++ {
		edits = append(edits, edit{'+', b[j]})
	}

	return edits
}
