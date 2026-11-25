package desktop_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	databases "github.com/sailry/sailry-code/modules/code/databases/desktop"
	databasesnode "github.com/sailry/sailry-code/modules/code/databases/node"
	databasesservice "github.com/sailry/sailry-code/modules/code/databases/service"
	sshnode "github.com/sailry/sailry-code/modules/code/ssh/node"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/node"
	nativeprotocol "github.com/sailry/sailry-harness/sdk/native/protocol"
	"github.com/sailry/sailry-harness/sdk/sdktest/protocol"
	_ "modernc.org/sqlite"
)

const legacyDatabaseDigest = "dcfb60fc83ceba85917853f890f27eb191df81379c8984774857a3ea1527d530"

func databaseHost() node.Assembly {
	host := node.HarnessAssembly()
	host.Modules = append(host.Modules, databasesnode.Module(), sshnode.Module())
	return host
}

func TestCodeDatabaseWorkspace(t *testing.T) {
	t.Parallel()
	w := openRecordedWorkspace(t, native.Assemble(databases.Register))
	if installed := w.Controller.Snapshot().Plugins; len(installed) != 0 {
		t.Fatalf("native workspace has an external plugin dependency: %+v", installed)
	}
	w.exercise(true)
}

func TestLegacyDatabaseWorkspace(t *testing.T) {
	t.Parallel()
	source := legacyDatabaseSource(t)
	w := openRecordedWorkspace(t, native.Assembly{})
	installDatabasePackage(w, source)
	w.Await("the exact Databases 0.1.26 package was not installed", func() bool {
		for _, pkg := range w.Controller.Snapshot().Plugins {
			if pkg.Name == "databases" {
				if pkg.Digest != legacyDatabaseDigest || pkg.Version == nil || *pkg.Version != "0.1.26" || !pkg.Enabled || pkg.Compatibility.Kind != "compatible" {
					t.Fatalf("unexpected installed package: %+v", pkg)
				}
				return true
			}
		}
		return false
	})
	w.exercise(false)
}

func legacyDatabaseSource(t *testing.T) string {
	t.Helper()
	source := os.Getenv("SAILRY_LEGACY_DATABASES")
	if source == "" {
		t.Skip("set SAILRY_LEGACY_DATABASES to the archived Databases 0.1.26 package directory")
	}
	if _, err := os.Stat(filepath.Join(source, "plugin.json")); err != nil {
		t.Fatal(err)
	}
	return source
}

type databaseWindow struct {
	*support.Window
	operations <-chan databaseOperation
}

type databaseOperation struct {
	kind string
	name string
	sql  string
	err  error
}

func openRecordedWorkspace(t *testing.T, assembly native.Assembly) *databaseWindow {
	t.Helper()
	host := databaseHost()
	operations := make(chan databaseOperation, 64)
	host.Databases = func(ctx context.Context, input node.DatabaseOperation) (nativeprotocol.DatabaseOutcome, error) {
		output, err := databasesservice.ExecuteOperation(ctx, input)
		var query string
		if input.Query != nil {
			query = input.Query.Sql
		}
		operations <- databaseOperation{kind: output.Kind, name: input.Profile.Name, sql: query, err: err}
		return output, err
	}
	return &databaseWindow{Window: support.Open(t, host, assembly), operations: operations}
}

