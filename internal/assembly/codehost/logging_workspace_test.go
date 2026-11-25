package codehost_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sailry/sailry-code/internal/testsupport/node"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/getsentry/sentry-go"
	gogit "github.com/go-git/go-git/v5"
	"github.com/sailry/sailry-code/internal/assembly/codehost"
	"github.com/sailry/sailry-harness/sdk/native/process"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestWorkspaceReadCommandDiagnostics(t *testing.T) {
	home := t.TempDir()
	for _, key := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME"} {
		t.Setenv(key, home)
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "")
	root := filepath.Join(t.TempDir(), "private-project-path")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := gogit.PlainInit(root, false); err != nil {
		t.Fatal(err)
	}
	const entry, contents, missing = "private-entry.txt", "private file contents", "private-missing-directory"
	if err := os.WriteFile(filepath.Join(root, entry), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(t.TempDir(), "profile")
	closeLog, err := sdktest.InstallLogging(profile, "desktop")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeLog)
	server := nodetest.NewSentry(t)
	const localCause = "private Git process failure"
	var inspected atomic.Bool
	node, err := native.StartWithOptions(t.Context(), profile, native.Options{
		Assembly:    codehost.Assembly(),
		Environment: new(process.NewEnvironment(nil)),
		MCP:         sdktest.MCPOptions(),
		Git: sdktest.GitOptions{Run: func(_ context.Context, _ string, args []string, _ []byte) (sdktest.GitCommandResult, error) {
			switch {
			case slices.Contains(args, "status"):
				inspected.Store(true)
				return sdktest.GitCommandResult{}, errors.New(localCause)
			case slices.Contains(args, "worktree"):
				return sdktest.GitCommandResult{}, &protocol.Fault{Code: protocol.ErrorCodeNotConfigured, Message: "Git executable not found on execution Node"}
			default:
				return sdktest.GitCommandResult{}, fmt.Errorf("unexpected Git arguments: %v", args)
			}
		}},
		Telemetry: server.Options(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = node.Stop(context.Background()) })
	if !node.Telemetry().Errors() {
		t.Fatal("fixture must allow automatic error reports")
	}
	const projectName = "private workspace name"
	project := nodetest.Command(t, node, protocol.Command{Kind: "register_project", Data: protocol.CommandRegisterProjectPayload{Name: projectName, Path: root}}).Data.(protocol.Project)
	trees := nodetest.Snapshot(t, node).Worktrees
	index := slices.IndexFunc(trees, func(tree protocol.Worktree) bool { return tree.Project != nil && *tree.Project == project.ID })
	if index < 0 {
		t.Fatal("registered project has no worktree")
	}
	tree := trees[index].ID
	directory := func(path string) protocol.Command {
		return protocol.Command{Kind: "list_directory", Data: protocol.CommandListDirectoryPayload{Worktree: tree, Path: path}}
	}
	success := protocol.NewRequest(node.ID(), directory("."))
	if got := nodetest.Dispatch(t, node, node.ID(), success); got.Fault != nil || got.Output.Kind != "directory" {
		t.Fatalf("workspace root listing = %v", got)
	}
	type failure struct {
		id    protocol.RequestID
		kind  string
		level string
		code  protocol.ErrorCode
		ref   string
	}
	var failures []failure
	var inspectRef string
	for _, test := range []struct {
		command protocol.Command
		level   string
		code    protocol.ErrorCode
	}{
		{directory(missing), "WARN", protocol.ErrorCodeNotFound},
		{protocol.Command{Kind: "inspect_git", Data: protocol.CommandInspectGitPayload{Worktree: tree}}, "ERROR", protocol.ErrorCodeUnavailable},
		{protocol.Command{Kind: "list_worktrees", Data: protocol.CommandListWorktreesPayload{Worktree: tree}}, "WARN", protocol.ErrorCodeNotConfigured},
	} {
		request := protocol.NewRequest(node.ID(), test.command)
		fault := nodetest.Dispatch(t, node, node.ID(), request).Fault
		if fault == nil || fault.Code != test.code {
			t.Fatalf("%s = %v, want %s", test.command.Kind, fault, test.code)
		}
		if !sdktest.ValidReportRef(fault.ReportRef) {
			t.Fatalf("%s failure has no valid reference: %q", test.command.Kind, fault.ReportRef)
		}
		if test.command.Kind == "inspect_git" {
			if fault.GitInspectionReason != protocol.GitInspectionStatusCommandFailed {
				t.Fatalf("inspection fault lost its Git failure reason: %+v", fault)
			}
			inspectRef = fault.ReportRef
		}
		failures = append(failures, failure{request.ID, test.command.Kind, test.level, test.code, fault.ReportRef})
	}
	if !inspected.Load() {
		t.Fatal("inspection did not reach native Git status")
	}
	const invalidPackage = "private package/name"
	install := protocol.NewRequest(node.ID(), nodetest.DecodeCommand(t, "install_plugin", map[string]any{
		"worktree": tree, "path": ".", "name": invalidPackage, "expected_revision": 0,
	}))
	admission, err := node.Dispatch(t.Context(), node.ID(), install)
	if err != nil || !admission.Receipt.Durable {
		t.Fatalf("fixture installation was not admitted durably: %v, %v", admission.Receipt, err)
	}
	completion := <-admission.Completion
	if completion.Fault == nil || completion.Fault.Code != protocol.ErrorCodeInvalidRequest {
		t.Fatalf("invalid package did not fail after admission: %+v", completion)
	}
	installRef := completion.Fault.ReportRef
	if !sdktest.ValidReportRef(installRef) {
		t.Fatalf("installation failure has no valid reference: %q", installRef)
	}
	if err := node.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	path, err := sdktest.CurrentLogFile()
	if err != nil {
		t.Fatal(err)
	}
	local, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(local), "\n")
	for _, line := range lines {
		if strings.Contains(line, " request="+success.ID.String()+" ") {
			_, elapsed, _ := strings.Cut(line, " duration_us=")
			var duration uint64
			if _, err := fmt.Sscanf(elapsed, "%d", &duration); err != nil || duration < 100_000 || !strings.Contains(line, " phase=completed ") {
				t.Errorf("routine success was logged as %q", line)
			}
		}
	}
	for _, want := range failures {
		matched := 0
		for _, line := range lines {
			if !strings.Contains(line, " request="+want.id.String()+" ") {
				continue
			}
			matched++
			if !strings.Contains(line, " "+want.level+" desktop area=command phase=failed ") ||
				!strings.Contains(line, " worktree="+tree.String()+" command="+want.kind+" ") ||
				!strings.Contains(line, " command_stage=completion ") || !slices.Contains(strings.Fields(line), "report_ref="+want.ref) || !strings.Contains(line, " code="+string(want.code)+" ") {
				t.Errorf("%s failure logged as %q", want.kind, line)
			}
		}
		if matched != 1 {
			t.Errorf("%s failure recorded %d times", want.kind, matched)
		}
	}
	installFailures := 0
	for _, line := range lines {
		if strings.Contains(line, " request="+install.ID.String()+" ") && strings.Contains(line, " phase=failed ") {
			installFailures++
			fields := strings.Fields(line)
			if !strings.Contains(line, " WARN desktop area=command phase=failed ") ||
				!slices.Contains(fields, "command=install_plugin") || !slices.Contains(fields, "command_stage=completion") ||
				!slices.Contains(fields, "code=invalid_request") || !slices.Contains(fields, "report_ref="+installRef) {
				t.Errorf("installation failure logged as %q", line)
			}
		}
	}
	if installFailures != 1 {
		t.Errorf("installation failure recorded %d times", installFailures)
	}
	tail, err := sdktest.LogTail(path)
	if err != nil {
		t.Fatal(err)
	}
	private := []string{filepath.Base(root), projectName, entry, contents, missing, localCause, invalidPackage}
	for _, value := range private {
		if bytes.Contains(local, []byte(value)) || bytes.Contains(tail, []byte(value)) {
			t.Errorf("diagnostics kept private value %q", value)
		}
	}
	identifiers := []string{node.ID().String(), project.ID.String(), tree.String(), success.ID.String(), install.ID.String()}
	for _, failure := range failures {
		identifiers = append(identifiers, failure.id.String())
	}
	for _, id := range identifiers {
		if bytes.Contains(tail, []byte(id)) {
			t.Errorf("diagnostic attachment kept local identifier %s", id)
		}
	}
	if bytes.Contains(tail, []byte(`"report_ref"`)) {
		t.Error("diagnostic attachment kept a report reference")
	}
	assertWorkspaceReadSentryEvent(t, server, inspectRef, append(private, identifiers...))
}

