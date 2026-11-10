package transit

import (
	"io/fs"
	"math"
	"sort"
	"strconv"
)

// LatLon is a position in degrees.
type LatLon struct {
	Lat, Lon float64
}

// BBox is a rectangle in degrees.
type BBox struct {
	West, South, East, North float64
}

func (b BBox) Contains(p LatLon) bool {
	return p.Lon >= b.West && p.Lon <= b.East && p.Lat >= b.South && p.Lat <= b.North
}

const metersPerDegree = 111_320.0

// Distance is the distance in meters between a and b, on a flat
// approximation that is good enough at city scale.
func Distance(a, b LatLon) float64 {
	k := math.Cos((a.Lat + b.Lat) / 2 * math.Pi / 180)
	return metersPerDegree * math.Hypot((b.Lon-a.Lon)*k, b.Lat-a.Lat)
}

// Path is a polyline, such as a GTFS shape, measured in meters from its
// start.
type Path struct {
	Points []LatLon
	dist   []float64 // from the start to each point
}

func NewPath(pts []LatLon) Path {
	p := Path{Points: pts, dist: make([]float64, len(pts))}
	for i := 1; i < len(pts); i++ {
		p.dist[i] = p.dist[i-1] + Distance(pts[i-1], pts[i])
	}
	return p
}

func (p Path) Length() float64 {
	if len(p.dist) == 0 {
		return 0
	}
	return p.dist[len(p.dist)-1]
}

// At is the point along meters from the start of p.
func (p Path) At(along float64) LatLon {
	if len(p.Points) == 0 {
		return LatLon{}
	}
	i := sort.SearchFloat64s(p.dist, along)
	if i == 0 {
		return p.Points[0]
	}
	if i == len(p.Points) {
		return p.Points[i-1]
	}
	a, b := p.Points[i-1], p.Points[i]
	t := (along - p.dist[i-1]) / (p.dist[i] - p.dist[i-1])
	return LatLon{a.Lat + t*(b.Lat-a.Lat), a.Lon + t*(b.Lon-a.Lon)}
}

// Locate finds the point on p closest to q. It returns how far along p that
// point is and how far it is from q, both in meters.
func (p Path) Locate(q LatLon) (along, off float64) {
	off = math.Inf(1)
	k := math.Cos(q.Lat * math.Pi / 180)
	for i := 1; i < len(p.Points); i++ {
		a, b := p.Points[i-1], p.Points[i]
		ax, ay := (a.Lon-q.Lon)*k, a.Lat-q.Lat
		bx, by := (b.Lon-q.Lon)*k, b.Lat-q.Lat
		dx, dy := bx-ax, by-ay
		t := 0.0
		if l := dx*dx + dy*dy; l > 0 {
			t = math.Max(0, math.Min(1, -(ax*dx+ay*dy)/l))
		}
		d := metersPerDegree * math.Hypot(ax+t*dx, ay+t*dy)
		if d < off {
			off = d
			along = p.dist[i-1] + t*(p.dist[i]-p.dist[i-1])
		}
	}
	return along, off
}

// StopsAlong returns the stops that are within meters of p, by how far
// along p they are.
func StopsAlong(p Path, stops map[string]Stop, within float64) map[string]float64 {
	out := map[string]float64{}
	if len(p.Points) == 0 {
		return out
	}
	// Only look closer at stops near the path's bounding box.
	b := BBox{p.Points[0].Lon, p.Points[0].Lat, p.Points[0].Lon, p.Points[0].Lat}
	for _, q := range p.Points {
		b = BBox{min(b.West, q.Lon), min(b.South, q.Lat), max(b.East, q.Lon), max(b.North, q.Lat)}
	}
	d := within / metersPerDegree
	b = BBox{b.West - 2*d, b.South - d, b.East + 2*d, b.North + d}
	for id, s := range stops {
		if !b.Contains(s.LatLon) {
			continue
		}
		if a, off := p.Locate(s.LatLon); off <= within {
			out[id] = a
		}
	}
	return out
}

// Within returns the stretch of p that runs through b around the point
// along meters from the start: from the last point outside b before it to
// the first point outside b after it. The second result is where the
// stretch starts on p, in meters.
func (p Path) Within(b BBox, along float64) (Path, float64) {
	if len(p.Points) < 2 {
		return p, 0
	}
	// The segment that along is on.
	i := max(1, min(sort.SearchFloat64s(p.dist, along), len(p.Points)-1))
	from, to := i-1, i
	for from > 0 && b.Contains(p.Points[from]) {
		from--
	}
	for to < len(p.Points)-1 && b.Contains(p.Points[to]) {
		to++
	}
	return NewPath(p.Points[from : to+1]), p.dist[from]
}

// Network merges paths that share track into their distinct segments
// within b, so that each piece of track is drawn once. Paths from the same
// GTFS feed use the same points where they overlap.
func Network(paths []Path, b BBox) [][2]LatLon {
	type key [2]LatLon
	seen := map[key]bool{}
	var segs [][2]LatLon
	for _, p := range paths {
		for i := 1; i < len(p.Points); i++ {
			a, c := p.Points[i-1], p.Points[i]
			if !b.Contains(a) && !b.Contains(c) {
				continue
			}
			if seen[key{a, c}] || seen[key{c, a}] {
				continue
			}
			seen[key{a, c}] = true
			segs = append(segs, [2]LatLon{a, c})
		}
	}
	return segs
}

// LoadShapes reads the shapes in shapes.txt of a static GTFS feed for which
// keep returns true, by shape id.
func LoadShapes(gtfs fs.FS, keep func(id string) bool) (map[string]Path, error) {
	type pt struct {
		seq int
		LatLon
	}
	pts := map[string][]pt{}
	err := readFile(gtfs, "shapes.txt", func(row Row) {
		id := row("shape_id")
		if !keep(id) {
			return
		}
		seq, _ := strconv.Atoi(row("shape_pt_sequence"))
		lat, _ := strconv.ParseFloat(row("shape_pt_lat"), 64)
		lon, _ := strconv.ParseFloat(row("shape_pt_lon"), 64)
		pts[id] = append(pts[id], pt{seq, LatLon{lat, lon}})
	})
	if err != nil {
		return nil, err
	}
	shapes := map[string]Path{}
	for id, ps := range pts {
		sort.Slice(ps, func(i, j int) bool { return ps[i].seq < ps[j].seq })
		ll := make([]LatLon, len(ps))
		for i, p := range ps {
			ll[i] = p.LatLon
		}
		shapes[id] = NewPath(ll)
	}
	return shapes, nil
}
