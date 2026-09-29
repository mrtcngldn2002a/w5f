package comics

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"

	"w5f/internal/store"
)

func pngBytes(t *testing.T, shade uint8) []byte {
	img := image.NewGray(image.Rect(0, 0, 4, 6))
	for i := range img.Pix {
		img.Pix[i] = shade
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	_ = color.Black
	return b.Bytes()
}

// writeCBZ makes a CBZ with pages named as given (content = page index) and
// an optional ComicInfo.
func writeCBZ(t *testing.T, path string, pages []string, comicInfo string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	if comicInfo != "" {
		w, _ := z.Create("ComicInfo.xml")
		io.WriteString(w, comicInfo)
	}
	for i, name := range pages {
		w, _ := z.Create(name)
		w.Write(pngBytes(t, uint8(i)))
	}
	z.Close()
	f.Close()
}

func TestOpenOrdersPagesNaturally(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.cbz")
	writeCBZ(t, p, []string{"10.jpg", "2.jpg", "1.png", "__MACOSX/._1.png", "notes.txt"},
		`<ComicInfo><Series>Nausicaä</Series><Number>3</Number><Manga>YesAndRightToLeft</Manga></ComicInfo>`)
	pages, info, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer pages.Close()
	if pages.Len() != 3 || pages.Name(0) != "1.png" || pages.Name(1) != "2.jpg" || pages.Name(2) != "10.jpg" {
		t.Errorf("order: %d %q %q %q", pages.Len(), pages.Name(0), pages.Name(1), pages.Name(2))
	}
	if info.Series != "Nausicaä" || !info.RTL() {
		t.Errorf("info: %+v", info)
	}
	rc, err := pages.Open(1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(rc); err != nil {
		t.Errorf("page decodes: %v", err)
	}
	rc.Close()
	if _, err := pages.Open(9); err == nil {
		t.Error("page out of range")
	}
	bad := filepath.Join(dir, "bad.cbz")
	os.WriteFile(bad, []byte("PK\x03\x04 truncated"), 0o644)
	if _, _, err := Open(bad); err == nil {
		t.Error("a damaged CBZ is an error, not a crash")
	}
}

func TestScanLibrary(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	writeCBZ(t, filepath.Join(root, "Berserk", "Berserk v02.cbz"), []string{"1.jpg", "2.jpg"}, "")
	writeCBZ(t, filepath.Join(root, "Berserk", "Berserk v01.cbz"), []string{"1.jpg"}, "")
	writeCBZ(t, filepath.Join(root, "Suwayomi", "mangas", "Local", "Alpha", "Chapter 12.cbz"), []string{"a.jpg"},
		`<ComicInfo><Series>Test Series Alpha</Series><Number>12</Number><Title>Deep</Title></ComicInfo>`)
	os.MkdirAll(filepath.Join(root, "Zines", "Issue 7"), 0o755)
	for _, n := range []string{"p1.png", "p2.png"} {
		os.WriteFile(filepath.Join(root, "Zines", "Issue 7", n), pngBytes(t, 9), 0o644)
	}
	os.WriteFile(filepath.Join(root, "Plastic Man 1.pdf"), []byte("%PDF"), 0o644)
	os.WriteFile(filepath.Join(root, "broken.cbz"), []byte("nope"), 0o644)

	n, err := Scan(db, root)
	if err != nil || n != 5 { // the broken file is not listed
		t.Fatalf("scan: %d %v", n, err)
	}
	series, _ := db.ComicSeriesList()
	names := map[string]int{}
	for _, s := range series {
		names[s.Name] = s.Issues
	}
	if names["Berserk"] != 2 || names["Test Series Alpha"] != 1 || names["Zines"] != 1 || names["Plastic Man"] != 1 {
		t.Fatalf("series: %+v", series)
	}
	issues, _ := db.ComicsInSeries("Berserk")
	if len(issues) != 2 || issues[0].Number != 1 || issues[1].Pages != 2 {
		t.Fatalf("issues: %+v", issues)
	}
	alpha, _ := db.ComicsInSeries("Test Series Alpha")
	if alpha[0].Number != 12 || alpha[0].Title != "Deep" {
		t.Errorf("ComicInfo wins: %+v", alpha[0])
	}

	// Progress survives a rescan; a deleted file disappears.
	db.SaveComicPage(issues[1].ID, 1, 2)
	os.Remove(filepath.Join(root, "Berserk", "Berserk v01.cbz"))
	if _, err := Scan(db, root); err != nil {
		t.Fatal(err)
	}
	issues, _ = db.ComicsInSeries("Berserk")
	if len(issues) != 1 || issues[0].Page != 1 || !issues[0].Finished {
		t.Errorf("after rescan: %+v", issues)
	}
	recent, _ := db.RecentComics(5)
	if len(recent) != 1 || recent[0].Series != "Berserk" {
		t.Errorf("recent: %+v", recent)
	}
}
