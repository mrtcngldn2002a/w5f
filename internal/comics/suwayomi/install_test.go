package suwayomi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallChecksTheJar(t *testing.T) {
	jar := []byte("pretend jar bytes")
	sum := sha256.Sum256(jar)
	good := hex.EncodeToString(sum[:])
	checksum := good
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprintf(w, `{"tag_name":"v9","assets":[{"name":"Suwayomi-Server-v9.jar","browser_download_url":"%[1]s/jar","size":%[2]d},{"name":"Checksums.sha256","browser_download_url":"%[1]s/sums"}]}`, srv.URL, len(jar))
		case "/jar":
			w.Write(jar)
		case "/sums":
			fmt.Fprintf(w, "%s  Suwayomi-Server-v9.jar\nabc  other.tar.gz\n", checksum)
		}
	}))
	defer srv.Close()
	old := ReleaseAPI
	ReleaseAPI = srv.URL + "/latest"
	defer func() { ReleaseAPI = old }()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Suwayomi-Server-v8.jar"), []byte("old"), 0o644)

	checksum = strings.Repeat("0", 64)
	if _, err := Install(context.Background(), dir, nil); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("a wrong checksum is refused: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 1 {
		t.Errorf("a refused download leaves nothing behind: %v", left)
	}
	checksum = good
	name, err := Install(context.Background(), dir, nil)
	if err != nil || name != "Suwayomi-Server-v9.jar" {
		t.Fatalf("install: %q %v", name, err)
	}
	if s := (Server{Dir: dir}); filepath.Base(s.Jar()) != name {
		t.Errorf("the new jar is used and the old one removed: %q", s.Jar())
	}
}
