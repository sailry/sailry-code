package desktop

import (
	"context"
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func TestRefreshKeepsLoadedSelectionAfterEarlierInsertion(t *testing.T) {
	const count = 2500
	listing := make([]protocol.FileEntry, count+1)
	state := &directoryPage{}
	for i := range listing {
		name := fmt.Sprintf("%04d.txt", i)
		listing[i] = protocol.FileEntry{Name: name, Kind: protocol.EntryKindFile}
		if i > 0 {
			state.entries = append(state.entries, treeEntry{path: name, name: name})
		}
	}
	selected := listing[count].Name
	tree := fileTree{
		ctx: t.Context(), pages: map[string]*directoryPage{"": state},
		sel: selection{current: selected, selected: map[string]bool{selected: true}},
	}
	var offset int
	tree.host = native.FileDirectoryHost{Read: func(_ context.Context, _ string, after *protocol.DirectoryCursor, done func(*ui.Context, protocol.Directory, error)) {
		if offset != 0 && (after == nil || after.Name != listing[offset-1].Name) {
			t.Fatalf("unexpected cursor at offset %d: %+v", offset, after)
		}
		end := min(offset+protocol.MAX_DIRECTORY_ENTRIES, len(listing))
		page := protocol.Directory{Entries: listing[offset:end]}
		if end < len(listing) {
			page.Next = &protocol.DirectoryCursor{Name: listing[end-1].Name, Revision: "updated"}
		}
		offset = end
		done(nil, page, nil)
	}}

	tree.refresh("")
	if offset != len(listing) || tree.sel.current != selected || !tree.sel.selected[selected] {
		t.Fatalf("refresh stopped at %d entries and left selection %+v", offset, tree.sel)
	}
}
