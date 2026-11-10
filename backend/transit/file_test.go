package transit

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestFileMirrors(t *testing.T) {
	mirrorPause = 0
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "busy", http.StatusBadGateway)
	}))
	defer down.Close()
	asked := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		io.WriteString(w, "data")
	}))
	defer up.Close()

	read := FromFile(func(r io.Reader) (string, error) {
		b, err := io.ReadAll(r)
		return string(b), err
	})
	f := NewFile(down.URL, filepath.Join(t.TempDir(), "f"), read).WithMirrors(up.URL)

	if !f.refreshAny(context.Background()) {
		t.Fatal("no source worked")
	}
	if got, ok := f.Get(); !ok || got != "data" {
		t.Errorf("got %q, want data", got)
	}
	// The mirror that worked is tried first next time.
	f.refreshAny(context.Background())
	if asked != 2 || f.next != 1 {
		t.Errorf("mirror asked %d times, next %d; want 2 and 1", asked, f.next)
	}

	g := NewFile(down.URL, filepath.Join(t.TempDir(), "g"), read)
	if g.refreshAny(context.Background()) {
		t.Errorf("refreshed from a server that is down")
	}
}
