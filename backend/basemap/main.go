// Command basemap makes the background map for the train map: roads, parks
// and water around the station, from OpenStreetMap via the Overpass API.
//
// The map never moves, so this runs once (or whenever OSM has improved) and
// its output is committed:
//
//	go run ./basemap > ../frontend/public/basemap.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// classes sorts OSM features into the map's layers, which the frontend
// draws back to front in this order. The first matching tag wins.
var classes = []struct {
	layer string
	key   string
	vals  []string
}{
	{"water", "natural", []string{"water"}},
	{"water", "waterway", []string{"riverbank"}},
	{"green", "leisure", []string{"park", "garden", "nature_reserve", "golf_course", "playground", "pitch"}},
	{"green", "landuse", []string{"cemetery", "grass", "forest", "recreation_ground"}},
	{"green", "natural", []string{"wood"}},
	{"minor", "highway", []string{"residential", "unclassified", "living_street", "pedestrian"}},
	{"medium", "highway", []string{"secondary", "tertiary", "secondary_link", "tertiary_link"}},
	{"major", "highway", []string{"motorway", "trunk", "primary", "motorway_link", "trunk_link", "primary_link"}},
}

var areaLayers = map[string]bool{"water": true, "green": true}

const query = `[out:json][timeout:180][bbox:%[1]s];
(
  way[highway~"^(motorway|trunk|primary|secondary|tertiary|unclassified|residential|motorway_link|trunk_link|primary_link|secondary_link|tertiary_link|living_street|pedestrian)$"];
  way[leisure~"^(park|garden|nature_reserve|golf_course|playground|pitch)$"];
  relation[leisure~"^(park|garden|nature_reserve|golf_course)$"];
  way[landuse~"^(cemetery|grass|forest|recreation_ground)$"];
  relation[landuse~"^(cemetery|grass|forest|recreation_ground)$"];
  way[natural~"^(water|wood)$"];
  relation[natural~"^(water|wood)$"];
  way[waterway=riverbank];
);
out geom qt;`

type point struct{ Lat, Lon float64 }

type element struct {
	Type     string            `json:"type"`
	Tags     map[string]string `json:"tags"`
	Geometry []point           `json:"geometry"`
	Members  []struct {
		Type     string  `json:"type"`
		Role     string  `json:"role"`
		Geometry []point `json:"geometry"`
	} `json:"members"`
}

// basemap is the output: per layer, a list of features, each a list of
// lines (roads) or rings (areas, drawn with the even-odd rule), each a
// flat list of lon, lat, lon, lat, ...
type basemap struct {
	BBox   [4]float64 // west, south, east, north
	Layers map[string][][][]float64
}

func main() {
	bbox := flag.String("bbox", "40.672,-73.905,40.748,-73.735", "south,west,north,east")
	in := flag.String("in", "", "read Overpass JSON from this file instead of querying")
	server := flag.String("overpass", "https://overpass-api.de/api/interpreter", "Overpass API endpoint")
	tolerance := flag.Float64("tolerance", 2, "simplification tolerance in meters")
	flag.Parse()

	var s, w, n, e float64
	if _, err := fmt.Sscanf(*bbox, "%f,%f,%f,%f", &s, &w, &n, &e); err != nil {
		log.Fatalf("bad -bbox: %v", err)
	}

	var r io.Reader
	if *in != "" {
		f, err := os.Open(*in)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		r = f
	} else {
		body := url.Values{"data": {fmt.Sprintf(query, *bbox)}}.Encode()
		req, err := http.NewRequest("POST", *server, strings.NewReader(body))
		if err != nil {
			log.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", "wall-basemap (github.com/jeinarsson/wall)")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			log.Fatalf("overpass: %s", resp.Status)
		}
		r = resp.Body
	}

	var osm struct{ Elements []element }
	if err := json.NewDecoder(r).Decode(&osm); err != nil {
		log.Fatal(err)
	}

	out := basemap{BBox: [4]float64{w, s, e, n}, Layers: map[string][][][]float64{}}
	for _, el := range osm.Elements {
		layer := classify(el.Tags)
		if layer == "" {
			continue
		}
		var parts [][]point
		switch {
		case el.Type == "way":
			parts = [][]point{el.Geometry}
		case el.Type == "relation" && areaLayers[layer]:
			var ways [][]point
			for _, m := range el.Members {
				if m.Type == "way" && (m.Role == "outer" || m.Role == "inner") {
					ways = append(ways, m.Geometry)
				}
			}
			parts = rings(ways)
		}
		if areaLayers[layer] {
			parts = closed(parts)
		}
		var feature [][]float64
		for _, p := range parts {
			p = simplify(p, *tolerance)
			if len(p) < 2 {
				continue
			}
			flat := make([]float64, 0, 2*len(p))
			for _, pt := range p {
				flat = append(flat, round(pt.Lon), round(pt.Lat))
			}
			feature = append(feature, flat)
		}
		if feature != nil {
			out.Layers[layer] = append(out.Layers[layer], feature)
		}
	}

	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		log.Fatal(err)
	}
}

