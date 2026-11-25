package desktop

import (
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	files "github.com/sailry/sailry-code/modules/code/files/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/desktop/theme"
)

// dividers counts the diffs drawn side by side: each has a divider between
// its two texts, taller than the handle between a list and its document.
func dividers(tt *ui.Tester) (n int) {
	for _, e := range tt.AccessAll() {
		if e.Role == ui.RoleSplitter && e.Bounds.H > handle {
			n++
		}
	}
	return n
}

// toggles are the diff layout buttons showing, by either of their names.
func toggles(tt *ui.Tester) (out []ui.AccessInfo) {
	for _, e := range tt.AccessAll() {
		if e.Role == ui.RoleButton && (e.Name == en("git_diff_side_by_side") || e.Name == en("git_diff_inline")) {
			out = append(out, e)
		}
	}
	return out
}

func testDiffFooter(t *testing.T) {
	pr, tt := pageTester(t, Changes)
	clickRole(t, tt, ui.RoleTreeItem, "README.md", 0)
	split := false
	const width, height = 1100, 760
	tt2 := ui.NewTesterWith(func(c *ui.Context) {
		theme.Apply(c, "", theme.Light)
		fixtures.BindWorkspaceLayout(c, &split, nil)
		pageView(c, Changes, pr)
	}, width, height, ui.TesterOptions{Preferences: ui.Preferences{ReduceTransparency: true}})
	tt2.SetLocale("en")
	tt2.Frame()
	tt2.Frame()
	// The diff starts under the page's strip: no toolbar row, and no
	// segmented choice anywhere.
	text, ok := tt2.Access("New text")
	if !ok || text.Bounds.H <= 0 || text.Bounds.Y > barHeight+1 {
		t.Fatalf("the diff starts at %v (%v), under a toolbar's room", text.Bounds, ok)
	}
	edge := width - pr.view.side
	for _, e := range tt2.AccessAll() {
		if (e.Role == ui.RoleRadio || e.Role == ui.RoleTab) && e.Bounds.X < edge {
			t.Fatalf("a diff still has a choice of segments: %+v", e)
		}
	}
	// One button ends the footer, after the counts, 4 DIPs from the main
	// area's edge and centred in the bar.
	button := func() ui.AccessInfo {
		t.Helper()
		all := toggles(tt2)
		if len(all) != 1 {
			t.Fatalf("%d diff layout buttons: %q", len(all), tt2.Texts())
		}
		return all[0]
	}
	b := button()
	if b.Name != en("git_diff_side_by_side") || b.Disabled || b.Bounds.W != 24 || b.Bounds.H != 24 ||
		b.Bounds.X+b.Bounds.W != edge-4 || b.Bounds.Y != height-footer+1+float32(footer-1-24)/2 {
		t.Fatalf("the footer's button is %+v, want 24 square ending at %v", b, edge-4)
	}
	var removed ui.Rect
	found := false
	for _, text := range tt2.Texts() {
		if r, ok := tt2.Find(text); ok && strings.HasPrefix(text, "−") && r.Y >= height-footer {
			removed, found = r, true
		}
	}
	if !found || removed.Y < height-footer || removed.X+removed.W > b.Bounds.X || b.Bounds.X-(removed.X+removed.W) > 12 {
		t.Fatalf("the counts end at %v (%v), the button starts at %v", removed, found, b.Bounds)
	}
	if n := dividers(tt2); n != 0 {
		t.Fatalf("the diff starts with %d sides divided", n)
	}
	press := func() {
		t.Helper()
		b := button().Bounds
		tt2.ClickAt(b.X+b.W/2, b.Y+b.H/2)
		tt2.Frame()
		tt2.Frame()
	}
	press()
	if n := dividers(tt2); n != 1 || !split || button().Name != en("git_diff_inline") {
		t.Fatalf("the button divides %d diffs (%v) and is named %q", n, split, button().Name)
	}
	// All changes is one document: one footer, one button, every file's
	// diff following it.
	clickRole(t, tt2, ui.RoleButton, en("git_diff"), 0)
	if pr.Git.Change() != allChanges {
		t.Fatalf("shown %q", pr.Git.Change())
	}
	if n := dividers(tt2); n < 2 {
		t.Fatalf("the whole diff divides %d of its files", n)
	}
	if b := button(); b.Bounds.X+b.Bounds.W != edge-4 || b.Bounds.Y != height-footer+1+float32(footer-1-24)/2 {
		t.Fatalf("All changes' button is at %v", b.Bounds)
	}
	press()
	if n := dividers(tt2); n != 0 || split || button().Name != en("git_diff_side_by_side") {
		t.Fatalf("the button leaves %d diffs divided (%v)", n, split)
	}
	// A commit's and a stash's documents end with the same footer; command
	// output and a file's editor show no diff and no button.
	file := native.GitDocumentFile{FileChange: pr.Data.Changes[0]}
	other := native.GitDocumentFile{FileChange: pr.Data.Changes[1]}
	for _, open := range []func() string{
		func() string {
			return pr.SetGitCommit("c0ffee", "Commit", "message", []native.GitDocumentFile{file, other}, false)
		},
		func() string { return pr.SetGitDiff("stash", "Stash", file) },
	} {
		id := open()
		tt2.Frame()
		tt2.Frame()
		if b := button(); pr.Git.Change() != id || b.Bounds.X+b.Bounds.W != edge-4 {
			t.Fatalf("%s: the button is at %v", id, b.Bounds)
		}
	}
	pr.SetGitText("output", "Output", "done")
	tt2.Frame()
	if all := toggles(tt2); len(all) != 0 {
		t.Fatalf("command output shows %d diff layout buttons", len(all))
	}
	_, files := pageTester(t, Files)
	if all := toggles(files); len(all) != 0 {
		t.Fatalf("the file viewer shows %d diff layout buttons", len(all))
	}
}

