package node

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func detachedConnectionScope(session protocol.Session, turn protocol.TurnID, owner protocol.PluginReference) protocol.PluginContext {
	scope := protocol.DefaultPluginContext()
	scope.Package, scope.Turn, scope.Session, scope.Worktree = owner, &turn, &session.ID, &session.Worktree
	return scope
}

func testDetachedConnectionAuthority(t *testing.T, node *Node, session protocol.Session, turn protocol.TurnID, owners map[string]protocol.PluginReference, database protocol.DatabaseProfile, ssh protocol.SshProfile, created protocol.DatabaseProfile) protocol.Request {
	t.Helper()
	dbOwner, sshOwner := owners["databases"], owners["ssh"]
	dbScope, sshScope := detachedConnectionScope(session, turn, dbOwner), detachedConnectionScope(session, turn, sshOwner)
	dbSave, sshSave := protocol.ToolOperationSaveDatabaseConnection, protocol.ToolOperationSaveSshConnection
	dbTool, sshTool := sdktest.Alias(dbOwner.Name, "database_save_connection"), sdktest.Alias(sshOwner.Name, "ssh_save_connection")
	dbEdit := protocol.CommandSaveDatabasePayload{Profile: database, ExpectedRevision: database.Revision}
	sshEdit := protocol.CommandSaveSshPayload{Profile: ssh, ExpectedRevision: ssh.Revision}
	dbShare, sshShare := dbEdit, sshEdit
	dbShare.Profile.Sharing, sshShare.Profile.Sharing = nil, nil
	dbCreate, sshCreate := dbEdit, sshEdit
	dbCreate.Profile.ID, dbCreate.ExpectedRevision = protocol.NewDatabaseID(), 0
	sshCreate.Profile.ID, sshCreate.ExpectedRevision = protocol.NewSshID(), 0
	sshCreate.Profile.Revision, sshCreate.Profile.HostKey = 0, nil
	sshCreate.Profile.Authentication, sshCreate.Profile.AuthenticationNode = protocol.SshAuthenticationAgent, nil
	sshCreate.Credential = &protocol.SshCredential{Kind: "agent"}
	dbStale, sshStale := dbEdit, sshEdit
	dbStale.ExpectedRevision--
	sshStale.ExpectedRevision--
	for _, test := range []struct {
		name    string
		scope   *protocol.PluginContext
		command protocol.Command
		binding *sdktest.ApprovalBinding
		code    protocol.ErrorCode
		message string
	}{
		{"database edit without approval", &dbScope, protocol.Command{Kind: "save_database", Data: &dbEdit}, nil, protocol.ErrorCodePermissionDenied, "connection save has no claimed approval"},
		{"SSH edit without approval", &sshScope, protocol.Command{Kind: "save_ssh", Data: &sshEdit}, nil, protocol.ErrorCodePermissionDenied, "connection save has no claimed approval"},
		{"database share change", &dbScope, protocol.Command{Kind: "save_database", Data: &dbShare}, &sdktest.ApprovalBinding{Owner: dbOwner, Tool: dbTool, Operation: &dbSave}, protocol.ErrorCodePermissionDenied, "connection sharing changes require a controller view"},
		{"SSH share change", &sshScope, protocol.Command{Kind: "save_ssh", Data: &sshShare}, &sdktest.ApprovalBinding{Owner: sshOwner, Tool: sshTool, Operation: &sshSave}, protocol.ErrorCodePermissionDenied, "connection sharing changes require a controller view"},
		{"global database creation", &dbScope, protocol.Command{Kind: "save_database", Data: &dbCreate}, &sdktest.ApprovalBinding{Owner: dbOwner, Tool: dbTool, Operation: &dbSave}, protocol.ErrorCodePermissionDenied, "connection sharing changes require a controller view"},
		{"global SSH creation", &sshScope, protocol.Command{Kind: "save_ssh", Data: &sshCreate}, &sdktest.ApprovalBinding{Owner: sshOwner, Tool: sshTool, Operation: &sshSave}, protocol.ErrorCodePermissionDenied, "connection sharing changes require a controller view"},
		{"approval of another package", &dbScope, protocol.Command{Kind: "save_database", Data: &dbEdit}, &sdktest.ApprovalBinding{Owner: sshOwner, Tool: dbTool, Operation: &dbSave}, protocol.ErrorCodePermissionDenied, "connection save differs from its claimed approval"},
		{"stale database edit", &dbScope, protocol.Command{Kind: "save_database", Data: &dbStale}, &sdktest.ApprovalBinding{Owner: dbOwner, Tool: dbTool, Operation: &dbSave}, protocol.ErrorCodeRevisionConflict, "configuration revision changed; reload before editing"},
		{"stale SSH edit", &sshScope, protocol.Command{Kind: "save_ssh", Data: &sshStale}, &sdktest.ApprovalBinding{Owner: sshOwner, Tool: sshTool, Operation: &sshSave}, protocol.ErrorCodeRevisionConflict, "configuration revision changed; reload before editing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := protocol.NewRequest(node.ID(), test.command)
			request.Plugin = test.scope
			var approval protocol.ApprovalID
			if test.binding != nil {
				approval = claimedConnectionSave(t, node, request, *test.binding)
			}
			response := dispatchFixture(t, node, node.ID(), request)
			if response.Fault == nil || response.Fault.Code != test.code || response.Fault.Message != test.message {
				t.Fatalf("save returned %+v, want %s %s", response, test.code, test.message)
			}
			if test.binding != nil {
				var proof string
				if err := sdktest.Inspect(node).Store().Node.Read(t.Context(), func(db *sql.Conn) error {
					return db.QueryRowContext(t.Context(), "SELECT prepared FROM agent_approvals WHERE id=?1", approval.String()).Scan(&proof)
				}); err != nil || proof != "null" {
					t.Fatalf("failed save retained proof %s: %v", proof, err)
				}
			}
		})
	}
	if err := sdktest.Inspect(node).Store().Node.Read(t.Context(), func(db *sql.Conn) error {
		target := protocol.ConnectionResource{Kind: "database", Data: created.ID}
		nativeOwner := protocol.PluginReference{}
		for _, owner := range []protocol.PluginReference{nativeOwner, dbOwner, sshOwner} {
			proved, err := sdktest.ConnectionCreated(t.Context(), db, turn, owner, target)
			if err != nil || proved != (owner == nativeOwner) {
				return fmt.Errorf("creation proof for %q = %v: %v", owner.Name, proved, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	request := protocol.NewRequest(node.ID(), protocol.Command{Kind: "save_database", Data: &dbEdit})
	request.Plugin = &dbScope
	claimedConnectionSave(t, node, request, sdktest.ApprovalBinding{Owner: dbOwner, Tool: dbTool, Operation: &dbSave})
	if err := sdktest.Inspect(node).ValidatePluginRequest(t.Context(), node.ID(), request); err != nil {
		t.Fatalf("active turn could not use its own approval: %v", err)
	}
	return request
}

func admitDetachedConnectionEffects(t *testing.T, node *Node, session protocol.Session, turn protocol.TurnID, owners map[string]protocol.PluginReference, database protocol.DatabaseProfile, ssh protocol.SshProfile) []protocol.Request {
	t.Helper()
	dbScope, sshScope := detachedConnectionScope(session, turn, owners["databases"]), detachedConnectionScope(session, turn, owners["ssh"])
	dbQuery := protocol.CommandQueryDatabasePayload{
		Profile: database.ID, ExpectedRevision: database.Revision, ReadOnly: true, Sql: "SELECT 1", RowLimit: 1, TimeoutMs: 1000,
	}
	sshCheck := protocol.CommandCheckSshPayload{Profile: ssh.ID, ExpectedRevision: ssh.Revision}
	stale := dbQuery
	stale.ExpectedRevision--
	queries := []protocol.Request{
		protocol.NewRequest(node.ID(), protocol.Command{Kind: "query_database", Data: &stale}),
		protocol.NewRequest(node.ID(), protocol.Command{Kind: "query_database", Data: &dbQuery}),
		protocol.NewRequest(node.ID(), protocol.Command{Kind: "check_ssh", Data: &sshCheck}),
	}
	queries[0].Plugin, queries[1].Plugin, queries[2].Plugin = &dbScope, &dbScope, &sshScope
	for _, request := range queries {
		ctx, err := sdktest.Inspect(node).AuthorizeCommand(t.Context(), node.ID(), request)
		if err != nil {
			t.Fatal(err)
		}
		if err := sdktest.Inspect(node).ValidatePluginRequest(ctx, node.ID(), request); err != nil {
			t.Fatalf("effect preflight failed: %v", err)
		}
		// Commit admission without scheduling the worker so the test can change sharing first.
		body, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if err := sdktest.Inspect(node).Store().Node.Write(ctx, func(db *sql.Conn) error {
			previous, err := sdktest.Admit(ctx, db, node.ID(), request, body)
			if previous != nil {
				return fmt.Errorf("unexpected effect replay: %+v", previous)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	return queries
}

func assertDetachedEffectFault(t *testing.T, node *Node, request protocol.Request, code protocol.ErrorCode) {
	t.Helper()
	ctx, err := sdktest.Inspect(node).AuthorizeCommand(t.Context(), node.ID(), request)
	if err != nil {
		t.Fatal(err)
	}
	completed := sdktest.Inspect(node).ExecuteAdmittedHost(ctx, node.ID(), request)
	if completed.Fault == nil || completed.Fault.Code != code {
		t.Fatalf("%s worker returned %+v, want %s", request.Command.Kind, completed, code)
	}
	replayed := dispatchFixture(t, node, node.ID(), request)
	if replayed.Fault == nil || replayed.Fault.Code != code || replayed.Fault.Message != completed.Fault.Message {
		t.Fatalf("%s worker did not replay the settled fault: %+v, %+v", request.Command.Kind, completed, replayed)
	}
}
