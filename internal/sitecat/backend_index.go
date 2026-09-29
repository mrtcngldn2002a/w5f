package sitecat

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	"w5f/internal/doc"
	"w5f/internal/fetch"
)

// detectIndex recognises a directory listing ("Index of /…").
func detectIndex(ctx context.Context, s *site) []candidate {
	if _, _, ok := readListing(s.Body, s.Home.String(), s.Home); !ok {
		return nil
	}
	return []candidate{{Spec: SearchSpec{Kind: "index", Template: s.Home.String()}, Desc: "file listing (" + s.Home.String() + ")"}}
}

// indexBackend searches a directory catalog through its stored file list.
type indexBackend struct{}

func (indexBackend) First(p *Profile, q Query) (Request, bool) { return Request{}, false }

func (indexBackend) Page(ctx context.Context, f *fetch.Fetcher, p *Profile, pageNo int, body []byte, pageURL string) (pageOut, error) {
	return pageOut{}, nil
}

func (indexBackend) Item(ctx context.Context, f *fetch.Fetcher, p *Profile, itemURL string) ([]Download, error) {
	if d, ok := directDownload(itemURL); ok {
		return []Download{d}, nil
	}
	return nil, nil
}

// fileResult turns an indexed file into a result: name as title, folder and
// size as extra.
func fileResult(root string, fl indexFile) Result {
	rel := strings.TrimPrefix(fl.Path, root)
	if un, err := url.PathUnescape(rel); err == nil {
		rel = un
	}
	name := path.Base(rel)
	title := strings.TrimSuffix(name, path.Ext(name))
	title = strings.Join(strings.Fields(strings.ReplaceAll(title, "_", " ")), " ")
	extra := strings.TrimSuffix(path.Dir(rel), ".")
	if fl.Size != "" {
		if extra != "" {
			extra += " · "
		}
		extra += fl.Size
	}
	return Result{Title: title, URL: fl.Path, Extra: extra}
}

func (indexBackend) SearchLocal(p *Profile, q Query) ([]Result, bool, error) {
	ix, err := loadIndex(p.Search.Index)
	if err != nil {
		return nil, false, fmt.Errorf("the file list of this catalog is missing — re-check it (g → catalogs)")
	}
	words := strings.Fields(fold(q.Words))
	var out []Result
	for _, fl := range ix.Files {
		rel := strings.TrimPrefix(fl.Path, ix.Root)
		if un, err := url.PathUnescape(rel); err == nil {
			rel = un
		}
		hay := fold(rel)
		all := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				all = false
				break
			}
		}
		if all {
			out = append(out, fileResult(ix.Root, fl))
		}
	}
	return out, q.Field != "", nil
}

// ProbeLocal reads the first folders only (the full index is built when
// the catalog is added).
func (indexBackend) ProbeLocal(ctx context.Context, f *fetch.Fetcher, p *Profile, word string) ([]Result, string, error) {
	ix, err := crawl(ctx, f, p.Search.Template, 10, 2, nil)
	if err != nil {
		return nil, "", err
	}
	if len(ix.Files) == 0 {
		return nil, "", errors.New("no book files in the first folders")
	}
	var rs []Result
	for _, fl := range ix.Files {
		rs = append(rs, fileResult(ix.Root, fl))
	}
	return rs, fmt.Sprintf("%d book files in the first %d folders; the full list is built when the catalog is added (up to %d folders)", len(ix.Files), ix.Folders, p.Search.MaxFolders), nil
}

// buildIndex crawls a directory catalog and stores its file list; a
// cancelled crawl keeps what it found (marked partial).
func buildIndex(ctx context.Context, f *fetch.Fetcher, p *Profile) error {
	defer CurrentProgress.Store("")
	ix, err := crawl(ctx, f, p.Search.Template, p.Search.MaxFolders, 5, func(folders, files int) {
		CurrentProgress.Store(fmt.Sprintf("indexing folder %d · %d files", folders, files))
	})
	if ix != nil {
		if serr := saveIndex(p.Search.Index, ix); serr != nil {
			return serr
		}
	}
	return err
}

// dirDoc lists one folder of a directory catalog (read live).
func dirDoc(ctx context.Context, env Env, p Profile, dir string) (*doc.Document, error) {
	root, err := url.Parse(p.Search.Template)
	if err != nil {
		return nil, err
	}
	du, err := url.Parse(dir)
	if err != nil || du.Host != root.Host || !strings.HasPrefix(du.Path, root.Path) {
		return nil, errors.New("that folder is outside this catalog")
	}
	resp, err := env.Fetcher.Get(ctx, du, fetch.Options{})
	if err != nil {
		return nil, err
	}
	folders, files, ok := readListing(resp.Body, resp.URL.String(), root)
	if !ok {
		return nil, errors.New("this folder no longer shows a file listing")
	}
	rel := strings.TrimPrefix(du.Path, root.Path)
	if un, err := url.PathUnescape(rel); err == nil {
		rel = un
	}
	d := &doc.Document{Title: p.Name + ": /" + rel, URL: "w5f:catalog/" + p.ID + "?" + url.Values{"dir": {dir}}.Encode(), Origin: "live", Lang: "en"}
	if ix, err := loadIndex(p.Search.Index); err == nil && dir == p.Search.Template {
		status := fmt.Sprintf("%d book files indexed in %d folders on %s", len(ix.Files), ix.Folders, ix.Built.Format("2006-01-02"))
		if ix.Failed > 0 {
			status += fmt.Sprintf("; %d folders could not be read", ix.Failed)
		}
		if ix.Partial {
			status += " (partial — re-check to continue)"
		}
		d.Blocks = append(d.Blocks, para(doc.Span{Text: status, Style: doc.Italic}),
			para(doc.Span{Text: "Search file names: g → cat " + p.ID + " <words>", Style: doc.Italic}))
	}
	var items [][]doc.Block
	for _, fo := range folders {
		name := strings.TrimPrefix(fo, du.String())
		if un, err := url.PathUnescape(name); err == nil {
			name = un
		}
		href := "w5f:catalog/" + p.ID + "?" + url.Values{"dir": {fo}}.Encode()
		items = append(items, []doc.Block{para(doc.Span{Text: name, Style: doc.Bold, Link: link(d, href, name)})})
	}
	for _, fl := range files {
		r := fileResult(root.String(), fl)
		name := path.Base(strings.TrimPrefix(fl.Path, du.String()))
		if un, err := url.PathUnescape(name); err == nil {
			name = un
		}
		href := "w5f:catalog/" + p.ID + "/item?" + url.Values{"u": {fl.Path}, "t": {r.Title}}.Encode()
		sp := []doc.Span{{Text: name, Link: link(d, href, name)}}
		if fl.Size != "" {
			sp = append(sp, doc.Span{Text: "  " + fl.Size, Style: doc.Italic})
		}
		items = append(items, []doc.Block{para(sp...)})
	}
	if len(items) == 0 {
		d.Blocks = append(d.Blocks, para(doc.Span{Text: "This folder has no sub-folders or book files.", Style: doc.Italic}))
		return d, nil
	}
	d.Blocks = append(d.Blocks, doc.List{Items: items})
	return d, nil
}
