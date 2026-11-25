package sshnode_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sailry/sailry-code/internal/testsupport/node"
	sshnode "github.com/sailry/sailry-code/modules/code/ssh/node"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	nativeprotocol "github.com/sailry/sailry-harness/sdk/native/protocol"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

type sshResourceSessionService struct {
	options native.SSHServiceOptions
	key     protocol.SshHostKey
	runs    atomic.Int32
}

func (s *sshResourceSessionService) Execute(ctx context.Context, _ protocol.NodeID, request protocol.Request) (protocol.Output, error) {
	var profileID protocol.SshID
	var revision uint64
	switch input := request.Command.Data.(type) {
	case *protocol.CommandCheckSshPayload:
		profileID, revision = input.Profile, input.ExpectedRevision
	case *protocol.CommandRunSshPayload:
		profileID, revision = input.Profile, input.ExpectedRevision
	default:
		return protocol.Output{}, fmt.Errorf("unexpected SSH worker command: %s", request.Command.Kind)
	}
	profile, _, err := s.options.ReadSSH(ctx, profileID, revision)
	if err != nil {
		return protocol.Output{}, err
	}
	trusted := profile.HostKey != nil && *profile.HostKey == s.key
	outcome := protocol.SshOutcome{Kind: "connected"}
	if request.Command.Kind == "run_ssh" {
		s.runs.Add(1)
		if !trusted {
			return protocol.Output{}, errors.New("SSH run reached worker without the approved key")
		}
		outcome = protocol.SshOutcome{Kind: "completed", Data: &protocol.SshOutcomeCompletedPayload{Stdout: "fixture"}}
	} else if !trusted {
		outcome = protocol.SshOutcome{Kind: "host_key_required", Data: &nativeprotocol.SshOutcomeHostKeyRequiredPayload{Key: s.key, Changed: profile.HostKey != nil}}
	}
	return protocol.Output{Kind: "ssh_outcome", Data: outcome}, nil
}

func (*sshResourceSessionService) OpenTerminal(context.Context, protocol.NodeID, protocol.Request, nativeprotocol.CommandOpenSshTerminalPayload) (protocol.SshOutcome, error) {
	return protocol.SshOutcome{}, errors.New("unexpected SSH terminal open")
}

func (*sshResourceSessionService) Invalidate(protocol.SshID) {}
func (*sshResourceSessionService) Close() error              { return nil }

