package transit

import (
	"sync"
	"time"
)

// Knot is where a train is at a time, in meters along its path. A train
// moves from knot to knot; before the first and after the last it stands
// still.
type Knot struct {
	Time  time.Time
	Along float64
}

// PositionAt is where a train following knots is at t. With ease it speeds
// up after each knot and slows down into the next, like a train between
// stations; otherwise it goes steadily. It returns false without knots.
func PositionAt(knots []Knot, ease bool, t time.Time) (float64, bool) {
	if len(knots) == 0 {
		return 0, false
	}
	for i := 1; i < len(knots); i++ {
		a, b := knots[i-1], knots[i]
		if !t.Before(b.Time) {
			continue
		}
		if t.Before(a.Time) {
			return a.Along, true
		}
		f := float64(t.Sub(a.Time)) / float64(b.Time.Sub(a.Time))
		if ease {
			f = f * f * (3 - 2*f)
		}
		return a.Along + (b.Along-a.Along)*f, true
	}
	return knots[len(knots)-1].Along, true
}

// DeadReckon plans the way of a train from a GPS fix: at the fix's time it
// is along meters along its path, and it goes on at speed (m/s) until it
// reaches until, where it stops next, for at most ahead. A train whose
// fixes stop coming stops, rather than running on unchecked.
func DeadReckon(fix time.Time, along, speed, until float64, ahead time.Duration) []Knot {
	knots := []Knot{{fix, along}}
	if speed <= 0 || until <= along {
		return knots
	}
	d := min(until-along, speed*ahead.Seconds())
	return append(knots, Knot{fix.Add(seconds(d / speed)), along + d})
}

// SubwayModel plans the way of subway trains, which have no GPS. What the
// NYC subway reliably reports is which stop a train is at: while it
// stands there it is reported every few seconds, and once it moves the
// reports stop until it gets to the next stop. Its predictions are revised
// by up to a minute at every update, and would move trains back and forth
// by hundreds of meters.
//
// So a train stands at the stop it is at until it is no longer reported
// there (see SubwayTracker), then runs to its next stop in the time that
// was predicted when it left, at between MinSpeed and MaxSpeed (m/s) on
// average, and waits there until the feed says it got there. It is never
// drawn more than a stop ahead of where it is known to be, and never moves
// backwards.
type SubwayModel struct {
	MinSpeed, MaxSpeed float64
	// Lost is how long after its last report, or after it was due at its
	// next stop, a train is given up on: the feed has lost track of it.
	Lost time.Duration
}

// Knots plans the way of a train along its path from v, where it was last
// reported. along places a stop on the path (false if it isn't on it).
// It returns nil if the train can't be placed or is lost.
func (m SubwayModel) Knots(v Visit, along func(stopID string) (float64, bool), now time.Time) []Knot {
	from, ok := along(v.StopID)
	if !ok {
		return nil
	}
	if v.Left.IsZero() {
		if now.Sub(v.Reported) > m.Lost {
			return nil
		}
		return []Knot{{v.Arrived, from}}
	}
	for _, s := range v.Ahead {
		to, ok := along(s.StopID)
		if !ok || to <= from {
			continue
		}
		d := to - from
		run := min(max(s.Time.Sub(v.Left), seconds(d/m.MaxSpeed)), seconds(d/m.MinSpeed))
		if now.Sub(v.Left.Add(run)) > m.Lost {
			return nil
		}
		return []Knot{{v.Left, from}, {v.Left.Add(run), to}}
	}
	if now.Sub(v.Left) > m.Lost {
		return nil
	}
	return []Knot{{v.Left, from}}
}

// Visit is a train's stay at a stop, from when it got there until it
// left, as far as the feed tells.
type Visit struct {
	StopID   string
	Arrived  time.Time
	Reported time.Time // when it was last reported there
	// Left is when it left, or zero while it is there. It is the time of
	// its last report there, which is noticed only a while later.
	Left time.Time
	// Ahead are the stops it was to go to when it left, with the times
	// it was predicted at them then.
	Ahead []LiveStop
}

// SubwayTracker follows subway trains from stop to stop. The NYC subway
// feed says which stop a train is at (or last passed), with the time of
// its latest report. The report in which the stop changes is the arrival;
// a train has left when it is no longer reported there: its stop drops
// off its trip some 30-45 s after the last report, or the reports go
// stale. Update it with every version of the feeds.
type SubwayTracker struct {
	mu    sync.Mutex
	trips map[string]visit // by trip id
}

type visit struct {
	Visit
	seen time.Time // last in the feed
}

const (
	// trackerKept is how long trains are remembered after they are last
	// seen.
	trackerKept = 10 * time.Minute
	// subwayStale is how long after its last report a train that the
	// feed still lists at its stop has left anyway. Standing trains are
	// reported at least every ~35 s.
	subwayStale = 60 * time.Second
)

// Update notes where the trains in trips, a feed of time feedTime, are,
// and forgets those that have been gone from the feeds for a while. It
// can be called with the same feed again.
func (tr *SubwayTracker) Update(trips []LiveTrip, feedTime time.Time) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.trips == nil {
		tr.trips = map[string]visit{}
	}
	for _, t := range trips {
		// Trips are listed before they start, reported in the future.
		if t.StoppedAt == "" || t.Reported.IsZero() || t.Reported.After(feedTime) {
			continue
		}
		v, ok := tr.trips[t.TripID]
		listed := -1
		for i, s := range t.Stops {
			if s.StopID == t.StoppedAt {
				listed = i
			}
		}
		switch {
		case !ok || v.StopID != t.StoppedAt:
			v.Visit = Visit{StopID: t.StoppedAt, Arrived: t.Reported, Reported: t.Reported}
			// The feed still lists the stop for a while, with the time
			// it got there if that was before the report.
			if listed >= 0 && t.Stops[listed].Time.Before(v.Arrived) {
				v.Arrived = t.Stops[listed].Time
			}
		case v.Left.IsZero() && t.Reported.After(v.Reported):
			v.Reported = t.Reported
		}
		if v.Left.IsZero() && (listed < 0 || feedTime.Sub(v.Reported) > subwayStale) {
			v.Left = v.Reported
			v.Ahead = nil
			for _, s := range t.Stops[listed+1:] {
				if s.Time.After(v.Left) {
					v.Ahead = append(v.Ahead, s)
				}
			}
		}
		if feedTime.After(v.seen) {
			v.seen = feedTime
		}
		tr.trips[t.TripID] = v
	}
	for id, v := range tr.trips {
		if feedTime.Sub(v.seen) > trackerKept {
			delete(tr.trips, id)
		}
	}
}

// Get returns the latest visit of a trip, if known.
func (tr *SubwayTracker) Get(tripID string) (Visit, bool) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	v, ok := tr.trips[tripID]
	return v.Visit, ok
}

func seconds(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}
