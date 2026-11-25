package sshnode_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sailry/sailry-code/internal/testsupport/node"
	sshnode "github.com/sailry/sailry-code/modules/code/ssh/node"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

const sshPassword = "fixture-ssh-password"

func sshConnectionFixture(t *testing.T) (*native.Node, protocol.Session, protocol.SshProfile, *nodetest.HeldModel) {
	t.Helper()
	held := &nodetest.HeldModel{Started: make(chan struct{}), Release: make(chan struct{})}
	node := nodetest.Start(t, filepath.Join(t.TempDir(), "profile"), held, native.Options{Assembly: native.Assembly{Modules: []native.Module{sshnode.Module()}}})
	t.Cleanup(func() {
		close(held.Release)
		if err := node.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	project := nodetest.Command(t, node, protocol.Command{Kind: "create_project", Data: protocol.ProjectsDraft{
		Name: "Fixture", Path: t.TempDir(), Source: protocol.ProjectsSource{Kind: "local"}, Appearance: protocol.DefaultProjectsAppearance(),
	}}).Data.(protocol.Project)
	config := nodetest.SessionConfig(t, node, 32000, protocol.PermissionAsk)
	output := nodetest.Command(t, node, protocol.Command{Kind: "save_ssh", Data: protocol.CommandSaveSshPayload{
		Profile: protocol.SshProfile{ID: protocol.NewSshID(), Name: "Bastion", Host: "bastion.fixture.test", Port: 22, Username: "deploy",
			Authentication: protocol.SshAuthenticationPassword, Sharing: &protocol.ConnectionSharing{Kind: "projects", Data: &[]protocol.ProjectID{project.ID}}},
		Credential: &protocol.SshCredential{Kind: "password", Data: &protocol.SshCredentialPasswordPayload{Password: protocol.NewSecret(sshPassword)}}}})
	bastion, valid := nodetest.Payload[protocol.SshProfile](output.Data)
	if !valid {
		t.Fatalf("saved SSH connection = %#v", output)
	}
	session := nodetest.Command(t, node, protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Project: &project.ID, Config: &config}}).Data.(protocol.Session)
	return node, session, bastion, held
}

func TestSSHConnectionTools(t *testing.T) {
	t.Parallel()
	t.Run("arguments", func(t *testing.T) {
		id := protocol.NewDatabaseID()
		for _, entry := range []struct {
			name      string
			operation protocol.ToolOperation
			arguments string
			message   string
		}{
			{"list rejects fields", protocol.ToolOperationListSshConnections, `{"connection":"x"}`, "invalid connection operation: "},
			{"target must be an identifier", protocol.ToolOperationRunSsh, `{"connection":"production","command":"uptime","timeout_ms":1}`, "invalid connection operation: "},
			{"transfer direction", protocol.ToolOperationTransferSsh, `{"connection":"` + id.String() + `","path":"a","remote_path":"/a","direction":"sideways","timeout_ms":1}`, "invalid connection operation: "},
			{"save rejects unknown credentials", protocol.ToolOperationSaveSshConnection, `{"credential":{"kind":"token"}}`, "invalid SSH credential"},
			{"save rejects scalar credentials", protocol.ToolOperationSaveSshConnection, `{"credential":"secret"}`, "invalid SSH credential"},
		} {
			_, err := sdktest.ConnectionCall(entry.operation, json.RawMessage(entry.arguments))
			var fault *protocol.Fault
			if !errors.As(err, &fault) || fault.Code != protocol.ErrorCodeInvalidRequest || !strings.HasPrefix(fault.Message, entry.message) {
				t.Errorf("%s: error = %v", entry.name, err)
			}
		}
	})

	node, session, bastion, held := sshConnectionFixture(t)
	ctx := t.Context()
	inv := nodetest.HoldTurn(t, node, session, held.Started)
	declarations := nodetest.ConnectionDeclarations(nodetest.ConnectionTools(t, node, inv))
	captured := []protocol.ConnectionResource{{Kind: "ssh", Data: bastion.ID}}

	t.Run("commands", func(t *testing.T) {
		for _, entry := range []struct {
			operation protocol.ToolOperation
			arguments map[string]any
			command   protocol.Command
		}{
			{protocol.ToolOperationListSshConnections, map[string]any{}, protocol.Command{Kind: "list_ssh"}},
			{protocol.ToolOperationRunSsh, map[string]any{"connection": bastion.ID, "command": "uptime", "timeout_ms": 120000},
				protocol.Command{Kind: "run_ssh", Data: &protocol.CommandRunSshPayload{Profile: bastion.ID, ExpectedRevision: bastion.Revision, Command: "uptime", TimeoutMs: 120000}}},
			{protocol.ToolOperationTransferSsh, map[string]any{"connection": bastion.ID, "path": "build.tar", "remote_path": "/tmp/build.tar", "direction": "upload", "timeout_ms": 120000},
				protocol.Command{Kind: "transfer_ssh", Data: &protocol.CommandTransferSshPayload{Profile: bastion.ID, ExpectedRevision: bastion.Revision, TimeoutMs: 120000,
					Transfer: protocol.SshTransfer{Worktree: inv.Run.Worktree, Path: "build.tar", RemotePath: "/tmp/build.tar", Direction: protocol.SshDirectionUpload}}}},
		} {
			arguments, err := json.Marshal(entry.arguments)
			if err != nil {
				t.Fatal(err)
			}
			command, err := sdktest.Inspect(node).ConnectionCommand(ctx, inv, declarations[entry.operation], arguments, captured)
			if err != nil || !reflect.DeepEqual(command, entry.command) {
				t.Errorf("%s command = %#v %v", entry.operation, command, err)
			}
		}
	})

	t.Run("sharing", func(t *testing.T) {
		for _, entry := range []struct {
			name      string
			operation protocol.ToolOperation
			arguments map[string]any
			captured  []protocol.ConnectionResource
		}{
			{"unknown SSH connection", protocol.ToolOperationRunSsh, map[string]any{"connection": protocol.NewSshID(), "command": "uptime", "timeout_ms": 1000}, captured},
		} {
			arguments, err := json.Marshal(entry.arguments)
			if err != nil {
				t.Fatal(err)
			}
			command, err := sdktest.Inspect(node).ConnectionCommand(ctx, inv, declarations[entry.operation], arguments, entry.captured)
			var fault *protocol.Fault
			if !errors.As(err, &fault) || fault.Code != protocol.ErrorCodePermissionDenied {
				t.Errorf("%s: command = %#v, error = %v", entry.name, command, err)
			}
		}
	})
}
