package desktop

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
)

func TestGitProjectionRefreshesTextAndInPlaceStatus(t *testing.T) {
	file := native.WorkspaceChange{Path: "a", Unstaged: "M", Hunks: []native.WorkspaceHunk{{Lines: []native.WorkspaceDiffLine{{Kind: native.DiffAdded, Text: "first"}}}}}
	var shown string
	var row *changeRow
	tt := ui.NewTester(func(c *ui.Context) {
		box := ui.Column(c.Key("file"))
		_, shown = native.DiffTexts(box, file)
		rows := changeListRows(box, []native.WorkspaceChange{file}, changeNav{grouping: "none"})
		if len(rows) != 1 {
			t.Fatal("expected one status row")
		}
		row = rows[0]
	}, 200, 200)
	tt.Frame()
	first := row
	if shown != "first\n" || row.entry.Hunks != nil {
		t.Fatal("initial diff or sidebar projection is incorrect")
	}
	file.Hunks = []native.WorkspaceHunk{{Lines: []native.WorkspaceDiffLine{{Kind: native.DiffAdded, Text: "replacement"}}}}
	tt.Frame()
	if shown != "replacement\n" || row != first {
		t.Fatal("replacing hunks should update the editor, not rebuild status rows")
	}
	file.Unstaged = "D"
	tt.Frame()
	if row.entry.Unstaged != "D" {
		t.Fatal("in-place status update was not shown")
	}
	file.Hunks = nil
	tt.Frame()
	if shown != "\n" {
		t.Fatal("empty diff retained the old editor input")
	}
}
