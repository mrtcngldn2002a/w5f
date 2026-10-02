// Package source resolves a target (URL, file path, shorthand or w5f: address)
// into a Document. It owns the adapters' shared plumbing: the cached fetcher,
// embedded-block resolution and the archive fallback for dead pages.
package source

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"w5f/internal/books"
	"w5f/internal/comics"
	"w5f/internal/comics/suwayomi"
	"w5f/internal/config"
	"w5f/internal/crom"
	"w5f/internal/dict"
	"w5f/internal/discover"
	"w5f/internal/doc"
	"w5f/internal/feeds"
	"w5f/internal/fetch"
	"w5f/internal/fiction"
	"w5f/internal/htmlconv"
	"w5f/internal/index"
	"w5f/internal/personal"
	"w5f/internal/reddit"
	"w5f/internal/search"
	"w5f/internal/sitecat"
	"w5f/internal/smallweb"
	"w5f/internal/solo"
	"w5f/internal/store"
	"w5f/internal/ultan"
	"w5f/internal/usenet"
	"w5f/internal/weeding"
)

// Version is reported in the User-Agent.
var Version = "0.0.0-dev"

// Fetcher is the shared HTTP client. Init replaces it with a cached one; the
// default (no disk cache) keeps tests and `dump` self-contained.
var Fetcher = newFetcher("", Version)

func newFetcher(cacheDir, version string) *fetch.Fetcher {
	f := fetch.New(cacheDir, version)
	f.SolverURL = config.SolverURL()
	return f
}

// Init configures the shared fetcher.
func Init(cacheDir string, offline bool) {
	Fetcher = newFetcher(cacheDir, Version)
	Fetcher.Offline = offline
}

// WaybackAPI is the Internet Archive availability endpoint (variable for tests).
var WaybackAPI = "https://archive.org/wayback/available"

// upgradeSnapshots rewrites snapshot URLs to https (disabled in tests).
var upgradeSnapshots = true

// minEmbedText is the amount of readable text (non-space characters) an
// embedded block needs before it is worth inlining. Wikidot pages often embed
// blocks that only carry scripts or styling.
const minEmbedText = 40

// Options tune a load.
type Options struct {
	Reload bool // ask the server again even if the cached copy is fresh
	hops   int  // meta refreshes followed so far
}

// Load resolves and converts target.
func Load(ctx context.Context, target string, opts Options) (*doc.Document, error) {
	target = strings.TrimSpace(target)
	if strings.HasPrefix(target, "w5f:random/") {
		return loadRandom(ctx, strings.TrimPrefix(target, "w5f:random/"))
	}
	if strings.HasPrefix(target, "w5f:search/") {
		return loadSearch(ctx, target)
	}
	if target == ultan.LedgerTarget {
		db, err := store.Default()
		if err != nil {
			return nil, fmt.Errorf("opening the local database: %w", err)
		}
		return ultan.Ledger(db, time.Now())
	}
	if weeding.IsTarget(target) {
		env, err := WeedingEnv()
		if err != nil {
			return nil, err
		}
		return weeding.Route(ctx, target, env)
	}
	if personal.IsTarget(target) || index.IsTarget(target) {
		db, err := store.Default()
		if err != nil {
			return nil, fmt.Errorf("opening the local database: %w", err)
		}
		if index.IsTarget(target) {
			return index.Route(target, db)
		}
		return personal.Route(target, db)
	}
	if sitecat.IsTarget(target) {
		db, err := store.Default()
		if err != nil {
			return nil, fmt.Errorf("opening the local database: %w", err)
		}
		benv := booksEnv(db)
		return sitecat.Route(ctx, target, sitecat.Env{Fetcher: Fetcher, DB: db, Path: sitecat.Path(),
			OpenBook: func(ctx context.Context, b store.Book) (*doc.Document, error) {
				return books.Route(ctx, fmt.Sprintf("w5f:book/%d", b.ID), benv)
			}})
	}
	if books.IsTarget(target) {
		db, err := store.Default()
		if err != nil {
			return nil, fmt.Errorf("opening the local database: %w", err)
		}
		return books.Route(ctx, target, booksEnv(db))
	}
	if feeds.IsTarget(target) {
		env, err := FeedsEnv()
		if err != nil {
			return nil, err
		}
		return feeds.Route(ctx, target, env)
	}
	if solo.IsTarget(target) {
		env, err := SoloEnv()
		if err != nil {
			return nil, err
		}
		return solo.Route(ctx, target, env)
	}
	if usenet.IsTarget(target) {
		env, err := UsenetEnv()
		if err != nil {
			return nil, err
		}
		return usenet.Route(ctx, target, env)
	}
	if comics.IsTarget(target) {
		env, err := ComicsEnv()
		if err != nil {
			return nil, err
		}
		return comics.Route(ctx, target, env)
	}
	if fiction.IsTarget(target) {
		env, err := FictionEnv()
		if err != nil {
			return nil, err
		}
		return fiction.Route(ctx, target, env)
	}
	if target == MarginaliaRandom {
		return loadMarginaliaRandom(ctx)
	}
	if smallweb.IsTarget(target) {
		return smallweb.Load(ctx, SmallwebEnv(), target)
	}
	if discover.IsTarget(target) {
		env, err := DiscoverEnv()
		if err != nil {
			return nil, err
		}
		return discover.Route(ctx, target, env)
	}
	var d *doc.Document
	var err error
	if u, perr := url.Parse(target); perr == nil && (u.Scheme == "http" || u.Scheme == "https") {
		// Serial sites (Royal Road, AO3, forum threads with threadmarks…)
		// open inside their serial so ] and [ work at once.
		if env, eerr := FictionEnv(); eerr == nil {
			if _, _, ok := fiction.Match(Normalize(u)); ok || fiction.KnownChapter(env.DB, Normalize(u)) {
				d, err := fiction.OpenURL(ctx, env, Normalize(u))
				if !fiction.IsNotSerial(err) {
					return d, err
				}
			}
			// AO3's menus and work lists (home, fandoms, tags, searches).
			if fiction.IsAO3(Normalize(u)) {
				d, err := fiction.AO3Page(ctx, env, Normalize(u))
				if !fiction.IsNotAO3Page(err) {
					return d, err
				}
			}
		}
		d, err = loadURL(ctx, Normalize(u), opts)
		if err == nil {
			resolveEmbeds(ctx, d, !Fetcher.Offline)
		}
	} else {
		d, err = loadFile(target)
		if err == nil {
			resolveEmbeds(ctx, d, false)
		}
	}
	if err != nil {
		return nil, err
	}
	if doc.TextLength(d.Blocks) == 0 && d.Collapsibles == 0 && !hasNotice(d.Blocks) {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: "No readable text was found on this page. Press ← to go back."})
	}
	return d, nil
}

