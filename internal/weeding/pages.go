package weeding

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"w5f/internal/doc"
	"w5f/internal/personal"
	"w5f/internal/store"
	"w5f/internal/ultan"
)

// Target is the room's address.
const Target = "w5f:weeding"

// IsTarget reports the room's addresses.
func IsTarget(t string) bool { return t == Target || strings.HasPrefix(t, Target+"/") }

// Env is what the room needs: the database, W5F's folders, and the way
// to remove a chapter Suwayomi downloaded (through Suwayomi).
type Env struct {
	DB                            *store.DB
	CacheDir, DataDir             string
	NotesDir, BooksDir, ComicsDir string
	CacheLimit                    int64 // bytes; 0: none
	RemoveSuwayomi                func(ctx context.Context, path string) error
	// RescanComics brings the comics library up to date (files removed or
	// added outside W5F) before it is listed.
	RescanComics func()
}

// Route builds the room's pages. Removing anything takes two steps: the
// address alone asks, ?sure=yes removes; each removes one thing.
func Route(ctx context.Context, target string, env Env) (*doc.Document, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	sure := q.Get("sure") == "yes"
	switch strings.TrimPrefix(u.Opaque, "weeding") {
	case "", "/":
		return env.room(""), nil
	case "/cache":
		if !sure {
			return env.askCache(), nil
		}
		freed, n, _ := ClearCache(env.CacheDir)
		return env.room(fmt.Sprintf("The page cache is empty: %d files, %s freed. Pages are fetched again when opened.", n, Size(freed))), nil
	case "/notes":
		return env.notes(""), nil
	case "/note":
		return env.note(q.Get("f"), sure)
	case "/books":
		return env.books(""), nil
	case "/book":
		id, _ := strconv.ParseInt(q.Get("id"), 10, 64)
		return env.book(id, sure)
	case "/comics":
		return env.comics(""), nil
	case "/comic":
		id, _ := strconv.ParseInt(q.Get("id"), 10, 64)
		return env.comic(ctx, id, sure)
	}
	return nil, errors.New("unknown address in the Weeding Room: " + target)
}

type page struct{ d *doc.Document }

func newPage(title, addr string) *page {
	return &page{&doc.Document{Title: title, URL: addr, Origin: "local", Lang: "en"}}
}

func (p *page) link(href, text string) doc.Span {
	p.d.Links = append(p.d.Links, doc.Link{Href: href, Text: text})
	return doc.Span{Text: text, Link: len(p.d.Links)}
}

func (p *page) add(bs ...doc.Block) { p.d.Blocks = append(p.d.Blocks, bs...) }

func (p *page) heading(s string) { p.add(doc.Heading{Level: 2, Text: doc.Inline{{Text: s}}}) }

func (p *page) italic(s string) { p.add(doc.Paragraph{Text: doc.Inline{{Text: s, Style: doc.Italic}}}) }

func (p *page) notice(s string) {
	if s != "" {
		p.d.Blocks = append([]doc.Block{doc.Notice{Kind: "info", Text: s}}, p.d.Blocks...)
	}
}

// ask is the question every removal asks; "No" comes first, where the
// selection starts.
func (p *page) ask(what []string, no, yes string) {
	for _, w := range what {
		p.add(doc.Paragraph{Text: doc.Inline{{Text: w}}})
	}
	p.italic("It is deleted for good: there is no bin to take it back from.")
	p.add(doc.Paragraph{Text: doc.Inline{p.link(no, "No, keep it"), {Text: " · "}, func() doc.Span {
		s := p.link(yes, "Yes, remove it")
		s.Style = doc.Bold
		return s
	}()}})
}

// Size writes bytes the way people read them.
func Size(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}

// dirSize adds up a folder's files (links are not followed).
func dirSize(dir string) (int64, int) {
	var n int64
	files := 0
	filepath.WalkDir(dir, func(p string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() || de.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if info, err := de.Info(); err == nil {
			n += info.Size()
			files++
		}
		return nil
	})
	return n, files
}

func fileSize(paths ...string) int64 {
	var n int64
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			n += st.Size()
		}
	}
	return n
}

