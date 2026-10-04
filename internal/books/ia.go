package books

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math/rand/v2"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/store"
)

// The Internet Archive's texts, browsed from The Stacks (asked for by the
// owner, 2026-10-04): its collections and their sub-collections, sorted and
// filtered by language and period, searched with plain words or the
// Archive's own field syntax, and any public item read in W5F.

// IABase is the Internet Archive's address (tests use a fixture).
var IABase = "https://archive.org"

const (
	iaRows     = 50
	iaMaxFound = 10000 // the search API pages no further
)

// iaSorts are the orders a listing offers, in the order shown.
var iaSorts = []struct{ key, label, spec string }{
	{"downloads", "most read", "downloads desc"},
	{"week", "read this week", "week desc"},
	{"new", "newly added", "addeddate desc"},
	{"old", "oldest", "date asc"},
	{"recent", "newest", "date desc"},
	{"title", "title A–Z", "titleSorter asc"},
}

// iaLangs are the languages offered as filters; the Archive spells each a
// few ways.
var iaLangs = []struct{ key, label, query string }{
	{"en", "English", "English OR eng OR en"},
	{"tr", "Turkish", "Turkish OR tur OR tr"},
	{"ota", "Ottoman Turkish", "\"Ottoman Turkish\" OR ota"},
	{"fr", "French", "French OR fre OR fra OR fr"},
	{"de", "German", "German OR ger OR deu OR de"},
	{"la", "Latin", "Latin OR lat"},
	{"es", "Spanish", "Spanish OR spa OR es"},
	{"it", "Italian", "Italian OR ita OR it"},
	{"el", "Greek", "Greek OR gre OR ell OR grc"},
	{"ar", "Arabic", "Arabic OR ara"},
	{"fa", "Persian", "Persian OR per OR fas"},
	{"ru", "Russian", "Russian OR rus"},
}

// iaPeriods are the periods offered as filters.
var iaPeriods = []struct{ key, label, query string }{
	{"pre1600", "before 1600", "[0 TO 1599]"},
	{"1600", "1600s", "[1600 TO 1699]"},
	{"1700", "1700s", "[1700 TO 1799]"},
	{"1800", "1800s", "[1800 TO 1899]"},
	{"1900", "1900–1949", "[1900 TO 1949]"},
	{"1950", "1950–1999", "[1950 TO 1999]"},
	{"2000", "2000 on", "[2000 TO 2100]"},
}

var (
	reIAID      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	reIASpecial = regexp.MustCompile(`[():"\\\[\]{}^~*?!+\-&|/]+`)
	reTags      = regexp.MustCompile(`(?s)<[^>]*>`)
)

// iaView is what a listing shows: which collection, which words, in what
// order, filtered how, and which page.
type iaView struct {
	Coll, Q, Sort, Lang, Period string
	Colls                       bool // the collection's sub-collections, not its texts
	Lend                        bool // lending-library texts too (borrowed on the Archive, not downloaded)
	Page                        int
}

func iaViewOf(q url.Values) (iaView, error) {
	v := iaView{Coll: q.Get("c"), Q: strings.TrimSpace(q.Get("q")), Sort: q.Get("sort"), Lang: q.Get("lang"),
		Period: q.Get("y"), Colls: q.Get("show") == "coll", Lend: q.Get("lend") == "1"}
	v.Page, _ = strconv.Atoi(q.Get("page"))
	v.Page = max(v.Page, 1)
	if v.Coll != "" && !reIAID.MatchString(v.Coll) {
		return v, errors.New("not an Internet Archive collection name: " + v.Coll)
	}
	return v, nil
}

// href is the listing's address with some settings changed.
func (v iaView) href(change func(*iaView)) string {
	if change != nil {
		change(&v)
	}
	q := url.Values{}
	set := func(k, val string) {
		if val != "" {
			q.Set(k, val)
		}
	}
	set("c", v.Coll)
	set("q", v.Q)
	set("sort", v.Sort)
	set("lang", v.Lang)
	set("y", v.Period)
	if v.Colls {
		q.Set("show", "coll")
	}
	if v.Lend {
		q.Set("lend", "1")
	}
	if v.Page > 1 {
		q.Set("page", strconv.Itoa(v.Page))
	}
	if len(q) == 0 {
		return "w5f:books/ia"
	}
	return "w5f:books/ia?" + q.Encode()
}

func (v iaView) sortSpec() string {
	for _, s := range iaSorts {
		if s.key == v.Sort {
			return s.spec
		}
	}
	return iaSorts[0].spec
}

