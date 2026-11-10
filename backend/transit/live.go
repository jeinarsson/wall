// Package transit reads GTFS and GTFS-Realtime data from the MTA.
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

// maxAge is how long fetched realtime data is trusted when new fetches
// fail. Older data is dropped rather than shown as if current.
const maxAge = 10 * time.Minute

// Live is a realtime feed that is refreshed in the background by Poll.
type Live[T any] struct {
	url    string
	parse  func([]byte) (T, error)
	header http.Header // added to requests

	mu      sync.Mutex
	data    T
	fetched time.Time
}

// TripFeed is a GTFS-Realtime feed of trip updates.
type TripFeed = Live[*gtfs.FeedMessage]

// AlertFeed is an MTA service alert feed, in its JSON flavor which
// includes the MTA's alert types.
type AlertFeed = Live[[]Alert]

func NewTripFeed(url string) *TripFeed {
	return &TripFeed{url: url, parse: parseTrips}
}

func NewAlertFeed(url string) *AlertFeed {
	return &AlertFeed{url: url, parse: parseAlerts}
}

func parseTrips(b []byte) (*gtfs.FeedMessage, error) {
	msg := &gtfs.FeedMessage{}
	if err := proto.Unmarshal(b, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// Poll fetches the feed now and then every interval until ctx is done.
func (f *Live[T]) Poll(ctx context.Context, interval time.Duration) {
	for {
		if err := f.Fetch(ctx); err != nil {
			log.Printf("fetching %s: %v", f.url, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// Fetch fetches the feed now.
func (f *Live[T]) Fetch(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", f.url, nil)
	if err != nil {
		return err
	}
	for k, v := range f.header {
		req.Header[k] = v
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
	data, err := f.parse(b)
	if err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.data = data
	f.fetched = time.Now()
	return nil
}

// Get returns the latest data, or false if it is missing or stale.
func (f *Live[T]) Get(now time.Time) (T, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fetched.IsZero() || now.Sub(f.fetched) > maxAge {
		var zero T
		return zero, false
	}
	return f.data, true
}
