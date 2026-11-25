package desktop_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	ssh "github.com/sailry/sailry-code/modules/code/ssh/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

// The connection's assistant column lists and creates its resource sessions
// from the native page. A legacy session keeps its saved configuration, a
// new one is a fresh SSH resource session, and leaving the page keeps both
// the sessions and the remote terminals.
func TestCodeSSHResourceHistoryKeepsLegacySession(t *testing.T) {
	peer := newCodeSSHPeer(t)
	w := newCodeSSHWindow(t)
	primary := w.trustedSSHProfile("Legacy session connection", peer)
	other := w.trustedSSHProfile("Other session connection", peer)
	binding := w.installLegacySSHAssistant()
	config := w.sessionFixtureConfig()

	create := func(config protocol.SessionConfig, title string) protocol.Session {
		t.Helper()
		session := support.Result[protocol.Session](t, w.Do(protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Config: &config}}))
		return support.Result[protocol.Session](t, w.Do(protocol.Command{Kind: "rename_session", Data: protocol.CommandRenameSessionPayload{
			Session: session.ID, ExpectedRevision: session.Revision, Title: title,
		}}))
	}
	const legacyTitle, otherTitle, ordinaryTitle, freshMessage = "Legacy SSH review", "Other connection review", "Ordinary session", "Check the remote disk"
	legacyConfig := config
	legacyConfig.Assistant = &binding
	legacyConfig.Resource = &protocol.ConnectionResource{Kind: "ssh", Data: primary.ID}
	legacy := create(legacyConfig, legacyTitle)
	create(config, ordinaryTitle)
	if err := w.SetPluginEnabled(w.Ctx, "ssh", false); err != nil {
		t.Fatal(err)
	}
	otherSession := create(ssh.NewSessionConfig(other.ID, legacyConfig), otherTitle)
	if otherSession.Config.Assistant != nil || otherSession.Config.Permission != protocol.PermissionAsk || otherSession.Config.Resource == nil ||
		otherSession.Config.Resource.Kind != "ssh" || support.Result[protocol.SshID](t, protocol.Output{Data: otherSession.Config.Resource.Data}) != other.ID {
		t.Fatal("a new SSH resource session inherited the legacy assistant or permission, or used the wrong connection")
	}

	w.openSSHPage()
	w.openSSHTerminal(primary)
	w.checkSSHResourceEntry()
	if titles := w.sessionHistory(); !slices.Equal(titles, []string{legacyTitle}) {
		t.Fatalf("SSH session history for the legacy connection = %q, want the legacy session only", titles)
	}
	w.closeSessionHistory()

	// New starts a draft on the same connection, then the composer creates it.
	w.Click(w.control(ui.RoleButton, support.Label("chat_new")))
	w.Await("New did not open an empty SSH resource draft", func() bool {
		button, ok := w.Tester.AccessByRole(ui.RoleButton, support.Label("chat_new"))
		return ok && button.Disabled && w.Tester.HasText(support.Label("chat_sidebar_empty"))
	})
	w.Click(w.control(ui.RoleTextField, support.Label("composer")))
	w.Tester.Type(freshMessage)
	w.Tester.Key(0, ui.KeyEnter)
	var fresh protocol.Session
	w.Await("sending from the SSH draft did not create a resource session", func() bool {
		for _, session := range w.Controller.Snapshot().Sessions {
			if session.ID != legacy.ID && session.ID != otherSession.ID && session.Config.Resource != nil {
				fresh = session
				return true
			}
		}
		return false
	})
	if fresh.Config.Assistant != nil || fresh.Config.Permission != protocol.PermissionAsk || fresh.Config.Resource.Kind != "ssh" ||
		support.Result[protocol.SshID](t, protocol.Output{Data: fresh.Config.Resource.Data}) != primary.ID {
		t.Fatalf("the native draft created %+v, want a fresh session on %s", fresh.Config, primary.ID)
	}
	w.checkLegacySession(legacy, binding)
	// The fixture model is unreachable; the user stops the running turn, and
	// the session controls, blocked while it runs, become available again.
	w.Click(w.control(ui.RoleButton, support.Label("chat_stop")))
	w.Await("stopping the SSH session's turn did not re-enable its history", func() bool {
		history, ok := w.Tester.AccessByRole(ui.RoleButton, support.Label("chat_connection_history"))
		return ok && !history.Disabled
	})
	titles := w.sessionHistory()
	if len(titles) != 2 || !slices.Contains(titles, legacyTitle) || slices.Contains(titles, otherTitle) || slices.Contains(titles, ordinaryTitle) {
		t.Fatalf("after creating a session the legacy connection's history = %q", titles)
	}
	w.Click(w.commandRows()[slices.Index(titles, legacyTitle)])
	w.Await("choosing the legacy session did not close the history", func() bool { return !w.commandOpen() })
	w.Await("choosing the legacy session did not leave the draft", func() bool {
		button, ok := w.Tester.AccessByRole(ui.RoleButton, support.Label("chat_new"))
		return ok && !button.Disabled
	})
	w.checkLegacySession(legacy, binding)

	// The other connection lists only its own session.
	w.Click(w.control(ui.RoleButton, sshText("home")))
	w.openSSHTerminal(other)
	w.checkSSHResourceEntry()
	if titles := w.sessionHistory(); !slices.Equal(titles, []string{otherTitle}) {
		t.Fatalf("SSH session history for the other connection = %q, want %q", titles, otherTitle)
	}
	w.closeSessionHistory()

	// Leaving the page releases its views without closing the remote
	// terminals or changing any session; returning shows the history again.
	w.App.Navigate(native.ConversationPage)
	w.Await("leaving SSH did not release the page", func() bool { return !w.Tester.HasText(sshText("ssh_assistant")) })
	running := 0
	for _, terminal := range w.Controller.Snapshot().Terminals {
		if terminal.Ssh != nil && terminal.Status.Kind == "running" {
			running++
		}
	}
	if running != 2 {
		t.Fatalf("leaving the SSH page left %d remote terminals running, want 2", running)
	}
	w.checkLegacySession(legacy, binding)
	shells := peer.shells.Load()
	w.Click(w.sshRail())
	w.checkSSHResourceEntry()
	// Reopening a connection from the landing selects its running terminal.
	w.Click(w.control(ui.RoleButton, sshText("home")))
	w.openSSHTerminal(other)
	w.checkSSHResourceEntry()
	if started := peer.shells.Load(); started != shells {
		t.Fatalf("returning to SSH started %d remote shells, want none", started-shells)
	}
	if titles := w.sessionHistory(); !slices.Equal(titles, []string{otherTitle}) {
		t.Fatalf("after returning the other connection's history = %q, want %q", titles, otherTitle)
	}
	w.closeSessionHistory()
}