// userPath turns a typed file name into an absolute path: quotes around it
// are dropped and ~ is the home folder.
func userPath(p string) string {
	p = strings.Trim(strings.TrimSpace(p), `"'`)
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if h, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(h, p[1:])
		}
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// hasNotice reports a page that already says what it is (a picture, say).
func hasNotice(bs []doc.Block) bool {
	for _, b := range bs {
		if _, ok := b.(doc.Notice); ok {
			return true
		}
	}
	return false
}

var (
	reSCP     = regexp.MustCompile(`(?i)^scp-\d{3,4}(-j|-ex|-arc|-d)?$`)
	reBareNum = regexp.MustCompile(`^\d{3,4}$`)
	reSub     = regexp.MustCompile(`^/?(r|u|user)/[A-Za-z0-9_\-]+$`)
)

// Resolve expands what the user typed at the "go to" prompt into a target:
//
//	https://…, domain.tld      open the address
//	scp-173, 173               SCP Wiki shorthand
//	w <words>                  Wikipedia (jumps straight to an exact match)
//	scp <words>                search the SCP Wiki, Wanderers' Library, Backrooms
//	r/name, reddit <words>     a subreddit, or a Reddit search
//	gut <words>, se <words>    search Project Gutenberg / Standard Ebooks
//	books, feeds               the Library / Periodicals
//	?<words> or several words  web search
//	a path to a file           open the file
func Resolve(input string) string {
	s := strings.TrimSpace(input)
	lower := strings.ToLower(s)
	switch {
	case s == "":
		return ""
	case strings.HasPrefix(lower, "catalog-add ") && len(strings.Fields(s)) > 1:
		fields := strings.Fields(s[len("catalog-add "):])
		v := url.Values{"url": {fields[0]}}
		if len(fields) > 1 {
			v.Set("w", strings.Join(fields[1:], " "))
		}
		return "w5f:catalog/check?" + v.Encode()
	case lower == "catalogs":
		return "w5f:catalogs"
	case lower == "solo", lower == "solo rpg", lower == "oracle":
		return "w5f:solo"
	case strings.HasPrefix(lower, "roll ") && isDice(s[len("roll "):]):
		return "w5f:solo/roll?" + url.Values{"d": {strings.TrimSpace(s[len("roll "):])}}.Encode()
	case strings.HasPrefix(lower, "ask ") && askTarget(s[len("ask "):]) != "":
		return askTarget(s[len("ask "):])
	case lower == "spark":
		return "w5f:solo/spark/words"
	case strings.HasPrefix(lower, "spark ") && isSpark(strings.TrimSpace(lower[len("spark "):])):
		return "w5f:solo/spark/" + strings.TrimSpace(lower[len("spark "):])
	case strings.HasPrefix(lower, "npc "), strings.HasPrefix(lower, "thread "), strings.HasPrefix(lower, "counter ") && isCounter(s[len("counter "):]):
		// The table's lists: g → npc Name — a note, g → thread …, g → counter
		// Health 5/5 (with a number, so "counter strike" stays a search).
		word, rest, _ := strings.Cut(s, " ")
		list := map[string]string{"npc": "character", "thread": "thread", "counter": "counter"}[strings.ToLower(word)]
		return "w5f:solo/add/" + list + "?" + url.Values{"q": {strings.TrimSpace(rest)}}.Encode()
	case lower == "pick npc", lower == "pick character", lower == "pick thread":
		if strings.HasSuffix(lower, "thread") {
			return "w5f:solo/pick/thread"
		}
		return "w5f:solo/pick/character"
	case lower == "usenet", lower == "news":
		return "w5f:usenet"
	case strings.HasPrefix(lower, "usenet ") && len(strings.Fields(s)) > 1:
		return "w5f:usenet/find?" + url.Values{"q": {strings.TrimSpace(s[len("usenet "):])}}.Encode()
	case strings.HasPrefix(lower, "news:") && usenet.ValidGroup(strings.TrimPrefix(lower, "news:")):
		return "w5f:usenet/g/" + strings.TrimPrefix(lower, "news:")
	case lower == "tarot":
		return "w5f:discover/tarot"
	case lower == "iching", lower == "i ching", lower == "i-ching":
		return "w5f:discover/iching"
	case lower == "almanac", lower == "on this day":
		return "w5f:almanac"
	case strings.HasPrefix(lower, "opml-import ") && len(strings.Fields(s)) > 1:
		return "w5f:feeds/import?" + url.Values{"f": {userPath(s[len("opml-import "):])}}.Encode()
	case lower == "opml-export":
		return "w5f:feeds/export"
	case strings.HasPrefix(lower, "opml-export "):
		return "w5f:feeds/export?" + url.Values{"f": {userPath(s[len("opml-export "):])}}.Encode()
	case strings.HasPrefix(lower, "cat ") && isCatalogID(strings.Fields(s)[1]):
		fields := strings.SplitN(strings.TrimSpace(s[4:]), " ", 2)
		if len(fields) == 1 {
			return "w5f:catalog/" + fields[0]
		}
		return "w5f:catalog/" + fields[0] + "?" + url.Values{"q": {strings.TrimSpace(fields[1])}}.Encode()
	case strings.HasPrefix(s, "w5f:"), strings.Contains(s, "://"):
		return s
	case strings.HasPrefix(s, "?"):
		return searchTarget("web", strings.TrimSpace(s[1:]))
	case strings.HasPrefix(lower, "w "), strings.HasPrefix(lower, "wiki "):
		q := strings.TrimSpace(s[strings.Index(s, " "):])
		return "https://en.wikipedia.org/w/index.php?" + url.Values{"search": {q}, "title": {"Special:Search"}, "go": {"Go"}}.Encode()
	case strings.HasPrefix(lower, "scp "):
		return searchTarget("wikis", strings.TrimSpace(s[4:]))
	case strings.HasPrefix(lower, "gut "), strings.HasPrefix(lower, "gutenberg "):
		return "w5f:books/gutenberg?" + url.Values{"q": {strings.TrimSpace(s[strings.Index(s, " "):])}}.Encode()
	case strings.HasPrefix(lower, "se "):
		return "w5f:books/se?" + url.Values{"q": {strings.TrimSpace(s[3:])}}.Encode()
	case lower == "libgen" || lower == "lg":
		return "w5f:books/libgen"
	case lower == "libgen-status":
		return "w5f:books/libgen/status"
	case strings.HasPrefix(lower, "libgen-md5 "):
		return "w5f:books/libgen/item?" + url.Values{"md5": {strings.TrimSpace(s[11:])}}.Encode()
	case strings.HasPrefix(lower, "libgen-link "):
		return "w5f:books/libgen/link?" + url.Values{"md5": {strings.TrimSpace(s[12:])}}.Encode()
	case strings.HasPrefix(lower, "libgen "), strings.HasPrefix(lower, "lg "):
		return "w5f:books/libgen?" + url.Values{"q": {strings.TrimSpace(s[strings.Index(s, " "):])}}.Encode()
	case lower == "books" || lower == "library":
		return "w5f:books"
	case lower == "fiction" || lower == "internet fiction":
		return "w5f:fiction"
	case lower == "following":
		return "w5f:following"
	case lower == "comics" || lower == "manga" || lower == "comic":
		return "w5f:comics"
	case lower == "smallweb" || lower == "small web" || lower == "gemini" || lower == "gopher":
		return "w5f:smallweb"
	case lower == "worlds" || lower == "archived worlds":
		return "w5f:worlds"
	case lower == "packet" || lower == "daily" || lower == "daily packet":
		return "w5f:packet"
	case lower == "x" || lower == "deep random":
		return "w5f:discover/random"
	case strings.HasPrefix(lower, "serial ") && len(strings.Fields(s)) > 1:
		return "w5f:serial/open?" + url.Values{"u": {withScheme(strings.TrimSpace(s[7:]))}}.Encode()
	case strings.HasPrefix(lower, "rr "):
		return "w5f:fiction/rr/search?" + url.Values{"q": {strings.TrimSpace(s[3:])}}.Encode()
	case strings.HasPrefix(lower, "ao3 "):
		return "w5f:fiction/ao3/search?" + url.Values{"q": {strings.TrimSpace(s[4:])}}.Encode()
	case lower == "ffr":
		return "w5f:fiction/ffr"
	case strings.HasPrefix(lower, "ffr "):
		return "w5f:fiction/ffr?" + url.Values{"u": {withScheme(strings.TrimSpace(s[4:]))}}.Encode()
	case lower == "feeds" || lower == "periodicals":
		return "w5f:feeds"
	case lower == "queue":
		return "w5f:queue"
	case lower == "notes" || lower == "clippings":
		return "w5f:notes"
	case lower == "history":
		return "w5f:history"
	case lower == "weeding" || lower == "weeding room":
		return "w5f:weeding"
	case lower == "ledger" || lower == "ultan":
		return ultan.LedgerTarget
	case strings.HasPrefix(lower, "find "):
		return "w5f:find?" + url.Values{"q": {strings.TrimSpace(s[5:])}}.Encode()
	case strings.HasPrefix(lower, "reddit "):
		return "https://www.reddit.com/search/?" + url.Values{"q": {strings.TrimSpace(s[7:])}}.Encode()
	case reSub.MatchString(s):
		return "https://www.reddit.com/" + strings.TrimPrefix(s, "/") + "/"
	case lower == "reddit":
		return "https://www.reddit.com/"
	case reSCP.MatchString(s):
		return "https://scp-wiki.wikidot.com/" + lower
	case reBareNum.MatchString(s):
		n := s
		for len(n) < 3 {
			n = "0" + n
		}
		return "https://scp-wiki.wikidot.com/scp-" + n
	}
	if _, err := os.Stat(s); err == nil {
		return s
	}
	if !strings.ContainsAny(s, " \\") && strings.Contains(s, ".") {
		return "https://" + s
	}
	return searchTarget("web", s)
}

