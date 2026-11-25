package desktop

import (
	"reflect"
	"slices"
	"testing"

	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestSSHHistoryRetainsLegacySessionConfiguration(t *testing.T) {
	profile, other := protocol.SshID{1}, protocol.SshID{2}
	legacyBinding := &protocol.PluginConversationBinding{Package: protocol.PluginReference{Name: "ssh"}, ID: "ssh"}
	legacy := protocol.Session{ID: protocol.SessionID{1}, Config: protocol.SessionConfig{
		Resource:   &protocol.ConnectionResource{Kind: "ssh", Data: &profile},
		Assistant:  legacyBinding,
		Permission: protocol.PermissionFull,
	}}
	native := protocol.Session{ID: protocol.SessionID{2}, Config: NewSessionConfig(profile, legacy.Config)}
	archived, delegated := native, native
	archived.ID, delegated.ID = protocol.SessionID{4}, protocol.SessionID{5}
	archived.Archived = true
	field := reflect.ValueOf(&delegated).Elem().FieldByName("Delegation")
	field.Set(reflect.New(field.Type().Elem()))
	wrongProfile := protocol.Session{ID: protocol.SessionID{3}, Config: NewSessionConfig(other, native.Config)}
	all := []protocol.Session{legacy, wrongProfile, native, archived, delegated}
	var got []protocol.Session
	for _, session := range slices.Backward(all) {
		if SessionMatches(session, profile) {
			got = append(got, session)
		}
	}
	if len(got) != 2 || got[0].ID != native.ID || got[1].ID != legacy.ID {
		t.Fatal("history must list only the resource's live non-delegated sessions, newest first")
	}
	if got[1].Config.Assistant != legacyBinding || got[1].Config.Permission != protocol.PermissionFull {
		t.Fatal("showing a legacy session must not remove its captured assistant or change its permission")
	}
	if native.Config.Assistant != nil || native.Config.Permission != protocol.PermissionAsk {
		t.Fatal("a new resource draft must not copy the legacy assistant or permission")
	}
	if legacy.Config.Assistant != legacyBinding || legacy.Config.Permission != protocol.PermissionFull {
		t.Fatal("seeding a new draft changed the saved session's configuration")
	}
}
