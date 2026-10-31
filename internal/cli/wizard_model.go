package cli

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type (
	wizardChoice struct{ label, value string }
	wizardField  struct {
		id, label, help, value string
		choices                []wizardChoice
		fallback               func(map[string]string) string
	}
)

type wizardModel struct {
	kind, title, context                        string
	fields                                      []wizardField
	index, cursor, selected, width, height      int
	review, editing, confirmed, cancelled, menu bool
	message                                     string
	args                                        []string
	build                                       func(map[string]string) ([]string, error)
}

func (m wizardModel) Init() tea.Cmd { return nil }

func (m wizardModel) values() map[string]string {
	values := map[string]string{}
	for _, f := range m.fields {
		values[f.id] = f.value
	}

	return values
}

func (m wizardModel) plan() ([]string, error) {
	for _, f := range m.fields {
		for _, r := range f.value {
			if unicode.IsControl(r) {
				return nil, errors.New(
					"wizard values cannot contain control characters",
				)
			}
		}

		if len([]rune(f.value)) > 2048 {
			return nil, errors.New(
				"wizard values must be at most 2048 characters",
			)
		}
	}

	return m.build(m.values())
}

func (m *wizardModel) focus(index int) {
	m.index = index

	f := &m.fields[index]
	if f.value == "" && f.fallback != nil {
		f.value = f.fallback(m.values())
	}

	m.cursor = len([]rune(f.value))
	m.message = ""
}

func (m *wizardModel) advance() tea.Cmd {
	if m.menu {
		m.args, _ = m.build(m.values())
		m.confirmed = true

		return tea.Quit
	}

	if m.editing || m.index == len(m.fields)-1 {
		m.review, m.editing = true, false
		m.selected = len(m.fields)

		return nil
	}

	m.focus(m.index + 1)

	return nil
}

func (m wizardModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = msg.Width
		}

		if msg.Height > 0 {
			m.height = msg.Height
		}
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" || key == "esc" {
			m.cancelled = true
			return m, tea.Quit
		}

		if m.review {
			return m.reviewKey(key)
		}

		f := &m.fields[m.index]

		switch key {
		case "shift+tab", "ctrl+p":
			if m.index > 0 {
				m.focus(m.index - 1)
			}
		case "enter", "tab", "ctrl+n":
			cmd := m.advance()
			return m, cmd
		default:
			if len(f.choices) > 0 {
				m.choose(key)
			} else {
				m.edit(msg)
			}
		}
	}

	return m, nil
}

func (m wizardModel) reviewKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "shift+tab", "ctrl+p":
		m.selected = (m.selected + len(m.fields)) % (len(m.fields) + 1)
	case "down", "tab", "ctrl+n":
		m.selected = (m.selected + 1) % (len(m.fields) + 1)
	case "enter":
		if m.selected < len(m.fields) {
			m.review, m.editing = false, true
			m.focus(m.selected)
		} else {
			args, err := m.plan()
			if err != nil {
				m.message = err.Error()
				return m, nil
			}

			m.args, m.confirmed = args, true

			return m, tea.Quit
		}
	}

	return m, nil
}

func (m *wizardModel) choose(key string) {
	f := &m.fields[m.index]
	current := 0

	for i, c := range f.choices {
		if c.value == f.value {
			current = i
		}
	}

	switch key {
	case "up", "left", "k":
		current = (current + len(f.choices) - 1) % len(f.choices)
	case "down", "right", "j", " ":
		current = (current + 1) % len(f.choices)
	default:
		return
	}

	f.value = f.choices[current].value
}

func (m *wizardModel) edit(msg tea.KeyMsg) {
	f := &m.fields[m.index]
	value := []rune(f.value)

	switch msg.String() {
	case "left":
		m.cursor = max(0, m.cursor-1)
	case "right":
		m.cursor = min(len(value), m.cursor+1)
	case "home", "ctrl+a":
		m.cursor = 0
	case "end", "ctrl+e":
		m.cursor = len(value)
	case "ctrl+u":
		value = nil
		m.cursor = 0
	case "backspace", "ctrl+h":
		if m.cursor > 0 {
			value = append(value[:m.cursor-1], value[m.cursor:]...)
			m.cursor--
		}
	case "delete":
		if m.cursor < len(value) {
			value = append(value[:m.cursor], value[m.cursor+1:]...)
		}
	default:
		if msg.Type != tea.KeyRunes && msg.Type != tea.KeySpace {
			return
		}

		runes := msg.Runes
		if msg.Type == tea.KeySpace {
			runes = []rune{' '}
		}

		for _, r := range runes {
			if unicode.IsControl(r) || len(value) >= 2048 {
				continue
			}

			tail := append([]rune(nil), value[m.cursor:]...)
			value = append(append(value[:m.cursor], r), tail...)
			m.cursor++
		}
	}

	f.value = string(value)
}

func displayText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}

		return r
	}, value)
}

func (m wizardModel) View() string {
	if m.cancelled || m.confirmed {
		return ""
	}

	width := max(10, min(78, m.width-2))

	var b strings.Builder
	fmt.Fprintf(&b, "\n  P L U T O  /  LAUNCH CONTROL\n\n  %s\n", m.title)

	if m.context != "" {
		fmt.Fprintf(&b, "  %s\n", displayText(m.context))
	}

	if m.review {
		b.WriteString("\n  Review your choices. Select a field to edit.\n\n")

		rows := max(2, m.height-13)

		start := max(0, m.selected-rows+1)
		for i := start; i <= len(m.fields) && i < start+rows; i++ {
			marker := "  "
			if i == m.selected {
				marker = "> "
			}

			if i == len(m.fields) {
				fmt.Fprintf(&b, "  %sRun this command\n", marker)

				continue
			}

			f := m.fields[i]

			value := f.value
			for _, c := range f.choices {
				if c.value == value {
					value = c.label
				}
			}

			if value == "" {
				value = "(none)"
			}

			fmt.Fprintf(&b, "  %s%s: %s\n", marker, f.label, displayText(value))
		}

		b.WriteString("\n  Up/down: select   Enter: edit/run   Esc: cancel\n")
	} else {
		f := m.fields[m.index]
		fmt.Fprintf(
			&b,
			"\n  %d/%d  %s\n  %s\n\n",
			m.index+1,
			len(m.fields),
			f.label,
			f.help,
		)

		if len(f.choices) > 0 {
			for _, c := range f.choices {
				marker := "  "
				if c.value == f.value {
					marker = "> "
				}

				fmt.Fprintf(&b, "  %s%s\n", marker, c.label)
			}
		} else {
			text := []rune(displayText(f.value))
			cursor := min(m.cursor, len(text))
			fmt.Fprintf(
				&b,
				"  > %s|%s\n",
				string(text[:cursor]),
				string(text[cursor:]),
			)
		}

		b.WriteString("\n  Enter: next   Shift+Tab: back   Esc: cancel\n")

		if len(f.choices) > 0 {
			b.WriteString("  Arrow keys: choose\n")
		} else {
			b.WriteString("  Ctrl+U: clear   Arrow keys: move cursor\n")
		}
	}

	if m.message != "" {
		fmt.Fprintf(&b, "\n  %s\n", displayText(m.message))
	}

	return ansi.Hardwrap(b.String(), width, true)
}

func shellCommand(args []string) string {
	quoted := []string{"pluto"}
	for _, arg := range args {

		// Display only. Execution always passes the original argument slice.
		quoted = append(quoted, "'"+strings.ReplaceAll(arg, "'", "'\\''")+"'")
	}

	return strings.Join(quoted, " ")
}
