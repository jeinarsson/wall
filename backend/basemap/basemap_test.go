package basemap

import (
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	const osm = `{"elements": [
		{"type": "way", "tags": {"highway": "residential"},
		 "geometry": [{"lat": 0, "lon": 0}, {"lat": 0, "lon": 0.0001}, {"lat": 0, "lon": 0.0002}]},
		{"type": "way", "tags": {"highway": "footway"},
		 "geometry": [{"lat": 0, "lon": 0}, {"lat": 1, "lon": 1}]},
		{"type": "way", "tags": {"leisure": "park"},
		 "geometry": [{"lat": 0, "lon": 0}, {"lat": 1, "lon": 1}]},
		{"type": "relation", "tags": {"landuse": "cemetery"}, "members": [
			{"type": "way", "role": "outer", "geometry": [{"lat": 0, "lon": 0}, {"lat": 0, "lon": 1}, {"lat": 1, "lon": 1}]},
			{"type": "way", "role": "outer", "geometry": [{"lat": 0, "lon": 0}, {"lat": 1, "lon": 0}, {"lat": 1, "lon": 1}]}
		]}
	]}`
	layers, err := Load(strings.NewReader(osm), 2)
	if err != nil {
		t.Fatal(err)
	}
	// The straight street is simplified to its ends; the footway is left
	// out, as is the park mapped as a line.
	if got := layers["minor"]; len(got) != 1 || len(got[0]) != 1 || len(got[0][0]) != 4 {
		t.Errorf("minor roads = %v, want one line of two points", got)
	}
	// The cemetery's two halves are joined into one ring.
	if got := layers["green"]; len(got) != 1 || len(got[0]) != 1 || len(got[0][0]) != 10 {
		t.Errorf("green = %v, want one ring of five points", got)
	}
	if len(layers) != 2 {
		t.Errorf("got layers %v", layers)
	}
}