// query is the search for the view: words written with the Archive's field
// syntax (subject:alchemy, creator:"Blake, William") are passed as written.
func (v iaView) query() string {
	var parts []string
	if v.Colls {
		parts = append(parts, "mediatype:collection")
		parts = append(parts, "collection:("+orTexts(v.Coll)+")")
	} else {
		parts = append(parts, "mediatype:texts")
		if v.Coll != "" && v.Coll != "texts" {
			parts = append(parts, "collection:("+v.Coll+")")
		}
		if !v.Lend {
			parts = append(parts, "-collection:inlibrary", "-collection:printdisabled", "-collection:lendinglibrary")
		}
	}
	if v.Q != "" {
		if strings.ContainsAny(v.Q, ":\"") {
			parts = append(parts, "("+v.Q+")")
		} else if w := strings.Join(strings.Fields(reIASpecial.ReplaceAllString(v.Q, " ")), " "); w != "" {
			parts = append(parts, "("+w+")")
		}
	}
	for _, l := range iaLangs {
		if l.key == v.Lang {
			parts = append(parts, "language:("+l.query+")")
		}
	}
	for _, p := range iaPeriods {
		if p.key == v.Period {
			parts = append(parts, "year:"+p.query)
		}
	}
	return strings.Join(parts, " AND ")
}

func orTexts(c string) string {
	if c == "" {
		return "texts"
	}
	return c
}

type iaDoc struct {
	Identifier string          `json:"identifier"`
	Title      string          `json:"title"`
	Creator    json.RawMessage `json:"creator"`
	Year       json.RawMessage `json:"year"`
	Downloads  json.RawMessage `json:"downloads"`
	Mediatype  string          `json:"mediatype"`
}

type iaFound struct {
	Total int
	Docs  []iaDoc
}

func iaSearch(ctx context.Context, f *fetch.Fetcher, query, sort string, rows, page int) (iaFound, error) {
	v := url.Values{"q": {query}, "fl[]": {"identifier", "title", "creator", "year", "downloads", "mediatype"},
		"rows": {strconv.Itoa(rows)}, "page": {strconv.Itoa(page)}, "output": {"json"}}
	if sort != "" {
		v["sort[]"] = []string{sort}
	}
	u, _ := url.Parse(IABase + "/advancedsearch.php?" + v.Encode())
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return iaFound{}, err
	}
	var r struct {
		Error    string `json:"error"`
		Response struct {
			NumFound int     `json:"numFound"`
			Docs     []iaDoc `json:"docs"`
		} `json:"response"`
	}
	if err := json.Unmarshal(resp.Body, &r); err != nil {
		return iaFound{}, errors.New("unexpected answer from the Internet Archive search")
	}
	if r.Error != "" {
		return iaFound{}, errors.New("the Internet Archive could not search for that: " + r.Error)
	}
	return iaFound{Total: r.Response.NumFound, Docs: r.Response.Docs}, nil
}

// iaMeta is an item's (or a collection's) record.
type iaMeta struct {
	Metadata map[string]json.RawMessage `json:"metadata"`
	Files    []struct {
		Name    string          `json:"name"`
		Format  string          `json:"format"`
		Size    json.RawMessage `json:"size"`
		Private json.RawMessage `json:"private"`
	} `json:"files"`
}

func (m iaMeta) get(k string) string { return iaString(m.Metadata[k]) }

// list reads a field that is one value or several.
func (m iaMeta) list(k string) []string {
	raw := m.Metadata[k]
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	if s := iaString(raw); s != "" {
		return []string{s}
	}
	return nil
}

func iaMetadata(ctx context.Context, f *fetch.Fetcher, id string) (iaMeta, error) {
	u, _ := url.Parse(IABase + "/metadata/" + url.PathEscape(id))
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return iaMeta{}, err
	}
	var m iaMeta
	if err := json.Unmarshal(resp.Body, &m); err != nil {
		return iaMeta{}, errors.New("unexpected answer from the Internet Archive")
	}
	if len(m.Metadata) == 0 {
		return iaMeta{}, errors.New("the Internet Archive has no item " + id)
	}
	return m, nil
}

// iaString reads a JSON string, number or list of strings.
func iaString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return strings.Join(list, "; ")
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

