package databasesnode_test

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/sailry/sailry-code/internal/testsupport/node"
	"github.com/sailry/sailry-harness/sdk/native/link"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func testDatabaseSaveProof(t *testing.T, node *native.Node, inv *native.AgentInvocation, declarations map[protocol.ToolOperation]sdktest.Declaration) {
	t.Helper()
	ctx := t.Context()
	declaration := declarations[protocol.ToolOperationSaveDatabaseConnection]
	scope := protocol.PluginContext{Package: declaration.Owner, Turn: &inv.Run.Turn, Session: &inv.Session.ID, Worktree: &inv.Session.Worktree, Surface: protocol.PluginDesktopSurface{Kind: "workspace"}}
	password := protocol.NewSecret("database-save-proof-secret")
	profile := protocol.DatabaseProfile{ID: protocol.NewDatabaseID(), Name: "", ReadOnly: true, Sharing: &protocol.ConnectionSharing{Kind: "projects", Data: []protocol.ProjectID{*inv.Session.Project}}, Connection: protocol.DatabaseConnection{Kind: "network", Data: &protocol.DatabaseConnectionNetworkPayload{
		Engine: protocol.DatabaseEnginePostgres, Host: "proof.fixture.test", Port: 5432, Username: "reader", Database: "app", Tls: protocol.DatabaseTlsRequire,
	}}}
	command := protocol.Command{Kind: "save_database", Data: &protocol.CommandSaveDatabasePayload{Profile: profile, Password: &password}}
	save := func(command protocol.Command) link.Response {
		t.Helper()
		return nodetest.SaveConnection(t, node, scope, declaration, command, func(proof protocol.Command) {
			input, valid := nodetest.Payload[protocol.CommandSaveDatabasePayload](proof.Data)
			if !valid || input.Password != nil {
				t.Fatal("database save proof retained a password")
			}
		})
	}
	failed := save(command)
	if failed.Fault == nil || failed.Fault.Code != protocol.ErrorCodeInvalidRequest {
		t.Fatalf("invalid database save returned %+v", failed)
	}
	if err := sdktest.Inspect(node).Store().Node.Read(ctx, func(db *sql.Conn) error {
		stored, err := sdktest.ReadDatabase(ctx, db, profile.ID)
		if err == nil && stored != nil {
			t.Error("failed database save retained a profile")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	profile.Name = "Created in this turn"
	command.Data = &protocol.CommandSaveDatabasePayload{Profile: profile, Password: &password}
	result := save(command)
	if result.Fault != nil {
		t.Fatal(result.Fault)
	}
	saved, valid := nodetest.Payload[protocol.DatabaseProfile](result.Output.Data)
	if !valid || saved.ID != profile.ID || saved.Revision != 1 {
		t.Fatalf("database save did not commit once: %+v", result)
	}
	if err := sdktest.Inspect(node).Store().Node.Read(ctx, func(db *sql.Conn) error {
		created, err := sdktest.ConnectionCreated(ctx, db, inv.Run.Turn, declaration.Owner, protocol.ConnectionResource{Kind: "database", Data: saved.ID})
		if err == nil && !created {
			t.Error("the completed database save is unavailable in its creating turn")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(map[string]any{"connection": saved.ID, "sql": "select 1", "row_limit": 1, "timeout_ms": 1000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sdktest.Inspect(node).ConnectionCommand(ctx, inv, declarations[protocol.ToolOperationQueryDatabase], args, nil); err != nil {
		t.Fatalf("new database requires another turn to be captured: %v", err)
	}
}
