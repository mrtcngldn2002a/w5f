// Package books is the Library: a reader for EPUB books, the local library
// (~/Archive/Books), reading progress, and the Project Gutenberg and Standard
// Ebooks catalogs.
package books

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/doc"
	"w5f/internal/htmlconv"
)

// EPUB is an opened EPUB file.
type EPUB struct {
	Title    string
	Author   string
	Lang     string
	Chapters []Chapter // in reading (spine) order
	zr       *zip.ReadCloser
	files    map[string]*zip.File
}

// Chapter is one spine item.
type Chapter struct {
	Href  string // path inside the zip
	Title string // from the table of contents, if any
}

type container struct {
	Rootfiles []struct {
		Path string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

type opf struct {
	Metadata struct {
		Titles    []string `xml:"title"`
		Creators  []string `xml:"creator"`
		Languages []string `xml:"language"`
	} `xml:"metadata"`
	Manifest []struct {
		ID         string `xml:"id,attr"`
		Href       string `xml:"href,attr"`
		MediaType  string `xml:"media-type,attr"`
		Properties string `xml:"properties,attr"`
	} `xml:"manifest>item"`
	Spine struct {
		Toc   string `xml:"toc,attr"`
		Items []struct {
			IDRef  string `xml:"idref,attr"`
			Linear string `xml:"linear,attr"`
		} `xml:"itemref"`
	} `xml:"spine"`
}

// OpenEPUB opens and indexes an EPUB file.
func OpenEPUB(p string) (*EPUB, error) {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return nil, fmt.Errorf("not an EPUB (zip): %w", err)
	}
	e := &EPUB{zr: zr, files: map[string]*zip.File{}}
	for _, f := range zr.File {
		e.files[f.Name] = f
	}
	var c container
	if err := e.readXML("META-INF/container.xml", &c); err != nil || len(c.Rootfiles) == 0 {
		zr.Close()
		return nil, errors.New("EPUB has no container.xml")
	}
	opfPath := c.Rootfiles[0].Path
	var o opf
	if err := e.readXML(opfPath, &o); err != nil {
		zr.Close()
		return nil, fmt.Errorf("reading %s: %w", opfPath, err)
	}
	dir := path.Dir(opfPath)
	join := func(href string) string {
		h, _ := url.PathUnescape(strings.SplitN(href, "#", 2)[0])
		if dir == "." {
			return path.Clean(h)
		}
		return path.Clean(dir + "/" + h)
	}
	if len(o.Metadata.Titles) > 0 {
		e.Title = strings.TrimSpace(o.Metadata.Titles[0])
	}
	if len(o.Metadata.Creators) > 0 {
		e.Author = strings.TrimSpace(o.Metadata.Creators[0])
	}
	if len(o.Metadata.Languages) > 0 {
		e.Lang = strings.ToLower(strings.TrimSpace(o.Metadata.Languages[0]))
	}
	byID := map[string]string{}
	var navHref, ncxHref string
	for _, it := range o.Manifest {
		byID[it.ID] = join(it.Href)
		if strings.Contains(it.Properties, "nav") {
			navHref = join(it.Href)
		}
		if it.MediaType == "application/x-dtbncx+xml" {
			ncxHref = join(it.Href)
		}
	}
	if o.Spine.Toc != "" && byID[o.Spine.Toc] != "" {
		ncxHref = byID[o.Spine.Toc]
	}
	for _, it := range o.Spine.Items {
		if h := byID[it.IDRef]; h != "" && it.Linear != "no" {
			e.Chapters = append(e.Chapters, Chapter{Href: h})
		}
	}
	if len(e.Chapters) == 0 {
		zr.Close()
		return nil, errors.New("EPUB has an empty reading order")
	}
	titles := map[string]string{}
	if navHref != "" {
		e.navTitles(navHref, titles)
	}
	if len(titles) == 0 && ncxHref != "" {
		e.ncxTitles(ncxHref, titles)
	}
	for i := range e.Chapters {
		e.Chapters[i].Title = titles[e.Chapters[i].Href]
	}
	return e, nil
}

// Close releases the file.
func (e *EPUB) Close() error { return e.zr.Close() }

func (e *EPUB) read(name string) ([]byte, error) {
	f := e.files[name]
	if f == nil {
		return nil, fmt.Errorf("%s missing from EPUB", name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, 32<<20))
}

func (e *EPUB) readXML(name string, v any) error {
	b, err := e.read(name)
	if err != nil {
		return err
	}
	dec := xml.NewDecoder(strings.NewReader(string(b)))
	dec.Strict = false
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	return dec.Decode(v)
}

// navTitles reads the EPUB 3 navigation document (its <a href> entries).
func (e *EPUB) navTitles(navHref string, out map[string]string) {
	b, err := e.read(navHref)
	if err != nil {
		return
	}
	gq, err := goquery.NewDocumentFromReader(strings.NewReader(string(b)))
	if err != nil {
		return
	}
	base, _ := url.Parse("epub:/" + navHref)
	gq.Find("nav a[href]").Each(func(_ int, a *goquery.Selection) {
		ref, err := url.Parse(a.AttrOr("href", ""))
		if err != nil {
			return
		}
		p := strings.TrimPrefix(base.ResolveReference(ref).Path, "/")
		if t := strings.Join(strings.Fields(a.Text()), " "); t != "" {
			if _, ok := out[p]; !ok {
				out[p] = t
			}
		}
	})
}

type ncx struct {
	Points []ncxPoint `xml:"navMap>navPoint"`
}

type ncxPoint struct {
	Label   string `xml:"navLabel>text"`
	Content struct {
		Src string `xml:"src,attr"`
	} `xml:"content"`
	Points []ncxPoint `xml:"navPoint"`
}

// ncxTitles reads the EPUB 2 table of contents.
func (e *EPUB) ncxTitles(ncxHref string, out map[string]string) {
	var n ncx
	if e.readXML(ncxHref, &n) != nil {
		return
	}
	dir := path.Dir(ncxHref)
	var walk func([]ncxPoint)
	walk = func(ps []ncxPoint) {
		for _, p := range ps {
			h, _ := url.PathUnescape(strings.SplitN(p.Content.Src, "#", 2)[0])
			full := path.Clean(dir + "/" + h)
			if _, ok := out[full]; !ok {
				out[full] = strings.TrimSpace(p.Label)
			}
			walk(p.Points)
		}
	}
	walk(n.Points)
}

// ChapterIndex maps a path inside the EPUB to its spine position.
func (e *EPUB) ChapterIndex(p string) int {
	for i, c := range e.Chapters {
		if c.Href == p {
			return i
		}
	}
	return -1
}

// ChapterDoc converts chapter i into a document. Links to other chapters are
// rewritten through link (spine index → address); external links are kept.
func (e *EPUB) ChapterDoc(i int, link func(ch int) string) (*doc.Document, error) {
	if i < 0 || i >= len(e.Chapters) {
		return nil, errors.New("no such chapter")
	}
	c := e.Chapters[i]
	b, err := e.read(c.Href)
	if err != nil {
		return nil, err
	}
	d, err := htmlconv.XHTML(b, "epub:/"+c.Href)
	if err != nil {
		return nil, err
	}
	for li, l := range d.Links {
		u, err := url.Parse(l.Href)
		if err != nil || u.Scheme != "epub" {
			continue
		}
		if ch := e.ChapterIndex(strings.TrimPrefix(u.Path, "/")); ch >= 0 {
			d.Links[li].Href = link(ch)
		}
	}
	return d, nil
}