// plain turns a description's HTML into one paragraph of text, at most n runes.
func plain(s string, n int) string {
	s = strings.NewReplacer("<br>", " ", "<br/>", " ", "<br />", " ", "</p>", " ").Replace(s)
	s = clean(html.UnescapeString(reTags.ReplaceAllString(s, " ")))
	if r := []rune(s); len(r) > n {
		s = strings.TrimSpace(string(r[:n])) + "…"
	}
	return s
}

func iaRoute(ctx context.Context, p string, q url.Values, env Env) (*doc.Document, error) {
	switch p {
	case "books/ia":
		v, err := iaViewOf(q)
		if err != nil {
			return nil, err
		}
		return iaListDoc(ctx, env, v)
	case "books/ia/random":
		v, err := iaViewOf(q)
		if err != nil {
			return nil, err
		}
		id, err := iaRandom(ctx, env.Fetcher, v)
		if err != nil {
			return nil, err
		}
		return iaItemDoc(ctx, env, id, "A random text from here · open “a random text” again for another")
	case "books/ia/item":
		id := q.Get("id")
		if !reIAID.MatchString(id) {
			return nil, errors.New("not an Internet Archive item: " + id)
		}
		return iaItemDoc(ctx, env, id, "")
	case "books/ia/get":
		id := q.Get("id")
		b, err := Download(ctx, env.Fetcher, env.DB, "ia:"+id+"/"+q.Get("f"))
		if err != nil {
			return nil, err
		}
		return chapterDoc(env, b, b.Chapter, b.Pos)
	}
	return nil, errors.New("unknown Internet Archive address")
}