// room is the Weeding Room itself: what W5F keeps, by weight, and what
// can go.
func (env Env) room(note string) *doc.Document {
	p := newPage("The Weeding Room", Target)
	p.add(ultan.Note{Text: "A library that never weeds its shelves drowns in them. I keep a knife for paper here; it cuts one thing at a time."}.Blocks(func(href, text string) int {
		return p.link(href, text).Link
	})...)

	p.heading("What the library keeps")
	cache, cacheFiles := CacheSize(env.CacheDir)
	logs, _ := filepath.Glob(filepath.Join(env.DataDir, "history*.log"))
	db := filepath.Join(env.DataDir, "w5f.db")
	rows := [][]doc.Inline{{{{Text: "", Style: doc.Bold}}, {{Text: "size", Style: doc.Bold}}, {{Text: "where", Style: doc.Bold}}}}
	row := func(what string, n int64, where string) {
		rows = append(rows, []doc.Inline{{{Text: what}}, {{Text: Size(n)}}, {{Text: where, Style: doc.Italic}}})
	}
	row(fmt.Sprintf("The page cache (%d files)", cacheFiles), cache, env.CacheDir)
	row("The catalogue: history, periodicals, search index", fileSize(db, db+"-wal", db+"-shm"), db)
	row("The visit log", fileSize(logs...), filepath.Join(env.DataDir, "history.log"))
	for _, d := range []struct{ what, dir string }{
		{"The dictionary", filepath.Join(env.DataDir, "dict")},
		{"Suwayomi: the server and its data", filepath.Join(env.DataDir, "suwayomi")},
		{"Books", env.BooksDir}, {"Comics", env.ComicsDir}, {"Notes, clippings, saved pages", env.NotesDir},
	} {
		n, _ := dirSize(d.dir)
		row(d.what, n, d.dir)
	}
	p.add(doc.Table{Header: true, Rows: rows})
	if env.CacheLimit > 0 {
		p.italic(fmt.Sprintf("The page cache is kept under %s: when it grows past that, the pages read longest ago go first ([cache] limit_mb in config.toml; 0 keeps everything). Only W5F's own cache folders are ever touched.", Size(env.CacheLimit)))
	} else {
		p.italic("The page cache has no limit ([cache] limit_mb = 0 in config.toml). Only W5F's own cache folders are ever touched.")
	}

	p.heading("What can go")
	item := func(href, label, what string) []doc.Block {
		return []doc.Block{doc.Paragraph{Text: doc.Inline{p.link(href, label), {Text: "  " + what, Style: doc.Italic}}}}
	}
	p.add(doc.List{Items: [][]doc.Block{
		item(Target+"/cache", "Empty the page cache…", "every page and picture kept for offline reading; they come back when opened"),
		item(Target+"/notes", "Remove a note…", "a note, a clipping or a saved page, one at a time"),
		item(Target+"/books", "Remove a book…", "a book of The Stacks and its file, one at a time"),
		item(Target+"/comics", "Remove a comic…", "an issue or a chapter you downloaded, one at a time"),
		item("w5f:history/clear", "Clear the history…", "The Register and Ultan's Ledger start again; the old log is kept"),
		item("w5f:desk", "Clear the desk…", "pages left open are set aside"),
		item("w5f:solo/log/clear", "Clear the Gaming Table's log…", "for a new campaign; the old days are kept"),
	}})
	p.italic("Notes, books and comics are removed one at a time, never all together. Every removal asks first.")
	p.notice(note)
	return p.d
}

func (env Env) askCache() *doc.Document {
	p := newPage("Empty the page cache?", Target+"/cache")
	n, files := CacheSize(env.CacheDir)
	if files == 0 {
		p.add(doc.Paragraph{Text: doc.Inline{{Text: "The page cache is already empty. "}, p.link(Target, "back to the Weeding Room")}})
		return p.d
	}
	p.add(doc.Paragraph{Text: doc.Inline{{Text: fmt.Sprintf("%d files, %s: the pages, pictures and PDF texts kept so they open quickly and offline.", files, Size(n))}}})
	p.italic("Notes, books, comics, the history and the search index are not touched, nor anything of other programs in the cache folder. Pages are fetched again when next opened.")
	p.add(doc.Paragraph{Text: doc.Inline{p.link(Target, "No, keep it"), {Text: " · "}, func() doc.Span {
		s := p.link(Target+"/cache?sure=yes", "Yes, empty it")
		s.Style = doc.Bold
		return s
	}()}})
	return p.d
}

