package transit

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// Route is a row from a GTFS routes.txt.
type Route struct {
	ShortName string // "E"; empty for LIRR branches
	LongName  string // "8 Avenue Local", "Babylon Branch"
	Color     string // hex without '#'
	TextColor string
}

// Static holds the parts of a static GTFS feed needed to label realtime
// data: stop names and route colors.
type Static struct {
	stops  map[string]string
	routes map[string]Route
}

// LoadStatic reads stops.txt and routes.txt from fsys.
func LoadStatic(fsys fs.FS) (*Static, error) {
	s := &Static{stops: map[string]string{}, routes: map[string]Route{}}
	err := readCSV(fsys, "stops.txt", func(row func(string) string) {
		s.stops[row("stop_id")] = row("stop_name")
	})
	if err != nil {
		return nil, err
	}
	err = readCSV(fsys, "routes.txt", func(row func(string) string) {
		s.routes[row("route_id")] = Route{
			ShortName: row("route_short_name"),
			LongName:  row("route_long_name"),
			Color:     row("route_color"),
			TextColor: row("route_text_color"),
		}
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Static) StopName(id string) string {
	if name, ok := s.stops[id]; ok {
		return name
	}
	return id
}

func (s *Static) Route(id string) Route {
	if r, ok := s.routes[id]; ok {
		return r
	}
	return Route{ShortName: id}
}

// readCSV calls fn for each data row of name, with a lookup by column name.
func readCSV(fsys fs.FS, name string, fn func(row func(string) string)) error {
	f, err := fsys.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()

	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if len(records) == 0 {
		return fmt.Errorf("%s: empty", name)
	}
	cols := map[string]int{}
	for i, c := range records[0] {
		cols[c] = i
	}
	for _, rec := range records[1:] {
		fn(func(col string) string {
			if i, ok := cols[col]; ok && i < len(rec) {
				return rec[i]
			}
			return ""
		})
	}
	return nil
}

// StaticFeed is a static GTFS zip kept in a local file, loaded at startup
// and refreshed in the background by Poll.
type StaticFeed struct {
	url  string
	path string

	mu     sync.Mutex
	static *Static
}

// NewStaticFeed returns a static feed downloaded from url and cached at path.
func NewStaticFeed(url, path string) *StaticFeed {
	return &StaticFeed{url: url, path: path}
}

// Poll loads the cached file, if any, and then checks url for a newer
// version now and every interval. Until there is data it retries every
// minute.
func (f *StaticFeed) Poll(ctx context.Context, interval time.Duration) {
	if s, err := loadZip(f.path); err == nil {
		f.set(s)
	} else if !errors.Is(err, fs.ErrNotExist) {
		log.Printf("loading %s: %v", f.path, err)
	}

	for {
		if err := f.refresh(ctx); err != nil {
			log.Printf("refreshing %s: %v", f.url, err)
		}
		wait := interval
		if f.current() == nil {
			wait = time.Minute
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// refresh downloads url if it is newer than the cached file. The file's
// modification time is set to the server's Last-Modified, so the request is
// a cheap 304 when nothing has changed.
func (f *StaticFeed) refresh(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", f.url, nil)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(f.path); err == nil && f.current() != nil {
		req.Header.Set("If-Modified-Since", fi.ModTime().UTC().Format(http.TimeFormat))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %s", resp.Status)
	}

	// Write next to the cache file and only replace it once the download
	// has been read successfully.
	tmp := f.path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	s, err := loadZip(tmp)
	if err != nil {
		return err
	}
	if t, err := http.ParseTime(resp.Header.Get("Last-Modified")); err == nil {
		os.Chtimes(tmp, t, t)
	}
	if err := os.Rename(tmp, f.path); err != nil {
		return err
	}
	f.set(s)
	log.Printf("updated %s from %s", f.path, f.url)
	return nil
}

func (f *StaticFeed) set(s *Static) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.static = s
}

// current returns the loaded static data, or nil if there is none yet.
func (f *StaticFeed) current() *Static {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.static
}

func loadZip(path string) (*Static, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	return LoadStatic(z)
}
