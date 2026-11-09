package transit

import (
	"math"
	"testing"
)

// A straight track east along the equator, 1 km between points.
var track = NewPath([]LatLon{{0, 0}, {0, 1000 / metersPerDegree}, {0, 2000 / metersPerDegree}, {0, 3000 / metersPerDegree}})

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestPathLocate(t *testing.T) {
	along, off := track.Locate(LatLon{100 / metersPerDegree, 1500 / metersPerDegree})
	if !near(along, 1500) || !near(off, 100) {
		t.Errorf("Locate = %.2f, %.2f, want 1500, 100", along, off)
	}
	if p := track.At(2500); !near(p.Lon*metersPerDegree, 2500) || p.Lat != 0 {
		t.Errorf("At(2500) = %v", p)
	}
	if p := track.At(5000); p != track.Points[3] {
		t.Errorf("At past the end = %v, want the end", p)
	}
}

func TestPathWithin(t *testing.T) {
	b := BBox{West: 500 / metersPerDegree, South: -1, East: 1500 / metersPerDegree, North: 1}
	piece, start := track.Within(b, 1200)
	// The point inside, and one on either side.
	if len(piece.Points) != 3 || start != 0 || !near(piece.Length(), 2000) {
		t.Errorf("Within = %d points from %.0f, %.0f long; want 3 from 0, 2000 long", len(piece.Points), start, piece.Length())
	}
}

func TestNetwork(t *testing.T) {
	other := NewPath([]LatLon{track.Points[2], track.Points[1], {1, 1}})
	segs := Network([]Path{track, other}, BBox{-1, -1, 1, 1})
	if len(segs) != 4 {
		t.Errorf("got %d segments, want 4 (3 of track, 1 of other)", len(segs))
	}
}