func (w *codeSSHWindow) trustedSSHProfile(name string, peer *codeSSHPeer) protocol.SshProfile {
	w.T.Helper()
	profile := w.saveSSHProfile(name, peer.port)
	return support.Result[protocol.SshProfile](w.T, w.Do(protocol.Command{Kind: "trust_ssh", Data: protocol.CommandTrustSshPayload{
		Profile: profile.ID, ExpectedRevision: profile.Revision, Key: peer.key,
	}}))
}

func (w *codeSSHWindow) sessionFixtureConfig() protocol.SessionConfig {
	w.T.Helper()
	provider := protocol.DefaultConversationProvider()
	provider.ID, provider.Name, provider.Enabled = protocol.NewProviderID(), "SSH session fixture", true
	provider.Api, provider.Authentication, provider.Endpoint = protocol.ConversationModelApiChatCompletions, protocol.AuthenticationApiKey, "http://127.0.0.1:9/v1"
	model := protocol.DefaultConversationModel()
	model.ID, model.Context, model.Output = "fixture", 32000, 8000
	provider.DefaultModel, provider.Models = model.ID, []protocol.ConversationModel{model}
	secret := protocol.NewSecret("fixture-key")
	w.Do(protocol.Command{Kind: "save_provider", Data: protocol.CommandSaveProviderPayload{Provider: provider, Secret: &secret}})
	config := protocol.DefaultSessionConfig()
	config.Provider, config.Model, config.Effort = provider.ID, model.ID, model.DefaultEffort
	config.Mode, config.Permission = protocol.WorkModeCode, protocol.PermissionProject
	return config
}

