package desktop

import (
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func (w *Window) CreateProject(name, path string) protocol.Project {
	w.T.Helper()
	return Result[protocol.Project](w.T, w.Do(protocol.Command{Kind: "create_project", Data: protocol.ProjectsDraft{
		Name: name, Path: path, Appearance: protocol.DefaultProjectsAppearance(), Source: protocol.ProjectsSource{Kind: "local"},
	}}))
}

func (w *Window) OpenProjectSession(project protocol.ProjectID) protocol.Session {
	w.T.Helper()
	provider := protocol.DefaultConversationProvider()
	provider.ID, provider.Name, provider.Enabled = protocol.NewProviderID(), "Workspace fixture", true
	provider.Api, provider.Authentication, provider.Endpoint = protocol.ConversationModelApiChatCompletions, protocol.AuthenticationApiKey, "http://127.0.0.1:9/v1"
	model := protocol.DefaultConversationModel()
	model.ID, model.Context, model.Output = "fixture", 32000, 8000
	provider.DefaultModel, provider.Models = model.ID, []protocol.ConversationModel{model}
	secret := protocol.NewSecret("fixture-key")
	w.Do(protocol.Command{Kind: "save_provider", Data: protocol.CommandSaveProviderPayload{Provider: provider, Secret: &secret}})
	config := protocol.DefaultSessionConfig()
	config.Provider, config.Model, config.Effort = provider.ID, model.ID, model.DefaultEffort
	config.Mode, config.Permission = protocol.WorkModeCode, protocol.PermissionFull
	session := Result[protocol.Session](w.T, w.Do(protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Project: &project, Config: &config}}))
	id := session.ID.String()
	w.OpenSession(id)
	w.Await("the requested session did not reach the conversation page with its composer", func() bool {
		if w.App.Page != native.ConversationPage || w.App.Sidebar.Selected != id || w.App.FocusedConversation() != id || w.App.ActiveTerminal() != "" || !w.App.Panel.Session {
			return false
		}
		composer, ok := w.Tester.AccessByRole(ui.RoleTextField, Label("composer"))
		return ok && visible(composer)
	})
	return session
}

func (w *Window) ProjectMenu(name string) []string {
	w.T.Helper()
	var row ui.Rect
	w.Await("the sidebar has no project row "+name, func() bool {
		for _, entry := range w.Tester.AccessAll() {
			b := entry.Bounds
			if entry.Name == name && visible(entry) && b.Y >= native.HeaderHeight && b.X >= native.RailWidth && b.X+b.W <= native.RailWidth+w.App.NavWidth {
				row = b
				return true
			}
		}
		return false
	})
	w.Tester.RightClickAt(row.X+row.W/2, row.Y+row.H/2)
	w.Tester.Frame()
	return slices.Clone(w.Tester.Menu())
}

func (w *Window) OpenConversationDetails() {
	w.T.Helper()
	w.Tester.Frame()
	if w.App.Page != native.ConversationPage || w.App.FocusedConversation() == "" {
		w.T.Fatal("details require a focused conversation on the conversation page")
	}
	if !w.App.PanelOpen {
		toggle, ok := w.Tester.AccessByRole(ui.RoleButton, Label("details"))
		if !ok || toggle.Offscreen || toggle.Disabled || toggle.Bounds.Y+toggle.Bounds.H > native.HeaderHeight {
			w.T.Fatalf("no enabled conversation details toggle in the header: %+v", toggle)
		}
		w.Click(toggle)
	}
	w.Await("the conversation details did not open", func() bool { return w.App.PanelOpen })
}

func (w *Window) InDetails(entry ui.AccessInfo) bool {
	b := entry.Bounds
	return visible(entry) && b.X >= Width-w.App.PanelWidth && b.X+b.W <= Width
}

func (w *Window) DetailsLauncherButtons() []ui.AccessInfo {
	w.T.Helper()
	w.OpenConversationDetails()
	if len(w.App.Panel.Tabs) != 0 {
		w.T.Fatalf("a new conversation should show the launcher, not tabs %v", w.App.Panel.Tabs)
	}
	w.Await("the conversation details column did not finish opening", func() bool {
		for _, entry := range w.Tester.AccessAll() {
			if b := entry.Bounds; entry.Name == Label("details") && visible(entry) && b.X > native.RailWidth && b.Y >= native.HeaderHeight && b.H > b.W {
				return true
			}
		}
		return false
	})
	var entries []ui.AccessInfo
	for _, entry := range w.Tester.AccessAll() {
		if entry.Role == ui.RoleButton && w.InDetails(entry) && entry.Bounds.Y >= native.HeaderHeight {
			entries = append(entries, entry)
		}
	}
	return entries
}

func visible(entry ui.AccessInfo) bool {
	return !entry.Offscreen && entry.Bounds.W > 0 && entry.Bounds.H > 0
}
