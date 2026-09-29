package fiction

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestFFRHelperProcess acts as fanficfare when run by the tests below.
func TestFFRHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_FFR_HELPER") != "1" {
		t.Skip()
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	u := args[len(args)-1]
	if strings.Contains(u, "fail") {
		fmt.Println("Downloading", u)
		fmt.Println("Story does not exist:", u)
		os.Exit(1)
	}
	if !strings.Contains(strings.Join(args, " "), "--non-interactive") {
		os.Exit(3)
	}
	f, _ := os.Create("Test Story-ffr_1.epub")
	zw := zip.NewWriter(f)
	w, _ := zw.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	w.Write([]byte("application/epub+zip"))
	w, _ = zw.Create("META-INF/container.xml")
	w.Write([]byte(`<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`))
	w, _ = zw.Create("content.opf")
	w.Write([]byte(`<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" version="2.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Test Story</dc:title><dc:creator>Writer</dc:creator><dc:language>en</dc:language></metadata><manifest><item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c1"/></spine></package>`))
	w, _ = zw.Create("c1.xhtml")
	w.Write([]byte(`<html xmlns="http://www.w3.org/1999/xhtml"><head><title>One</title></head><body><h1>One</h1><p>Saved by FanFicFare, a long enough paragraph to read in W5F.</p></body></html>`))
	zw.Close()
	f.Close()
	fmt.Println("Successfully wrote 'Test Story-ffr_1.epub'")
	os.Exit(0)
}

func TestFanFicFareBridge(t *testing.T) {
	env := testEnv(t, &fakeAdapter{})
	t.Setenv("W5F_BOOKS", t.TempDir())
	ctx := context.Background()
	old := ffrCommand
	defer func() { ffrCommand = old }()

	ffrCommand = []string{"w5f-no-such-fanficfare"}
	d, err := Route(ctx, "w5f:fiction/ffr?u=https%3A%2F%2Fexample.org%2Fs%2F1", env)
	if err != nil || !strings.Contains(flat(d), "pip install FanFicFare") {
		t.Fatalf("missing command page: %v\n%s", err, flat(d))
	}

	ffrCommand = []string{os.Args[0], "-test.run=TestFFRHelperProcess", "--"}
	t.Setenv("GO_WANT_FFR_HELPER", "1")
	d, err = Route(ctx, "w5f:fiction/ffr?u=https%3A%2F%2Fexample.org%2Fs%2F1", env)
	if err != nil || !strings.Contains(flat(d), "Saved by FanFicFare") {
		t.Fatalf("saved book: %v\n%s", err, flat(d))
	}
	if b, ok := env.DB.BookBySource("ffr:https://example.org/s/1"); !ok || b.Title != "Test Story" {
		t.Errorf("library entry: %+v %v", b, ok)
	}
	// Saving again updates the same book instead of adding a copy.
	Route(ctx, "w5f:fiction/ffr?u=https%3A%2F%2Fexample.org%2Fs%2F1", env)
	if bs, _ := env.DB.Books("author", 0); len(bs) != 1 {
		t.Errorf("books after a second save: %d", len(bs))
	}
	d, err = Route(ctx, "w5f:fiction/ffr?u=https%3A%2F%2Fexample.org%2Ffail", env)
	if err != nil || !strings.Contains(flat(d), "Story does not exist") {
		t.Errorf("failure page: %v\n%s", err, flat(d))
	}
}