// withScheme adds https:// to a bare address.
func withScheme(s string) string {
	if strings.Contains(s, "://") {
		return s
	}
	return "https://" + s
}

// FictionEnv is what Internet Fiction pages need; the mature forums show
// when config.toml has `mature = true` under [fiction].
func FictionEnv() (fiction.Env, error) {
	db, err := store.Default()
	if err != nil {
		return fiction.Env{}, fmt.Errorf("opening the local database: %w", err)
	}
	cfg := config.Load()
	fiction.SetRedditLister(func(ctx context.Context, author string, maxPages int, stop func(reddit.PostInfo) bool) ([]reddit.PostInfo, error) {
		return reddit.Submitted(ctx, Fetcher, author, maxPages, stop)
	})
	return fiction.Env{Fetcher: Fetcher, DB: db, Mature: cfg.Fiction.Mature, Downloads: config.Downloads(),
		Reddit: func(ctx context.Context, u *url.URL) (*doc.Document, error) {
			return reddit.Load(ctx, Fetcher, u, false)
		}}, nil
}

// ComicsServer is the Suwayomi server W5F starts for Comics: its jar and
// data in <data>/suwayomi, downloads and the Local source inside the Comics
// folder so the library sees them.
func ComicsServer() suwayomi.Server {
	root := comics.Root()
	return suwayomi.Server{Dir: filepath.Join(store.DataDir(), "suwayomi"), Java: config.Load().Comics.Java, Solver: config.SolverURL(),
		Downloads: filepath.Join(root, "Suwayomi"), Local: filepath.Join(root, "Local")}
}

