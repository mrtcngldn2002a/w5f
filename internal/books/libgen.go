package books

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"w5f/internal/doc"
	"w5f/internal/libgen"
	"w5f/internal/store"
)

func libgenRoute(ctx context.Context, p string, q url.Values, env Env) (*doc.Document, error) {
	c, err := libgen.New(env.Fetcher)
	if err != nil {
		return nil, err
	}
	d := &doc.Document{Title: "Library Genesis", URL: "w5f:books/libgen", Origin: "live", Lang: "en"}
	para := func(s string) { d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: s}}}) }
	addLink := func(s, u string) {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: s, Link: link(d, u, s)}}})
	}
	switch p {
	case "books/libgen/status":
		for _, s := range c.Status(ctx) {
			para(s)
		}
		return d, ctx.Err()
	case "books/libgen/item":
		hash := q.Get("md5")
		b, err := c.Details(ctx, hash)
		if err != nil {
			return nil, err
		}
		d.Title = b.Title
		para(b.Author)
		para(strings.Join([]string{b.Publisher, b.Year, b.Language, strings.ToUpper(b.Extension), b.Size + " bytes"}, " · "))
		if saved, ok := env.DB.BookBySource("libgen:" + b.MD5); ok {
			if _, e := os.Stat(saved.Path); e == nil {
				addLink("✓ open from your library", bookHref(saved))
			}
		}
		addLink("Download "+strings.ToUpper(b.Extension)+" into your library", "w5f:books/get/libgen/"+b.MD5)
		addLink("Resolve direct download link", "w5f:books/libgen/link?md5="+b.MD5)
		return d, nil
	case "books/libgen/link":
		l, err := c.Resolve(ctx, q.Get("md5"))
		if err != nil {
			return nil, err
		}
		para("This link is temporary and requires the issuing mirror's Referer. Use the library download action for a verified download.")
		para(l.URL)
		addLink("Download into your library", "w5f:books/get/libgen/"+strings.ToLower(q.Get("md5")))
		return d, nil
	}
	if p != "books/libgen" {
		return nil, fmt.Errorf("unknown LibGen address: %s", p)
	}
	if strings.TrimSpace(q.Get("q")) == "" {
		d.Origin = "local"
		para("Search: g → libgen <words> (or lg <words>)")
		para(`Filters: ext:epub,pdf lang:english year:1897 author:"Bram Stoker" title:Dracula publisher:Penguin sort:title sort:-year limit:50`)
		para("Filters apply to each result page. Follow more results even when a page has no matching books. Page sizes: 25, 50, 100.")
		para("Open by MD5: g → libgen-md5 <hash>")
		para("Mirror and default format settings: " + libgen.ConfigPath())
		addLink("Check mirrors", "w5f:books/libgen/status")
		return d, nil
	}
	o, err := libgen.ParseQuery(q.Get("q"))
	if err != nil {
		return nil, err
	}
	o.Page, _ = strconv.Atoi(q.Get("page"))
	o.Page = max(o.Page, 1)
	rs, err := c.Search(ctx, o)
	if err != nil {
		return nil, err
	}
	d.Title = "Library Genesis: " + q.Get("q")
	d.URL = "w5f:books/libgen?" + q.Encode()
	para(fmt.Sprintf("%d matches · page %d · %s", len(rs.Books), o.Page, rs.Mirror))
	for _, b := range rs.Books {
		href := "w5f:books/libgen/item?md5=" + b.MD5
		mark := ""
		if saved, ok := env.DB.BookBySource("libgen:" + b.MD5); ok {
			if _, e := os.Stat(saved.Path); e == nil {
				href, mark = bookHref(saved), "✓ "
			}
		}
		addLink(mark+b.Title, href)
		para(strings.Join([]string{b.Author, b.Year, b.Language, strings.ToUpper(b.Extension), b.Size}, " · "))
	}
	if rs.More {
		v := url.Values{"q": {q.Get("q")}, "page": {strconv.Itoa(o.Page + 1)}}
		d.Next = "w5f:books/libgen?" + v.Encode()
		addLink("more results ›", d.Next)
	}
	return d, nil
}

func downloadLibgen(ctx context.Context, env Env, hash string) (store.Book, error) {
	c, err := libgen.New(env.Fetcher)
	if err != nil {
		return store.Book{}, err
	}
	b, err := c.Details(ctx, hash)
	if err != nil {
		return store.Book{}, err
	}
	if !readable["."+b.Extension] && !external["."+b.Extension] {
		return store.Book{}, fmt.Errorf("unsupported book format %q", b.Extension)
	}
	p, err := c.Download(ctx, b, LibraryDir())
	if err != nil {
		return store.Book{}, err
	}
	defer os.Remove(p)
	return AddFile(env.DB, p, b.Extension, b.Title, b.Author, "libgen:"+b.MD5)
}
