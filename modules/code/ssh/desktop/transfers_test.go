package desktop

import (
	"path/filepath"
	"testing"

	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func TestLateTransferCompletionRetainsRetry(t *testing.T) {
	store := newTransferStore()
	var worker func()
	access := sshTransferAccess{host: transferHost{context: t.Context(), async: func(run func()) bool {
		worker = run
		return true
	}}}
	missing := filepath.Join(t.TempDir(), "missing")
	start := func() (protocol.RequestID, func()) {
		id := store.insert(access, true, missing, false, "/remote")
		if err := store.action(id.String(), "start", func(protocol.SshProfile, bool) bool { return true }); err != nil {
			t.Fatal(err)
		}
		return id, worker
	}
	slow, finish := start()
	oldest, finishFirst := start()
	finishFirst()
	for range maxFinishedSSHTransfers - 1 {
		_, run := start()
		run()
	}
	finish()
	if len(store.jobs) != maxFinishedSSHTransfers || store.jobs[oldest] != nil {
		t.Fatal("the oldest completion was not evicted")
	}
	job := store.jobs[slow]
	if job == nil || job.snapshot(slow)["can_start"] != true {
		t.Fatal("the latest failure disappeared before it could be retried")
	}
}
