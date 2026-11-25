package desktop

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/sailry/sailry-harness/sdk/desktop/view"
)

func TestDatabaseWindowObservationReleasesHiddenReads(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	started, stopped := make(chan string, 1), make(chan struct{}, 1)
	p := &window{ctx: ctx, entries: visibility{changed: make(chan struct{})}, services: view.Services{
		Next: func(ctx context.Context, prior string) (view.Change, error) {
			if prior == "" {
				return view.Change{Cursor: "same-node-revision", Connected: true}, nil
			}
			started <- prior
			<-ctx.Done()
			stopped <- struct{}{}
			return view.Change{}, ctx.Err()
		},
	}}
	p.entries.set(ctx, true)
	value, err := p.nextChange(ctx, []json.RawMessage{json.RawMessage(`""`)})
	if err != nil {
		t.Fatal(err)
	}
	for entry := 1; entry < 4; entry++ {
		var change struct{ Cursor, Entry string }
		if json.Unmarshal(value, &change) != nil || change.Entry != strconv.Itoa(entry) {
			t.Fatalf("unexpected reentry: %s", value)
		}
		seen, _ := json.Marshal(change.Cursor)
		type result struct {
			value json.RawMessage
			err   error
		}
		reply := make(chan result, 1)
		go func() {
			value, err := p.nextChange(ctx, []json.RawMessage{seen})
			reply <- result{value, err}
		}()
		select {
		case prior := <-started:
			if prior != "same-node-revision" {
				t.Fatalf("workspace cursor leaked into Node subscription: %s", prior)
			}
		case <-ctx.Done():
			t.Fatal("visible workspace did not observe the Node")
		}
		p.entries.set(ctx, false)
		select {
		case <-stopped:
		case <-ctx.Done():
			t.Fatal("leaving the workspace did not release the current Node subscription")
		}
		select {
		case next := <-reply:
			t.Fatalf("observation did not wait for reentry: %+v", next)
		default:
		}
		p.entries.set(ctx, true)
		select {
		case next := <-reply:
			if next.err != nil {
				t.Fatal(next.err)
			}
			value = next.value
		case <-ctx.Done():
			t.Fatal("reentering did not publish a new entry for the same Node revision")
		}
	}
}
