package gitnode_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/sailry/sailry-code/internal/testsupport/node"
	gitnode "github.com/sailry/sailry-code/modules/code/git/node"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/getsentry/sentry-go"
	gogit "github.com/go-git/go-git/v5"
	"github.com/sailry/sailry-harness/sdk/native/process"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestGitBadgeCommandReporting(t *testing.T) {
	home := t.TempDir()
	for _, key := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME"} {
		t.Setenv(key, home)
	}
	root := t.TempDir()
	if _, err := gogit.PlainInit(root, false); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(t.TempDir(), "profile")
	closeLog, err := sdktest.InstallLogging(profile, "desktop")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeLog)
	server := nodetest.NewSentry(t)
	const localCause = "private fixture Git process failure"
	var inspected atomic.Bool
	node, err := native.StartWithOptions(t.Context(), profile, native.Options{
		Assembly:    native.Assembly{Modules: []native.Module{gitnode.Module()}},
		Environment: new(process.NewEnvironment(nil)),
		Git: sdktest.GitOptions{Run: func(_ context.Context, _ string, args []string, _ []byte) (sdktest.GitCommandResult, error) {
			if slices.Contains(args, "status") {
				inspected.Store(true)
			}
			return sdktest.GitCommandResult{}, errors.New(localCause)
		}},
		Telemetry: server.Options(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = node.Stop(context.Background()) })
	const entry = "dev.sailry.platform/host/main.js"
	nodetest.InstallPlugin(t, node, "git", map[string]string{
		"plugin.json": `{"$schema":"` + sdktest.ManifestSchema + `","name":"git","version":"0.1.0","extensions":{"dev.sailry.platform":{"api_version":"v1","scope":"host","requires_modules":["git"],"actions":["git.read"],"ui":[{"id":"changes","slot":"status","kind":"button","label":{"label":"Git"},"handler":"changes","order":0}],"host":{"entry":"` + entry + `","resources":["` + entry + `"],"handlers":["badge"],"read_only":["badge"],"status":{"read":"badge","changes":["repository"]}}}}}`,
		entry:         `import {inspectGit} from 'sailry/sdk'; export async function badge() { await inspectGit(); return []; }`,
	})
	project := nodetest.Command(t, node, protocol.Command{Kind: "register_project", Data: protocol.CommandRegisterProjectPayload{Name: "Git badge", Path: root}}).Data.(protocol.Project)
	config := diagnosticSessionConfigFixture(t, node)
	session := nodetest.Command(t, node, protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Project: &project.ID, Config: &config}}).Data.(protocol.Session)
	output := nodetest.Command(t, node, protocol.Command{Kind: "read_plugin", Data: protocol.CommandReadPluginPayload{Name: "git"}})
	info, ok := nodetest.Payload[protocol.PluginInfo](output.Data)
	if !ok || info.Extension == nil || info.Extension.Host == nil || info.Extension.Host.Status == nil {
		t.Fatal("fixture Git status callback is unavailable")
	}
	command := protocol.Command{Kind: "read_plugin_callback", Data: protocol.CommandReadPluginCallbackPayload{Handler: info.Extension.Host.Status.Read, Input: json.RawMessage(`{}`)}}
	scope := protocol.PluginContext{Surface: protocol.PluginDesktopSurface{Kind: "composer"}, Package: sdktest.PackageReference(info), Worktree: &session.Worktree, Session: &session.ID}
	missing := protocol.NewRequest(node.ID(), command)
	missingScope := scope
	missingScope.Package.Name = "missing-git-fixture"
	missing.Plugin = &missingScope
	if fault := nodetest.Dispatch(t, node, node.ID(), missing).Fault; fault == nil || fault.Code != protocol.ErrorCodeNotFound {
		t.Fatalf("missing package returned %v", fault)
	}
	request := protocol.NewRequest(node.ID(), command)
	request.Plugin = &scope
	fault := nodetest.Dispatch(t, node, node.ID(), request).Fault
	if fault == nil || fault.Code != protocol.ErrorCodeUnavailable || fault.Message != "Git repository operation failed: "+localCause {
		t.Fatalf("Git callback returned %v", fault)
	}
	if !inspected.Load() {
		t.Fatal("callback did not reach native Git status")
	}
	report, ok := diagnosticFault(fault)
	if !ok || report.Source != protocol.NotificationSystemSystemSourcePluginCallback || report.Phase != protocol.NotificationSystemDiagnosticPhaseCallback ||
		report.Reason != protocol.NotificationSystemDiagnosticReasonFailure || report.Code != fault.Code || report.Exception != nil ||
		report.Worktree == nil || *report.Worktree != session.Worktree || report.Session == nil || *report.Session != session.ID {
		t.Fatalf("callback diagnostic = %+v", report)
	}
	if err := node.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	logFile, err := sdktest.CurrentLogFile()
	if err != nil {
		t.Fatal(err)
	}
	logged, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct{ id, level, code, stage string }{
		{missing.ID.String(), "WARN", "not_found", "admission"},
		{request.ID.String(), "ERROR", "unavailable", "completion"},
	} {
		if !slices.ContainsFunc(strings.Split(string(logged), "\n"), func(line string) bool {
			return strings.Contains(line, " "+want.level+" desktop area=command phase=failed ") && strings.Contains(line, " request="+want.id+" ") &&
				strings.Contains(line, " code="+want.code) && strings.Contains(line, " command_stage="+want.stage)
		}) {
			t.Errorf("missing local %s record for %s (%s)", want.level, want.code, want.stage)
		}
	}
	bodies := server.Bodies()
	if len(bodies) == 0 {
		t.Fatal("Git failure did not reach the Sentry fixture")
	}
	for _, body := range bodies {
		lines := bytes.SplitN(body, []byte("\n"), 4)
		var event sentry.Event
		if len(lines) < 3 || json.Unmarshal(lines[2], &event) != nil {
			t.Fatal("invalid Sentry fixture envelope")
		}
		fields := event.Contexts["sailry"]
		if fields["code"] != "unavailable" || fields["command_stage"] != "completion" || fields["package"] != "third_party" ||
			len(event.Exception) != 1 || event.Exception[0].Type != "SailryError" || event.Exception[0].Mechanism == nil ||
			event.Exception[0].Mechanism.Handled == nil || !*event.Exception[0].Mechanism.Handled {
			t.Fatalf("unexpected Sentry event: %s", lines[2])
		}
		for _, private := range []string{localCause, root, session.Worktree.String(), session.ID.String(), missingScope.Package.Name} {
			if bytes.Contains(body, []byte(private)) {
				t.Fatal("Sentry fixture received private data")
			}
		}
	}
}