// viewTester shows a project's panel tabs at the panel's width, in English.
func viewTester(t *testing.T, width int, split *bool, share *float32) (*native.PanelState, *repository, *ui.Tester) {
	t.Helper()
	pr := viewOf(fixtures.PreviewProject(false))
	s := native.NewPreview(native.Assemble(Register, files.Register)).Panel
	tt := ui.NewTesterWith(func(c *ui.Context) {
		theme.Apply(c, "", theme.Light)
		fixtures.BindWorkspaceLayout(c, split, share)
		ui.Column(c).Fill().Children(func() { fixtures.RenderWorkspacePanel(c, s, pr.Project) })
	}, width, 772, ui.TesterOptions{Preferences: ui.Preferences{ReduceTransparency: true}})
	tt.SetLocale("en")
	tt.Frame()
	return s, pr, tt
}

func testLayout(t *testing.T) {
	const height = 772
	access := func(t *testing.T, tt *ui.Tester, role ui.Role, name string) ui.Rect {
		t.Helper()
		e, ok := tt.AccessByRole(role, name)
		if !ok {
			t.Fatalf("no %q: %q", name, tt.Texts())
		}
		return e.Bounds
	}
	text := func(t *testing.T, tt *ui.Tester, name string) ui.Rect {
		t.Helper()
		e, ok := tt.Access(name)
		if !ok {
			t.Fatalf("no %q: %q", name, tt.Texts())
		}
		return e.Bounds
	}
	// grip is the handle between a stacked list and its document.
	grip := func(tt *ui.Tester) (ui.Rect, bool) {
		for _, e := range tt.AccessAll() {
			if e.Role == ui.RoleSplitter && e.Bounds.H == handle {
				return e.Bounds, true
			}
		}
		return ui.Rect{}, false
	}
	show := func(tt *ui.Tester, s *native.PanelState, tab native.PanelTab) {
		s.Show(tab)
		tt.Frame()
		tt.Frame()
	}
	// At the panel's first width the list is above its document, both the
	// panel's full width; at 900 they sit side by side, the list at its own
	// width on the right.
	t.Run("StacksByWidth", func(t *testing.T) {
		split, share := false, float32(0)
		s, pr, tt := viewTester(t, 560, &split, &share)
		for _, o := range []struct {
			tab       native.PanelTab
			row, text string
		}{
			{native.PanelTab{Kind: Changes, ID: "README.md"}, "README.md", "New text"},
			{native.PanelTab{Kind: Files, ID: "sailry-go/go.mod"}, "go.mod", "Code editor"},
		} {
			tt.SetSize(560, height)
			show(tt, s, o.tab)
			row, doc := access(t, tt, ui.RoleTreeItem, o.row), text(t, tt, o.text)
			line, ok := grip(tt)
			if !ok || line.X != 0 || line.W != 560 {
				t.Fatalf("%v at 560: the handle is %v (%v)", o.tab, line, ok)
			}
			if row.X > 16 || row.X+row.W < 560-16 || row.Y+row.H > line.Y+handle/2 {
				t.Errorf("%v at 560: the list's row %v is not above the handle %v at full width", o.tab, row, line)
			}
			if doc.X != 0 || doc.W != 560 || doc.Y < line.Y+handle/2 {
				t.Errorf("%v at 560: the document %v is not under the handle %v at full width", o.tab, doc, line)
			}
			// The list is bounded: its share of the height at most.
			if at := line.Y + handle/2; at > ListShare*height+1 || at < 3*rowHeight {
				t.Errorf("%v at 560: the list is %v high of %v", o.tab, at, height)
			}
			tt.SetSize(900, height)
			tt.Frame()
			tt.Frame()
			row, doc = access(t, tt, ui.RoleTreeItem, o.row), text(t, tt, o.text)
			side := pr.view.side
			if o.tab.Kind == Files {
				side = float32(sideWidth)
			}
			if _, ok := grip(tt); ok {
				t.Errorf("%v at 900: the list is still stacked", o.tab)
			}
			if row.X < 900-side || doc.X != 0 || doc.W != 900-side || doc.Y > 2*barHeight {
				t.Errorf("%v at 900: the row %v and the document %v are not side by side", o.tab, row, doc)
			}
		}
		// A list of few rows is no higher than they are.
		s, pr, tt = viewTester(t, 560, &split, &share)
		pr.Data.Changes = pr.Data.Changes[:1]
		pr.view.nav.grouping, pr.view.nav.hierarchical = "none", false
		tt.SetSize(560, height)
		show(tt, s, native.PanelTab{Kind: Changes, ID: pr.Data.Changes[0].Path})
		tt.Advance(time.Second)
		tt.Frame()
		line, _ := grip(tt)
		if want := float32(barHeight + 36 + rowHeight + 8 + 49); line.Y+handle/2 != want {
			t.Errorf("one change: the list is %v high, want its bars and row, %v; active=%+v body=%q", line.Y+handle/2, want, s.Active, tt.Texts())
		}
	})
	// The layout follows the width as it was laid out, with a few DIPs of
	// slack once stacked.
	t.Run("Threshold", func(t *testing.T) {
		split, share := false, float32(0)
		s, _, tt := viewTester(t, stackBelow-1, &split, &share)
		show(tt, s, native.PanelTab{Kind: Changes, ID: "README.md"})
		stacked := func(width int) bool {
			tt.SetSize(width, height)
			tt.Frame()
			tt.Frame()
			_, ok := grip(tt)
			return ok
		}
		for _, o := range []struct {
			width int
			want  bool
		}{{stackBelow - 1, true}, {stackBelow + stackSlack - 1, true}, {stackBelow + stackSlack, false}, {stackBelow, false}, {stackBelow - 1, true}} {
			if got := stacked(o.width); got != o.want {
				t.Errorf("at %d the list is stacked %v, want %v", o.width, got, o.want)
			}
		}
	})
	// Dragging the handle gives the list more or less of the height, within
	// its bars and a few rows and the window's share; a double click is not
	// needed to come back, the share is the window's.
	t.Run("Divider", func(t *testing.T) {
		split, share := false, float32(0)
		s, _, tt := viewTester(t, 560, &split, &share)
		show(tt, s, native.PanelTab{Kind: Files, ID: "sailry-go/go.mod"})
		before, ok := grip(tt)
		if !ok || share != ListShare {
			t.Fatalf("no handle (%v), share %v", ok, share)
		}
		drag := func(dy float32) ui.Rect {
			at, _ := grip(tt)
			x, y := at.X+at.W/2, at.Y+at.H/2
			tt.Press(x, y)
			tt.Frame()
			tt.Move(x, y+dy/2)
			tt.Frame()
			tt.Move(x, y+dy)
			tt.Frame()
			tt.Release(x, y+dy)
			tt.Frame()
			tt.Frame()
			at, _ = grip(tt)
			return at
		}
		if after := drag(-80); after.Y != before.Y-80 || share >= ListShare {
			t.Fatalf("dragging up 80 moved the handle from %v to %v, share %v", before.Y, after.Y, share)
		}
		if after := drag(-600); after.Y+handle/2 < filesLeast-1 || share < listShareMin {
			t.Fatalf("dragged to the top the list is %v high, share %v", after.Y+handle/2, share)
		}
		// Another tab's list has the same share.
		kept := share
		show(tt, s, native.PanelTab{Kind: Changes, ID: "README.md"})
		if at, _ := grip(tt); share != kept || at.Y+handle/2 > kept*height+1 {
			t.Fatalf("Git's list is %v high with share %v, kept %v", at.Y+handle/2, share, kept)
		}
	})
	// A stacked document is too narrow for two columns: the diff shows
	// unified whatever the window's choice, which stays, and the footer's
	// button is disabled and says why.
	t.Run("NarrowSplit", func(t *testing.T) {
		split, share := true, float32(0)
		s, _, tt := viewTester(t, 560, &split, &share)
		show(tt, s, native.PanelTab{Kind: Changes, ID: "README.md"})
		all := toggles(tt)
		if len(all) != 1 || !all[0].Disabled || all[0].Name != en("git_diff_side_by_side") || all[0].Description != en("git_diff_needs_width") {
			t.Fatalf("the narrow panel's button: %+v", all)
		}
		if n := dividers(tt); n != 0 || !split {
			t.Fatalf("the narrow panel divides %d diffs, the choice is %v", n, split)
		}
		b := all[0].Bounds
		tt.ClickAt(b.X+b.W/2, b.Y+b.H/2)
		tt.Frame()
		if !split {
			t.Fatal("the disabled button changed the window's choice")
		}
		tt.SetSize(900, height)
		tt.Frame()
		tt.Frame()
		tt.Frame()
		all = toggles(tt)
		if len(all) != 1 || all[0].Disabled || all[0].Name != en("git_diff_inline") || dividers(tt) != 1 {
			t.Fatalf("at 900 the button is %+v and %d diffs are divided", all, dividers(tt))
		}
	})
}