// iaListDoc is a collection: its sub-collections first (on its first page),
// then its texts, with the orders and filters above them.
func iaListDoc(ctx context.Context, env Env, v iaView) (*doc.Document, error) {
	d := &doc.Document{Title: "Internet Archive — texts", URL: v.href(nil), Origin: "live", Lang: "en"}
	var desc string
	var parents []string
	if v.Coll != "" && v.Coll != "texts" {
		m, err := iaMetadata(ctx, env.Fetcher, v.Coll)
		if err != nil {
			return nil, err
		}
		if t := m.get("title"); t != "" {
			d.Title = t
		}
		desc = plain(m.get("description"), 600)
		parents = m.list("collection")
	}
	page := min(v.Page, iaMaxFound/iaRows)
	found, err := iaSearch(ctx, env.Fetcher, v.query(), v.sortSpec(), iaRows, page)
	if err != nil {
		return nil, err
	}

	// Where this is: the collections it belongs to, and the texts' front.
	where := doc.Inline{{Text: "in: ", Style: doc.Italic}}
	where = append(where, doc.Span{Text: "all texts", Link: link(d, "w5f:books/ia", "all texts")})
	for _, c := range parents {
		if c == "texts" || !reIAID.MatchString(c) {
			continue
		}
		where = append(where, doc.Span{Text: " · "}, doc.Span{Text: c, Link: link(d, "w5f:books/ia?c="+c, c)})
	}
	if v.Coll != "" {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: where})
	}
	if desc != "" {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: desc, Style: doc.Italic}}})
	}
	if v.Q != "" {
		d.Title += ": " + v.Q
	}

	// The orders and filters: the one in use is bold and not a link.
	choice := func(label string, on bool, href string) doc.Span {
		if on {
			return doc.Span{Text: label, Style: doc.Bold}
		}
		return doc.Span{Text: label, Link: link(d, href, label)}
	}
	row := func(name string, spans ...doc.Span) doc.Paragraph {
		in := doc.Inline{{Text: name + "  ", Style: doc.Italic}}
		for i, s := range spans {
			if i > 0 {
				in = append(in, doc.Span{Text: " · "})
			}
			in = append(in, s)
		}
		return doc.Paragraph{Text: in}
	}
	var sorts, langs, periods []doc.Span
	for i, s := range iaSorts {
		on := v.Sort == s.key || v.Sort == "" && i == 0
		sorts = append(sorts, choice(s.label, on, v.href(func(x *iaView) { x.Sort, x.Page = s.key, 1 })))
	}
	langs = append(langs, choice("any", v.Lang == "", v.href(func(x *iaView) { x.Lang, x.Page = "", 1 })))
	for _, l := range iaLangs {
		langs = append(langs, choice(l.label, v.Lang == l.key, v.href(func(x *iaView) { x.Lang, x.Page = l.key, 1 })))
	}
	periods = append(periods, choice("any", v.Period == "", v.href(func(x *iaView) { x.Period, x.Page = "", 1 })))
	for _, p := range iaPeriods {
		periods = append(periods, choice(p.label, v.Period == p.key, v.href(func(x *iaView) { x.Period, x.Page = p.key, 1 })))
	}
	show := []doc.Span{
		choice("texts", !v.Colls, v.href(func(x *iaView) { x.Colls, x.Page = false, 1 })),
		choice("collections in here", v.Colls, v.href(func(x *iaView) { x.Colls, x.Page = true, 1 })),
	}
	if !v.Colls {
		// A toggle: the lending library's texts are borrowed on the Archive.
		label := "with lending-library texts"
		if v.Lend {
			label = "public texts only"
		}
		show = append(show, doc.Span{Text: label, Link: link(d, v.href(func(x *iaView) { x.Lend, x.Page = !x.Lend, 1 }), label)})
	}
	d.Blocks = append(d.Blocks, row("show", show...), row("order", sorts...))
	if !v.Colls {
		d.Blocks = append(d.Blocks, row("language", langs...), row("period", periods...))
	}
	search := "search: g → ia <words>"
	if v.Coll != "" {
		search = "search here: g → ia @" + v.Coll + " <words>"
	}
	tools := doc.Inline{{Text: search, Style: doc.Italic}}
	if !v.Colls && found.Total > 0 {
		tools = append(tools, doc.Span{Text: "   "}, doc.Span{Text: "a random text from here",
			Link: link(d, strings.Replace(v.href(func(x *iaView) { x.Page = 1 }), "w5f:books/ia", "w5f:books/ia/random", 1), "random")})
	}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: tools}, doc.Rule{})

	// Sub-collections lead the first page of a collection's texts.
	if !v.Colls && v.Page == 1 && v.Q == "" && v.Lang == "" && v.Period == "" {
		sub, err := iaSearch(ctx, env.Fetcher, iaView{Coll: v.Coll, Colls: true}.query(), "downloads desc", 12, 1)
		if err == nil && len(sub.Docs) > 0 {
			var items [][]doc.Block
			for _, c := range sub.Docs {
				items = append(items, []doc.Block{doc.Paragraph{Text: doc.Inline{
					{Text: iaTitle(c), Link: link(d, "w5f:books/ia?c="+c.Identifier, iaTitle(c))}}}})
			}
			if sub.Total > len(sub.Docs) {
				more := fmt.Sprintf("all %d collections in here ›", sub.Total)
				items = append(items, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: more, Link: link(d, v.href(func(x *iaView) { x.Colls = true }), more)}}}})
			}
			d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Collections"}}}, doc.List{Items: items})
		}
	}

	heading := "Texts"
	if v.Colls {
		heading = "Collections"
	}
	from := (page-1)*iaRows + 1
	switch {
	case found.Total == 0:
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: heading}}},
			doc.Paragraph{Text: doc.Inline{{Text: "Nothing here with these settings.", Style: doc.Italic}}})
	default:
		heading += fmt.Sprintf(" — %d–%d of %d", from, from+len(found.Docs)-1, found.Total)
		var items [][]doc.Block
		for _, it := range found.Docs {
			href := "w5f:books/ia/item?id=" + url.QueryEscape(it.Identifier)
			if v.Colls || it.Mediatype == "collection" {
				href = "w5f:books/ia?c=" + url.QueryEscape(it.Identifier)
			}
			in := doc.Inline{{Text: iaTitle(it), Style: doc.Bold, Link: link(d, href, iaTitle(it))}}
			var sub []string
			if c := iaString(it.Creator); c != "" {
				sub = append(sub, c)
			}
			if y := iaString(it.Year); y != "" {
				sub = append(sub, y)
			}
			if n := iaString(it.Downloads); n != "" {
				sub = append(sub, n+" reads")
			}
			if len(sub) > 0 {
				in = append(in, doc.Span{Text: "  · " + strings.Join(sub, " · "), Style: doc.Italic})
			}
			items = append(items, []doc.Block{doc.Paragraph{Text: in}})
		}
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: heading}}}, doc.List{Ordered: true, Items: items})
	}
	var nav doc.Inline
	if page > 1 {
		d.Prev = v.href(func(x *iaView) { x.Page = page - 1 })
		nav = append(nav, doc.Span{Text: "‹ previous page", Link: link(d, d.Prev, "previous page")}, doc.Span{Text: "   "})
	}
	if from+len(found.Docs)-1 < found.Total {
		if page < iaMaxFound/iaRows {
			d.Next = v.href(func(x *iaView) { x.Page = page + 1 })
			nav = append(nav, doc.Span{Text: "next page ›", Link: link(d, d.Next, "next page")})
		} else {
			nav = append(nav, doc.Span{Text: "the Archive lists no further: narrow it with a language, a period or words", Style: doc.Italic})
		}
	}
	if len(nav) > 0 {
		d.Blocks = append(d.Blocks, doc.Rule{}, doc.Paragraph{Text: nav})
	}
	return d, nil
}

