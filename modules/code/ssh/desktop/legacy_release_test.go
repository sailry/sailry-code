package desktop_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	nodetest "github.com/sailry/sailry-code/internal/testsupport/node"
	ssh "github.com/sailry/sailry-code/modules/code/ssh/desktop"
	"github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

const (
	sshReleaseDigest = "9990c72c27c647a3000cb1385b26550af1e9c5eb2a7cd2823aaed3c21e484a8f"
	sshReleaseTitle  = "Archived SSH assistant"
	sshReleasePrompt = "Use the saved SSH connection"
)

func TestCodeSSHLegacyReleaseRuns(t *testing.T) {
	w, script, peer, _, binding := openArchivedSSH(t)
	profile := w.saveSSHProfile("Archived SSH connection", peer.port)
	legacy := legacyReleaseSession(w, binding, profile.ID)
	const download = "legacy-download.txt"
	worktrees := nodetest.Snapshot(t, w.Owner).Worktrees
	index := slices.IndexFunc(worktrees, func(tree protocol.Worktree) bool { return tree.ID == legacy.Worktree })
	if index < 0 {
		t.Fatal("the legacy session lost its resource worktree")
	}
	target := filepath.Join(worktrees[index].Path, download)
	runArgs := func(id protocol.SshID) func() string {
		return nodetest.Arguments(map[string]any{"connection": id, "command": codeSSHCommand, "timeout_ms": 10000})
	}
	script.Reset(
		nodetest.Step{ID: "find-ssh", Tool: "tool_search", Arguments: nodetest.Arguments(map[string]string{"query": "select:ssh_run,ssh_transfer"})},
		nodetest.Step{ID: "outside-scope", Tool: "ssh_run", Arguments: runArgs(protocol.NewSshID())},
		nodetest.Step{ID: "bound-command", Tool: "ssh_run", Arguments: runArgs(profile.ID)},
		nodetest.Step{ID: "bound-transfer", Tool: "ssh_transfer", Arguments: nodetest.Arguments(map[string]any{
			"connection": profile.ID, "direction": "download", "path": download, "remote_path": codeSSHHome + "/docs/" + codeSSHFile,
		})},
		nodetest.Step{ID: "answer"},
	)
	approvals := 0
	captured, results := nodetest.Turn(t, w.Owner, legacy, sshReleasePrompt, 25*time.Second, func(approval protocol.ConversationApproval) {
		if approval.Session != legacy.ID || approval.Source != protocol.ConversationApprovalSourceUser || approvals >= 2 {
			t.Fatalf("unexpected SSH approval: %+v", approval)
		}
		if got := peer.commands.Load(); got != int32(approvals) {
			t.Fatalf("the peer received %d commands before approval %d", got, approvals+1)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("the download was not absent before approval: %v", err)
		}
		if approvals == 0 {
			want := protocol.SshTrust{Profile: profile.ID, ExpectedRevision: profile.Revision, Host: profile.Host, Port: profile.Port, Key: peer.key}
			if approval.Ssh == nil || *approval.Ssh != want || approval.CanRemember || peer.authentications.Load() != 0 {
				t.Fatalf("first approval must present the bound fingerprint before authentication: %+v", approval)
			}
		} else if approval.Ssh != nil {
			t.Fatalf("the transfer did not use the already approved fingerprint: %+v", approval)
		}
		approvals++
	})
	if !slices.Equal(captured, []protocol.PluginReference{binding.Package}) || approvals != 2 || peer.commands.Load() != 1 {
		t.Fatalf("legacy turn captured %v, approvals %d, peer commands %d", captured, approvals, peer.commands.Load())
	}
	if rejected := results["outside-scope"]; rejected == nil || rejected.Status != sdktest.ToolErrorStatus ||
		!strings.Contains(string(rejected.Result.Value), "SSH connection is unavailable to this session") {
		t.Fatalf("a target outside the captured catalog did not return its scope error: %+v", rejected)
	}
	result := nodetest.Result(t, results, "bound-command", "ssh_run")
	outcome, ok := result["data"].(map[string]any)
	if !ok || result["kind"] != "ssh_outcome" || result["connection_name"] != profile.Name ||
		outcome["kind"] != "completed" || outcome["exit_code"] != float64(0) || outcome["stdout"] != codeSSHOutput {
		t.Fatalf("unexpected bound SSH result: %v", result)
	}
	if result := nodetest.Result(t, results, "bound-transfer", "ssh_transfer"); result["connection_name"] != profile.Name {
		t.Fatalf("unexpected bound transfer result: %v", result)
	}
	if contents, err := os.ReadFile(target); err != nil || string(contents) != codeSSHContents {
		t.Fatalf("the transfer did not land in its captured worktree: %q, %v", contents, err)
	}
	if trusted := w.onlySSHProfile(); trusted.ID != profile.ID || trusted.Revision != profile.Revision+1 || trusted.HostKey == nil || *trusted.HostKey != peer.key {
		t.Fatalf("approval did not record trust on the bound profile: %+v", trusted)
	}
	first := script.Requests()[0]
	t.Logf("SSH in the initial model request: run=%t transfer=%t (both succeeded after discovery)", slices.Contains(first, "ssh_run"), slices.Contains(first, "ssh_transfer"))
	stored, history := readSSHSession(t, w.Owner, legacy.ID)
	if len(history.Page.Runs) != 1 {
		t.Fatalf("unexpected legacy history: %+v", history.Page.Runs)
	}
	turn := history.Page.Runs[0].Turn
	assertLegacySSH(t, stored, legacy, profile.ID)

	w.openSSHPage()
	w.openSSHTerminal(profile)
	w.checkSSHResourceEntry()
	showLegacySSH(w)
	info := support.Result[protocol.PluginInfo](t, w.Do(nodetest.DecodeCommand(t, "read_plugin", map[string]any{"name": "ssh"})))
	info = support.Result[protocol.PluginInfo](t, w.Do(protocol.Command{Kind: "set_plugin_enabled", Data: protocol.CommandSetPluginEnabledPayload{
		Name: "ssh", ExpectedRevision: info.Summary.Revision, Enabled: false,
	}}))
	assertSSHRefused(t, w.Owner, stored, "plugin_disabled")
	if info.Summary.Enabled || sdktest.PackageReference(info) != binding.Package {
		t.Fatalf("disabling changed the installed package: %+v", info.Summary)
	}
	w.Do(nodetest.DecodeCommand(t, "remove_plugin", map[string]any{"name": "ssh", "expected_revision": info.Summary.Revision}))
	assertSSHRefused(t, w.Owner, stored, "plugin_unavailable")
	if got := support.Result[[]protocol.PluginSummary](t, w.Do(protocol.Command{Kind: "list_plugins"})); len(got) != 0 {
		t.Fatalf("the removed package was reinstalled: %+v", got)
	}
	persisted, history := readSSHSession(t, w.Owner, legacy.ID)
	assertLegacySSH(t, persisted, stored, profile.ID)
	if len(history.Page.Runs) != 1 || history.Page.Runs[0].Turn != turn || history.Page.Runs[0].Status != protocol.ConversationStatusCompleted || len(script.Requests()) != 5 || peer.commands.Load() != 1 {
		t.Fatal("disable or remove changed the legacy history or admitted another run")
	}
	showLegacySSH(w)

	script.Reset(
		nodetest.Step{ID: "native-tools", Tool: "tool_search", Arguments: nodetest.Arguments(map[string]string{"query": "select:ssh_run,ssh_transfer,ssh_connections,ssh_save_connection"})},
		nodetest.Step{ID: "answer"},
	)
	w.Click(w.control(ui.RoleButton, support.Label("chat_new")))
	w.Await("New did not open a fresh SSH draft", func() bool { return w.Tester.HasText(support.Label("chat_sidebar_empty")) })
	w.Click(w.control(ui.RoleTextField, support.Label("composer")))
	w.Tester.Type("Start a native SSH conversation")
	w.Tester.Key(0, ui.KeyEnter)
	var fresh protocol.Session
	w.Await("the native draft did not submit without a package", func() bool {
		for _, session := range w.Controller.Snapshot().Sessions {
			if session.ID == legacy.ID || !ssh.SessionMatches(session, profile.ID) {
				continue
			}
			fresh = session
			_, history := readSSHSession(t, w.Owner, session.ID)
			return len(history.Page.Runs) == 1 && history.Page.Runs[0].Status == protocol.ConversationStatusCompleted
		}
		return false
	})
	if fresh.Config.Assistant != nil || fresh.Config.Permission != protocol.PermissionAsk {
		t.Fatalf("the native session inherited the old assistant or permission: %+v", fresh.Config)
	}
	_, offered := script.Observation("native-tools")
	if !slices.Contains(offered, "ssh_run") || !slices.Contains(offered, "ssh_transfer") ||
		slices.Contains(offered, "ssh_connections") || slices.Contains(offered, "ssh_save_connection") {
		t.Fatalf("the native session discovered tools outside its primary connection scope: %v", offered)
	}
}

