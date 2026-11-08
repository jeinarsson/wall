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
	"os"
	"sync"
	"time"
)

// File is static data downloaded to a local file and loaded from there.
// Poll loads the cached file at startup and keeps it up to date.
type File[T any] struct {
	url  string
	path string
	load func(path string) (T, error)

	mu     sync.Mutex
	data   T
	loaded bool
}

// NewFile returns a file downloaded from url, cached at path and read
// with load.
func NewFile[T any](url, path string, load func(path string) (T, error)) *File[T] {
	return &File[T]{url: url, path: path, load: load}
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

// FromCSV adapts a loader for CSV rows to a loader for the file's path.
func FromCSV[T any](load func(io.Reader) (T, error)) func(path string) (T, error) {
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
// version now and every interval. Until there is data it retries every
// minute.
func (f *File[T]) Poll(ctx context.Context, interval time.Duration) {
	if data, err := f.load(f.path); err == nil {
		f.set(data)
	} else if !errors.Is(err, fs.ErrNotExist) {
		log.Printf("loading %s: %v", f.path, err)
	}

	for {
		if err := f.refresh(ctx); err != nil {
			log.Printf("refreshing %s: %v", f.url, err)
		}
		wait := interval
		if _, ok := f.Get(); !ok {
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
func (f *File[T]) refresh(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", f.url, nil)
	if err != nil {
		return err
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
	log.Printf("updated %s from %s", f.path, f.url)
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
