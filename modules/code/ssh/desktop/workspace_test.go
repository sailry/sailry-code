package desktop_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	ssh "github.com/sailry/sailry-code/modules/code/ssh/desktop"
	sshnode "github.com/sailry/sailry-code/modules/code/ssh/node"
	terminalnode "github.com/sailry/sailry-code/modules/code/terminal/node"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/node"
	nativeprotocol "github.com/sailry/sailry-harness/sdk/native/protocol"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestCodeSSHNativeWorkspace(t *testing.T) {
	peer := newCodeSSHPeer(t)
	w := newCodeSSHWindow(t)
	if plugins := support.Result[[]protocol.PluginSummary](t, w.Do(protocol.Command{Kind: "list_plugins"})); len(plugins) != 0 {
		t.Fatalf("SSH workspace fixture must have zero plugins: %+v", plugins)
	}
	profile := w.saveSSHProfile("Saved SSH fixture", peer.port)
	w.openSSHPage()

	openUntrusted := func() {
		t.Helper()
		w.clickNamed(profile.Name)
		w.Await("opening SSH did not present the exact first host fingerprint", func() bool {
			return w.Tester.HasText(peer.key.Fingerprint) && w.hasControl(ui.RoleButton, sshText("ssh_trust"))
		})
		if peer.authentications.Load() != 0 || peer.shells.Load() != 0 || len(w.Controller.Snapshot().Terminals) != 0 {
			t.Fatal("SSH authentication or terminal creation preceded fingerprint confirmation")
		}
	}
	openUntrusted()
	w.Click(w.control(ui.RoleButton, sshText("settings_cancel")))
	w.Await("cancelling the fingerprint prompt did not return to the connection list", func() bool {
		return !w.Tester.HasText(peer.key.Fingerprint) && w.Tester.HasText(profile.Name)
	})
	if stored := w.onlySSHProfile(); stored.Revision != profile.Revision || stored.HostKey != nil || peer.authentications.Load() != 0 {
		t.Fatalf("cancelling the fingerprint prompt changed the connection: %+v", stored)
	}
	openUntrusted()
	w.Click(w.control(ui.RoleButton, sshText("ssh_trust")))
	var terminal nativeprotocol.TerminalID
	w.Await("trusting the key did not resume opening the saved SSH terminal", func() bool {
		terminals := w.Controller.Snapshot().Terminals
		if len(terminals) != 1 || terminals[0].Ssh == nil || *terminals[0].Ssh != profile.ID || terminals[0].Status.Kind != "running" {
			return false
		}
		terminal = terminals[0].ID
		return peer.shells.Load() == 1
	})
	trusted := w.onlySSHProfile()
	if trusted.ID != profile.ID || trusted.Revision != profile.Revision+1 || trusted.HostKey == nil || *trusted.HostKey != peer.key {
		t.Fatalf("SSH trust was not saved on the original connection: %+v", trusted)
	}
	w.Await("the SSH terminal did not receive the fixture's shell output", func() bool {
		body, err := json.Marshal(map[string]any{"kind": "inspect_terminal", "data": map[string]any{"terminal": terminal}})
		if err != nil {
			t.Fatal(err)
		}
		var inspect protocol.Command
		if err := json.Unmarshal(body, &inspect); err != nil {
			t.Fatalf("decode inspect_terminal: %v", err)
		}
		output := w.Do(inspect)
		if output.Kind != "terminal_snapshot" {
			t.Fatalf("inspect_terminal returned %q", output.Kind)
		}
		var snapshot struct {
			Screen struct {
				VT *struct{ Bytes nativeprotocol.Bytes } `json:"vt"`
			} `json:"screen"`
		}
		body, err = json.Marshal(output.Data)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &snapshot); err != nil {
			t.Fatal(err)
		}
		return snapshot.Screen.VT != nil && strings.Contains(string(snapshot.Screen.VT.Bytes), codeSSHGreeting)
	})
	w.checkSSHSplit()
	w.checkSSHFiles(trusted)
	w.checkSSHResourceEntry()
}

