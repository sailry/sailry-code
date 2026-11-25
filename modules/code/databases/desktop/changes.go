package desktop

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
)

// The window retains the SQL and connection drafts. Only the visible page
// observes Node changes; reentering asks the script to refresh its catalogs.
type visibility struct {
	mu      sync.Mutex
	changed chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	entry   uint64
}

func (v *visibility) set(parent context.Context, shown bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if shown == (v.ctx != nil) {
		return
	}
	if shown {
		v.ctx, v.cancel = context.WithCancel(parent)
		v.entry++
	} else {
		v.cancel()
		v.ctx, v.cancel = nil, nil
	}
	close(v.changed)
	v.changed = make(chan struct{})
}

func (v *visibility) current() (context.Context, string, <-chan struct{}) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.ctx, strconv.FormatUint(v.entry, 10), v.changed
}

func (p *window) nextChange(ctx context.Context, args []json.RawMessage) (json.RawMessage, error) {
	seen, err := argument[string](args, 0)
	if err != nil {
		return nil, err
	}
	if p.services.Next == nil {
		return nil, fault("change observation is unavailable")
	}
	priorEntry, cursor, _ := strings.Cut(seen, "/")
	for ctx.Err() == nil {
		active, entry, changed := p.entries.current()
		if active == nil {
			select {
			case <-ctx.Done():
			case <-changed:
			}
			continue
		}
		if entry != priorEntry {
			cursor = ""
		}
		read, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(active, cancel)
		value, err := p.services.Next(read, cursor)
		stop()
		cancel()
		if active.Err() != nil {
			continue
		}
		if err != nil {
			return nil, err
		}
		return json.Marshal(struct {
			Cursor    string `json:"cursor"`
			Entry     string `json:"entry"`
			Connected bool   `json:"connected"`
		}{entry + "/" + value.Cursor, entry, value.Connected})
	}
	return nil, ctx.Err()
}