// WeedingEnv is what the Weeding Room needs.
func WeedingEnv() (weeding.Env, error) {
	db, err := store.Default()
	if err != nil {
		return weeding.Env{}, fmt.Errorf("opening the local database: %w", err)
	}
	cenv := comics.Env{DB: db, Root: comics.Root(), Server: ComicsServer()}
	return weeding.Env{DB: db, CacheDir: Fetcher.CacheDir, DataDir: store.DataDir(), NotesDir: personal.Dir(),
		BooksDir: books.LibraryDir(), ComicsDir: comics.Root(), CacheLimit: config.CacheLimit(),
		RemoveSuwayomi: cenv.RemoveDownloaded, RescanComics: func() { comics.Scan(db, comics.Root()) }}, nil
}

// ComicsEnv is what the Comics pages need.
// SoloEnv assembles what the solo RPG table needs.
func SoloEnv() (solo.Env, error) {
	db, err := store.Default()
	if err != nil {
		return solo.Env{}, fmt.Errorf("opening the local database: %w", err)
	}
	return solo.Env{DB: db, Fetcher: Fetcher, Notes: personal.Dir(),
		LogDir: filepath.Join(store.DataDir(), "solo", "log"),
		Dict:   func() (*dict.Dict, error) { return dict.Shared(dict.Dir(store.DataDir())) }}, nil
}

// UsenetEnv assembles what the Usenet pages need.
func UsenetEnv() (usenet.Env, error) {
	db, err := store.Default()
	if err != nil {
		return usenet.Env{}, fmt.Errorf("opening the local database: %w", err)
	}
	return usenet.Env{DB: db, ConfigPath: filepath.Join(store.DataDir(), "usenet.toml")}, nil
}

func ComicsEnv() (comics.Env, error) {
	db, err := store.Default()
	if err != nil {
		return comics.Env{}, fmt.Errorf("opening the local database: %w", err)
	}
	return comics.Env{DB: db, Root: comics.Root(), Server: ComicsServer(), View: OpenComic}, nil
}