func iaTitle(it iaDoc) string {
	if t := clean(it.Title); t != "" {
		return t
	}
	return it.Identifier
}

// iaRandom picks a text of the view at random: the count first, then one
// row of it.
func iaRandom(ctx context.Context, f *fetch.Fetcher, v iaView) (string, error) {
	v.Colls = false
	q := v.query()
	all, err := iaSearch(ctx, f, q, "", 1, 1)
	if err != nil {
		return "", err
	}
	if all.Total == 0 {
		return "", errors.New("no texts here to pick from")
	}
	n := rand.IntN(min(all.Total, iaMaxFound)) + 1
	one, err := iaSearch(ctx, f, q, "identifier asc", 1, n)
	if err != nil {
		return "", err
	}
	if len(one.Docs) == 0 {
		return "", errors.New("the Internet Archive lost its place; try again")
	}
	return one.Docs[0].Identifier, nil
}

// iaFile is an item file W5F can keep in the library.
type iaFile struct{ Name, Format, Label string }

// iaFiles are the item's public files a book can be made of, best first:
// EPUB, then the text PDFs, other ebook formats, and the full text.
func iaFiles(m iaMeta) []iaFile {
	var out []iaFile
	rank := map[string]int{"epub": 0, "pdf": 1, "mobi": 2, "azw3": 2, "fb2": 2, "txt": 3}
	for _, fl := range m.Files {
		if iaString(fl.Private) == "true" {
			continue
		}
		low := strings.ToLower(fl.Name)
		var format, label string
		switch {
		case strings.HasSuffix(low, "_djvu.txt"):
			format, label = "txt", "full text (read from the scans)"
		case strings.HasSuffix(low, ".epub"):
			format = "epub"
		case strings.HasSuffix(low, ".pdf"):
			format = "pdf"
		case strings.HasSuffix(low, ".txt"):
			format = "txt"
		case strings.HasSuffix(low, ".mobi"):
			format = "mobi"
		case strings.HasSuffix(low, ".azw3"):
			format = "azw3"
		case strings.HasSuffix(low, ".fb2"):
			format = "fb2"
		default:
			continue
		}
		if label == "" {
			label = fl.Name
			if fl.Format != "" {
				label += " — " + fl.Format
			}
		}
		if n, err := strconv.ParseInt(iaString(fl.Size), 10, 64); err == nil && n > 0 {
			label += fmt.Sprintf(" (%.1f MB)", float64(n)/(1<<20))
		}
		out = append(out, iaFile{Name: fl.Name, Format: format, Label: label})
	}
	sortStable(out, func(a, b iaFile) bool { return rank[a.Format] < rank[b.Format] })
	return out
}

func sortStable[T any](xs []T, less func(a, b T) bool) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && less(xs[j], xs[j-1]); j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