func assertWorkspaceReadSentryEvent(t *testing.T, server *nodetest.SentryFixture, ref string, private []string) {
	t.Helper()
	bodies := server.Bodies()
	if len(bodies) != 1 {
		t.Fatalf("automatic reports = %d, want only the inspect_git failure", len(bodies))
	}
	body := bodies[0]
	lines := bytes.SplitN(bytes.TrimSuffix(body, []byte("\n")), []byte("\n"), 4)
	var item struct {
		Type string `json:"type"`
	}
	var event sentry.Event
	if len(lines) != 3 || json.Unmarshal(lines[1], &item) != nil || item.Type != "event" || json.Unmarshal(lines[2], &event) != nil {
		t.Fatal("expected one Sentry event without attachments")
	}
	const message = "command.inspect_git:unavailable"
	if event.Message != message || event.Level != sentry.LevelError {
		t.Error("Sentry event does not describe an inspect_git error")
	}
	if len(event.Exception) != 1 || event.Exception[0].Type != "SailryError" || event.Exception[0].Value != message ||
		event.Exception[0].Mechanism == nil || event.Exception[0].Mechanism.Handled == nil || !*event.Exception[0].Mechanism.Handled {
		t.Error("Sentry event lacks the sanitized handled failure")
	}
	fields := event.Contexts["sailry"]
	want := map[string]string{
		"area": "command", "phase": "failed", "operation": "inspect_git", "code": "unavailable", "command_stage": "completion",
		"report_ref": ref, "stage": "run_status", "reason": string(protocol.GitInspectionStatusCommandFailed),
	}
	for key, value := range want {
		if fields[key] != value {
			t.Errorf("inspect_git Sentry event has unexpected %s", key)
		}
	}
	for key := range fields {
		if _, ok := want[key]; !ok && key != "duration_us" {
			t.Errorf("inspect_git Sentry event has unexpected field %q", key)
		}
	}
	if event.Request != nil || !event.User.IsEmpty() || event.ServerName != "" {
		t.Error("Sentry event kept request, user or host information")
	}
	for index, value := range private {
		if bytes.Contains(body, []byte(value)) {
			t.Errorf("Sentry envelope kept private canary %d", index)
		}
	}
}