// OpenComic starts the comics viewer (w5f view) in its own window; without
// a display, a local file goes to the system viewer.
func OpenComic(v comics.ViewRequest) error {
	args := []string{"view"}
	switch {
	case v.ChapterID > 0:
		args = append(args, "--chapter", strconv.Itoa(v.ChapterID))
	case v.ComicID > 0:
		args = append(args, "--comic", strconv.FormatInt(v.ComicID, 10), v.Path)
	default:
		args = append(args, v.Path)
	}
	if runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" {
		if v.Path != "" {
			return books.OpenExternal(v.Path)
		}
		return errors.New("the comics viewer needs X (it runs in the W5F session, not over SSH or on the console)")
	}
	if runtime.GOOS != "linux" {
		if v.Path != "" {
			return books.OpenExternal(v.Path)
		}
		return errors.New("reading Suwayomi chapters needs the W5F viewer, which runs on Linux with X")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, args...)
	return cmd.Start()
}

// SmallwebEnv is where Gemini's known hosts and the small web cache live.
func SmallwebEnv() smallweb.Env {
	return smallweb.Env{DataDir: store.DataDir(), CacheDir: Fetcher.CacheDir, Offline: Fetcher.Offline}
}

// DiscoverEnv is what Deep Random and the Daily Packet need.
func DiscoverEnv() (discover.Env, error) {
	db, err := store.Default()
	if err != nil {
		return discover.Env{}, fmt.Errorf("opening the local database: %w", err)
	}
	env := discover.Env{Fetcher: Fetcher, DB: db,
		Load: func(ctx context.Context, target string) (*doc.Document, error) { return Load(ctx, target, Options{}) },
		Crom: func(ctx context.Context, preset string) (string, error) {
			p, ok := crom.Presets[preset]
			if !ok {
				return "", fmt.Errorf("unknown preset %s", preset)
			}
			return crom.Random(ctx, Fetcher, p)
		},
		Smallweb: SmallwebEnv(),
	}
	if reddit.LoadSession() != "" {
		env.RedditTop = func(ctx context.Context, sub string) ([]string, error) {
			u, _ := url.Parse("https://www.reddit.com/r/" + sub + "/top/?t=all")
			d, err := reddit.Load(ctx, Fetcher, u, false)
			if err != nil {
				return nil, err
			}
			var out []string
			for _, l := range d.Links {
				if strings.Contains(l.Href, "/r/"+sub+"/comments/") && !strings.Contains(l.Href, "#") {
					out = append(out, l.Href)
				}
			}
			return out, nil
		}
	}
	return env, nil
}

func searchTarget(kind, q string) string {
	return "w5f:search/" + kind + "?" + url.Values{"q": {q}}.Encode()
}

func loadSearch(ctx context.Context, target string) (*doc.Document, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	kind := strings.TrimPrefix(u.Opaque, "search/")
	q := u.Query().Get("q")
	if q == "" {
		return nil, errors.New("empty search")
	}
	switch kind {
	case "wikis":
		return search.Wikis(ctx, Fetcher, q)
	default:
		return search.Web(ctx, Fetcher, q)
	}
}

// Normalize fixes addresses that are known not to work as typed:
// "www." in front of a Wikidot site (its certificate covers one level only),
// the SCP Wiki's old domains, and Wikipedia's language portal.
func Normalize(u *url.URL) *url.URL {
	n := *u
	host := strings.ToLower(n.Host)
	switch {
	case strings.HasPrefix(host, "www.") && strings.HasSuffix(host, ".wikidot.com") && strings.Count(host, ".") >= 3:
		host = strings.TrimPrefix(host, "www.")
	case host == "scp-wiki.net" || host == "www.scp-wiki.net" || host == "scpwiki.com" || host == "www.scpwiki.com":
		host = "scp-wiki.wikidot.com"
	case (host == "wikipedia.org" || host == "www.wikipedia.org" || host == "wikipedia.com" || host == "www.wikipedia.com") &&
		(n.Path == "" || n.Path == "/"):
		host, n.Path = "en.wikipedia.org", "/wiki/Main_Page"
	}
	if strings.HasSuffix(host, ".wikidot.com") || host == "wikidot.com" {
		n.Scheme = "https"
	}
	n.Host = host
	return &n
}

