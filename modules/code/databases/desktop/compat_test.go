package desktop_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/egoist/mygo/ui"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	databases "github.com/sailry/sailry-code/modules/code/databases/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
	"github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/native/process"
	"github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func installDatabasePackage(w *databaseWindow, source string) {
	t := w.T
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(filepath.Join(root, "databases"), os.DirFS(source)); err != nil {
		t.Fatal(err)
	}
	project := w.CreateProject("Plugin source fixture", root)
	var id protocol.WorktreeID
	w.Await("the isolated plugin source has no worktree", func() bool {
		for _, worktree := range w.Controller.Snapshot().Worktrees {
			if worktree.Project != nil && *worktree.Project == project.ID && worktree.Path == root {
				id = worktree.ID
				return true
			}
		}
		return false
	})
	w.Do(command(t, "install_plugin", map[string]any{"name": "databases", "worktree": id, "path": "databases", "expected_revision": 0}))
}

func TestCodeDatabaseWithUnrelatedSameNamePackage(t *testing.T) {
	t.Parallel()
	source := t.TempDir()
	files := map[string]string{
		"plugin.json":                         `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"databases","version":"0.1.26","extensions":{"dev.sailry.platform":{"api_version":"v1","actions":["databases.read"],"desktop":{"entry":"dev.sailry.platform/desktop/main.js","resources":["dev.sailry.platform/desktop/main.js"],"navigation":{"label":"Third-party Databases","icon":"reicon:ui/database"},"navigation_options":{"target":"node","pinned":true,"order":800,"details":true}}}}}`,
		"dev.sailry.platform/desktop/main.js": `import {View,div} from 'gpui-kit'; export default class ThirdParty extends View { render(){return div().child('Unrelated same-name package');} }`,
	}
	for name, body := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(source, name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	w := &databaseWindow{Window: support.Open(t, databaseHost(), native.Assemble(databases.Register))}
	installDatabasePackage(w, source)
	w.clickRail("Third-party Databases")
	w.Await("native replacement suppressed a same-name third-party view", func() bool { return w.Tester.HasText("Unrelated same-name package") })
	w.clickRail("Databases")
	w.Await("same-name package captured the native page", func() bool {
		return w.App.Page == databases.PageID && w.Tester.HasText("Open a database connection")
	})
}

func TestCodeDatabaseInheritsArchivedPackagePreferences(t *testing.T) {
	t.Parallel()
	source := legacyDatabaseSource(t)
	n := support.StartNode(t, databaseHost())
	legacy := &databaseWindow{Window: n.Connect(native.Assembly{})}
	installDatabasePackage(legacy, source)
	old := "plugin:" + n.Owner.ID().String() + "/databases"
	legacy.Preferences.Data.FeatureOrder = new([]string{old})
	legacy.Preferences.Data.FeaturePins = new(map[string]bool{old: false})
	if err := legacy.Preferences.Save(); err != nil {
		t.Fatal(err)
	}
	code := n.Connect(native.Assemble(databases.Register))
	page := string(databases.PageID)
	order, pins := code.App.FeaturePreferences()
	if position := slices.Index(order, old); position < 0 || slices.Index(order, page) != position+1 || pins[page] {
		t.Fatalf("native page did not inherit the verified package's order and unpinned choice: %v, %v", order, pins)
	}
	legacyKey, nativeKey := old+"/database-workspace", page+"/database-workspace"
	prefs := code.App.Prefs()
	layout := fixtures.PluginWorkspacePrefs{Width: 367, Folded: true}
	prefs.PluginWorkspaces = map[string]fixtures.PluginWorkspacePrefs{legacyKey: layout}
	code.App.SetPrefs(prefs)
	if saved := code.App.Prefs().PluginWorkspaces; saved[nativeKey] != layout || saved[legacyKey] != layout {
		t.Fatalf("native page did not inherit the workspace width and fold: %v", saved)
	}
}

func TestCodeDatabasePreservesLegacyAssistantAdmission(t *testing.T) {
	t.Parallel()
	source := legacyDatabaseSource(t)
	_, file := sqliteFixture(t)
	script := &databaseModel{plans: make(chan databaseModelCall, 2), observed: make(chan databaseModelObservation, 8)}
	profile := filepath.Join(t.TempDir(), "profile")
	environment := process.NewEnvironment([]string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir(), "SHELL=/bin/sh"})
	options := node.Options{Assembly: databaseHost(), Environment: &environment}
	options.Model = scriptedModelFactory(options.Model, script)
	owner, err := node.StartWithOptions(t.Context(), profile, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Stop(context.Background()) })
	window := (&support.Node{T: t, Ctx: t.Context(), Profile: profile, Owner: owner}).Connect(native.Assemble(databases.Register))
	w := &databaseWindow{Window: window}
	installDatabasePackage(w, source)
	w.clickRail("Databases")
	w.Await("the official package shadows the registered native database page", func() bool {
		return w.App.Page == databases.PageID && w.Tester.HasText("Open a database connection")
	})
	installed := w.Controller.Snapshot().Plugins
	if len(installed) != 1 || installed[0].Digest != legacyDatabaseDigest || !installed[0].Enabled || pageCount(w, "Databases") != 1 {
		t.Fatalf("expected one native rail entry beside the installed official package: %+v", installed)
	}
	connection := support.Result[protocol.DatabaseProfile](t, w.Do(protocol.Command{Kind: "save_database", Data: protocol.CommandSaveDatabasePayload{
		Profile: protocol.DatabaseProfile{ID: protocol.NewDatabaseID(), Name: "Legacy assistant SQLite", ReadOnly: true,
			Connection: protocol.DatabaseConnection{Kind: "sqlite", Data: protocol.DatabaseConnectionSqlitePayload{Path: file}}},
	}}))
	provider := protocol.DefaultConversationProvider()
	provider.ID, provider.Name, provider.Enabled = protocol.NewProviderID(), "Scripted database assistant", true
	provider.Api, provider.Authentication, provider.Endpoint, provider.DefaultModel = protocol.ConversationModelApiChatCompletions, protocol.AuthenticationApiKey, "http://fixture.invalid/v1", "fixture"
	model := protocol.DefaultConversationModel()
	model.ID, model.Context, model.Output, model.Tools = "fixture", 32000, 8000, true
	provider.Models = []protocol.ConversationModel{model}
	secret := protocol.NewSecret("fixture-key")
	w.Do(protocol.Command{Kind: "save_provider", Data: protocol.CommandSaveProviderPayload{Provider: provider, Secret: &secret}})
	config := protocol.DefaultSessionConfig()
	config.Provider, config.Model, config.Mode, config.Permission = provider.ID, model.ID, protocol.WorkModeCode, protocol.PermissionAsk
	config.Resource = &protocol.ConnectionResource{Kind: "database", Data: connection.ID}
	config.Assistant = &protocol.PluginConversationBinding{ID: "database", Package: protocol.PluginReference{
		Name: "databases", Digest: installed[0].Digest, SettingsRevision: installed[0].SettingsRevision,
	}}
	session := support.Result[protocol.Session](t, w.Do(protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Config: &config}}))
	w.clickByName(connection.Name)
	w.Await("native database assistant did not mount the saved resource session", func() bool {
		return w.Tester.HasText("Database assistant") && w.has(ui.RoleButton, support.Label("chat_connection_history"))
	})
	const query = "SELECT label || '-from-assistant' AS proof FROM fixture_rows"
	input := `{"connection":"` + connection.ID.String() + `","sql":"` + query + `"}`
	script.plans <- databaseModelCall{"database_query", input}
	submit := func() protocol.Command {
		for _, saved := range w.Controller.Snapshot().Sessions {
			if saved.ID == session.ID {
				return protocol.Command{Kind: "submit_turn", Data: protocol.CommandSubmitTurnPayload{
					Session: saved.ID, ExpectedRevision: saved.Revision, Message: protocol.ConversationInput{Text: "Query the saved SQLite fixture"},
				}}
			}
		}
		t.Fatal("saved database session disappeared")
		return protocol.Command{}
	}
	w.Do(submit())
	w.Await("assistant query did not reach the native SQL log and data grid", func() bool {
		return w.Tester.HasText("sqlite-actual-row-from-assistant") && w.Tester.HasText(query)
	})
	legacy := script.observation(t, "database_query")
	for _, required := range []string{"database_catalog", "database_query", "database_execute", "database_connections", "database_save_connection"} {
		if !slices.Contains(legacy.tools, required) {
			t.Fatalf("legacy assistant lost preferred native tool %s: %v", required, legacy.tools)
		}
	}
	const assistant = "You are the database workspace assistant."
	const nativeScope = "Work only with the saved connection bound to this conversation."
	if strings.Count(legacy.prompt, assistant) != 1 || strings.Contains(legacy.prompt, nativeScope) ||
		!strings.Contains(legacy.prompt, "Work only with the saved connection exposed by your tools.") {
		t.Fatal("the legacy turn lost or duplicated the captured manifest instructions")
	}
	w.Await("the legacy assistant turn did not finish", func() bool { return w.Tester.HasText("Database fixture complete") })
	if err := w.SetPluginEnabled(w.Ctx, "databases", false); err != nil {
		t.Fatal(err)
	}
	w.Await("the official package was not disabled", func() bool {
		packages := w.Controller.Snapshot().Plugins
		return len(packages) == 1 && !packages[0].Enabled
	})
	checkDenied := func(state string) {
		if _, err := w.Controller.Do(w.Ctx, submit()); err == nil || !strings.Contains(err.Error(), "assistant plugin is disabled or removed") {
			t.Fatalf("legacy assistant admission with %s package: %v", state, err)
		}
	}
	checkDenied("disabled")
	w.Do(command(t, "remove_plugin", map[string]any{"name": "databases", "expected_revision": w.Controller.Snapshot().Plugins[0].Revision}))
	w.Await("the old package was not removed", func() bool { return len(w.Controller.Snapshot().Plugins) == 0 })
	checkDenied("removed")

	const freshQuery = "SELECT label || '-from-native' AS proof FROM fixture_rows"
	script.plans <- databaseModelCall{"tool_search", `{"query":"select:database_query"}`}
	script.plans <- databaseModelCall{"call_tool", `{"name":"database_query","arguments":{"connection":"` + connection.ID.String() + `","sql":"` + freshQuery + `"}}`}
	w.Click(w.control(ui.RoleButton, support.Label("chat_new")))
	w.Click(w.control(ui.RoleTextField, support.Label("composer")))
	w.Tester.Type("Query this SQLite resource in a new session")
	w.Tester.Key(0, ui.KeyEnter)
	w.Await("new database session did not query SQLite without the old plugin", func() bool {
		return w.Tester.HasText("sqlite-actual-row-from-native") && w.Tester.HasText(freshQuery)
	})
	fresh := script.observation(t, "tool_search")
	if strings.Count(fresh.prompt, assistant) != 1 {
		t.Fatal("the native turn did not receive one scoped instruction before tool discovery")
	}
	for _, phrase := range []string{
		nativeScope, "MySQL backticks", "PostgreSQL or SQLite double quotes", "Use the database argument to select the database",
		"Inspect catalogs and schema before composing SQL", "Use database_query for read-only queries", "runtime reviews its exact SQL under the session permission policy",
		"Respect the saved connection's read-only setting", "never automatically replay a write", "never request passwords",
	} {
		if !strings.Contains(fresh.prompt, phrase) {
			t.Errorf("native assistant's first model call lost %q", phrase)
		}
	}
	for _, saved := range w.Controller.Snapshot().Sessions {
		if saved.ID != session.ID && saved.Config.Resource != nil && saved.Config.Resource.Kind == "database" {
			if saved.Config.Assistant != nil || saved.Config.Permission != protocol.PermissionAsk ||
				support.Result[protocol.DatabaseID](t, protocol.Output{Data: saved.Config.Resource.Data}) != connection.ID {
				t.Fatalf("the new conversation inherited an old binding or changed resource: %+v", saved.Config)
			}
			return
		}
	}
	t.Fatal("the native database draft did not create a new resource session")
}

