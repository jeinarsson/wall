package transit

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

// File is static data downloaded to a local file and loaded from there.
// Poll loads the cached file at startup and keeps it up to date.
type File[T any] struct {
	urls   []string // the first, and mirrors
	path   string
	load   func(path string) (T, error)
	header http.Header
	next   int // the url to try first: the last that worked

	mu     sync.Mutex
	data   T
	loaded bool
}

// NewFile returns a file downloaded from url, cached at path and read
// with load.
func NewFile[T any](url, path string, load func(path string) (T, error)) *File[T] {
	return &File[T]{urls: []string{url}, path: path, load: load}
}

// WithMirrors adds urls to try in turn when the others fail.
func (f *File[T]) WithMirrors(urls ...string) *File[T] {
	f.urls = append(f.urls, urls...)
	return f
}

// mirrorPause is how long to wait before trying the next mirror, as the
// Overpass API asks after errors.
var mirrorPause = 30 * time.Second

// WithHeader adds header to the requests for f.
func (f *File[T]) WithHeader(header http.Header) *File[T] {
	f.header = header
	return f
}

// FromZip adapts a loader for the contents of a zip archive, such as a
// static GTFS feed, to a loader for the archive's path.
func FromZip[T any](load func(fs.FS) (T, error)) func(path string) (T, error) {
	return func(path string) (T, error) {
		z, err := zip.OpenReader(path)
		if err != nil {
			var zero T
			return zero, err
		}
		defer z.Close()
		return load(z)
	}
}

// FromFile adapts a loader for the file's contents to a loader for its
// path.
func FromFile[T any](load func(io.Reader) (T, error)) func(path string) (T, error) {
	return func(path string) (T, error) {
		f, err := os.Open(path)
		if err != nil {
			var zero T
			return zero, err
		}
		defer f.Close()
		return load(f)
	}
}

// Poll loads the cached file, if any, and then checks url for a newer
// version every interval: first when the cached file is interval old (by
// its modification time), so right away if there is none. Until there is
// data it retries after a minute, then backs off up to interval. Each time
// it goes through the mirrors, if any, until one works, starting with the
// one that worked last.
func (f *File[T]) Poll(ctx context.Context, interval time.Duration) {
	if data, err := f.load(f.path); err == nil {
		f.set(data)
		if fi, err := os.Stat(f.path); err == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Until(fi.ModTime().Add(interval))):
			}
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		log.Printf("loading %s: %v", f.path, err)
	}

	retry := time.Minute
	for {
		wait := interval
		if !f.refreshAny(ctx) {
			if _, ok := f.Get(); !ok {
				wait = retry
				retry = min(2*retry, interval)
			}
			log.Printf("refreshing %s failed from all %d sources; trying again in %v", f.path, len(f.urls), wait)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// refreshAny refreshes from the urls in turn until one works, and reports
// whether one did.
func (f *File[T]) refreshAny(ctx context.Context) bool {
	for i := range f.urls {
		n := (f.next + i) % len(f.urls)
		err := f.refresh(ctx, f.urls[n])
		if err == nil {
			f.next = n
			return true
		}
		log.Printf("refreshing %s from %s: %v", f.path, host(f.urls[n]), err)
		if i == len(f.urls)-1 {
			break
		}
		next := f.urls[(n+1)%len(f.urls)]
		log.Printf("trying %s in %v", host(next), mirrorPause)
		select {
		case <-ctx.Done():
			return false
		case <-time.After(mirrorPause):
		}
	}
	return false
}

// host is the host of url, for logs: Overpass URLs carry a whole query.
func host(u string) string {
	if p, err := url.Parse(u); err == nil && p.Host != "" {
		return p.Host
	}
	return u
}

// refresh downloads url if it is newer than the cached file. The file's
// modification time is set to the server's Last-Modified, so the request is
// a cheap 304 when nothing has changed.
func (f *File[T]) refresh(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	for k, v := range f.header {
		req.Header[k] = v
	}
	if fi, err := os.Stat(f.path); err == nil {
		if _, ok := f.Get(); ok {
			req.Header.Set("If-Modified-Since", fi.ModTime().UTC().Format(http.TimeFormat))
		}
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
	// has been loaded successfully.
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
	data, err := f.load(tmp)
	if err != nil {
		return err
	}
	if t, err := http.ParseTime(resp.Header.Get("Last-Modified")); err == nil {
		os.Chtimes(tmp, t, t)
	}
	if err := os.Rename(tmp, f.path); err != nil {
		return err
	}
	f.set(data)
	log.Printf("updated %s from %s", f.path, host(url))
	return nil
}

func (f *File[T]) set(data T) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data = data
	f.loaded = true
}

// Get returns the loaded data, or false if there is none yet.
func (f *File[T]) Get() (T, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.data, f.loaded
}

// FromCSV is the old name of FromFile.
func FromCSV[T any](load func(io.Reader) (T, error)) func(path string) (T, error) {
	return FromFile(load)
}