func (w *databaseWindow) exercise(compiled bool) {
	t := w.T
	db, path := sqliteFixture(t)
	const savedName = "SQLite workspace fixture"
	w.clickRail("Databases")
	w.Await("database workspace did not open", func() bool { return w.Tester.HasText("Open a database connection") })
	w.Click(w.control(ui.RoleButton, "New connection"))
	w.replace("Name", savedName)
	w.clickByName("MySQL")
	if field := w.control(ui.RoleTextField, "Password"); !field.Password || field.Value != "" {
		t.Fatalf("password is not a blank protected field: %+v", field)
	}
	w.clickByName("SQLite")
	w.replace("Database file path", path)
	w.clickByName("Global sharing")
	if compiled {
		w.testConnection(savedName)
		if profiles := w.profiles(); len(profiles) != 0 {
			t.Fatal("testing a connection saved a profile")
		}
	}
	w.clickConnectionAction("Save")
	w.Await("saving did not close the editor and show the saved profile", func() bool {
		return !w.has(ui.RoleTextField, "Database file path") && w.has(ui.RoleButton, savedName) && w.has(ui.RoleStatus, "Saved")
	})
	profiles := w.profiles()
	if len(profiles) != 1 || profiles[0].Connection.Kind != "sqlite" || !profiles[0].ReadOnly || profiles[0].Sharing == nil || profiles[0].Sharing.Kind != "global" {
		t.Fatalf("saved profiles = %+v", profiles)
	}
	w.Click(w.control(ui.RoleButton, "Database connections"))
	w.choose(savedName)
	w.choose("Edit")
	w.Await("the connection picker did not hand off to the saved SQLite editor", func() bool {
		field, ok := w.Tester.AccessByRole(ui.RoleTextField, "Database file path")
		return w.Tester.HasText("Edit connection") && !w.has(ui.RoleTextField, "Search") && ok && visible(field) && field.Value == path
	})
	const unsavedName = "SQLite draft checked without saving"
	w.replace("Name", unsavedName)
	w.testConnection(unsavedName)
	w.clickConnectionAction("Cancel")
	w.Await("cancel did not close the connection editor", func() bool { return !w.has(ui.RoleTextField, "Database file path") })
	if saved := w.profiles(); len(saved) != 1 || saved[0].Name != savedName || saved[0].Revision != profiles[0].Revision {
		t.Fatalf("testing and canceling changed the saved profile: %+v", saved)
	}
	w.clickByName(savedName)
	w.expand("main", "fixture_rows")
	w.Click(w.control(ui.RoleButton, "New query"))
	const query = "SELECT id, label FROM fixture_rows"
	w.replace("Code editor", query)
	w.Click(w.control(ui.RoleButton, "Run"))
	w.Await("query did not show the original SQLite value and exact integer", func() bool {
		return w.Tester.HasText("sqlite-actual-row") && w.Tester.HasText("9223372036854775807") && w.Tester.HasText(query)
	})
	w.Await("the resource-bound assistant did not mount", func() bool {
		return w.Tester.HasText("Database assistant") && w.has(ui.RoleButton, support.Label("chat_connection_history"))
	})
	w.Click(w.control(ui.RoleButton, "New query"))
	const draft = "SELECT 'keep unsent SQL draft'"
	w.replace("Code editor", draft)
	w.App.Navigate(native.ConversationPage)
	w.Await("switching away did not hide the SQL editor", func() bool {
		return w.App.Page == native.ConversationPage && !w.has(ui.RoleTextField, "Code editor")
	})
	if _, err := db.Exec("CREATE TABLE arrived_while_hidden (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	w.clickRail("Databases")
	if compiled {
		field := w.control(ui.RoleTextField, "Code editor")
		if field.Value != draft {
			t.Fatalf("reentering replaced the SQL draft: %q", field.Value)
		}
		w.Click(w.control(ui.RoleButton, "Cancel"))
		w.Await("entry refresh did not keep the expanded catalog, previous result and query log", func() bool {
			return w.has(ui.RoleTreeItem, "arrived_while_hidden") && w.Tester.HasText("sqlite-actual-row") && w.Tester.HasText(query)
		})
	} else {
		// The historical mount is dropped when leaving its rail page on
		// this Harness baseline. Its script also has an entry refresh path
		// when the host retains a mount, covered by the separate script test.
		w.Await("the legacy database page did not return to the saved-connection landing", func() bool {
			return w.Tester.HasText("Open a database connection") && w.Tester.HasText(savedName)
		})
		w.clickByName(savedName)
		w.expand("main", "arrived_while_hidden")
	}
	if w.Tester.HasText("keep unsent SQL draft") {
		t.Fatal("reentering executed the unsent SQL draft")
	}
	var queries []string
	for len(w.operations) > 0 {
		operation := <-w.operations
		if operation.kind == "connected" {
			t.Fatalf("unexpected additional connection test: %+v", operation)
		}
		if operation.sql != "" {
			queries = append(queries, operation.sql)
		}
	}
	if len(queries) != 1 || queries[0] != query {
		t.Fatalf("reentering dispatched unexpected SQL: %q", queries)
	}
}

func (w *databaseWindow) clickConnectionAction(label string) {
	w.T.Helper()
	w.Await("connection editor action is not reachable: "+label, func() bool {
		// The picker's saved-connection actions also include “Test connection”.
		button, ok := w.Tester.AccessByRole(ui.RoleButton, label)
		return !w.has(ui.RoleTextField, "Search") && w.has(ui.RoleTextField, "Database file path") &&
			ok && visible(button) && !button.Disabled && w.Tester.Click(label) == nil
	})
}

func (w *databaseWindow) testConnection(name string) {
	w.T.Helper()
	w.clickConnectionAction("Test connection")
	completed := false
	w.Await("SQLite connection test did not execute and acknowledge the editor draft", func() bool {
		select {
		case operation := <-w.operations:
			if completed || operation.err != nil || operation.kind != "connected" || operation.name != name {
				w.T.Fatalf("unexpected editor database test: %+v", operation)
			}
			completed = true
		default:
		}
		return completed && w.has(ui.RoleStatus, "Connected")
	})
}

func sqliteFixture(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE fixture_rows (id INTEGER PRIMARY KEY, label TEXT); INSERT INTO fixture_rows VALUES (9223372036854775807, 'sqlite-actual-row')`); err != nil {
		t.Fatal(err)
	}
	return db, path
}

func (w *databaseWindow) profiles() []protocol.DatabaseProfile {
	return support.Result[[]protocol.DatabaseProfile](w.T, w.Do(protocol.Command{Kind: "list_databases"}))
}

func visible(entry ui.AccessInfo) bool {
	return !entry.Offscreen && entry.Bounds.W > 0 && entry.Bounds.H > 0
}

func (w *databaseWindow) has(role ui.Role, name string) bool {
	for _, item := range w.Tester.AccessAll() {
		if item.Role == role && item.Name == name && visible(item) {
			return true
		}
	}
	return false
}

func (w *databaseWindow) control(role ui.Role, name string) ui.AccessInfo {
	w.T.Helper()
	var result ui.AccessInfo
	w.Await("missing enabled control "+name, func() bool {
		for _, item := range w.Tester.AccessAll() {
			if item.Role == role && item.Name == name && visible(item) && !item.Disabled {
				result = item
				return true
			}
		}
		return false
	})
	return result
}

func (w *databaseWindow) clickRail(name string) {
	w.T.Helper()
	var result ui.AccessInfo
	w.Await("no pinned rail entry "+name, func() bool {
		for _, item := range w.Tester.AccessAll() {
			if item.Role == ui.RoleButton && item.Name == name && visible(item) && item.Bounds.X+item.Bounds.W <= native.RailWidth && !item.Disabled {
				result = item
				return true
			}
		}
		return false
	})
	w.Click(result)
}

func (w *databaseWindow) clickByName(name string) {
	w.T.Helper()
	w.Await("cannot click "+name, func() bool { return w.Tester.Click(name) == nil })
}

func (w *databaseWindow) choose(name string) {
	w.T.Helper()
	var choice ui.AccessInfo
	w.Await("the database command picker does not offer "+name, func() bool {
		items := w.Tester.AccessAll()
		for _, popup := range items {
			if popup.Name != "Search" || popup.Role == ui.RoleTextField || popup.Role == ui.RoleButton || !visible(popup) {
				continue
			}
			bounds := popup.Bounds
			for _, entry := range items {
				r := entry.Bounds
				if entry.Role == ui.RoleButton && entry.Name == name && visible(entry) && !entry.Disabled &&
					r.X >= bounds.X && r.Y >= bounds.Y && r.X+r.W <= bounds.X+bounds.W && r.Y+r.H <= bounds.Y+bounds.H {
					choice = entry
					return true
				}
			}
		}
		return false
	})
	w.Click(choice)
}

func (w *databaseWindow) replace(name, value string) {
	w.T.Helper()
	w.Click(w.control(ui.RoleTextField, name))
	w.Tester.Key(ui.Cmd, ui.KeyA)
	w.Tester.Type(value)
}

func (w *databaseWindow) expand(name, child string) {
	w.T.Helper()
	row := w.control(ui.RoleTreeItem, name)
	if !w.has(ui.RoleTreeItem, child) {
		w.Tester.ClickAt(row.Bounds.X+12, row.Bounds.Y+row.Bounds.H/2)
	}
	w.Await("catalog did not expand "+name+" to "+child, func() bool { return w.has(ui.RoleTreeItem, child) })
}

func command(t *testing.T, kind string, data any) protocol.Command {
	t.Helper()
	body, err := json.Marshal(map[string]any{"kind": kind, "data": data})
	if err != nil {
		t.Fatal(err)
	}
	var value protocol.Command
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("decode %s: %v", kind, err)
	}
	return value
}

func pageCount(w *databaseWindow, label string) int {
	var count int
	for _, entry := range w.Tester.AccessAll() {
		if strings.EqualFold(entry.Name, label) && entry.Role == ui.RoleButton && visible(entry) && entry.Bounds.X+entry.Bounds.W <= native.RailWidth {
			count++
		}
	}
	return count
}