// iaItemDoc is an item: what it is, where it belongs, and its files to read.
func iaItemDoc(ctx context.Context, env Env, id, note string) (*doc.Document, error) {
	m, err := iaMetadata(ctx, env.Fetcher, id)
	if err != nil {
		return nil, err
	}
	if m.get("mediatype") == "collection" {
		return iaListDoc(ctx, env, iaView{Coll: id, Page: 1})
	}
	title := clean(m.get("title"))
	if title == "" {
		title = id
	}
	d := &doc.Document{Title: title, URL: "w5f:books/ia/item?id=" + url.QueryEscape(id), Origin: "live", Lang: "en"}
	var meta []string
	for _, k := range []string{"creator", "date", "publisher", "language"} {
		if s := clean(strings.Join(m.list(k), "; ")); s != "" {
			meta = append(meta, s)
		}
	}
	if n := m.get("imagecount"); n != "" {
		meta = append(meta, n+" pages")
	}
	if len(meta) > 0 {
		d.Meta = []doc.KV{{Key: "·", Value: strings.Join(meta, " · ")}}
	}
	if note != "" {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: note})
	}
	for _, f := range iaFiles(m) {
		if b, ok := env.DB.BookBySource("ia:" + id + "/" + f.Name); ok {
			d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "✓ in your library — ", Style: doc.Bold},
				{Text: "open it", Link: link(d, bookHref(b), "open")}}})
			break
		}
	}
	if s := plain(m.get("description"), 1500); s != "" {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: s}}})
	}
	tagRow := func(name string, values []string, href func(string) string) {
		in := doc.Inline{{Text: name + "  ", Style: doc.Italic}}
		n := 0
		for _, val := range values {
			val = clean(val)
			h := href(val)
			if val == "" || h == "" {
				continue
			}
			if n > 0 {
				in = append(in, doc.Span{Text: " · "})
			}
			in = append(in, doc.Span{Text: val, Link: link(d, h, val)})
			if n++; n == 20 {
				break
			}
		}
		if n > 0 {
			d.Blocks = append(d.Blocks, doc.Paragraph{Text: in})
		}
	}
	var subjects []string
	for _, s := range m.list("subject") {
		subjects = append(subjects, strings.Split(s, ";")...)
	}
	tagRow("subjects", subjects, func(s string) string {
		return "w5f:books/ia?" + url.Values{"q": {`subject:"` + strings.ReplaceAll(s, `"`, "") + `"`}}.Encode()
	})
	tagRow("collections", m.list("collection"), func(c string) string {
		if !reIAID.MatchString(c) {
			return ""
		}
		return "w5f:books/ia?c=" + c
	})
	if cs := m.list("creator"); len(cs) > 0 {
		tagRow("more by", cs, func(c string) string {
			return "w5f:books/ia?" + url.Values{"q": {`creator:"` + strings.ReplaceAll(c, `"`, "") + `"`}}.Encode()
		})
	}
	d.Blocks = append(d.Blocks, doc.Rule{})
	files := iaFiles(m)
	switch {
	case m.get("access-restricted-item") == "true":
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: "This is a lending-library text: it can be borrowed on the Internet Archive's site, not downloaded."})
	case len(files) == 0:
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: "This item has no public file W5F can read."})
	default:
		var items [][]doc.Block
		for _, f := range files {
			get := "w5f:books/ia/get?" + url.Values{"id": {id}, "f": {f.Name}}.Encode()
			label := strings.ToUpper(f.Format) + " — " + f.Label
			items = append(items, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: label, Link: link(d, get, label)}}}})
		}
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: "Read it (it is kept in your library)"}}}, doc.List{Items: items})
	}
	page := IABase + "/details/" + url.PathEscape(id)
	d.Blocks = append(d.Blocks, doc.Rule{}, doc.Paragraph{Text: doc.Inline{{Text: "→ the item on archive.org", Link: link(d, page, "archive.org")}}})
	return d, nil
}

// downloadIA keeps an item file in the library (src is ia:<id>/<file>).
func downloadIA(ctx context.Context, f *fetch.Fetcher, db *store.DB, src string) (store.Book, error) {
	id, name, ok := strings.Cut(strings.TrimPrefix(src, "ia:"), "/")
	if !ok || !reIAID.MatchString(id) || name == "" {
		return store.Book{}, errors.New("bad Internet Archive file: " + src)
	}
	m, err := iaMetadata(ctx, f, id)
	if err != nil {
		return store.Book{}, err
	}
	if m.get("access-restricted-item") == "true" {
		return store.Book{}, errors.New("this is a lending-library text: it can be borrowed on archive.org, not downloaded")
	}
	var file *iaFile
	for _, x := range iaFiles(m) {
		if x.Name == name {
			file = &x
			break
		}
	}
	if file == nil {
		return store.Book{}, errors.New("the item has no public file " + name)
	}
	segs := strings.Split(name, "/")
	for i := range segs {
		segs[i] = url.PathEscape(segs[i])
	}
	dir := LibraryDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return store.Book{}, err
	}
	tmp, err := os.CreateTemp(dir, ".download-*."+file.Format)
	if err != nil {
		return store.Book{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	err = getTo(ctx, f.UserAgent, IABase+"/download/"+url.PathEscape(id)+"/"+strings.Join(segs, "/"), tmp)
	tmp.Close()
	if err != nil {
		return store.Book{}, err
	}
	author := m.list("creator")
	first := ""
	if len(author) > 0 {
		first = author[0]
	}
	return AddFile(db, tmpName, file.Format, clean(m.get("title")), first, src)
}
