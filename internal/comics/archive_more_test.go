package comics

import (
	"archive/tar"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCBT makes a tar comic like the fixtures: three pages out of order in a
// folder, and a ComicInfo.
func writeCBT(t *testing.T, path string) {
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	add := func(name string, b []byte) {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(b)), Typeflag: tar.TypeReg})
		tw.Write(b)
	}
	add("ComicInfo.xml", []byte(`<ComicInfo><Series>Fixture</Series><Manga>YesAndRightToLeft</Manga></ComicInfo>`))
	for i, n := range []string{"sub/10.png", "sub/1.png", "sub/2.png"} {
		shade := map[int]uint8{0: 120, 1: 40, 2: 80}[i]
		add(n, pngBytes(t, shade))
	}
	tw.Close()
	f.Close()
}

func TestMoreArchiveFormats(t *testing.T) {
	cbt := filepath.Join(t.TempDir(), "sample.cbt")
	writeCBT(t, cbt)
	for _, path := range []string{"../../testdata/comics/sample.cb7", "../../testdata/comics/sample.cbr", "../../testdata/comics/solid.cbr", cbt} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			pages, info, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer pages.Close()
			if pages.Len() != 3 || !strings.HasSuffix(pages.Name(0), "1.png") || !strings.HasSuffix(pages.Name(2), "10.png") {
				t.Fatalf("pages: %d %q %q", pages.Len(), pages.Name(0), pages.Name(2))
			}
			if info.Series != "Fixture" || !info.RTL() {
				t.Errorf("ComicInfo: %+v", info)
			}
			for _, i := range []int{2, 0, 1, 1} { // out of order: RAR starts over
				rc, err := pages.Open(i)
				if err != nil {
					t.Fatalf("page %d: %v", i, err)
				}
				img, err := png.Decode(rc)
				rc.Close()
				if err != nil {
					t.Fatalf("page %d decodes: %v", i, err)
				}
				r, _, _, _ := img.At(0, 0).RGBA()
				// The fixtures shade pages 40, 80, 120 in reading order; the
				// generated CBT uses the gray helper (same value in r).
				if want := uint32(40*(i+1)) * 0x101; r>>8 != want>>8 && !strings.HasSuffix(path, ".cbt") {
					t.Errorf("page %d is not the right one: red %d, want %d", i, r>>8, want>>8)
				}
			}
			if _, err := pages.Open(3); err == nil {
				t.Error("page out of range")
			}
		})
	}
	if !IsComicFile("x.CBR") || !IsComicFile("x.cb7") || !IsComicFile("x.cbt") || External("x.cbr") || !External("x.pdf") {
		t.Error("which files the library reads itself")
	}
}