// closeItems closes every open file and diff of the project and shows none.
func closeItems(pr *repository) {
	pr.Files.Show(pr.Files.File())
	pr.Git.Show(pr.Git.Change())
	for _, id := range slices.Clone(pr.Files.Open()) {
		pr.Files.Close(id)
	}
	for _, id := range slices.Clone(pr.Git.Open()) {
		pr.Git.Close(id)
	}
}

// An empty state standing for a document is centred both ways in the
// document's region: beside the list, or under it when stacked, never the
// whole panel and never at the region's top.
func testEmptyCentred(t *testing.T) {
	const height = 772
	grip := func(tt *ui.Tester) ui.Rect {
		t.Helper()
		for _, e := range tt.AccessAll() {
			if e.Role == ui.RoleSplitter && e.Bounds.H == handle && e.Bounds.W > handle {
				return e.Bounds
			}
		}
		t.Fatalf("no handle under the list: %q", tt.Texts())
		return ui.Rect{}
	}
	split, share := false, float32(0)
	s, pr, tt := viewTester(t, 560, &split, &share)
	for _, o := range []struct {
		tab  native.PanelTab
		key  string
		side float32
	}{
		{native.PanelTab{Kind: Changes}, "git_diff_empty", pr.view.side},
		{native.PanelTab{Kind: Files}, "file_empty", float32(sideWidth)},
	} {
		closeItems(pr)
		tt.SetSize(560, height)
		s.Tabs, s.Active = []native.PanelTab{o.tab}, o.tab
		tt.Frame()
		tt.Frame()
		top := grip(tt).Y + handle/2
		fixtures.AssertCentered(t, tt, en(o.key), ui.Rect{Y: top, W: 560, H: height - top})
		tt.SetSize(900, height)
		tt.Frame()
		tt.Frame()
		fixtures.AssertCentered(t, tt, en(o.key), ui.Rect{W: 900 - o.side, H: height})
	}
	// The rail's pages head the document with their strip of open items.
	for _, o := range []struct {
		kind native.PanelKind
		key  string
	}{{Files, "file_empty"}, {Changes, "git_diff_empty"}} {
		pr, tt := pageTester(t, o.kind)
		closeItems(pr)
		tt.Frame()
		tt.Frame()
		side := float32(sideWidth)
		if o.kind == Changes {
			side = pr.view.side
		}
		fixtures.AssertCentered(t, tt, en(o.key), ui.Rect{Y: barHeight, W: 1100 - side, H: 760 - barHeight})
	}
}