func TestDefaultHostKeepsInstalledSSH(t *testing.T) {
	w, script, peer, profilePath, binding := openArchivedSSH(t)
	profile := w.trustedSSHProfile("Saved SSH for default Host", peer)
	legacy := legacyReleaseSession(w, binding, profile.ID)
	script.Reset(nodetest.Step{ID: "answer"})
	nodetest.Turn(t, w.Owner, legacy, "Save a legacy turn", 15*time.Second, nil)
	if err := w.Window.Close(); err != nil {
		t.Fatal(err)
	}
	stopSSHNode(t, w.Owner)
	defaultScript := nodetest.NewScript()
	host := nodetest.Start(t, profilePath, defaultScript, node.Options{Assembly: node.HarnessAssembly()})
	t.Cleanup(func() { stopSSHNode(t, host) })
	inventory := nodetest.Snapshot(t, host).Plugins
	if len(inventory) != 1 || inventory[0].Name != "ssh" || inventory[0].Digest != binding.Package.Digest ||
		inventory[0].SettingsRevision != binding.Package.SettingsRevision || !inventory[0].Enabled || inventory[0].Compatibility.Kind != "unsupported" {
		t.Fatalf("default Host should retain the enabled installation but mark it unsupported: %+v", inventory)
	}
	if _, err := os.Stat(filepath.Join(profilePath, "plugins", "packages", binding.Package.Digest, "plugin.json")); err != nil {
		t.Fatalf("default Host did not retain the captured immutable content: %v", err)
	}
	stored, history := readSSHSession(t, host, legacy.ID)
	assertLegacySSH(t, stored, legacy, profile.ID)
	if len(history.Page.Runs) != 1 || history.Page.Runs[0].Status != protocol.ConversationStatusCompleted {
		t.Fatalf("default Host did not preserve the old turn: %+v", history.Page.Runs)
	}
	assertSSHRefused(t, host, stored, "plugin_unavailable")
	if len(defaultScript.Requests()) != 0 || peer.commands.Load() != 0 {
		t.Fatal("the unsupported package ran on the default Host")
	}
}