func TestCodeSSHNativeEditsSavedConnection(t *testing.T) {
	peer := newCodeSSHPeer(t)
	w := newCodeSSHWindow(t)
	profile := w.saveSSHProfile("Legacy SSH connection", peer.port)
	profile = support.Result[protocol.SshProfile](t, w.Do(protocol.Command{Kind: "trust_ssh", Data: protocol.CommandTrustSshPayload{
		Profile: profile.ID, ExpectedRevision: profile.Revision, Key: peer.key,
	}}))
	w.openSSHPage()
	// The picker opens after the script handles the header action; its rows,
	// not the landing card of the same name, choose the connection.
	w.Click(w.control(ui.RoleButton, sshText("ssh_connections")))
	w.chooseCommand(profile.Name)
	w.chooseCommand(sshText("connection_action_edit"))
	name := sshText("settings_name")
	if field := w.control(ui.RoleTextField, name); field.Value != profile.Name {
		t.Fatalf("the connection editor did not load the saved name: %q", field.Value)
	}
	if field := w.control(ui.RoleTextField, sshText("ssh_password")); !field.Password || field.Value != "" {
		t.Fatal("the edit form exposed or prefilled the saved password")
	}
	const updatedName = "Renamed SSH connection"
	w.replaceText(name, updatedName)
	w.Click(w.control(ui.RoleButton, sshText("settings_save")))
	w.Await("saving the edited connection did not update its landing card", func() bool {
		return w.Tester.HasText(updatedName) && !w.hasControl(ui.RoleTextField, name)
	})
	stored := w.onlySSHProfile()
	want := profile
	want.Name, want.Revision = updatedName, profile.Revision+1
	if !reflect.DeepEqual(stored, want) {
		t.Fatalf("editing the saved name changed other connection data: got %+v, want %+v", stored, want)
	}
	if peer.authentications.Load() != 0 || peer.shells.Load() != 0 || len(w.Controller.Snapshot().Terminals) != 0 {
		t.Fatal("choosing and editing a saved connection connected to it or opened a terminal")
	}
	store := sdktest.Inspect(w.Owner).Store()
	if err := store.Node.Read(w.Ctx, func(db *sql.Conn) error {
		_, credential, err := sdktest.ReadSsh(w.Ctx, db, stored.ID, stored.Revision)
		if err != nil {
			return err
		}
		password := support.Result[protocol.SshCredentialPasswordPayload](t, protocol.Output{Kind: credential.Kind, Data: credential.Data})
		if credential.Kind != "password" || password.Password.Expose() != codeSSHPassword {
			t.Error("editing a name without entering a new password did not keep the saved credential")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

type codeSSHWindow struct{ *support.Window }

// sshHost is the Harness Node with the Code modules SSH terminals use.
func sshHost() node.Assembly {
	host := node.HarnessAssembly()
	host.Modules = append(host.Modules, sshnode.Module(), terminalnode.Module())
	return host
}

// sshDesktop is the shared Desktop with only this module's page.
func sshDesktop() native.Assembly { return native.Assemble(ssh.Register) }

// codeSSHCatalog includes the Code modules' own labels, such as the rail's.
var codeSSHCatalog = sync.OnceValue(func() *i18n.Catalog { return i18n.New(sshDesktop().Locales...) })

// codeSSHTexts are the English messages the SSH page's template draws.
var codeSSHTexts = sync.OnceValues(func() (map[string]string, error) {
	body, err := os.ReadFile(filepath.Join("view", "locales.js"))
	if err != nil {
		return nil, err
	}
	english, _, found := strings.Cut(string(body), "\nconst zh = {")
	if !found {
		return nil, errors.New("the SSH template has no English messages")
	}
	texts := map[string]string{}
	for _, match := range regexp.MustCompile(`(?m)^\s*"([a-z0-9_]+)":\s*("(?:[^"\\]|\\.)*")`).FindAllStringSubmatch(english, -1) {
		value, err := strconv.Unquote(match[2])
		if err != nil {
			return nil, err
		}
		texts[match[1]] = value
	}
	return texts, nil
})

func sshText(key string) string {
	texts, err := codeSSHTexts()
	if err != nil {
		panic(err)
	}
	text, ok := texts[key]
	if !ok {
		panic("the SSH template has no English message " + key)
	}
	return text
}

func newCodeSSHWindow(t *testing.T) *codeSSHWindow {
	t.Helper()
	return &codeSSHWindow{support.Open(t, sshHost(), sshDesktop())}
}

func (w *codeSSHWindow) openSSHPage() {
	w.T.Helper()
	w.Click(w.sshRail())
	w.Await("the SSH rail did not open the native workspace", func() bool {
		return w.App.Page != native.ConversationPage && w.App.NativePageEnabled(w.App.Page) && w.Tester.HasText(sshText("ssh_workspace_empty"))
	})
}

func (w *codeSSHWindow) sshRail() ui.AccessInfo {
	w.T.Helper()
	var rail ui.AccessInfo
	w.Await("the Code assembly did not show a pinned SSH rail entry", func() bool {
		name := codeSSHCatalog().In("en", "ssh")
		for _, entry := range w.Tester.AccessAll() {
			if entry.Role == ui.RoleButton && entry.Name == name && entry.Bounds.W > 0 && entry.Bounds.X+entry.Bounds.W <= native.RailWidth && !entry.Disabled {
				rail = entry
				return true
			}
		}
		return false
	})
	return rail
}

func (w *codeSSHWindow) saveSSHProfile(name string, port uint16) protocol.SshProfile {
	w.T.Helper()
	return support.Result[protocol.SshProfile](w.T, w.Do(protocol.Command{Kind: "save_ssh", Data: protocol.CommandSaveSshPayload{
		Profile: protocol.SshProfile{ID: protocol.NewSshID(), Name: name, Host: "127.0.0.1", Port: port,
			Username: codeSSHUser, Authentication: protocol.SshAuthenticationPassword, Sharing: &protocol.ConnectionSharing{Kind: "global"}},
		Credential: &protocol.SshCredential{Kind: "password", Data: protocol.SshCredentialPasswordPayload{Password: protocol.NewSecret(codeSSHPassword)}},
	}}))
}

func (w *codeSSHWindow) onlySSHProfile() protocol.SshProfile {
	w.T.Helper()
	profiles := support.Result[[]protocol.SshProfile](w.T, w.Do(protocol.Command{Kind: "list_ssh"}))
	if len(profiles) != 1 {
		w.T.Fatalf("saved SSH profiles = %+v, want one", profiles)
	}
	return profiles[0]
}

func (w *codeSSHWindow) hasControl(role ui.Role, name string) bool {
	entry, ok := w.Tester.AccessByRole(role, name)
	return ok && !entry.Offscreen && entry.Bounds.W > 0 && entry.Bounds.H > 0
}

func (w *codeSSHWindow) control(role ui.Role, name string) ui.AccessInfo {
	w.T.Helper()
	var result ui.AccessInfo
	w.Await("no enabled SSH control named "+name, func() bool {
		for _, entry := range w.Tester.AccessAll() {
			if entry.Role == role && entry.Name == name && !entry.Disabled && !entry.Offscreen && entry.Bounds.W > 0 && entry.Bounds.H > 0 {
				result = entry
				return true
			}
		}
		return false
	})
	return result
}

func (w *codeSSHWindow) clickNamed(name string) {
	w.T.Helper()
	w.Await("could not click "+name, func() bool {
		return w.Tester.Click(name) == nil
	})
}

func (w *codeSSHWindow) replaceText(name, value string) {
	w.T.Helper()
	w.Click(w.control(ui.RoleTextField, name))
	w.Tester.Key(ui.Cmd, ui.KeyA)
	w.Tester.Type(value)
}

func (w *codeSSHWindow) checkSSHSplit() {
	w.T.Helper()
	terminalLabel, home := support.Label("terminal"), codeSSHSegment(codeSSHHome)
	w.Await("the SSH files and terminal did not occupy separate Shell columns", func() bool {
		path, hasPath := w.Tester.AccessByRole(ui.RoleButton, home)
		terminal, hasTerminal := w.Tester.Find(terminalLabel)
		return w.App.NavShown() && hasPath && path.Bounds.W > 0 && hasTerminal && terminal.W > 0 && path.Bounds.X+path.Bounds.W <= terminal.X
	})
	navigation := support.Label("sidebar_toggle")
	w.Click(w.control(ui.RoleButton, navigation))
	w.Await("folding the Shell navigation did not hide only the SSH file column", func() bool {
		terminal, ok := w.Tester.Find(terminalLabel)
		return !w.App.NavShown() && !w.hasControl(ui.RoleButton, home) && !w.hasControl(ui.RoleTreeItem, "docs") && ok && terminal.W > 0
	})
	w.Click(w.control(ui.RoleButton, navigation))
	w.Await("unfolding the Shell navigation did not restore the remote file column", func() bool {
		return w.App.NavShown() && w.hasControl(ui.RoleButton, home) && w.hasControl(ui.RoleTreeItem, "docs")
	})
}

func (w *codeSSHWindow) checkSSHResourceEntry() {
	w.T.Helper()
	w.Await("the active SSH connection did not expose its resource-session entry in the right column", func() bool {
		terminal, ok := w.Tester.Find(support.Label("terminal"))
		if !ok || terminal.W <= 0 {
			return false
		}
		assistant, ok := w.Tester.Find(sshText("ssh_assistant"))
		if !ok || assistant.W <= 0 || assistant.X < terminal.X+terminal.W-1 {
			return false
		}
		for _, entry := range w.Tester.AccessAll() {
			if (entry.Role == ui.RoleButton || entry.Role == ui.RoleMenuButton) &&
				(entry.Name == support.Label("chat_new") || entry.Name == support.Label("chat_connection_history")) &&
				!entry.Offscreen && entry.Bounds.W > 0 && entry.Bounds.X >= terminal.X+terminal.W-1 {
				return true
			}
		}
		return false
	})
}

func (w *codeSSHWindow) checkSSHFiles(profile protocol.SshProfile) {
	w.T.Helper()
	const folder, renamed = "docs", "remote-notes.txt"
	remotePath := sshText("ssh_remote_path")
	home := codeSSHSegment(codeSSHHome)
	w.Await("the saved connection did not list the remote directory under its breadcrumbs", func() bool {
		return w.hasControl(ui.RoleButton, "/") && w.hasControl(ui.RoleButton, home) && w.hasControl(ui.RoleTreeItem, folder) &&
			!w.hasControl(ui.RoleTextField, remotePath)
	})
	// A blank click on the path row edits the path; Escape discards the text.
	if field := w.editRemotePath(home); field.Value != codeSSHHome {
		w.T.Fatalf("the path editor opened on %q, want %q", field.Value, codeSSHHome)
	}
	w.replaceText(remotePath, codeSSHHome+"/missing")
	w.Tester.Key(0, ui.KeyEscape)
	w.Await("Escape did not discard the typed path and restore the breadcrumbs", func() bool {
		return !w.hasControl(ui.RoleTextField, remotePath) && w.hasControl(ui.RoleButton, home) && w.hasControl(ui.RoleTreeItem, folder)
	})
	w.editRemotePath(home)
	w.replaceText(remotePath, codeSSHHome+"/"+folder)
	w.Tester.Key(0, ui.KeyEnter)
	w.Await("the path field did not open the remote subdirectory", func() bool {
		return w.hasControl(ui.RoleTreeItem, codeSSHFile) && !w.hasControl(ui.RoleTreeItem, folder) &&
			!w.hasControl(ui.RoleTextField, remotePath) && w.hasControl(ui.RoleButton, folder)
	})
	row := w.control(ui.RoleTreeItem, codeSSHFile).Bounds
	w.Tester.RightClickAt(row.X+row.W/2, row.Y+row.H/2)
	w.Await("the remote file did not offer Rename", func() bool {
		return slices.Contains(w.Tester.Menu(), sshText("files_rename"))
	})
	if err := w.Tester.ChooseMenuItem(sshText("files_rename")); err != nil {
		w.T.Fatal(err)
	}
	field := sshText("files_entry_name")
	if got := w.control(ui.RoleTextField, field).Value; got != codeSSHFile {
		w.T.Fatalf("the rename form named %q, want %q", got, codeSSHFile)
	}
	w.replaceText(field, renamed)
	w.Click(w.control(ui.RoleButton, sshText("settings_save")))
	w.Await("renaming the remote file did not refresh the file list", func() bool {
		return w.hasControl(ui.RoleTreeItem, renamed) && !w.hasControl(ui.RoleTreeItem, codeSSHFile)
	})
	outcome := support.Result[nativeprotocol.SshOutcome](w.T, w.Do(protocol.Command{Kind: "browse_ssh_directory", Data: nativeprotocol.CommandBrowseSshDirectoryPayload{
		Profile: profile.ID, ExpectedRevision: profile.Revision, Path: codeSSHHome + "/" + folder,
	}}))
	directory := support.Result[nativeprotocol.SshDirectory](w.T, protocol.Output{Kind: outcome.Kind, Data: outcome.Data})
	if outcome.Kind != "directory" || len(directory.Entries) != 1 || directory.Entries[0].Name != renamed ||
		directory.Entries[0].Kind != nativeprotocol.EntryKindFile || directory.Entries[0].Size != uint64(len(codeSSHContents)) {
		w.T.Fatalf("remote directory after the UI rename = %+v", directory)
	}
	w.Click(w.control(ui.RoleButton, home))
	w.Await("the parent breadcrumb did not return to the remote connection's starting directory", func() bool {
		return w.hasControl(ui.RoleTreeItem, folder) && !w.hasControl(ui.RoleTreeItem, renamed) && !w.hasControl(ui.RoleButton, folder)
	})
	if field := w.editRemotePath(home); field.Value != codeSSHHome {
		w.T.Fatalf("after the breadcrumb the path is %q, want %q", field.Value, codeSSHHome)
	}
	w.Tester.Key(0, ui.KeyEscape)
	w.Await("Escape did not close the path editor", func() bool { return !w.hasControl(ui.RoleTextField, remotePath) })
}

func codeSSHSegment(path string) string { return path[strings.LastIndex(path, "/")+1:] }

// editRemotePath clicks the blank space of the files header between the last
// breadcrumb and the refresh action, as a user does to type a path.
func (w *codeSSHWindow) editRemotePath(last string) ui.AccessInfo {
	w.T.Helper()
	var segment, refresh ui.AccessInfo
	var center float32
	var row []string
	// The column may still be settling after the navigation was unfolded.
	w.Await("the files header has no visible refresh action beside "+last, func() bool {
		segment, refresh, row = w.control(ui.RoleButton, last), ui.AccessInfo{}, row[:0]
		center = segment.Bounds.Y + segment.Bounds.H/2
		for _, entry := range w.Tester.AccessAll() {
			if entry.Bounds.Y > center || center > entry.Bounds.Y+entry.Bounds.H {
				continue
			}
			row = append(row, fmt.Sprintf("%q(role=%v bounds=%+v offscreen=%t)", entry.Name, entry.Role, entry.Bounds, entry.Offscreen))
			if entry.Role == ui.RoleButton && entry.Name == sshText("refresh") && !entry.Offscreen && entry.Bounds.X > segment.Bounds.X {
				refresh = entry
			}
		}
		return refresh.Bounds.W > 0
	})
	blank := segment.Bounds.X + segment.Bounds.W
	if refresh.Bounds.X-blank < 8 {
		w.T.Fatalf("the files header has no blank space between %q and refresh: %v", last, row)
	}
	w.Tester.ClickAt((blank+refresh.Bounds.X)/2, center)
	return w.control(ui.RoleTextField, sshText("ssh_remote_path"))
}