// Friendly turns a load error into a sentence for the status bar.
func Friendly(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	var he *fetch.HTTPError
	switch {
	case errors.Is(err, fetch.ErrOffline):
		return "Offline, and this page is not in the cache yet."
	case errors.As(err, &he) && (he.Status == 404 || he.Status == 410):
		return fmt.Sprintf("Page not found (HTTP %d), and the archive has no copy either.", he.Status)
	case errors.As(err, &he) && (he.Status == 403 || he.Status == 429):
		return fmt.Sprintf("The site refused the request (HTTP %d) — it may block terminal readers.", he.Status)
	case errors.As(err, &he):
		return fmt.Sprintf("The site answered with an error (HTTP %d).", he.Status)
	case strings.Contains(s, "certificate"):
		return "Secure connection failed: the site's certificate does not match this address."
	case strings.Contains(s, "no such host"):
		return "No site at this address (the name could not be found)."
	case strings.Contains(s, "timeout") || strings.Contains(s, "connectex") || strings.Contains(s, "deadline exceeded"):
		return "The site did not respond in time."
	case strings.Contains(s, "connection refused"):
		return "The site refused the connection."
	case strings.Contains(s, "unsupported content type"):
		return "This is not a readable page (a file or media); it cannot be shown here yet."
	case errors.Is(err, reddit.ErrNoRedlib):
		return "Reddit needs Redlib, which is not installed. On W5F: put the redlib binary in ~/.local/share/w5f/bin."
	case strings.Contains(s, "redlib"):
		return "Reddit reader (Redlib) problem: " + s
	case strings.Contains(s, "not a readable EPUB") || strings.Contains(s, "not an EPUB"):
		return "This book file could not be read as an EPUB."
	case errors.Is(err, sitecat.ErrRefused):
		return "The site refused the download — it may need a login or block readers."
	case strings.Contains(s, "crom"):
		return "The wiki index (Crom) did not answer; try again in a moment."
	}
	return "Could not open: " + s
}

// FeedsEnv assembles what the Periodicals section needs.
func FeedsEnv() (feeds.Env, error) {
	db, err := store.Default()
	if err != nil {
		return feeds.Env{}, fmt.Errorf("opening the local database: %w", err)
	}
	cat, err := feeds.LoadCatalog()
	if err != nil {
		return feeds.Env{}, err
	}
	return feeds.Env{Fetcher: Fetcher, DB: db, Catalog: cat,
		Article: func(ctx context.Context, u string) (*doc.Document, error) { return Load(ctx, u, Options{}) }}, nil
}

func loadRandom(ctx context.Context, preset string) (*doc.Document, error) {
	p, ok := crom.Presets[preset]
	if !ok {
		return nil, fmt.Errorf("unknown random source %q", preset)
	}
	u, err := crom.Random(ctx, Fetcher, p)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.Label, err)
	}
	return Load(ctx, u, Options{})
}

func loadURL(ctx context.Context, u *url.URL, opts Options) (*doc.Document, error) {
	if reddit.IsReddit(u) {
		d, info, err := reddit.LoadPost(ctx, Fetcher, u, opts.Reload)
		if err == nil && info != nil {
			if env, eerr := FictionEnv(); eerr == nil {
				fiction.DecorateRedditPost(ctx, env, d, info)
			}
		}
		return d, err
	}
	resp, err := Fetcher.Get(ctx, u, fetch.Options{Revalidate: opts.Reload})
	if err != nil {
		if !Fetcher.Offline && (fetch.Gone(err) || isUnreachable(err)) {
			if d, aerr := loadArchived(ctx, u); aerr == nil {
				return d, nil
			}
		}
		return nil, err
	}
	d, err := convertResponse(resp)
	if err != nil {
		return nil, err
	}
	if next := refreshTarget(resp, d); next != nil && opts.hops < 3 {
		// A page that only sends the reader on ("you asked for it!").
		opts.hops++
		return loadURL(ctx, next, opts)
	}
	switch {
	case resp.Stale:
		d.Origin = "cache"
		d.Blocks = append([]doc.Block{doc.Notice{Kind: "archive",
			Text: "Offline copy from " + resp.Fetched.Format("2006-01-02 15:04") + " — the site could not be reached."}}, d.Blocks...)
	case resp.FromCache:
		d.Origin = "cache"
	}
	return d, nil
}

// MarginaliaRandom is Marginalia's "explore random": a fresh list of small
// independent sites on every visit (never served from the cache).
const MarginaliaRandom = "w5f:smallweb/marginalia-random"

var marginaliaRandomURL = "https://old-search.marginalia.nu/explore/random"

func loadMarginaliaRandom(ctx context.Context) (*doc.Document, error) {
	u, _ := url.Parse(marginaliaRandomURL)
	resp, err := Fetcher.Get(ctx, u, fetch.Options{NoStore: true})
	if err != nil {
		return nil, err
	}
	d, err := convertResponse(resp)
	if err != nil {
		return nil, err
	}
	d.Links = append(d.Links, doc.Link{Href: MarginaliaRandom, Text: "new random sites"})
	d.Blocks = append([]doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "↻ new random sites", Style: doc.Bold, Link: len(d.Links)},
		{Text: "  · pick a site, or its “similar” link to steer", Style: doc.Italic}}}}, d.Blocks...)
	d.URL = MarginaliaRandom
	d.Renumber()
	return d, nil
}

var reMetaRefresh = regexp.MustCompile(`(?i)<meta[^>]+http-equiv=["']?refresh["']?[^>]*content=["']?\s*(\d+)\s*;\s*url=['"]?([^'">]+)`)

// refreshTarget is where a near-empty page's quick meta refresh sends the
// reader (web addresses only), or nil.
func refreshTarget(resp *fetch.Response, d *doc.Document) *url.URL {
	m := reMetaRefresh.FindSubmatch(resp.Body)
	if m == nil || doc.TextLength(d.Blocks) > 300 {
		return nil
	}
	if delay, _ := strconv.Atoi(string(m[1])); delay > 10 {
		return nil
	}
	next, err := resp.URL.Parse(strings.TrimSpace(string(m[2])))
	if err != nil || (next.Scheme != "http" && next.Scheme != "https") || next.String() == resp.URL.String() {
		return nil
	}
	return next
}