// layoutShots writes the review's frames of the panel's two layouts, only
// when $SAILRY_SNAPSHOTS names a folder: Git stacked at the panel's first
// width with a diff, Git side by side at 900 with the diff split, and Files
// stacked with a file open, at 2x in both appearances.
func layoutShots(t *testing.T) {
	if os.Getenv("SAILRY_SNAPSHOTS") == "" {
		t.Skip("SAILRY_SNAPSHOTS is not set")
	}
	for _, mode := range []theme.Mode{theme.Light, theme.Dark} {
		name := map[theme.Mode]string{theme.Light: "light", theme.Dark: "dark"}[mode]
		pr := viewOf(fixtures.PreviewProject(true))
		s := native.NewPreview(native.Assemble(Register, files.Register)).Panel
		split := false
		tt := ui.NewTesterWith(func(c *ui.Context) {
			theme.Apply(c, "", mode)
			fixtures.BindWorkspaceLayout(c, &split, nil)
			ui.Column(c).Fill().Background(theme.Of(c).Background).Children(func() { fixtures.RenderWorkspacePanel(c, s, pr.Project) })
		}, 560, 772, ui.TesterOptions{Dark: mode == theme.Dark, Preferences: ui.Preferences{ReduceTransparency: true}})
		tt.SetLocale("en")
		tt.SetScale(2)
		rest := func() {
			tt.Frame()
			tt.Advance(2 * time.Second)
			tt.Frame()
		}
		s.Show(native.PanelTab{Kind: Changes, ID: "internal/booking/reserve.go"})
		rest()
		shot(t, tt, "git-stacked-560-"+name)
		pr.view.view = 1
		rest()
		shot(t, tt, "git-history-stacked-560-"+name)
		pr.view.view = 0
		s.Show(native.PanelTab{Kind: Files, ID: "harbor-api/internal/booking/reserve.go"})
		rest()
		shot(t, tt, "files-stacked-560-"+name)
		split = true
		tt.SetSize(900, 772)
		s.Show(native.PanelTab{Kind: Changes, ID: "internal/booking/reserve.go"})
		rest()
		rest()
		shot(t, tt, "git-side-900-split-"+name)
		// Nothing chosen: the empty state centred in the document's region.
		closeItems(pr)
		for _, o := range []struct {
			kind native.PanelKind
			name string
		}{{Changes, "git"}, {Files, "files"}} {
			s.Tabs, s.Active = []native.PanelTab{{Kind: o.kind}}, native.PanelTab{Kind: o.kind}
			tt.SetSize(900, 772)
			rest()
			rest()
			shot(t, tt, o.name+"-empty-side-900-"+name)
			tt.SetSize(560, 772)
			rest()
			rest()
			shot(t, tt, o.name+"-empty-stacked-560-"+name)
		}
	}
}

func TestGitLayout(t *testing.T) {
	t.Run("DiffFooter", testDiffFooter)
	t.Run("Panel", testLayout)
	t.Run("EmptyCentred", testEmptyCentred)
	t.Run("Snapshots", layoutShots)
}

const (
	handle       = 6
	footer       = 32
	ListShare    = 0.38
	stackBelow   = sideWidth + 500
	stackSlack   = 8
	filesLeast   = barHeight + 3*rowHeight
	listShareMin = 0.2
)