func classify(tags map[string]string) string {
	for _, c := range classes {
		for _, v := range c.vals {
			if tags[c.key] == v {
				return c.layer
			}
		}
	}
	return ""
}

// rings joins the member ways of a multipolygon end to end into rings.
func rings(ways [][]point) [][]point {
	var out [][]point
	used := make([]bool, len(ways))
	for i, w := range ways {
		if used[i] || len(w) == 0 {
			continue
		}
		used[i] = true
		ring := append([]point{}, w...)
		for ring[0] != ring[len(ring)-1] {
			found := false
			for j, v := range ways {
				if used[j] || len(v) == 0 {
					continue
				}
				end := ring[len(ring)-1]
				switch end {
				case v[0]:
					ring = append(ring, v[1:]...)
				case v[len(v)-1]:
					for k := len(v) - 2; k >= 0; k-- {
						ring = append(ring, v[k])
					}
				default:
					continue
				}
				used[j], found = true, true
				break
			}
			if !found {
				break // unclosed; closed() takes care of it
			}
		}
		out = append(out, ring)
	}
	return out
}

// closed drops parts that aren't areas, such as a park mapped as a line.
func closed(parts [][]point) [][]point {
	var out [][]point
	for _, p := range parts {
		if len(p) >= 4 && p[0] == p[len(p)-1] {
			out = append(out, p)
		}
	}
	return out
}

// simplify is Douglas-Peucker with a tolerance in meters.
func simplify(pts []point, tolerance float64) []point {
	if len(pts) < 3 {
		return pts
	}
	keep := make([]bool, len(pts))
	keep[0], keep[len(pts)-1] = true, true
	var rec func(a, b int)
	rec = func(a, b int) {
		far, idx := 0.0, -1
		for i := a + 1; i < b; i++ {
			if d := segmentDistance(pts[i], pts[a], pts[b]); d > far {
				far, idx = d, i
			}
		}
		if far > tolerance {
			keep[idx] = true
			rec(a, idx)
			rec(idx, b)
		}
	}
	rec(0, len(pts)-1)
	var out []point
	for i, p := range pts {
		if keep[i] {
			out = append(out, p)
		}
	}
	return out
}

// segmentDistance is the distance in meters from p to the segment ab, on a
// local flat approximation that is fine at city scale.
func segmentDistance(p, a, b point) float64 {
	const m = 111_320.0
	k := math.Cos(a.Lat * math.Pi / 180)
	px, py := (p.Lon-a.Lon)*k*m, (p.Lat-a.Lat)*m
	bx, by := (b.Lon-a.Lon)*k*m, (b.Lat-a.Lat)*m
	t := 0.0
	if l := bx*bx + by*by; l > 0 {
		t = math.Max(0, math.Min(1, (px*bx+py*by)/l))
	}
	return math.Hypot(px-t*bx, py-t*by)
}

// round keeps five decimals, about a meter.
func round(x float64) float64 {
	return math.Round(x*1e5) / 1e5
}
