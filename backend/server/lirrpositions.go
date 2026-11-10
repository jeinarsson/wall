package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jeinarsson/wall/transit"
)

// lirrPositions keeps track of where the LIRR trains are: all of them from
// list, which is refreshed with the other live feeds, and the few that
// watch picks one by one and more often, as their GPS fixes go stale in
// seconds. Asking for a train is much less than for the whole list.
type lirrPositions struct {
	list  *transit.LocationFeed
	url   func(trainID string) string
	watch func(transit.TrainLocation) bool

	mu      sync.Mutex
	watched map[string]*transit.TrainLocationFeed // by train id
}

// Poll refreshes the watched trains every interval until ctx is done.
func (p *lirrPositions) Poll(ctx context.Context, interval time.Duration) {
	for {
		p.refresh(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func (p *lirrPositions) refresh(ctx context.Context) {
	trains, _ := p.list.Get(time.Now())
	p.mu.Lock()
	if p.watched == nil {
		p.watched = map[string]*transit.TrainLocationFeed{}
	}
	// Judge the trains by where they were last seen.
	watched := map[string]*transit.TrainLocationFeed{}
	for _, t := range trains {
		f, ok := p.watched[t.TrainID]
		if ok {
			if w, ok := f.Get(time.Now()); ok && w.Time.After(t.Time) {
				t = w
			}
		} else {
			f = transit.NewTrainLocationFeed(p.url(t.TrainID))
		}
		if p.watch(t) {
			watched[t.TrainID] = f
		}
	}
	p.watched = watched
	p.mu.Unlock()

	for id, f := range watched {
		if err := f.Fetch(ctx); err != nil {
			log.Printf("fetching LIRR train %s: %v", id, err)
		}
	}
}

// Get returns the trains in the list, with the latest positions of the
// watched ones.
func (p *lirrPositions) Get(now time.Time) []transit.TrainLocation {
	trains, _ := p.list.Get(now)
	out := make([]transit.TrainLocation, len(trains))
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, t := range trains {
		out[i] = t
		if f, ok := p.watched[t.TrainID]; ok {
			if w, ok := f.Get(now); ok && w.Time.After(t.Time) {
				out[i] = w
			}
		}
	}
	return out
}
