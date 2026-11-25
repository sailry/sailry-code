package sshnode_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sailry/sailry-code/internal/testsupport/node"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

// TestSSHPrimary exercises the real Node admission, sharing and save paths;
// network effects are translated and authorized without contacting a server.
func TestSSHPrimary(t *testing.T) {
	t.Parallel()
	node, ordinary, primary, held := sshConnectionFixture(t)
	ordinaryTurn := nodetest.HoldTurn(t, node, ordinary, held.Started)
	declarations := nodetest.ConnectionDeclarations(nodetest.ConnectionTools(t, node, ordinaryTurn))
	ctx := t.Context()
	const secret = "private-ssh-primary-regression"
	other := nodetest.Command(t, node, protocol.Command{Kind: "save_ssh", Data: protocol.CommandSaveSshPayload{
		Profile:    protocol.SshProfile{ID: protocol.NewSshID(), Name: "Other private host", Host: "other.fixture.test", Port: 22, Username: "deploy", Authentication: protocol.SshAuthenticationPassword},
		Credential: &protocol.SshCredential{Kind: "password", Data: &protocol.SshCredentialPasswordPayload{Password: protocol.NewSecret(secret)}},
	}}).Data.(protocol.SshProfile)
	config := ordinary.Config
	config.Resource = &protocol.ConnectionResource{Kind: "ssh", Data: primary.ID}
	owner := declarations[protocol.ToolOperationSaveSshConnection].Owner
	const entry = "dev.sailry.platform/desktop/main.js"
	sshPlugin := nodetest.InstallPlugin(t, node, "ssh", map[string]string{
		"plugin.json": `{"$schema":"` + sdktest.ManifestSchema + `","name":"ssh","version":"0.1.0","extensions":{"dev.sailry.platform":{"api_version":"v1","scope":"host","requires_modules":["ssh"],"actions":["ssh.read","ssh.control","ssh.manage","conversation.read","conversation.control","projects.read"],"desktop":{"entry":"` + entry + `","resources":["` + entry + `"],"conversations":[{"id":"ssh","resource":"ssh","tools":[{"kind":"plugin","name":"ssh_run"},{"kind":"plugin","name":"ssh_transfer"},{"kind":"plugin","name":"ssh_connections"},{"kind":"plugin","name":"ssh_save_connection"}],"context":"Manage the selected SSH connection."}]}}}}`,
		entry:         `export function create() { return {}; }`,
	})
	config.Assistant = &protocol.PluginConversationBinding{Package: sdktest.PackageReference(sshPlugin), ID: "ssh"}
	session := nodetest.Command(t, node, protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Config: &config}}).Data.(protocol.Session)
	controller := protocol.NodeID{93}
	nodetest.TrustCaller(t, node, controller)
	admitted := nodetest.Dispatch(t, node, controller, protocol.NewRequest(node.ID(), protocol.Command{Kind: "submit_turn", Data: protocol.CommandSubmitTurnPayload{Session: session.ID, ExpectedRevision: session.Revision, Message: protocol.ConversationInput{Text: "Manage another SSH connection"}}}))
	if admitted.Fault != nil {
		t.Fatal(admitted.Fault)
	}
	turn := admitted.Output.Data.(protocol.QueuedTurn)
	inv := &native.AgentInvocation{Caller: controller, Session: session, Turn: turn}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if err := sdktest.Inspect(node).Store().Node.Read(ctx, func(db *sql.Conn) error {
			var err error
			inv.Run, err = sdktest.ReadAgentRun(ctx, db, turn.ID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if inv.Run.Status == protocol.ConversationStatusRunning {
			break
		}
		if inv.Run.Status != protocol.ConversationStatusQueued || time.Now().After(deadline) {
			t.Fatalf("SSH assistant did not start: %+v", inv.Run)
		}
		time.Sleep(10 * time.Millisecond)
	}
	inv.Worktree.ID = session.Worktree
	var captured []protocol.ConnectionResource
	if err := sdktest.Inspect(node).Store().Node.Read(ctx, func(db *sql.Conn) error {
		ordinaryCatalog, err := sdktest.ReadConnectionCatalog(ctx, db, ordinary.ID)
		if err != nil {
			return err
		}
		for _, profile := range ordinaryCatalog.Ssh {
			if profile.ID == other.ID {
				t.Error("the SSH assistant widened the ordinary project's sharing")
			}
		}
		catalog, err := sdktest.ReadConnectionCatalog(ctx, db, session.ID)
		captured = catalog.Resources()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	input, err := sdktest.Inspect(node).ConnectionTurnInput(ctx, inv, []protocol.ToolOperation{protocol.ToolOperationRunSsh})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(input)
	if err != nil || !strings.Contains(string(encoded), primary.ID.String()) || !strings.Contains(string(encoded), other.ID.String()) || strings.Contains(string(encoded), secret) {
		t.Fatalf("SSH initializer omitted a private connection or exposed credentials: %s, %v", encoded, err)
	}
	scope := protocol.PluginContext{Package: owner, Turn: &turn.ID, Session: &session.ID, Worktree: &session.Worktree, Surface: protocol.PluginDesktopSurface{Kind: "workspace"}}
	list := protocol.NewRequest(node.ID(), protocol.Command{Kind: "list_ssh"})
	list.Plugin = &scope
	listed := nodetest.Dispatch(t, node, node.ID(), list)
	if listed.Fault != nil {
		t.Fatal(listed.Fault)
	}
	encoded, err = json.Marshal(listed.Output)
	if err != nil || !strings.Contains(string(encoded), other.ID.String()) || strings.Contains(string(encoded), secret) {
		t.Fatalf("SSH list lost private profiles or exposed credentials: %s, %v", encoded, err)
	}
	translate := func(operation protocol.ToolOperation, args map[string]any) protocol.Command {
		t.Helper()
		body, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		command, err := sdktest.Inspect(node).ConnectionCommand(ctx, inv, declarations[operation], body, captured)
		if err != nil {
			t.Fatal(err)
		}
		return command
	}
	run := translate(protocol.ToolOperationRunSsh, map[string]any{"connection": other.ID, "command": "uptime", "timeout_ms": 1000})
	transfer := translate(protocol.ToolOperationTransferSsh, map[string]any{"connection": other.ID, "path": "local.txt", "remote_path": "/tmp/remote.txt", "direction": "upload", "timeout_ms": 1000})
	for _, command := range []protocol.Command{run, transfer} {
		request := protocol.NewRequest(node.ID(), command)
		request.Plugin = &scope
		if err := sdktest.Inspect(node).Store().Node.Read(ctx, func(db *sql.Conn) error {
			_, _, err := sdktest.PluginConnectionAction(ctx, db, request)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	save := func(command protocol.Command) (protocol.Output, *protocol.Fault) {
		t.Helper()
		response := nodetest.SaveConnection(t, node, scope, declarations[protocol.ToolOperationSaveSshConnection], command, func(proof protocol.Command) {
			input, valid := nodetest.Payload[protocol.CommandSaveSshPayload](proof.Data)
			if !valid || input.Credential != nil {
				t.Fatal("SSH save proof retained credentials")
			}
		})
		return response.Output, response.Fault
	}
	edit := translate(protocol.ToolOperationSaveSshConnection, map[string]any{"connection": other.ID, "expected_revision": other.Revision, "name": "Renamed private host"})
	unapproved := protocol.NewRequest(node.ID(), edit)
	unapproved.Plugin = &scope
	if _, err := node.Dispatch(ctx, node.ID(), unapproved); !nodetest.FaultIs(err, protocol.ErrorCodePermissionDenied) {
		t.Fatalf("unapproved SSH edit was admitted: %v", err)
	}
	updated, fault := save(edit)
	if fault != nil {
		t.Fatal(fault)
	}
	other = updated.Data.(protocol.SshProfile)
	if other.Name != "Renamed private host" || other.Sharing != nil || other.HostKey != nil {
		t.Fatalf("private SSH edit changed its scope or trust: %+v", other)
	}
	if _, fault := save(edit); fault == nil || fault.Code != protocol.ErrorCodeRevisionConflict {
		t.Fatalf("stale SSH edit returned %+v", fault)
	}
	create := translate(protocol.ToolOperationSaveSshConnection, map[string]any{"name": "New private host", "host": "new.fixture.test", "port": 22, "username": "deploy", "authentication": "password", "credential": protocol.SshCredential{Kind: "password", Data: &protocol.SshCredentialPasswordPayload{Password: protocol.NewSecret("new-ssh-save-proof-secret")}}})
	created, fault := save(create)
	if fault != nil {
		t.Fatal(fault)
	}
	added := created.Data.(protocol.SshProfile)
	if added.ID == primary.ID || added.Sharing != nil || added.HostKey != nil {
		t.Fatalf("new SSH profile changed primary scope or trusted a key: %+v", added)
	}
	if err := sdktest.Inspect(node).Store().Node.Read(ctx, func(db *sql.Conn) error {
		created, err := sdktest.ConnectionCreated(ctx, db, turn.ID, owner, protocol.ConnectionResource{Kind: "ssh", Data: added.ID})
		if err == nil && !created {
			t.Error("a remote controller's turn lost its execution Node's SSH save proof")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	command := translate(protocol.ToolOperationRunSsh, map[string]any{"connection": added.ID, "command": "uname -a", "timeout_ms": 1000})
	if command.Data.(*protocol.CommandRunSshPayload).Profile != added.ID {
		t.Fatal("the new connection was not usable in its creating turn")
	}
	trust := protocol.NewRequest(node.ID(), protocol.Command{Kind: "trust_ssh", Data: protocol.CommandTrustSshPayload{Profile: added.ID, ExpectedRevision: added.Revision, Key: protocol.SshHostKey{Algorithm: "ssh-ed25519", Fingerprint: "SHA256:unapproved"}}})
	trust.Plugin = &scope
	if _, err := node.Dispatch(ctx, node.ID(), trust); !nodetest.FaultIs(err, protocol.ErrorCodePermissionDenied) {
		t.Fatalf("SSH key was trusted without its fingerprint approval: %v", err)
	}
	if err := sdktest.Inspect(node).Store().Node.Read(ctx, func(db *sql.Conn) error {
		current, err := sdktest.ReadSession(ctx, db, session.ID)
		if err != nil {
			return err
		}
		resource, err := json.Marshal(current.Config.Resource)
		if err != nil {
			return err
		}
		primaryResource, err := json.Marshal(session.Config.Resource)
		if err != nil {
			return err
		}
		if current.Worktree != session.Worktree || string(resource) != string(primaryResource) || !reflect.DeepEqual(current.Config.Assistant, session.Config.Assistant) || current.Config.Permission != session.Config.Permission || current.Config.Mode != session.Config.Mode {
			t.Error("SSH operations changed the primary conversation association")
		}
		_, credential, err := sdktest.ReadSsh(ctx, db, other.ID, other.Revision)
		if err == nil {
			password, valid := nodetest.Payload[protocol.SshCredentialPasswordPayload](credential.Data)
			if credential.Kind != "password" || !valid || password.Password.Expose() != secret {
				t.Error("an edit without credentials replaced the saved credential")
			}
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	disabled := nodetest.Command(t, node, protocol.Command{Kind: "set_plugin_enabled", Data: protocol.CommandSetPluginEnabledPayload{
		Name: sshPlugin.Summary.Name, ExpectedRevision: sshPlugin.Summary.Revision, Enabled: false,
	}}).Data.(protocol.PluginInfo)
	assertUnavailable := func(packageState string) {
		t.Helper()
		request := protocol.NewRequest(node.ID(), protocol.Command{Kind: "submit_turn", Data: protocol.CommandSubmitTurnPayload{
			Session: session.ID, ExpectedRevision: session.Revision, Message: protocol.ConversationInput{Text: "Reuse the legacy session"},
		}})
		if fault := nodetest.Dispatch(t, node, controller, request).Fault; fault == nil || fault.Code != protocol.ErrorCodeNotConfigured {
			t.Fatalf("legacy SSH session started with its package %s: %+v", packageState, fault)
		}
	}
	assertUnavailable("disabled")
	if installed := nodetest.Command(t, node, protocol.Command{Kind: "list_plugins"}).Data.([]protocol.PluginSummary); !slices.ContainsFunc(installed, func(summary protocol.PluginSummary) bool {
		return summary.Name == disabled.Summary.Name && !summary.Enabled && summary.Digest == sshPlugin.Summary.Digest
	}) {
		t.Fatal("reopening the legacy session changed its disabled package")
	}
	nodetest.Command(t, node, nodetest.DecodeCommand(t, "remove_plugin", map[string]any{
		"name": disabled.Summary.Name, "expected_revision": disabled.Summary.Revision,
	}))
	assertUnavailable("removed")
	if err := sdktest.Inspect(node).Store().Node.Read(ctx, func(db *sql.Conn) error {
		installed, err := sdktest.ReadPlugin(ctx, db, disabled.Summary.Name)
		if err != nil {
			return err
		}
		if installed != nil {
			return errors.New("reopening the legacy session restored its removed package")
		}
		stored, err := sdktest.ReadSession(ctx, db, session.ID)
		if err != nil {
			return err
		}
		history, err := sdktest.ReadConversation(ctx, db, session.ID, nil, 100)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(stored.Config.Assistant, session.Config.Assistant) || !slices.ContainsFunc(history.Page.Runs, func(run protocol.ConversationRun) bool { return run.Turn == turn.ID }) {
			return errors.New("unavailable SSH plugin changed the legacy binding or history")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
