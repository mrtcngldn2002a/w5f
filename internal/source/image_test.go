package source

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/doc"
)

func pngFile(t *testing.T) []byte {
	var b bytes.Buffer
	png.Encode(&b, image.NewGray(image.Rect(0, 0, 2, 2)))
	return b.Bytes()
}

func notice(d *doc.Document) string {
	for _, b := range d.Blocks {
		if n, ok := b.(doc.Notice); ok {
			return n.Text
		}
	}
	return ""
}

// Pictures are not read as text: files and links go to the viewer (here,
// with OpenImages off, the page says how to open them).
func TestPicturesAreNotText(t *testing.T) {
	withFetcher(t)
	p := filepath.Join(t.TempDir(), "page 3.jpg")
	os.WriteFile(p, pngFile(t), 0o644)
	d, err := Load(context.Background(), p, Options{})
	if err != nil || !strings.Contains(notice(d), "w5f view") || len(d.Links) != 0 || len(d.Blocks) != 1 {
		t.Fatalf("image file: %v %q, %d blocks (no \"no readable text\" after it)", err, notice(d), len(d.Blocks))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png; charset=binary")
		w.Write(pngFile(t))
	}))
	defer srv.Close()
	d, err = Load(context.Background(), srv.URL+"/strips/today", Options{})
	if err != nil || !strings.Contains(notice(d), "w5f view") {
		t.Fatalf("image link: %v %q", err, notice(d))
	}
	saved, _ := filepath.Glob(filepath.Join(Fetcher.CacheDir, "images", "*", "image.png"))
	if len(saved) != 1 {
		t.Errorf("the picture is kept in its own folder: %v", saved)
	}
}