func openArchivedSSH(t *testing.T) (*codeSSHWindow, *nodetest.Script, *codeSSHPeer, string, protocol.PluginConversationBinding) {
	t.Helper()
	source := os.Getenv("SAILRY_TEST_LEGACY_SSH_DIR")
	if source == "" {
		t.Skip("SAILRY_TEST_LEGACY_SSH_DIR must name the official ssh 0.1.33 release")
	}
	peer := newCodeSSHPeer(t)
	script := nodetest.NewScript()
	profile := filepath.Join(t.TempDir(), "profile")
	owner := nodetest.Start(t, profile, script, node.Options{ProductID: "code", Assembly: sshHost()})
	t.Cleanup(func() { stopSSHNode(t, owner) })
	testNode := &support.Node{T: t, Ctx: t.Context(), Profile: profile, Owner: owner}
	w := &codeSSHWindow{testNode.Connect(sshDesktop())}
	root := t.TempDir()
	if err := os.CopyFS(filepath.Join(root, "ssh"), os.DirFS(source)); err != nil {
		t.Fatal(err)
	}
	project := w.CreateProject("Archived package source", root)
	worktrees := nodetest.Snapshot(t, owner).Worktrees
	index := slices.IndexFunc(worktrees, func(tree protocol.Worktree) bool {
		return tree.Project != nil && *tree.Project == project.ID && tree.Main
	})
	if index < 0 {
		t.Fatal("the archive source project has no main worktree")
	}
	info := support.Result[protocol.PluginInfo](t, w.Do(nodetest.DecodeCommand(t, "install_plugin", map[string]any{
		"name": "ssh", "path": "ssh", "worktree": worktrees[index].ID, "expected_revision": 0, "expected_digest": sshReleaseDigest,
	})))
	if info.Summary.Version == nil || *info.Summary.Version != "0.1.33" || info.Summary.Digest != sshReleaseDigest ||
		!info.Summary.Enabled || info.Summary.Compatibility.Kind != "compatible" || len(info.Issues) != 0 || info.Extension == nil ||
		info.Extension.Host != nil || len(info.Extension.Tools) != 0 || info.Extension.Desktop == nil || len(info.Extension.Desktop.Conversations) != 1 {
		t.Fatalf("expected the real desktop-only ssh 0.1.33 package: %+v", info)
	}
	assistant := info.Extension.Desktop.Conversations[0]
	if assistant.ID != "ssh" || assistant.Resource != "ssh" || len(assistant.Tools) != 4 {
		t.Fatalf("unexpected archived SSH assistant: %+v", assistant)
	}
	return w, script, peer, profile, protocol.PluginConversationBinding{Package: sdktest.PackageReference(info), ID: assistant.ID}
}

