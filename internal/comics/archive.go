// Package comics is W5F's comics library: local CBZ files and image folders
// with ComicInfo metadata and reading progress, and the pages W5F shows for
// them and for the series a Suwayomi server follows.
package comics

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Pages is a comic's pages in reading order.
type Pages interface {
	Len() int
	Name(i int) string
	Open(i int) (io.ReadCloser, error)
	Close() error
}

// Info is the part of ComicInfo.xml W5F uses.
type Info struct {
	Series    string `xml:"Series"`
	Number    string `xml:"Number"`
	Title     string `xml:"Title"`
	Writer    string `xml:"Writer"`
	Penciller string `xml:"Penciller"`
	Summary   string `xml:"Summary"`
	Manga     string `xml:"Manga"` // "YesAndRightToLeft" reads right to left
	PageCount int    `xml:"PageCount"`
}

// RTL reports a right-to-left comic.
func (i Info) RTL() bool { return strings.EqualFold(i.Manga, "YesAndRightToLeft") }

var imageExt = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true, ".bmp": true}

// IsImage reports a page file name.
func IsImage(name string) bool { return imageExt[strings.ToLower(filepath.Ext(name))] }

// IsComicFile reports the files the library reads itself.
func IsComicFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".cbz", ".zip":
		return true
	}
	return false
}

// natLess orders names as people count: 2.jpg before 10.jpg.
func natLess(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	for a != "" && b != "" {
		if unicode.IsDigit(rune(a[0])) && unicode.IsDigit(rune(b[0])) {
			na, ra := leadingNumber(a)
			nb, rb := leadingNumber(b)
			if na != nb {
				return na < nb
			}
			a, b = ra, rb
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func leadingNumber(s string) (int, string) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	n, _ := strconv.Atoi(s[:min(i, 9)])
	return n, s[i:]
}

type zipPages struct {
	r     *zip.ReadCloser
	files []*zip.File
}

func (z *zipPages) Len() int          { return len(z.files) }
func (z *zipPages) Name(i int) string { return z.files[i].Name }
func (z *zipPages) Open(i int) (io.ReadCloser, error) {
	if i < 0 || i >= len(z.files) {
		return nil, errors.New("no such page")
	}
	return z.files[i].Open()
}
func (z *zipPages) Close() error { return z.r.Close() }

type dirPages struct {
	dir   string
	names []string
}

func (d *dirPages) Len() int          { return len(d.names) }
func (d *dirPages) Name(i int) string { return d.names[i] }
func (d *dirPages) Open(i int) (io.ReadCloser, error) {
	if i < 0 || i >= len(d.names) {
		return nil, errors.New("no such page")
	}
	return os.Open(filepath.Join(d.dir, d.names[i]))
}
func (d *dirPages) Close() error { return nil }

// Open reads a CBZ/ZIP file or a folder of images.
func Open(path string) (Pages, Info, error) {
	var info Info
	st, err := os.Stat(path)
	if err != nil {
		return nil, info, err
	}
	if st.IsDir() {
		ents, err := os.ReadDir(path)
		if err != nil {
			return nil, info, err
		}
		d := &dirPages{dir: path}
		for _, e := range ents {
			switch {
			case !e.IsDir() && IsImage(e.Name()):
				d.names = append(d.names, e.Name())
			case strings.EqualFold(e.Name(), "ComicInfo.xml"):
				if b, err := os.ReadFile(filepath.Join(path, e.Name())); err == nil {
					_ = xml.Unmarshal(b, &info)
				}
			}
		}
		if len(d.names) == 0 {
			return nil, info, errors.New("no pages in this folder")
		}
		sort.Slice(d.names, func(i, j int) bool { return natLess(d.names[i], d.names[j]) })
		return d, info, nil
	}
	if !IsComicFile(path) {
		return nil, info, fmt.Errorf("%s: not a comic W5F reads itself", filepath.Base(path))
	}
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, info, fmt.Errorf("%s: damaged or not a CBZ (%v)", filepath.Base(path), err)
	}
	z := &zipPages{r: r}
	for _, f := range r.File {
		base := filepath.Base(f.Name)
		switch {
		case f.FileInfo().IsDir() || strings.HasPrefix(base, ".") || strings.Contains(f.Name, "__MACOSX"):
		case IsImage(base):
			z.files = append(z.files, f)
		case strings.EqualFold(base, "ComicInfo.xml"):
			if rc, err := f.Open(); err == nil {
				b, _ := io.ReadAll(io.LimitReader(rc, 1<<20))
				rc.Close()
				_ = xml.Unmarshal(b, &info)
			}
		}
	}
	if len(z.files) == 0 {
		r.Close()
		return nil, info, fmt.Errorf("%s: no pages inside", filepath.Base(path))
	}
	sort.Slice(z.files, func(i, j int) bool { return natLess(z.files[i].Name, z.files[j].Name) })
	return z, info, nil
}
