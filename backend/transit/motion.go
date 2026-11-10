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

// SubwayModel plans the way of subway trains, which have no GPS. All the
// NYC subway reliably reports is when a train arrives at a stop; its
// predictions are revised by up to a minute at every update, and would
// move trains back and forth by hundreds of meters.
//
// So a train stands at the stop it last arrived at for Dwell, runs to its
// next stop at Speed (m/s, on average), and waits there until the feed
// says it got there. It is never drawn more than a stop ahead of where it
// is known to be, and never moves backwards.
type SubwayModel struct {
	Dwell time.Duration
	Speed float64
}

// Knots plans the way of t along its path. along places a stop on the path
// (false if it isn't on it), and stations are all the stations on the
// path, in meters along it.
//
// The train comes from last, the stop it last arrived at, if known (see
// Arrivals); else from the last stop the feed lists as left. At the start
// of its path, where trains wait for their time, it leaves in time for its
// predicted arrival at the next stop. With nothing known it comes from the
// station before its next stop, in time for its predicted arrival there.
func (m SubwayModel) Knots(t LiveTrip, last *Arrival, along func(stopID string) (float64, bool), stations []float64, now time.Time) []Knot {
	type stop struct {
		Knot
		left bool // the time is past
	}
	var on []stop
	for _, s := range t.Stops {
		if a, ok := along(s.StopID); ok {
			on = append(on, stop{Knot{s.Time, a}, !s.Time.After(now)})
		}
	}

	var from *Knot
	if last != nil {
		if a, ok := along(last.StopID); ok {
			from = &Knot{last.Time, a}
		}
	} else {
		// After a known arrival, a stop listed as left may just be late.
		for _, s := range on {
			if s.left {
				from = &s.Knot
			}
		}
	}
	var next *Knot
	for _, s := range on {
		if (from != nil && s.Along > from.Along) || (from == nil && !s.left) {
			next = &s.Knot
			break
		}
	}
	run := func(a, b float64) time.Duration { return seconds((b - a) / m.Speed) }

	switch {
	case next == nil && from == nil:
		return nil
	case next == nil:
		return []Knot{*from}
	case from == nil:
		prev := -1.0
		for _, a := range stations {
			if a < next.Along && a > prev {
				prev = a
			}
		}
		if prev < 0 {
			return []Knot{*next}
		}
		return []Knot{{next.Time.Add(-run(prev, next.Along)), prev}, *next}
	}
	leave := from.Time.Add(m.Dwell)
	first := true
	for _, a := range stations {
		first = first && a >= from.Along
	}
	if l := next.Time.Add(-run(from.Along, next.Along)); first && l.After(leave) {
		leave = l
	}
	return []Knot{*from, {leave, from.Along}, {leave.Add(run(from.Along, next.Along)), next.Along}}
}

// Arrival is when a train arrived at a stop.
type Arrival struct {
	StopID string
	Time   time.Time
}

// Arrivals remembers when each train got to the stop it is at. The NYC
// subway feed only says which stop a train is at (or last passed), with
// the time of its latest report; the report in which the stop changes is
// the arrival. Update it with every new version of the feed.
type Arrivals struct {
	mu    sync.Mutex
	trips map[string]arrival // by trip id
}

type arrival struct {
	Arrival
	seen time.Time // last in the feed
}

// arrivalsKept is how long trains are remembered after they are last seen.
const arrivalsKept = 10 * time.Minute

// Update notes the trains in trips that have reached a new stop, and
// forgets those that have been gone from the feed for a while.
func (a *Arrivals) Update(trips []LiveTrip, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.trips == nil {
		a.trips = map[string]arrival{}
	}
	for _, t := range trips {
		if t.StoppedAt == "" {
			continue
		}
		arr, ok := a.trips[t.TripID]
		if !ok || arr.StopID != t.StoppedAt {
			arr.Arrival = Arrival{t.StoppedAt, t.Reported}
			// The feed still lists the stop for a while, with its time.
			for _, s := range t.Stops {
				if s.StopID == t.StoppedAt && s.Time.Before(arr.Time) {
					arr.Time = s.Time
				}
			}
		}
		arr.seen = now
		a.trips[t.TripID] = arr
	}
	for id, arr := range a.trips {
		if now.Sub(arr.seen) > arrivalsKept {
			delete(a.trips, id)
		}
	}
}

// Get returns where and when the trip last arrived, if known.
func (a *Arrivals) Get(tripID string) (Arrival, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	arr, ok := a.trips[tripID]
	return arr.Arrival, ok
}

func seconds(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}