// Preserve the Node.Options callback's binding type without naming Harness internals.
func scriptedModelFactory[Binding any](_ func(context.Context, Binding) (fantasy.LanguageModel, error), model fantasy.LanguageModel) func(context.Context, Binding) (fantasy.LanguageModel, error) {
	return func(context.Context, Binding) (fantasy.LanguageModel, error) { return model, nil }
}

type databaseModel struct {
	fantasy.LanguageModel
	plans    chan databaseModelCall
	observed chan databaseModelObservation
}

type databaseModelCall struct{ name, input string }

type databaseModelObservation struct {
	call, prompt string
	tools        []string
}

func (model *databaseModel) observation(t *testing.T, call string) databaseModelObservation {
	t.Helper()
	for {
		select {
		case observation := <-model.observed:
			if observation.call == call {
				return observation
			}
		default:
			t.Fatalf("the fixture model did not reach %s", call)
		}
	}
}

func (model *databaseModel) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	prompt, err := json.Marshal(call.Prompt)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, tool := range call.Tools {
		names = append(names, tool.GetName())
	}
	var planned databaseModelCall
	select {
	case planned = <-model.plans:
	default:
	}
	select {
	case model.observed <- databaseModelObservation{call: planned.name, prompt: string(prompt), tools: names}:
	default:
	}
	return func(yield func(fantasy.StreamPart) bool) {
		if planned.name != "" {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: planned.name, ToolCallName: planned.name, ToolCallInput: planned.input}) {
				return
			}
		} else if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "answer", Delta: "Database fixture complete"}) {
			return
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}