// openSSHTerminal opens a trusted connection from the landing card and waits
// for its running terminal to be shown.
func (w *codeSSHWindow) openSSHTerminal(profile protocol.SshProfile) {
	w.T.Helper()
	w.clickNamed(profile.Name)
	w.Await("opening "+profile.Name+" did not show its running terminal", func() bool {
		for _, terminal := range w.Controller.Snapshot().Terminals {
			if terminal.Ssh != nil && *terminal.Ssh == profile.ID && terminal.Status.Kind == "running" {
				found, ok := w.Tester.Find(support.Label("terminal"))
				return ok && found.W > 0
			}
		}
		return false
	})
}

// sessionHistory opens the assistant column's history and returns its rows'
// titles in order.
func (w *codeSSHWindow) sessionHistory() []string {
	w.T.Helper()
	w.Click(w.control(ui.RoleButton, support.Label("chat_connection_history")))
	var rows []ui.AccessInfo
	w.Await("the SSH session history did not open", func() bool {
		rows = w.commandRows()
		return w.commandOpen() && (len(rows) != 0 || w.Tester.HasText(support.Label("chat_connection_history_empty")))
	})
	titles := make([]string, len(rows))
	for i, row := range rows {
		titles[i] = row.Name
	}
	return titles
}

func (w *codeSSHWindow) commandPopup() (ui.AccessInfo, bool) {
	for _, entry := range w.Tester.AccessAll() {
		// The popup is labelled with its search placeholder, which the rail's
		// Search button shares; only the popup is a container.
		if entry.Name == support.Label("connection_search") && entry.Role != ui.RoleTextField && entry.Role != ui.RoleButton &&
			entry.Bounds.W > 0 && entry.Bounds.H > 0 {
			return entry, true
		}
	}
	return ui.AccessInfo{}, false
}

func (w *codeSSHWindow) commandOpen() bool {
	_, ok := w.commandPopup()
	return ok
}

// chooseCommand clicks a row of the open command popup, such as the session
// history or the connections picker, once the popup offers it. Rows drawn
// behind the popup, such as landing cards, are never clicked in its place.
func (w *codeSSHWindow) chooseCommand(name string) {
	w.T.Helper()
	var row ui.AccessInfo
	w.Await("the command popup does not offer "+name, func() bool {
		for _, entry := range w.commandRows() {
			if entry.Name == name && !entry.Disabled {
				row = entry
				return true
			}
		}
		return false
	})
	w.Click(row)
}

// commandRows are the item buttons inside the open command popup.
func (w *codeSSHWindow) commandRows() []ui.AccessInfo {
	popup, ok := w.commandPopup()
	if !ok {
		return nil
	}
	inside := func(r ui.Rect) bool {
		return r.W > 0 && r.X >= popup.Bounds.X && r.Y >= popup.Bounds.Y &&
			r.X+r.W <= popup.Bounds.X+popup.Bounds.W && r.Y+r.H <= popup.Bounds.Y+popup.Bounds.H
	}
	var rows []ui.AccessInfo
	for _, entry := range w.Tester.AccessAll() {
		if entry.Role == ui.RoleButton && entry.Name != support.Label("close") && !entry.Offscreen && inside(entry.Bounds) {
			rows = append(rows, entry)
		}
	}
	return rows
}