func diagnosticSessionConfigFixture(t *testing.T, node *native.Node) protocol.SessionConfig {
	t.Helper()
	provider := protocol.DefaultConversationProvider()
	provider.ID, provider.Name = protocol.NewProviderID(), "Fixture"
	provider.Api, provider.Authentication, provider.Enabled = protocol.ConversationModelApiChatCompletions, protocol.AuthenticationApiKey, true
	provider.Endpoint, provider.DefaultModel = "https://fixture.invalid/v1", "fixture"
	model := protocol.DefaultConversationModel()
	model.ID, model.Context, model.Output = "fixture", 64000, 8192
	vision := false
	model.Capabilities = &protocol.ConversationDiscoveryCapabilities{Vision: &vision}
	provider.Models = []protocol.ConversationModel{model}
	key := protocol.NewSecret("test-only-session-fixture-key")
	response := nodetest.Dispatch(t, node, node.ID(), protocol.NewRequest(node.ID(), protocol.Command{Kind: "save_provider", Data: &protocol.CommandSaveProviderPayload{Provider: provider, Secret: &key}}))
	if response.Fault != nil {
		t.Fatal(response.Fault)
	}
	created := response.Output.Data.(protocol.ConversationProvider)
	config := protocol.DefaultSessionConfig()
	config.Provider, config.Model, config.Credential = created.ID, created.DefaultModel, created.Credential
	config.Mode, config.Permission = protocol.WorkModeCode, protocol.PermissionProject
	return config
}

func diagnosticFault(fault *protocol.Fault) (protocol.NotificationSystemSystemDiagnostic, bool) {
	if fault.Details == nil || fault.Details.Kind != "system_diagnostic" {
		return protocol.NotificationSystemSystemDiagnostic{}, false
	}
	return nodetest.Payload[protocol.NotificationSystemSystemDiagnostic](fault.Details.Data)
}
