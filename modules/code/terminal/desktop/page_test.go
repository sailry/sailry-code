package desktop

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/settings"
	"github.com/sailry/sailry-harness/sdk/desktop/theme"
)

var testCatalog = i18n.New(Locales)

// The terminal's font is chosen among the default and the system's
// monospaced families; a saved family the system does not list stays the
// choice shown.
func TestTerminalFontOptions(t *testing.T) {
	state := settings.New()
	m := state.Model
	m.Font = "No Such Mono"
	var options []string
	tt := ui.NewTesterWith(func(c *ui.Context) {
		i18n.Bind(c, testCatalog)
		options = nil
		for _, o := range fontOptions(c, m.Font) {
			options = append(options, o.Value)
		}
		Page().Render(c, state)
	}, 900, 900, ui.TesterOptions{})
	tt.SetLocale("en")
	tt.Frame()
	if len(options) < 2 || options[0] != "" || options[len(options)-1] != "No Such Mono" {
		t.Fatalf("font options = %q", options)
	}
	mono := map[string]bool{}
	for _, f := range ui.FontFamilies() {
		mono[f.Name] = f.Monospaced
	}
	for _, name := range options[1 : len(options)-1] {
		if !mono[name] {
			t.Errorf("%q is offered but is not a monospaced system family", name)
		}
	}
	if !tt.HasText("No Such Mono") {
		t.Errorf("the select does not show the saved font: %q", tt.Texts())
	}
	if _, ok := tt.AccessByRole(ui.RoleTextField, "Font"); ok {
		t.Error("the font is still typed in a text field")
	}
}

func TestTerminalVariableDraft(t *testing.T) {
	h := settings.TerminalHost{Env: []settings.EnvVar{{Key: "EDITOR", Value: "vi"}}}
	for _, c := range []struct {
		index      int
		key, want  string
		wantLength int
	}{
		{-1, "1X", "terminal_variable_invalid", 1},
		{-1, "EDITOR", "terminal_variable_duplicate", 1},
		{0, " EDITOR ", "", 1},
		{-1, "PATH_2", "", 2},
	} {
		if got := setVariable(&h, c.index, c.key, "v"); got != c.want || len(h.Env) != c.wantLength {
			t.Errorf("%+v: %q %d", c, got, len(h.Env))
		}
	}
}

func TestTerminalManagedNodeInHeader(t *testing.T) {
	state := settings.NewLive()
	page := Page()
	state.SetNativePages([]settings.NativePage{page})
	state.Open(page.Section)
	state.ProviderActions.Hosts = func() []settings.ProviderHost {
		return []settings.ProviderHost{
			{Node: "local", Local: true, Connected: true},
			{Node: "remote", Name: "Remote", Connected: true},
		}
	}
	tester := ui.NewTester(func(c *ui.Context) {
		i18n.Bind(c, testCatalog)
		theme.Apply(c, "", theme.Dark)
		settings.Page(c, state, settings.Look{})
	}, 940, 700)
	for _, lang := range []string{"en", "zh-CN"} {
		tester.SetLocale(lang)
		tester.Frame()
		title, titled := tester.AccessByRole(ui.RoleText, testCatalog.In(lang, string(page.Section)))
		description, described := tester.AccessByRole(ui.RoleText, testCatalog.In(lang, string(page.Section)+"_description"))
		control, selectable := tester.AccessByRole(ui.RoleMenuButton, testCatalog.In(lang, "project_host"))
		if !titled || !described || !selectable {
			t.Fatalf("%s %s has no complete header: %q", lang, page.Section, tester.Texts())
		}
		middle := control.Bounds.Y + control.Bounds.H/2
		if control.Bounds.X <= title.Bounds.X || middle < title.Bounds.Y || middle > description.Bounds.Y+description.Bounds.H {
			t.Errorf("%s %s: host control is outside the page header: %v", lang, page.Section, control.Bounds)
		}
	}
}

func TestTerminalVariableDialog(t *testing.T) {
	state := settings.New()
	state.Host.Draft.Env, state.Host.Saved.Env = nil, nil
	tester := ui.NewTesterWith(func(c *ui.Context) {
		i18n.Bind(c, testCatalog)
		theme.Apply(c, "", theme.Dark)
		Page().Render(c, state)
	}, 900, 900, ui.TesterOptions{Preferences: ui.Preferences{ReduceTransparency: true}})
	tester.SetLocale("en")
	click := func(label string) {
		t.Helper()
		tester.Frame()
		tester.Advance(time.Second)
		tester.Frame()
		button, ok := tester.AccessByRole(ui.RoleButton, label)
		if !ok {
			t.Fatalf("no %q button: %q", label, tester.Texts())
		}
		tester.ClickAt(button.Bounds.X+button.Bounds.W/2, button.Bounds.Y+button.Bounds.H/2)
		tester.Frame()
	}
	click(i18n.In("en", "settings_add"))
	editor := state.Model.TerminalEnvEditor()
	if !editor.Open || editor.Index != -1 {
		t.Fatalf("Add did not open a new variable draft: %+v", editor)
	}
	editor.Key, editor.Value = "SAILRY_TEST", "blocked"
	click(i18n.In("en", "settings_save"))
	if !editor.Open || len(state.Host.Draft.Env) != 0 {
		t.Fatal("a reserved variable was saved or its invalid draft was discarded")
	}
	editor.Key, editor.Value = "EDITOR", "fixture-editor"
	click(i18n.In("en", "settings_save"))
	if editor.Open || len(state.Host.Saved.Env) != 1 || state.Host.Saved.Env[0].Value != "fixture-editor" {
		t.Fatalf("valid variable was not saved: editor %+v, host %+v", editor, state.Host)
	}
	click(i18n.In("en", "settings_delete"))
	if len(state.Host.Draft.Env) != 0 || len(state.Host.Saved.Env) != 0 {
		t.Fatalf("Delete did not save the removal: %+v", state.Host)
	}
}
