package desktop

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/settings"
	"github.com/sailry/sailry-harness/sdk/desktop/theme"
)

func TestTerminalSourceKeys(t *testing.T) {
	files := token.NewFileSet()
	source, err := parser.ParseFile(files, "page.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(source, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		key, _ := strconv.Unquote(literal.Value)
		if !strings.HasPrefix(key, "terminal_") && !strings.HasPrefix(key, "settings_") && !strings.HasPrefix(key, "form_") {
			return true
		}
		for _, language := range []string{"en", "zh-CN"} {
			if value := testCatalog.In(language, key); value == "" || value == key {
				t.Errorf("%s: %s is not translated in %s", files.Position(literal.Pos()), key, language)
			}
		}
		return true
	})
}

func TestTerminalTranslations(t *testing.T) {
	for _, language := range []string{"en", "zh-CN"} {
		t.Run(language, func(t *testing.T) {
			state := settings.New()
			state.Host.Draft.Env = nil
			state.Shells = []settings.Shell{{}}
			state.Font = ""
			tester := ui.NewTesterWith(func(view *ui.Context) {
				i18n.Bind(view, testCatalog)
				theme.Apply(view, "", theme.Dark)
				Page().Render(view, state)
			}, 900, 1100, ui.TesterOptions{Preferences: ui.Preferences{ReduceTransparency: true}})
			tester.SetLocale(language)
			tester.Frame()
			for _, key := range []string{"terminal_environment", "terminal_shell_group", "terminal_shell", "terminal_shell_default", "terminal_input", "settings_paste_protection", "settings_paste_description", "settings_terminal_display", "settings_font_size", "settings_font_family", "settings_font_default"} {
				translated := testCatalog.In(language, key)
				if translated == key || !tester.HasText(translated) || tester.HasText(key) {
					t.Errorf("%s did not display %s as %q: %q", language, key, translated, tester.Texts())
				}
			}
			editor := state.TerminalEnvEditor()
			*editor = settings.TerminalEnvEditor{Open: true, Index: -1}
			tester.Frame()
			if title := testCatalog.In(language, "terminal_environment_add"); !tester.HasText(title) {
				t.Fatalf("dialog title is not translated: %q", tester.Texts())
			}
			for _, key := range []string{"terminal_variable", "terminal_value"} {
				label := testCatalog.In(language, key)
				if _, found := tester.AccessByRole(ui.RoleTextField, label); label == key || !found {
					t.Errorf("%s dialog has no translated %s field: %q", language, key, tester.Texts())
				}
			}
		})
	}
}