// OpenImages sends pictures (image files and links) to the comics viewer; the
// interactive reader turns it on, one-shot commands only describe them.
var OpenImages = false

// imageDoc opens a picture in the viewer (its folder, from that picture on)
// and says so.
func imageDoc(path, origin, address string) *doc.Document {
	d := &doc.Document{Title: filepath.Base(path), URL: address, Origin: origin}
	msg := "A picture. Open it in the W5F reader, or with: w5f view " + path
	if OpenImages {
		if err := OpenComic(comics.ViewRequest{Path: path, Title: filepath.Base(path)}); err != nil {
			msg = err.Error()
		} else {
			msg = "Opened in the viewer (q comes back here). Other pictures in the same folder follow it."
		}
	}
	d.Blocks = []doc.Block{doc.Notice{Kind: "info", Text: msg}}
	return d
}

// webImage keeps a picture from the web in its own folder in the cache (the
// viewer shows the pictures of a folder) and opens it.
func webImage(resp *fetch.Response) (*doc.Document, error) {
	sum := sha256.Sum256([]byte(resp.URL.String()))
	dir := filepath.Join(os.TempDir(), "w5f-images", hex.EncodeToString(sum[:8]))
	if Fetcher.CacheDir != "" {
		dir = filepath.Join(Fetcher.CacheDir, "images", hex.EncodeToString(sum[:8]))
	}
	name := filepath.Base(resp.URL.Path)
	if !comics.IsImage(name) {
		exts, _ := mime.ExtensionsByType(strings.TrimSpace(strings.Split(resp.ContentType, ";")[0]))
		name = "image.jpg" // Linux may list .jfif or .jpe first: take one the viewer knows
		for _, e := range exts {
			if comics.IsImage("image" + e) {
				name = "image" + e
				break
			}
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, resp.Body, 0o644); err != nil {
		return nil, err
	}
	return imageDoc(path, "live", resp.URL.String()), nil
}

func convertResponse(resp *fetch.Response) (*doc.Document, error) {
	ct := resp.ContentType
	if strings.HasPrefix(ct, "image/") {
		return webImage(resp)
	}
	if strings.HasPrefix(ct, "text/plain") {
		return plainText(string(resp.Body), resp.URL.String(), "live"), nil
	}
	if !strings.Contains(ct, "html") && ct != "" {
		return nil, fmt.Errorf("%s: unsupported content type %q (open it externally)", resp.URL, ct)
	}
	return convertHTML(resp.Body, resp.URL)
}

// isUnreachable reports DNS failures and refused connections — a site that
// no longer exists rather than a transient error.
func isUnreachable(err error) bool {
	s := err.Error()
	return strings.Contains(s, "no such host") || strings.Contains(s, "connection refused") ||
		strings.Contains(s, "server misbehaving")
}

type waybackReply struct {
	ArchivedSnapshots struct {
		Closest *struct {
			Available bool   `json:"available"`
			URL       string `json:"url"`
			Timestamp string `json:"timestamp"`
		} `json:"closest"`
	} `json:"archived_snapshots"`
}

var reWaybackTS = regexp.MustCompile(`/web/(\d{14})/`)

// loadArchived fetches the closest Wayback Machine snapshot of u. The raw
// ("id_") variant is used so the archive's toolbar is not part of the page,
// and links resolve against the original address.
func loadArchived(ctx context.Context, u *url.URL) (*doc.Document, error) {
	api, _ := url.Parse(WaybackAPI)
	q := api.Query()
	q.Set("url", u.String())
	api.RawQuery = q.Encode()
	resp, err := Fetcher.Get(ctx, api, fetch.Options{NoStore: true})
	if err != nil {
		return nil, err
	}
	var wr waybackReply
	if err := json.Unmarshal(resp.Body, &wr); err != nil {
		return nil, err
	}
	c := wr.ArchivedSnapshots.Closest
	if c == nil || !c.Available || c.URL == "" {
		return nil, errors.New("no archived copy")
	}
	raw := reWaybackTS.ReplaceAllString(c.URL, "/web/${1}id_/")
	if upgradeSnapshots {
		raw = strings.Replace(raw, "http://", "https://", 1)
	}
	ru, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	snap, err := Fetcher.Get(ctx, ru, fetch.Options{})
	if err != nil {
		return nil, err
	}
	d, err := convertHTML(snap.Body, u)
	if err != nil {
		return nil, err
	}
	when := c.Timestamp
	if t, err := time.Parse("20060102150405", c.Timestamp); err == nil {
		when = t.Format("2006-01-02")
	}
	d.Origin = "archive"
	d.Meta = append(d.Meta, doc.KV{Key: "archived", Value: when})
	d.Blocks = append([]doc.Block{doc.Notice{Kind: "archive",
		Text: "Archived copy from " + when + " (Wayback Machine) — the live page is gone."}}, d.Blocks...)
	return d, nil
}

// resolveEmbeds replaces Embed blocks: with live access the embedded page is
// fetched and inlined when it has readable text, and silently dropped when it
// has none. Without live access (saved files) a link to the block is offered.
func resolveEmbeds(ctx context.Context, d *doc.Document, live bool) {
	changed := false
	d.Blocks = doc.ReplaceBlocks(d.Blocks, func(b doc.Block) ([]doc.Block, bool) {
		e, ok := b.(doc.Embed)
		if !ok {
			return nil, false
		}
		changed = true
		if live {
			if u, err := url.Parse(e.Src); err == nil {
				if resp, err := Fetcher.Get(ctx, u, fetch.Options{}); err == nil {
					if ed, err := htmlconv.Generic(bytes.NewReader(resp.Body), resp.URL.String()); err == nil {
						if doc.TextLength(ed.Blocks) < minEmbedText {
							return nil, true
						}
						offset := len(d.Links)
						d.Links = append(d.Links, ed.Links...)
						return doc.ShiftLinks(ed.Blocks, offset), true
					}
				}
			}
		}
		d.Links = append(d.Links, doc.Link{Href: e.Src, Text: "embedded content"})
		msg := "Part of this page lives in an embedded block that could not be loaded."
		if !live {
			msg = "This saved page has an embedded block; open it to read what it contains."
		}
		return []doc.Block{
			doc.Notice{Kind: "gimmick", Text: msg},
			doc.Paragraph{Text: doc.Inline{{Text: "→ open embedded content", Link: len(d.Links)}}},
		}, true
	})
	if changed {
		d.Renumber()
	}
}

func loadFile(path string) (*doc.Document, error) {
	if comics.IsImage(path) {
		abs, _ := filepath.Abs(path)
		return imageDoc(abs, "file", (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String()), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	abs, _ := filepath.Abs(path)
	fileURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)})
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm", ".xhtml":
		base := fileURL
		// Saved Wikidot pages carry their origin; resolve links against it so
		// following a link from a fixture goes to the live wiki.
		if w := wikidotOrigin(data); w != nil {
			base = w
		}
		d, err := convertHTML(data, base)
		if err != nil {
			return nil, err
		}
		d.Origin = "file"
		return d, nil
	case ".md", ".markdown":
		return personal.ReadMarkdown(data, fileURL.String()), nil
	default:
		return plainText(string(data), fileURL.String(), "file"), nil
	}
}

