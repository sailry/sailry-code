package codehost_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"
	codehost "github.com/sailry/sailry-code/internal/assembly/codehost"
	"github.com/sailry/sailry-code/internal/testsupport/node"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/native/process"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

var connectionOperations = []protocol.ToolOperation{
	protocol.ToolOperationListDatabaseConnections, protocol.ToolOperationSaveDatabaseConnection, protocol.ToolOperationBrowseDatabase,
	protocol.ToolOperationQueryDatabase, protocol.ToolOperationExecuteDatabase,
	protocol.ToolOperationListSshConnections, protocol.ToolOperationSaveSshConnection, protocol.ToolOperationRunSsh, protocol.ToolOperationTransferSsh,
}

func connectionModelConfig(t *testing.T, node *native.Node) protocol.SessionConfig {
	t.Helper()
	return nodetest.SessionConfig(t, node, 32000, protocol.PermissionAsk)
}

func TestConnectionTools(t *testing.T) {
	t.Parallel()
	t.Run("detached", testDetachedConnectionTools)

	held := &nodetest.HeldModel{Started: make(chan struct{}), Release: make(chan struct{})}
	environment := process.NewEnvironment([]string{"PATH=/usr/bin:/bin"})
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	node, err := native.StartWithOptions(context.Background(), filepath.Join(t.TempDir(), "profile"), native.Options{Assembly: codehost.Assembly(), MCP: sdktest.MCPOptions(), Environment: &environment, Model: func(context.Context, sdktest.Binding) (fantasy.LanguageModel, error) { return held, nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		close(held.Release)
		if err := node.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	ctx := t.Context()
	project := nodetest.Command(t, node, protocol.Command{Kind: "create_project", Data: protocol.ProjectsDraft{Name: "Fixture", Path: root, Source: protocol.ProjectsSource{Kind: "local"}, Appearance: protocol.DefaultProjectsAppearance()}}).Data.(protocol.Project)
	config := connectionModelConfig(t, node)

	const databasePassword, sshPassword = "fixture-database-password", "fixture-ssh-password"
	network := func(host string) protocol.DatabaseConnection {
		return protocol.DatabaseConnection{Kind: "network", Data: &protocol.DatabaseConnectionNetworkPayload{
			Engine: protocol.DatabaseEnginePostgres, Host: host, Port: 5432, Database: "app", Username: "reader", Tls: protocol.DatabaseTlsRequire}}
	}
	saveDatabase := func(name, host string, sharing *protocol.ConnectionSharing) protocol.DatabaseProfile {
		password := protocol.NewSecret(databasePassword)
		output := nodetest.Command(t, node, protocol.Command{Kind: "save_database", Data: protocol.CommandSaveDatabasePayload{
			Profile: protocol.DatabaseProfile{ID: protocol.NewDatabaseID(), Name: name, Connection: network(host), ReadOnly: true, Sharing: sharing}, Password: &password}})
		profile, valid := nodetest.Payload[protocol.DatabaseProfile](output.Data)
		if !valid {
			t.Fatalf("saved database = %#v", output)
		}
		return profile
	}
	shared := saveDatabase("Shared", "shared.fixture.test", &protocol.ConnectionSharing{Kind: "global"})
	private := saveDatabase("Private", "private.fixture.test", nil)
	output := nodetest.Command(t, node, protocol.Command{Kind: "save_ssh", Data: protocol.CommandSaveSshPayload{
		Profile: protocol.SshProfile{ID: protocol.NewSshID(), Name: "Bastion", Host: "bastion.fixture.test", Port: 22, Username: "deploy",
			Authentication: protocol.SshAuthenticationPassword, Sharing: &protocol.ConnectionSharing{Kind: "projects", Data: &[]protocol.ProjectID{project.ID}}},
		Credential: &protocol.SshCredential{Kind: "password", Data: &protocol.SshCredentialPasswordPayload{Password: protocol.NewSecret(sshPassword)}}}})
	bastion, valid := nodetest.Payload[protocol.SshProfile](output.Data)
	if !valid {
		t.Fatalf("saved SSH connection = %#v", output)
	}

	session := nodetest.Command(t, node, protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Project: &project.ID, Config: &config}}).Data.(protocol.Session)
	nodetest.Command(t, node, protocol.Command{Kind: "submit_turn", Data: protocol.CommandSubmitTurnPayload{Session: session.ID, ExpectedRevision: session.Revision, Message: protocol.ConversationInput{Text: "Inspect the database"}}})
	select {
	case <-held.Started:
	case <-time.After(time.Minute):
		t.Fatal("the held turn never reached its model")
	}

	// Every reference is validated before a turn is admitted.
	desktop := t.TempDir()
	if err := os.WriteFile(filepath.Join(desktop, "plugin.json"), []byte(`{"$schema":"`+sdktest.ManifestSchema+`","name":"desktop-fixture","version":"0.1.0","extensions":{"dev.sailry.platform":{"api_version":"v1","scope":"desktop"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture, err := sdktest.Inspect(node).InstallPlugin(ctx, desktop, "", "desktop-fixture")
	if err != nil || len(fixture.Issues) != 0 {
		t.Fatalf("desktop fixture is invalid: %+v, %v", fixture.Issues, err)
	}
	if err := sdktest.Inspect(node).Store().Node.Write(ctx, func(db *sql.Conn) error {
		_, err := sdktest.InstallPlugin(ctx, db, fixture, 0, true)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	nodetest.InstallPlugin(t, node, "skill-fixture", map[string]string{
		"plugin.json": `{"$schema":"` + sdktest.ManifestSchema + `","name":"skill-fixture","version":"0.1.0","extensions":{"dev.sailry.platform":{"api_version":"v1","scope":"host"}}}`,
	})
	bound := config
	bound.Resource = &protocol.ConnectionResource{Kind: "database", Data: shared.ID}
	connection := nodetest.Command(t, node, protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Config: &bound}}).Data.(protocol.Session)
	detached := nodetest.Command(t, node, protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Config: &config}}).Data.(protocol.Session)
	if len(session.Roles.Profiles) < 2 {
		t.Fatalf("session roster = %+v", session.Roles)
	}
	target := func(kind string, data any) protocol.ConversationReference {
		return protocol.ConversationReference{Target: protocol.ConversationReferenceTarget{Kind: kind, Data: data}, Label: kind}
	}
	text := func(value string) *string { return &value }
	role := func(index int) *protocol.RoleReference {
		return &protocol.RoleReference{ID: session.Roles.Profiles[index].ID, Revision: session.Roles.Profiles[index].Revision}
	}
	unknownSsh, unknownSession, unknownAttachment := protocol.NewSshID(), protocol.NewSessionID(), protocol.NewAttachmentID()
	blank, long := target("file", text("note.txt")), target("file", text("note.txt"))
	blank.Label, long.Label = " ", strings.Repeat("a", 513)
	labels, roster := "references require distinct targets and bounded labels", "select one role from the frozen session roster"
	const managedWorktreePrefix = "file tools work only in this turn's worktree; delegate work in a managed worktree with spawn_agent and its worktree ID"
	for _, rejected := range []struct {
		name       string
		session    protocol.Session
		references []protocol.ConversationReference
		code       protocol.ErrorCode
		message    string
	}{
		{"blank label", session, []protocol.ConversationReference{blank}, protocol.ErrorCodeInvalidRequest, labels},
		{"long label", session, []protocol.ConversationReference{long}, protocol.ErrorCodeInvalidRequest, labels},
		{"repeated target", session, []protocol.ConversationReference{target("file", text("note.txt")), target("file", text("note.txt"))}, protocol.ErrorCodeInvalidRequest, labels},
		{"connection conversation", connection, []protocol.ConversationReference{target("file", text("note.txt"))}, protocol.ErrorCodePermissionDenied, "connection conversations only accept connection references"},
		{"missing plugin", session, []protocol.ConversationReference{target("plugin", text("missing-plugin"))}, protocol.ErrorCodeInvalidRequest, "referenced plugin is disabled or unavailable"},
		{"desktop plugin", session, []protocol.ConversationReference{target("plugin", text("desktop-fixture"))}, protocol.ErrorCodeInvalidRequest, "desktop-only plugins cannot be referenced by an agent"},
		{"missing skill", session, []protocol.ConversationReference{target("skill", &protocol.ConversationReferenceTargetSkillPayload{Package: "skill-fixture", Name: "missing"})}, protocol.ErrorCodeInvalidRequest, "referenced skill is unavailable in this plugin version"},
		{"unreachable SSH", session, []protocol.ConversationReference{target("ssh", &unknownSsh)}, protocol.ErrorCodePermissionDenied, "referenced SSH connection is not available to this conversation"},
		{"unreachable database", session, []protocol.ConversationReference{target("database", &protocol.ConversationReferenceTargetDatabasePayload{Connection: private.ID})}, protocol.ErrorCodePermissionDenied, "referenced database is not available to this conversation"},
		{"table without database", session, []protocol.ConversationReference{target("database", &protocol.ConversationReferenceTargetDatabasePayload{Connection: shared.ID, Table: &protocol.DatabaseTable{Schema: "public", Name: "users"}})}, protocol.ErrorCodeInvalidRequest, "invalid database reference"},
		{"parent path", session, []protocol.ConversationReference{target("file", text("../note.txt"))}, protocol.ErrorCodeInvalidRequest, "expected a normalized worktree-relative path"},
		{"managed worktree path", session, []protocol.ConversationReference{target("directory", text(".sailry/worktrees/other"))}, protocol.ErrorCodePermissionDenied, managedWorktreePrefix},
		{"role outside the roster", session, []protocol.ConversationReference{target("agent", &protocol.RoleReference{ID: protocol.NewRoleID(), Revision: 1})}, protocol.ErrorCodeInvalidRequest, roster},
		{"two roles", session, []protocol.ConversationReference{target("agent", role(0)), target("agent", role(1))}, protocol.ErrorCodeInvalidRequest, roster},
		{"missing session", session, []protocol.ConversationReference{target("session", &unknownSession)}, protocol.ErrorCodeNotFound, "session does not exist on this Node"},
		{"session of another project", session, []protocol.ConversationReference{target("session", &detached.ID)}, protocol.ErrorCodeWrongTarget, "conversation belongs to another project"},
		{"foreign attachment", session, []protocol.ConversationReference{target("attachment", &unknownAttachment)}, protocol.ErrorCodeInvalidRequest, "attachment references require an attachment of the same input"},
	} {
		turns := func() (count int) {
			if err := sdktest.Inspect(node).Store().Node.Read(ctx, func(db *sql.Conn) error {
				return db.QueryRowContext(ctx, "SELECT count(*) FROM turns WHERE session=?1", rejected.session.ID.String()).Scan(&count)
			}); err != nil {
				t.Fatal(err)
			}
			return count
		}
		before := turns()
		response := nodetest.Dispatch(t, node, node.ID(), protocol.NewRequest(node.ID(), protocol.Command{Kind: "queue_turn", Data: protocol.CommandQueueTurnPayload{
			Session: rejected.session.ID, ExpectedRevision: rejected.session.Revision, Message: protocol.ConversationInput{Text: "Use this", References: rejected.references},
		}}))
		after := turns()
		if response.Fault == nil || response.Fault.Code != rejected.code || after != before {
			t.Fatalf("%s: fault %+v, %d turns before and %d after", rejected.name, response.Fault, before, after)
		}
		message := response.Fault.Message
		if rejected.message == managedWorktreePrefix {
			if !strings.HasPrefix(message, managedWorktreePrefix) {
				t.Fatalf("%s: fault message %q does not start with %q", rejected.name, message, managedWorktreePrefix)
			}
		} else if message != rejected.message {
			t.Fatalf("%s: fault message %q, want %q", rejected.name, message, rejected.message)
		}
	}

	inv := &native.AgentInvocation{Caller: node.ID()}
	err = sdktest.Inspect(node).Store().Node.Read(ctx, func(db *sql.Conn) error {
		history, err := sdktest.ReadConversation(ctx, db, session.ID, nil, 100)
		if err != nil {
			return err
		}
		if len(history.Page.Runs) != 1 {
			return errors.New("the held turn has no run")
		}
		if inv.Run, err = sdktest.ReadAgentRun(ctx, db, history.Page.Runs[0].Turn); err != nil {
			return err
		}
		if inv.Session, err = sdktest.ReadSession(ctx, db, session.ID); err != nil {
			return err
		}
		if inv.User, err = sdktest.CurrentUser(ctx, db); err != nil {
			return err
		}
		inv.StorageUser = inv.User.ID
		var plugins []byte
		if err = db.QueryRowContext(ctx, "SELECT plugins FROM turns WHERE id=?1", inv.Run.Turn.String()).Scan(&plugins); err != nil {
			return err
		}
		return json.Unmarshal(plugins, &inv.Turn.Plugins)
	})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Run.Status != protocol.ConversationStatusRunning {
		t.Fatalf("held turn status = %s", inv.Run.Status)
	}
	inv.Worktree = protocol.Worktree{ID: inv.Session.Worktree, Project: inv.Session.Project, Path: root, Main: true}
	inv.Turn.ID, inv.Turn.Session, inv.Turn.Config = inv.Run.Turn, inv.Session.ID, inv.Session.Config
	bind := func(t *testing.T, inv *native.AgentInvocation) map[protocol.ToolOperation]native.Tool {
		t.Helper()
		return nodetest.ConnectionTools(t, node, inv)
	}
	declarations := nodetest.ConnectionDeclarations(bind(t, inv))
	if len(declarations) != len(connectionOperations) {
		t.Fatalf("captured connection declarations = %d", len(declarations))
	}
	captured := []protocol.ConnectionResource{{Kind: "database", Data: shared.ID}, {Kind: "ssh", Data: bastion.ID}}

	t.Run("visibility", func(t *testing.T) {
		databaseKind := func(operation protocol.ToolOperation) bool { return strings.HasPrefix(string(operation), "databases.") }
		conversation := func(project bool, resource *protocol.ConnectionResource) *native.AgentInvocation {
			value := *inv
			value.Turn.Config.Resource = resource
			if !project {
				value.Worktree.Project = nil
			}
			return &value
		}
		for _, entry := range []struct {
			name    string
			inv     *native.AgentInvocation
			visible func(protocol.ToolOperation) bool
		}{
			{"project", inv, func(protocol.ToolOperation) bool { return true }},
			{"database", conversation(false, &protocol.ConnectionResource{Kind: "database", Data: shared.ID}), databaseKind},
			{"ssh", conversation(false, &protocol.ConnectionResource{Kind: "ssh", Data: bastion.ID}), func(operation protocol.ToolOperation) bool { return !databaseKind(operation) }},
			{"plain", conversation(false, nil), func(protocol.ToolOperation) bool { return true }},
		} {
			bound := bind(t, entry.inv)
			for _, operation := range connectionOperations {
				tool, exists := bound[operation]
				if exists != entry.visible(operation) {
					t.Errorf("%s conversation: %s visible = %v", entry.name, operation, exists)
				}
				if exists && (tool.ReadOnly != declarations[operation].ReadOnly || tool.Approval == tool.ReadOnly || tool.Concurrent != declarations[operation].Concurrent || (!tool.ReadOnly && tool.Concurrent)) {
					t.Errorf("%s conversation: %s read-only %v, approval %v, concurrent %v", entry.name, operation, tool.ReadOnly, tool.Approval, tool.Concurrent)
				}
			}
		}
		// Native connection modules are independent of plugin captures;
		// omitting the modules from assembly binds no connection tools.
		savedAssembly := sdktest.Inspect(node).SwapAssembly(native.Assembly{})
		tools, err := sdktest.Inspect(node).ModuleTools(ctx, inv)
		sdktest.Inspect(node).SwapAssembly(savedAssembly)
		if err != nil || len(tools) != 0 {
			t.Errorf("an un-assembled Node bound %d connection tools, err=%v", len(tools), err)
		}
	})

	t.Run("results", func(t *testing.T) {
		encode := func(value any) string {
			t.Helper()
			body, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if text := string(body); strings.Contains(text, databasePassword) || strings.Contains(text, sshPassword) || strings.Contains(text, `"password":`) {
				t.Fatalf("model-visible result carries a secret: %s", text)
			}
			return string(body)
		}
		bound := bind(t, inv)
		for _, entry := range []struct {
			operation protocol.ToolOperation
			kind      string
			present   []string
			absent    []string
		}{
			{protocol.ToolOperationListDatabaseConnections, "database_profiles", []string{shared.ID.String(), `"credentials_required":false`}, []string{private.ID.String()}},
			{protocol.ToolOperationListSshConnections, "ssh_profiles", []string{bastion.ID.String()}, []string{"credentials_required"}},
		} {
			tool := bound[entry.operation]
			call := native.ToolRequest{ID: string(entry.operation), Name: tool.Definition.Name, Arguments: json.RawMessage(`{}`)}
			if err := tool.Prepare(ctx, call); err != nil {
				t.Fatalf("%s prepare: %v", entry.operation, err)
			}
			result := tool.Execute(ctx, call)
			if result.Failure != nil {
				t.Fatalf("%s failed: %v", entry.operation, result.Failure)
			}
			text := encode(result.Value)
			if !strings.Contains(text, `"kind":"`+entry.kind+`"`) {
				t.Errorf("%s result = %s", entry.operation, text)
			}
			for _, value := range entry.present {
				if !strings.Contains(text, value) {
					t.Errorf("%s result omits %s: %s", entry.operation, value, text)
				}
			}
			for _, value := range entry.absent {
				if strings.Contains(text, value) {
					t.Errorf("%s result exposes %s: %s", entry.operation, value, text)
				}
			}
		}

		owner := declarations[protocol.ToolOperationListDatabaseConnections].Owner
		outcome := protocol.Output{Kind: "ssh_outcome", Data: protocol.SshOutcome{Kind: "connected"}}
		if value, err := sdktest.Inspect(node).ConnectionResult(ctx, inv, owner, outcome, captured); err != nil || !reflect.DeepEqual(value, outcome) {
			t.Errorf("effect outcome = %#v %v", value, err)
		}

		input, err := sdktest.Inspect(node).ConnectionTurnInput(ctx, inv, []protocol.ToolOperation{protocol.ToolOperationQueryDatabase, protocol.ToolOperationWriteFile})
		text := encode(input)
		if err != nil || !strings.Contains(text, `"engine":"postgres"`) || !strings.Contains(text, `"database":"app"`) || !strings.Contains(text, shared.ID.String()) ||
			strings.Contains(text, private.ID.String()) || strings.Contains(text, bastion.ID.String()) || strings.Contains(text, "shared.fixture.test") {
			t.Errorf("turn input = %s %v", text, err)
		}
	})
}
