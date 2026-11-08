// Package transit reads GTFS-Realtime feeds and lists upcoming departures
// from a stop.
package transit

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"
)

// maxAge is how long a fetched feed is trusted when new fetches fail.
// Older predictions are dropped rather than shown as if current.
const maxAge = 10 * time.Minute

// Feed is a GTFS-Realtime feed that is refreshed in the background by Poll.
type Feed struct {
	url    string
	static *StaticFeed

	mu      sync.Mutex
	msg     *gtfs.FeedMessage
	fetched time.Time
}

// NewFeed returns a feed for url, whose stop and route ids are described
// by static.
func NewFeed(url string, static *StaticFeed) *Feed {
	return &Feed{url: url, static: static}
}

// Poll fetches the feed now and then every interval until ctx is done.
func (f *Feed) Poll(ctx context.Context, interval time.Duration) {
	for {
		if err := f.fetch(ctx); err != nil {
			log.Printf("fetching %s: %v", f.url, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func (f *Feed) fetch(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", f.url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %s", resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	msg := &gtfs.FeedMessage{}
	if err := proto.Unmarshal(b, msg); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.msg = msg
	f.fetched = time.Now()
	return nil
}

// current returns the latest feed message, or nil if it is missing or stale.
func (f *Feed) current(now time.Time) *gtfs.FeedMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.msg == nil || now.Sub(f.fetched) > maxAge {
		return nil
	}
	return f.msg
}