var (
	reDomain = regexp.MustCompile(`WIKIREQUEST\.info\.domain = "([^"]+)"`)
	rePage   = regexp.MustCompile(`WIKIREQUEST\.info\.requestPageName = "([^"]+)"`)
)

func wikidotOrigin(data []byte) *url.URL {
	dm := reDomain.FindSubmatch(data)
	pm := rePage.FindSubmatch(data)
	if dm == nil || pm == nil {
		return nil
	}
	return &url.URL{Scheme: "https", Host: string(dm[1]), Path: "/" + string(pm[1])}
}

func convertHTML(body []byte, u *url.URL) (*doc.Document, error) {
	if htmlconv.IsEksi(u) {
		return htmlconv.Eksi(body, u.String())
	}
	if htmlconv.IsMarginalia(u) {
		return htmlconv.Marginalia(body, u.String())
	}
	if htmlconv.IsWikidot(u) || bytes.Contains(body, []byte(`id="page-content"`)) && bytes.Contains(body, []byte("WIKIREQUEST")) {
		return htmlconv.Wikidot(bytes.NewReader(body), u.String())
	}
	return htmlconv.Article(body, u.String())
}

// plainText turns text into paragraphs split on blank lines.
func plainText(s, u, origin string) *doc.Document {
	d := &doc.Document{URL: u, Origin: origin}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	for i, para := range strings.Split(s, "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		if i == 0 && d.Title == "" && strings.HasPrefix(para, "# ") && !strings.Contains(para, "\n") {
			d.Title = strings.TrimPrefix(para, "# ")
			continue
		}
		var in doc.Inline
		for j, ln := range strings.Split(para, "\n") {
			if j > 0 {
				in = append(in, doc.Span{Break: true})
			}
			in = append(in, doc.Span{Text: ln})
		}
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: in})
	}
	return d
}

// booksEnv is the Library environment, including the added site catalogs.
func booksEnv(db *store.DB) books.Env {
	return books.Env{Fetcher: Fetcher, DB: db, LoadFile: loadFile, Catalogs: func() []books.CatalogLink {
		ps, _ := sitecat.LoadAll(sitecat.Path())
		out := make([]books.CatalogLink, 0, len(ps))
		for _, p := range ps {
			out = append(out, books.CatalogLink{ID: p.ID, Name: p.Name, Home: p.Home})
		}
		return out
	}}
}

// isCatalogID reports whether id names an added site catalog, so that
// "cat <id> …" searches it while other "cat …" words stay a web search.
func isCatalogID(id string) bool {
	ps, _ := sitecat.LoadAll(sitecat.Path())
	return sitecat.Find(ps, id) >= 0
}
