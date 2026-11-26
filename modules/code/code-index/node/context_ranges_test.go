package codeindex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sailry/sailry-harness/sdk/native/files"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

type snippetRangeHost struct {
	native.Host
	files    *files.Files
	revision string
	reads    int
}

func (h *snippetRangeHost) RunWorkspaceOperation(ctx context.Context, _ *native.AgentInvocation, op protocol.ToolOperation, raw json.RawMessage) (protocol.Output, error) {
	switch op {
	case protocol.ToolOperationSearchCode:
		var symbols []protocol.CodeIndexSymbol
		for _, n := range []uint32{2, 10} {
			symbols = append(symbols, protocol.CodeIndexSymbol{Path: "source", Name: "symbol", FileRevision: h.revision, Range: protocol.CodeIndexRange{StartLine: n, EndLine: n}})
		}
		return protocol.Output{Data: protocol.CodeIndexSearchPage{Symbols: symbols}}, nil
	case protocol.ToolOperationGraphCode:
		return protocol.Output{Data: protocol.CodeIndexGraphGraphPage{}}, nil
	case protocol.ToolOperationReadFileLines:
		h.reads++
		var args struct {
			Path             string
			Offset, Limit    uint64
			ExpectedRevision *string `json:"expected_revision"`
			Ranges           []protocol.FileLineRange
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return protocol.Output{}, err
		}
		page, err := h.files.ReadLineRanges(ctx, args.Path, args.Offset, args.Limit, args.ExpectedRevision, args.Ranges)
		return protocol.Output{Data: page}, err
	default:
		return protocol.Output{}, fmt.Errorf("unexpected operation %s", op)
	}
}

func TestCodeContextGroupsDisjointSnippets(t *testing.T) {
	for _, tc := range []struct {
		name      string
		longLine  bool
		wantReads int
	}{
		{"one scan", false, 1},
		{"second snippet exceeds the raw page", true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			source := strings.Repeat("x\n", 8) + "second\n" + strings.Repeat("y\n", 8)
			if tc.longLine {
				source = strings.Replace(source, "second", strings.Repeat("s", 128*1024), 1)
			}
			if err := os.WriteFile(filepath.Join(dir, "source"), []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			fs, err := files.OpenFiles(dir, files.RestrictedFiles)
			if err != nil {
				t.Fatal(err)
			}
			defer fs.Close()
			initial, err := fs.ReadLines(t.Context(), "source", 0, 1, nil)
			if err != nil {
				t.Fatal(err)
			}
			host := &snippetRangeHost{files: fs, revision: initial.Revision}
			result := runCodeContext(t.Context(), host, new(native.AgentInvocation), json.RawMessage(`{}`))
			data, ok := result.Value.(map[string]any)
			if !ok || host.reads != tc.wantReads {
				t.Fatalf("source reads = %d, want %d, result = %T", host.reads, tc.wantReads, result.Value)
			}
			snippets := data["snippets"].([]map[string]any)
			if len(snippets) != 2 || snippets[0]["text"] != strings.Repeat("x\n", 5) || snippets[0]["start_line"] != uint64(1) || snippets[1]["start_line"] != uint64(9) {
				t.Fatalf("unexpected snippets (%d)", len(snippets))
			}
			if tc.longLine {
				if text := snippets[1]["text"].(string); !strings.HasPrefix(text, "ssss") || snippets[1]["truncated"] != true || jsonSize(data) > maxCodeBytes {
					t.Fatal("oversized snippet lost its bounded prefix")
				}
			} else if snippets[1]["text"] != "second\n"+strings.Repeat("y\n", 4) || snippets[1]["truncated"] != false {
				t.Fatalf("second snippet = %+v", snippets[1])
			}
		})
	}
}