// closeSessionHistory presses the history popup's own Close button.
func (w *codeSSHWindow) closeSessionHistory() {
	w.T.Helper()
	popup, ok := w.commandPopup()
	if !ok {
		w.T.Fatal("the SSH session history is not open")
	}
	var close ui.AccessInfo
	for _, entry := range w.Tester.AccessAll() {
		b := entry.Bounds
		if entry.Role == ui.RoleButton && entry.Name == support.Label("close") && b.W > 0 && b.X >= popup.Bounds.X && b.Y >= popup.Bounds.Y &&
			b.X+b.W <= popup.Bounds.X+popup.Bounds.W && b.Y+b.H <= popup.Bounds.Y+popup.Bounds.H {
			close = entry
		}
	}
	if close.Bounds.W == 0 {
		w.T.Fatal("the SSH session history has no Close button")
	}
	w.Click(close)
	w.Await("Close did not close the SSH session history", func() bool { return !w.commandOpen() })
}

func (w *codeSSHWindow) checkLegacySession(legacy protocol.Session, binding protocol.PluginConversationBinding) {
	w.T.Helper()
	for _, session := range w.Controller.Snapshot().Sessions {
		if session.ID != legacy.ID {
			continue
		}
		if session.Config.Assistant == nil || *session.Config.Assistant != binding || session.Config.Permission != legacy.Config.Permission ||
			session.Revision != legacy.Revision || session.Worktree != legacy.Worktree {
			w.T.Fatalf("the native resource page changed the saved legacy session: %+v", session)
		}
		return
	}
	w.T.Fatal("the legacy SSH session disappeared")
}

func (w *codeSSHWindow) installLegacySSHAssistant() protocol.PluginConversationBinding {
	w.T.Helper()
	binding := installSSHPackageFixture(w.T, w.Ctx, w.Owner)
	if err := w.Controller.Refresh(w.Ctx); err != nil {
		w.T.Fatal(err)
	}
	return binding
}

// installSSHPackageFixture installs an enabled package named ssh whose tree
// is not a verified official release, as a same-name third-party package is.
func installSSHPackageFixture(t *testing.T, ctx context.Context, owner *node.Node) protocol.PluginConversationBinding {
	t.Helper()
	root := t.TempDir()
	const entry = "dev.sailry.platform/desktop.js"
	if err := os.Mkdir(filepath.Join(root, filepath.Dir(entry)), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"$schema":"` + sdktest.ManifestSchema + `","name":"ssh","version":"0.1.1","extensions":{"dev.sailry.platform":{
  "api_version":"v1","actions":["ssh.read","conversation.read","conversation.control"],
  "desktop":{"entry":"` + entry + `","resources":["` + entry + `"],"conversations":[{"id":"ssh","resource":"ssh","tools":[],"context":"Legacy SSH assistant fixture"}]}
}}}`
	script := `import {View, div} from 'gpui-kit';
export default class Fixture extends View { render() { return div(); } }`
	for name, data := range map[string]string{"plugin.json": manifest, entry: script} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	info := installSSHPackageTree(t, ctx, owner, root)
	return protocol.PluginConversationBinding{Package: sdktest.PackageReference(info), ID: "ssh"}
}

// installSSHPackageTree reads a package tree named ssh on the Node, which
// derives its digest, and installs it enabled.
func installSSHPackageTree(t *testing.T, ctx context.Context, owner *node.Node, root string) protocol.PluginInfo {
	t.Helper()
	access := sdktest.Inspect(owner)
	info, err := access.InstallPlugin(ctx, root, "", "ssh")
	if err != nil || len(info.Issues) != 0 {
		t.Fatalf("the SSH package at %s could not be read: %v; issues: %+v", root, err, info.Issues)
	}
	if err := access.Store().Node.Write(ctx, func(db *sql.Conn) error {
		_, err := sdktest.InstallPlugin(ctx, db, info, 0, true)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return info
}
