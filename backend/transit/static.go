package transit

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
)

// Route is a row from a GTFS routes.txt.
type Route struct {
	ShortName string // "E"; empty for LIRR branches
	LongName  string // "8 Avenue Local", "Babylon Branch"
	Color     string // hex without '#'
	TextColor string
}

// LoadRoutes reads routes.txt from a static GTFS feed, by route id.
func LoadRoutes(gtfs fs.FS) (map[string]Route, error) {
	routes := map[string]Route{}
	err := readFile(gtfs, "routes.txt", func(row Row) {
		routes[row("route_id")] = Route{
			ShortName: row("route_short_name"),
			LongName:  row("route_long_name"),
			Color:     row("route_color"),
			TextColor: row("route_text_color"),
		}
	})
	return routes, err
}

// LoadStops reads stop names from stops.txt in a static GTFS feed, by
// stop id.
func LoadStops(gtfs fs.FS) (map[string]string, error) {
	stops := map[string]string{}
	err := readFile(gtfs, "stops.txt", func(row Row) {
		stops[row("stop_id")] = row("stop_name")
	})
	return stops, err
}

// Station is a row from the MTA's subway Stations.csv, which adds
// boroughs and other details to the GTFS stops.
type Station struct {
	Name    string
	Borough string // "M", "Bk", "Bx", "Q" or "SI"
}

// LoadStations reads the MTA's Stations.csv, by GTFS stop id.
func LoadStations(r io.Reader) (map[string]Station, error) {
	stations := map[string]Station{}
	err := ReadCSV(r, func(row Row) {
		stations[row("GTFS Stop ID")] = Station{
			Name:    row("Stop Name"),
			Borough: row("Borough"),
		}
	})
	return stations, err
}

// Row looks up a column of a CSV row by name, returning "" if there is no
// such column.
type Row func(column string) string

// ReadCSV calls fn for each row after the header.
func ReadCSV(r io.Reader, fn func(Row)) error {
	cr := csv.NewReader(r)
	cr.ReuseRecord = true
	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("reading header: %w", err)
	}
	cols := map[string]int{}
	for i, c := range header {
		cols[c] = i
	}
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		fn(func(col string) string {
			if i, ok := cols[col]; ok && i < len(rec) {
				return rec[i]
			}
			return ""
		})
	}
}

func readFile(fsys fs.FS, name string, fn func(Row)) error {
	f, err := fsys.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := ReadCSV(f, fn); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