func legacyReleaseSession(w *codeSSHWindow, binding protocol.PluginConversationBinding, profile protocol.SshID) protocol.Session {
	w.T.Helper()
	config := nodetest.SessionConfig(w.T, w.Owner, 32000, protocol.PermissionAsk)
	config.Resource = &protocol.ConnectionResource{Kind: "ssh", Data: profile}
	config.Assistant = &binding
	session := support.Result[protocol.Session](w.T, w.Do(protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Config: &config}}))
	return support.Result[protocol.Session](w.T, w.Do(protocol.Command{Kind: "rename_session", Data: protocol.CommandRenameSessionPayload{
		Session: session.ID, ExpectedRevision: session.Revision, Title: sshReleaseTitle,
	}}))
}

func readSSHSession(t *testing.T, owner *node.Node, id protocol.SessionID) (protocol.Session, protocol.ConversationHistory) {
	t.Helper()
	var session protocol.Session
	var history protocol.ConversationHistory
	ctx := t.Context()
	if err := sdktest.Inspect(owner).Store().Node.Read(ctx, func(db *sql.Conn) error {
		var err error
		session, err = sdktest.ReadSession(ctx, db, id)
		if err == nil {
			history, err = sdktest.ReadConversation(ctx, db, id, nil, 100)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return session, history
}

func assertLegacySSH(t *testing.T, got, want protocol.Session, profile protocol.SshID) {
	t.Helper()
	if got.ID != want.ID || got.Worktree != want.Worktree || got.Config.Permission != want.Config.Permission ||
		got.Config.Assistant == nil || *got.Config.Assistant != *want.Config.Assistant || !ssh.SessionMatches(got, profile) {
		t.Fatalf("the saved SSH binding or scope changed: %+v", got)
	}
}

func assertSSHRefused(t *testing.T, owner *node.Node, session protocol.Session, reason string) {
	t.Helper()
	_, fault := nodetest.Call[protocol.QueuedTurn](t, owner, protocol.Command{Kind: "submit_turn", Data: protocol.CommandSubmitTurnPayload{
		Session: session.ID, ExpectedRevision: session.Revision, Message: protocol.ConversationInput{Text: "Reuse the archived assistant"},
	}})
	if fault == nil || fault.Code != protocol.ErrorCodeNotConfigured || string(fault.ConfigurationReason()) != reason {
		t.Fatalf("submission should be refused with %s, got %+v", reason, fault)
	}
}

func showLegacySSH(w *codeSSHWindow) {
	w.T.Helper()
	if titles := w.sessionHistory(); !slices.Equal(titles, []string{sshReleaseTitle}) {
		w.T.Fatalf("the SSH page lost the old session: %v", titles)
	}
	w.chooseCommand(sshReleaseTitle)
	w.Await("the SSH page did not reopen the old transcript", func() bool {
		return !w.commandOpen() && w.Tester.HasText(sshReleasePrompt)
	})
}

func stopSSHNode(t *testing.T, owner *node.Node) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := owner.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}