// within reports whether p lies inside dir (not dir itself), after
// cleaning both: removal never reaches outside W5F's folders.
func within(dir, p string) bool {
	if dir == "" || p == "" {
		return false
	}
	ad, err1 := filepath.Abs(dir)
	ap, err2 := filepath.Abs(p)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(ad, ap)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// regular reports a plain file (not a link, not a folder).
func regular(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.Mode().IsRegular()
}

// noteParts are the folders of the notes folder whose files can be removed
// (the queue is The Lectern's, not removed here).
var noteParts = []string{"Notes", "Clippings", "Saved"}

func (env Env) notes(note string) *doc.Document {
	p := newPage("Remove a note", Target+"/notes")
	any := false
	for _, part := range noteParts {
		type f struct {
			rel  string
			size int64
			mod  time.Time
		}
		var fs []f
		root := filepath.Join(env.NotesDir, part)
		filepath.WalkDir(root, func(path string, de os.DirEntry, err error) error {
			if err != nil || de.IsDir() || !strings.EqualFold(filepath.Ext(path), ".md") || !de.Type().IsRegular() {
				return nil
			}
			info, err := de.Info()
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(env.NotesDir, path)
			fs = append(fs, f{filepath.ToSlash(rel), info.Size(), info.ModTime()})
			return nil
		})
		if len(fs) == 0 {
			continue
		}
		any = true
		sort.Slice(fs, func(i, j int) bool { return fs[i].mod.After(fs[j].mod) })
		var items [][]doc.Block
		for _, x := range fs {
			items = append(items, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: x.rel},
				{Text: "  " + x.mod.Format("2006-01-02") + " · " + Size(x.size) + "  ", Style: doc.Italic},
				p.link(Target+"/note?"+url.Values{"f": {x.rel}}.Encode(), "remove")}}})
		}
		p.heading(map[string]string{"Notes": "Notes", "Clippings": "Clippings", "Saved": "Saved pages"}[part])
		p.add(doc.List{Items: items})
	}
	if !any {
		p.italic("No notes, clippings or saved pages.")
	}
	p.add(doc.Rule{}, doc.Paragraph{Text: doc.Inline{p.link(Target, "← the Weeding Room")}})
	p.notice(note)
	return p.d
}

func (env Env) notePath(rel string) (string, error) {
	path := filepath.Join(env.NotesDir, filepath.FromSlash(rel))
	for _, part := range noteParts {
		if within(filepath.Join(env.NotesDir, part), path) && strings.EqualFold(filepath.Ext(path), ".md") && regular(path) {
			return path, nil
		}
	}
	return "", errors.New("no such note in the notes folder: " + rel)
}

func (env Env) note(rel string, sure bool) (*doc.Document, error) {
	path, err := env.notePath(rel)
	if err != nil {
		return nil, err
	}
	if !sure {
		p := newPage("Remove this note?", Target+"/note?"+url.Values{"f": {rel}}.Encode())
		p.ask([]string{rel + " (" + Size(fileSize(path)) + ")", "It leaves the notes folder, and the search no longer finds it."},
			Target+"/notes", Target+"/note?"+url.Values{"f": {rel}, "sure": {"yes"}}.Encode())
		return p.d, nil
	}
	if err := os.Remove(path); err != nil {
		return nil, err
	}
	if env.DB != nil {
		_ = env.DB.DeleteDocs(personal.FileURL(path))
	}
	return env.notes("Removed: " + rel), nil
}

func (env Env) books(note string) *doc.Document {
	p := newPage("Remove a book", Target+"/books")
	var bs []store.Book
	if env.DB != nil {
		bs, _ = env.DB.Books("author", 0)
	}
	var items [][]doc.Block
	for _, b := range bs {
		in := doc.Inline{{Text: b.Title}}
		sub := []string{}
		if b.Author != "" {
			sub = append(sub, b.Author)
		}
		sub = append(sub, strings.ToUpper(b.Format), Size(fileSize(b.Path)))
		in = append(in, doc.Span{Text: "  " + strings.Join(sub, " · ") + "  ", Style: doc.Italic}, p.link(fmt.Sprintf("%s/book?id=%d", Target, b.ID), "remove"))
		items = append(items, []doc.Block{doc.Paragraph{Text: in}})
	}
	if len(items) == 0 {
		p.italic("No books in The Stacks.")
	} else {
		p.add(doc.List{Items: items})
	}
	p.add(doc.Rule{}, doc.Paragraph{Text: doc.Inline{p.link(Target, "← the Weeding Room")}})
	p.notice(note)
	return p.d
}

func (env Env) book(id int64, sure bool) (*doc.Document, error) {
	if env.DB == nil {
		return nil, errors.New("no library database")
	}
	b, err := env.DB.Book(id)
	if err != nil {
		return nil, fmt.Errorf("no book %d in The Stacks", id)
	}
	if !within(env.BooksDir, b.Path) || !regular(b.Path) {
		return nil, errors.New("this book's file is not in the library folder; W5F removes only what lies there: " + b.Path)
	}
	if !sure {
		p := newPage("Remove this book?", fmt.Sprintf("%s/book?id=%d", Target, id))
		what := b.Title
		if b.Author != "" {
			what += " — " + b.Author
		}
		p.ask([]string{what, b.Path + " (" + Size(fileSize(b.Path)) + ")", "Its file is deleted, its place in The Stacks and its reading progress go, and the search no longer finds it."},
			Target+"/books", fmt.Sprintf("%s/book?id=%d&sure=yes", Target, id))
		return p.d, nil
	}
	if err := os.Remove(b.Path); err != nil {
		return nil, err
	}
	_ = env.DB.DeleteDocs(fmt.Sprintf("w5f:book/%d/", id))
	_ = env.DB.ForgetBook(id)
	return env.books("Removed: " + b.Title), nil
}

