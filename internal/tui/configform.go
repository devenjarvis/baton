package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
)

// fieldKind distinguishes toggle fields from text input fields.
type fieldKind int

const (
	fieldToggle fieldKind = iota
	fieldText
)

// formField is a single field in a config form.
type formField struct {
	label       string
	kind        fieldKind
	toggleValue bool            // for fieldToggle
	textInput   textinput.Model // for fieldText
	editing     bool            // true when text field has cursor active
}

// configForm composes toggle and text input fields into a navigable form.
type configForm struct {
	fields  []formField
	focused int
	width   int
}

// configFormSaveMsg is emitted when the user saves the form (ctrl+s).
type configFormSaveMsg struct{}

// configFormCancelMsg is emitted when the user cancels (esc with no text editing).
type configFormCancelMsg struct{}

// newConfigForm creates a form with the given fields. Width controls text input sizing.
func newConfigForm(fields []formField, width int) configForm {
	f := configForm{fields: fields, width: width}
	if len(f.fields) > 0 {
		f.focusField(0)
	}
	return f
}

// addToggle appends a boolean toggle field.
func addToggle(fields []formField, label string, value bool) []formField {
	return append(fields, formField{
		label:       label,
		kind:        fieldToggle,
		toggleValue: value,
	})
}

// addTextInput appends a text input field.
func addTextInput(fields []formField, label, value, placeholder string, width int) []formField {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.SetValue(value)
	ti.CharLimit = 256
	if width > 0 {
		ti.SetWidth(width)
	}
	return append(fields, formField{
		label:     label,
		kind:      fieldText,
		textInput: ti,
	})
}

func (f *configForm) focusField(idx int) {
	if idx < 0 || idx >= len(f.fields) {
		return
	}
	// Blur previous
	if f.focused >= 0 && f.focused < len(f.fields) {
		old := &f.fields[f.focused]
		if old.kind == fieldText {
			old.textInput.Blur()
			old.editing = false
		}
	}
	f.focused = idx
}

// Update handles key events for the form.
func (f *configForm) Update(msg tea.Msg) tea.Cmd {
	if len(f.fields) == 0 {
		return nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		field := &f.fields[f.focused]

		// If editing a text field, delegate most keys to textinput.
		if field.editing {
			switch msg.String() {
			case "esc":
				field.textInput.Blur()
				field.editing = false
				return nil
			case "enter":
				field.textInput.Blur()
				field.editing = false
				return nil
			}
			var cmd tea.Cmd
			field.textInput, cmd = field.textInput.Update(msg)
			return cmd
		}

		// Navigation and actions when not editing.
		switch msg.String() {
		case "j", "down":
			if f.focused < len(f.fields)-1 {
				f.focusField(f.focused + 1)
			}
			return nil
		case "k", "up":
			if f.focused > 0 {
				f.focusField(f.focused - 1)
			}
			return nil
		case "enter", " ":
			switch field.kind {
			case fieldToggle:
				field.toggleValue = !field.toggleValue
			case fieldText:
				field.editing = true
				field.textInput.Focus()
			}
			return nil
		case "ctrl+s":
			return func() tea.Msg { return configFormSaveMsg{} }
		case "esc":
			return func() tea.Msg { return configFormCancelMsg{} }
		}
	}

	return nil
}

// View renders the form as a vertical list of labeled fields.
func (f configForm) View() string {
	if len(f.fields) == 0 {
		return StyleSubtle.Render("No settings available")
	}

	labelStyle := lipgloss.NewStyle().Width(22).Foreground(ColorText)
	focusedLabelStyle := lipgloss.NewStyle().Width(22).Foreground(ColorSecondary).Bold(true)
	toggleOn := StyleSuccess.Render("[x]")
	toggleOff := StyleSubtle.Render("[ ]")

	rows := make([]string, 0, len(f.fields))
	for i, field := range f.fields {
		ls := labelStyle
		cursor := "  "
		if i == f.focused {
			ls = focusedLabelStyle
			cursor = StyleActive.Render("> ")
		}

		label := ls.Render(field.label)
		var value string

		switch field.kind {
		case fieldToggle:
			if field.toggleValue {
				value = toggleOn
			} else {
				value = toggleOff
			}
		case fieldText:
			value = field.textInput.View()
		}

		rows = append(rows, cursor+label+" "+value)
	}

	return strings.Join(rows, "\n")
}

// toggleValue returns the value of a toggle field by label.
func (f configForm) toggleValue(label string) bool {
	for _, field := range f.fields {
		if field.label == label && field.kind == fieldToggle {
			return field.toggleValue
		}
	}
	return false
}

// textValue returns the value of a text field by label.
func (f configForm) textValue(label string) string {
	for _, field := range f.fields {
		if field.label == label && field.kind == fieldText {
			return field.textInput.Value()
		}
	}
	return ""
}