func TestNativeSSHResourceSession(t *testing.T) {
	t.Parallel()
	key := protocol.SshHostKey{Algorithm: "ssh-ed25519", Fingerprint: "SHA256:" + base64.RawStdEncoding.EncodeToString(make([]byte, 32))}
	service := &sshResourceSessionService{key: key}
	assembly := native.Assembly{Modules: []native.Module{sshnode.Module()}}
	assembly.SSH = func(options native.SSHServiceOptions) native.SSHService {
		service.options = options
		return service
	}
	script := nodetest.NewScript()
	node := nodetest.Start(t, filepath.Join(t.TempDir(), "profile"), script, native.Options{Assembly: assembly})
	t.Cleanup(func() {
		if err := node.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if installed := nodetest.Command(t, node, protocol.Command{Kind: "list_plugins"}).Data.([]protocol.PluginSummary); len(installed) != 0 {
		t.Fatalf("expected an empty plugin inventory, got %+v", installed)
	}
	save := func(caller protocol.NodeID, username string) protocol.SshProfile {
		t.Helper()
		request := protocol.NewRequest(node.ID(), protocol.Command{Kind: "save_ssh", Data: protocol.CommandSaveSshPayload{
			Profile: protocol.SshProfile{ID: protocol.NewSshID(), Name: username, Host: "primary.fixture.invalid", Port: 22, Username: username,
				Authentication: protocol.SshAuthenticationAgent, Sharing: &protocol.ConnectionSharing{Kind: "global"}},
			Credential: &protocol.SshCredential{Kind: "agent"},
		}})
		result := nodetest.Dispatch(t, node, caller, request)
		if result.Fault != nil {
			t.Fatal(result.Fault)
		}
		return result.Output.Data.(protocol.SshProfile)
	}
	profile, alternate := save(node.ID(), "primary"), save(node.ID(), "alternate")
	foreignController := protocol.NodeID{95}
	if err := sdktest.Inspect(node).Store().Node.Write(t.Context(), func(db *sql.Conn) error {
		account, err := sdktest.CreateUser(t.Context(), db, "Other SSH owner")
		if err != nil {
			return err
		}
		if err := sdktest.SetLinkTrust(t.Context(), db, foreignController, true); err != nil {
			return err
		}
		_, err = db.ExecContext(t.Context(), "UPDATE link_peers SET user=?1 WHERE identity=?2", account.ID.String(), foreignController[:])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	foreign := save(foreignController, "foreign")
	config := nodetest.SessionConfig(t, node, 32000, protocol.PermissionAsk)
	create := protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Config: &config}}
	ordinary := nodetest.Command(t, node, create).Data.(protocol.Session)
	for _, unavailable := range []protocol.SshID{protocol.NewSshID(), foreign.ID} {
		config.Resource = &protocol.ConnectionResource{Kind: "ssh", Data: unavailable}
		if fault := nodetest.Dispatch(t, node, node.ID(), protocol.NewRequest(node.ID(), create)).Fault; fault == nil || fault.Code != protocol.ErrorCodeNotFound {
			t.Fatalf("an unavailable SSH resource was accepted: %+v", fault)
		}
	}
	config.Resource = &protocol.ConnectionResource{Kind: "ssh", Data: profile.ID}
	session := nodetest.Command(t, node, create).Data.(protocol.Session)
	if session.Config.Resource == nil || session.Config.Resource.Kind != "ssh" || session.Config.Assistant != nil || session.Project != nil || session.Worktree == (protocol.WorktreeID{}) {
		t.Fatalf("native SSH session lost its binding: %+v", session)
	}
	if bound, ok := nodetest.Payload[protocol.SshID](session.Config.Resource.Data); !ok || bound != profile.ID {
		t.Fatalf("native SSH session bound the wrong profile: %+v", session.Config.Resource)
	}
	if err := sdktest.Inspect(node).Store().Node.Read(t.Context(), func(db *sql.Conn) error {
		for _, scope := range []struct {
			session protocol.SessionID
			want    []protocol.SshID
		}{{ordinary.ID, []protocol.SshID{profile.ID, alternate.ID}}, {session.ID, []protocol.SshID{profile.ID}}} {
			catalog, err := sdktest.ReadConnectionCatalog(t.Context(), db, scope.session)
			if err != nil {
				return err
			}
			if len(catalog.Ssh) != len(scope.want) || slices.ContainsFunc(catalog.Ssh, func(candidate protocol.SshProfile) bool {
				return !slices.Contains(scope.want, candidate.ID)
			}) {
				return fmt.Errorf("unexpected SSH catalog for %s: %+v", scope.session, catalog.Ssh)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	foreignTurn := protocol.NewRequest(node.ID(), protocol.Command{Kind: "submit_turn", Data: protocol.CommandSubmitTurnPayload{
		Session: session.ID, ExpectedRevision: session.Revision, Message: protocol.ConversationInput{Text: "Wrong account"},
	}})
	if fault := nodetest.Dispatch(t, node, foreignController, foreignTurn).Fault; fault == nil || fault.Code != protocol.ErrorCodePermissionDenied {
		t.Fatalf("another account could reuse the bound session: %+v", fault)
	}
	controller := protocol.NodeID{94}
	nodetest.TrustCaller(t, node, controller)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	changes, err := node.Subscribe(ctx, node.ID(), protocol.Topic{Kind: "node"})
	if err != nil {
		t.Fatal(err)
	}
	defer changes.Close()
	args := fmt.Sprintf(`{"connection":%q,"command":"fixture"}`, profile.ID.String())
	var first protocol.TurnID
	for index, call := range []string{"initial-run", "resumed-run"} {
		script.Reset(
			nodetest.Step{ID: "load-" + call, Tool: "tool_search", Arguments: func() string {
				return `{"query":"select:ssh_run,ssh_transfer,ssh_connections,ssh_save_connection"}`
			}},
			nodetest.Step{ID: call, Tool: "ssh_run", Arguments: func() string { return args }},
			nodetest.Step{ID: "answer"},
		)
		queued := nodetest.Command(t, node, protocol.Command{Kind: "submit_turn", Data: protocol.CommandSubmitTurnPayload{
			Session: session.ID, ExpectedRevision: session.Revision, Message: protocol.ConversationInput{Text: call},
		}}).Data.(protocol.QueuedTurn)
		if len(queued.Plugins) != 0 || queued.Config.Assistant != nil || queued.Session != session.ID {
			t.Fatalf("native SSH turn acquired plugin provenance: %+v", queued)
		}
		var decided protocol.ApprovalID
		for {
			var history protocol.ConversationHistory
			if err := sdktest.Inspect(node).Store().Node.Read(ctx, func(db *sql.Conn) error {
				var err error
				history, err = sdktest.ReadConversation(ctx, db, session.ID, nil, 100)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			for _, approval := range history.Page.Approvals {
				if approval.Turn != queued.ID || approval.State != protocol.ConversationApprovalStatePending || approval.ID == decided {
					continue
				}
				if decided != (protocol.ApprovalID{}) || approval.Session != session.ID || approval.Source != protocol.ConversationApprovalSourceUser || service.runs.Load() != int32(index) {
					t.Fatalf("SSH run did not wait for its own approval: %+v", approval)
				}
				if index == 0 {
					want := protocol.SshTrust{Profile: profile.ID, ExpectedRevision: profile.Revision, Host: profile.Host, Port: profile.Port, Key: key}
					if approval.Ssh == nil || *approval.Ssh != want || approval.CanRemember {
						t.Fatalf("unexpected host-key consent: %+v", approval)
					}
					assertNativeSSHResourceScope(t, node, session, queued, profile, alternate, foreign)
					scope := protocol.PluginContext{Turn: &queued.ID, Session: &session.ID, Worktree: &session.Worktree, Surface: protocol.PluginDesktopSurface{Kind: "workspace"}}
					unapproved := protocol.NewRequest(node.ID(), protocol.Command{Kind: "trust_ssh", Data: protocol.CommandTrustSshPayload{Profile: profile.ID, ExpectedRevision: profile.Revision, Key: key}})
					unapproved.Plugin = &scope
					if fault := nodetest.Dispatch(t, node, node.ID(), unapproved).Fault; fault == nil || fault.Code != protocol.ErrorCodePermissionDenied {
						t.Fatalf("an unapproved fingerprint was trusted: %+v", fault)
					}
					list := protocol.NewRequest(node.ID(), protocol.Command{Kind: "list_ssh"})
					list.Plugin = &scope
					if fault := nodetest.Dispatch(t, node, controller, list).Fault; fault == nil || fault.Code != protocol.ErrorCodePermissionDenied {
						t.Fatalf("a controller could claim execution-Node provenance: %+v", fault)
					}
					scope.Package = protocol.PluginReference{Name: "ssh", Digest: strings.Repeat("a", 64)}
					if fault := nodetest.Dispatch(t, node, node.ID(), list).Fault; fault == nil || fault.Code != protocol.ErrorCodePermissionDenied {
						t.Fatalf("a self-reported SSH package had authority: %+v", fault)
					}
				} else if approval.Ssh != nil {
					t.Fatalf("the resumed turn asked to trust the same approved key again: %+v", approval)
				}
				nodetest.Command(t, node, protocol.Command{Kind: "resolve_approval", Data: protocol.CommandResolveApprovalPayload{
					Session: session.ID, Approval: approval.ID, Decision: protocol.ConversationDecision{Kind: "approve"},
				}})
				decided = approval.ID
			}
			if run := slices.IndexFunc(history.Page.Runs, func(run protocol.ConversationRun) bool { return run.Turn == queued.ID }); run >= 0 {
				current := history.Page.Runs[run]
				if current.Status == protocol.ConversationStatusCompleted {
					result, offered := script.Observation(call)
					if !slices.Contains(offered, "ssh_run") || !slices.Contains(offered, "ssh_transfer") || slices.Contains(offered, "ssh_connections") || slices.Contains(offered, "ssh_save_connection") {
						t.Fatalf("native SSH session discovered the wrong tools: %v", offered)
					}
					if decided == (protocol.ApprovalID{}) || service.runs.Load() != int32(index+1) || !strings.Contains(result, `"stdout":"fixture"`) {
						t.Fatalf("SSH turn did not run exactly once under its approval: %s", result)
					}
					if index == 1 && !slices.ContainsFunc(history.Page.Runs, func(run protocol.ConversationRun) bool { return run.Turn == first }) {
						t.Fatal("resuming the SSH session lost the previous turn")
					}
					break
				}
				switch current.Status {
				case protocol.ConversationStatusFailed, protocol.ConversationStatusInterrupted, protocol.ConversationStatusCancelled:
					t.Fatalf("SSH resource turn failed: %+v", current)
				}
			}
			if _, err := changes.Next(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if index == 0 {
			first = queued.ID
			visible := nodetest.Snapshot(t, node).Sessions
			found := slices.IndexFunc(visible, func(candidate protocol.Session) bool { return candidate.ID == session.ID && !candidate.Archived })
			if found < 0 || visible[found].Worktree != session.Worktree || !reflect.DeepEqual(visible[found].Config, session.Config) {
				t.Fatal("the native SSH session could not be reopened with its existing scope")
			}
			session = visible[found]
		}
	}
}

func assertNativeSSHResourceScope(t *testing.T, node *native.Node, session protocol.Session, turn protocol.QueuedTurn, primary protocol.SshProfile, denied ...protocol.SshProfile) {
	t.Helper()
	access := sdktest.Inspect(node)
	inv := &native.AgentInvocation{Session: session, Turn: turn, Run: protocol.ConversationRun{Turn: turn.ID, Session: session.ID, Worktree: session.Worktree}}
	scope := protocol.PluginContext{Turn: &turn.ID, Session: &session.ID, Worktree: &session.Worktree, Surface: protocol.PluginDesktopSurface{Kind: "workspace"}}
	assertDenied := func(command protocol.Command, binding *sdktest.ApprovalBinding) {
		t.Helper()
		request := protocol.NewRequest(node.ID(), command)
		request.Plugin = &scope
		if binding != nil {
			nodetest.ClaimApproval(t, node, request, *binding)
		}
		if fault := nodetest.Dispatch(t, node, node.ID(), request).Fault; fault == nil || fault.Code != protocol.ErrorCodePermissionDenied {
			t.Fatalf("%s escaped the bound SSH scope: %+v", command.Kind, fault)
		}
	}
	assertDenied(protocol.Command{Kind: "list_ssh"}, nil)
	save := protocol.ToolOperationSaveSshConnection
	assertDenied(protocol.Command{Kind: "save_ssh", Data: protocol.CommandSaveSshPayload{Profile: primary, ExpectedRevision: primary.Revision}},
		&sdktest.ApprovalBinding{Tool: "ssh_save_connection", Operation: &save})
	assertDenied(protocol.Command{Kind: "run_ssh", Data: protocol.CommandRunSshPayload{Profile: primary.ID, ExpectedRevision: primary.Revision, Command: "fixture", TimeoutMs: 1000}}, nil)
	for _, profile := range denied {
		assertDenied(protocol.Command{Kind: "check_ssh", Data: protocol.CommandCheckSshPayload{Profile: profile.ID, ExpectedRevision: profile.Revision}}, nil)
		// Even a captured target supplied to the translator cannot widen the stored binding.
		captured := []protocol.ConnectionResource{{Kind: "ssh", Data: profile.ID}}
		for _, test := range []struct {
			tool      string
			operation protocol.ToolOperation
			command   protocol.Command
			args      string
		}{
			{"ssh_run", protocol.ToolOperationRunSsh, protocol.Command{Kind: "run_ssh", Data: protocol.CommandRunSshPayload{
				Profile: profile.ID, ExpectedRevision: profile.Revision, Command: "fixture", TimeoutMs: 1000,
			}}, `{"connection":%q,"command":"fixture","timeout_ms":1000}`},
			{"ssh_transfer", protocol.ToolOperationTransferSsh, protocol.Command{Kind: "transfer_ssh", Data: protocol.CommandTransferSshPayload{
				Profile: profile.ID, ExpectedRevision: profile.Revision, TimeoutMs: 1000,
				Transfer: protocol.SshTransfer{Worktree: session.Worktree, Path: "fixture", RemotePath: "fixture", Direction: protocol.SshDirectionUpload},
			}}, `{"connection":%q,"path":"fixture","remote_path":"fixture","direction":"upload","timeout_ms":1000}`},
		} {
			args := json.RawMessage(fmt.Sprintf(test.args, profile.ID.String()))
			declaration := sdktest.Declaration{Spec: sdktest.Spec{Operation: test.operation}}
			if _, err := access.ConnectionCommand(t.Context(), inv, declaration, args, captured); !nodetest.FaultIs(err, protocol.ErrorCodePermissionDenied) {
				t.Fatalf("%s allowed another SSH target: %v", test.operation, err)
			}
			assertDenied(test.command, &sdktest.ApprovalBinding{Tool: test.tool, Operation: &test.operation})
		}
	}
}