func (env Env) comics(note string) *doc.Document {
	p := newPage("Remove a comic", Target+"/comics")
	if env.RescanComics != nil {
		env.RescanComics()
	}
	var items [][]doc.Block
	if env.DB != nil {
		series, _ := env.DB.ComicSeriesList()
		for _, s := range series {
			cs, _ := env.DB.ComicsInSeries(s.Name)
			for _, c := range cs {
				if env.notAComic(c.Path) {
					continue
				}
				size := fileSize(c.Path)
				if st, err := os.Stat(c.Path); err == nil && st.IsDir() {
					size, _ = dirSize(c.Path)
				}
				where := ""
				if within(env.suwayomiDir(), c.Path) {
					where = " · downloaded by Suwayomi"
				}
				items = append(items, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: c.Series + " · " + c.Title},
					{Text: "  " + Size(size) + where + "  ", Style: doc.Italic}, p.link(fmt.Sprintf("%s/comic?id=%d", Target, c.ID), "remove")}}})
			}
		}
	}
	if len(items) == 0 {
		p.italic("No comics in The Picture Vault.")
	} else {
		p.add(doc.List{Items: items})
	}
	p.italic("A chapter Suwayomi downloaded is removed through Suwayomi, so it knows; the series stays followed.")
	p.add(doc.Rule{}, doc.Paragraph{Text: doc.Inline{p.link(Target, "← the Weeding Room")}})
	p.notice(note)
	return p.d
}

func (env Env) suwayomiDir() string { return filepath.Join(env.ComicsDir, "Suwayomi") }

// notAComic is what an older scan took for one: Suwayomi's covers.
func (env Env) notAComic(p string) bool {
	return filepath.Clean(p) == filepath.Join(env.suwayomiDir(), "thumbnails")
}

func (env Env) comic(ctx context.Context, id int64, sure bool) (*doc.Document, error) {
	if env.DB == nil {
		return nil, errors.New("no library database")
	}
	c, err := env.DB.Comic(id)
	if err != nil {
		return nil, fmt.Errorf("no comic %d in The Picture Vault", id)
	}
	st, err := os.Lstat(c.Path)
	inRoot := c.Path == env.ComicsDir // the loose images lying in the comics folder itself
	if err != nil || !(within(env.ComicsDir, c.Path) || inRoot) || st.Mode()&fs.ModeSymlink != 0 || env.notAComic(c.Path) {
		return nil, errors.New("this comic is not in the comics folder; W5F removes only what lies there: " + c.Path)
	}
	suwayomi := within(env.suwayomiDir(), c.Path)
	if !sure {
		p := newPage("Remove this comic?", fmt.Sprintf("%s/comic?id=%d", Target, id))
		what := []string{c.Series + " · " + c.Title, c.Path}
		switch {
		case suwayomi:
			what = append(what, "Suwayomi removes the chapter's file; the series stays followed, and the chapter can be downloaded again.")
		case st.IsDir():
			what = append(what, "Its pictures are deleted (only those in that folder, not what lies in folders below it), and its place in The Picture Vault goes.")
		default:
			what = append(what, "Its file is deleted, and its place in The Picture Vault goes.")
		}
		p.ask(what, Target+"/comics", fmt.Sprintf("%s/comic?id=%d&sure=yes", Target, id))
		return p.d, nil
	}
	switch {
	case suwayomi:
		if env.RemoveSuwayomi == nil {
			return nil, errors.New("Suwayomi is not set up here")
		}
		if err := env.RemoveSuwayomi(ctx, c.Path); err != nil {
			return nil, err
		}
	case st.IsDir():
		if err := removePictures(c.Path, c.Path != env.ComicsDir); err != nil {
			return nil, err
		}
	default:
		if err := os.Remove(c.Path); err != nil {
			return nil, err
		}
	}
	_ = env.DB.ForgetComic(id)
	return env.comics("Removed: " + c.Series + " · " + c.Title), nil
}

// pictureExts are what a folder comic is made of.
var pictureExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true, ".bmp": true}

// removePictures deletes a folder comic's pages (and its ComicInfo.xml):
// plain files directly in it, nothing deeper; the folder goes too when it
// is left empty and may go (never the comics folder itself).
func removePictures(dir string, folderToo bool) error {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		name := e.Name()
		if !e.Type().IsRegular() || !(pictureExts[strings.ToLower(filepath.Ext(name))] || strings.EqualFold(name, "ComicInfo.xml")) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	if folderToo {
		_ = os.Remove(dir) // only succeeds when empty
	}
	return nil
}
