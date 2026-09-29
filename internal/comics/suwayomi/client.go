// Package suwayomi talks to a Suwayomi-Server over its GraphQL API: library,
// sources, following, updates, downloads and extensions. W5F ships no
// extension repositories; which ones are added is the user's choice.
package suwayomi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Client is a Suwayomi GraphQL client.
type Client struct {
	Base string // e.g. http://127.0.0.1:4567
	HTTP *http.Client
}

// New returns a client for a server address.
func New(base string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), HTTP: &http.Client{Timeout: 2 * time.Minute}}
}

// ErrNotRunning means nothing answers at the server address.
var ErrNotRunning = errors.New("the Suwayomi server is not running")

func (c *Client) do(ctx context.Context, query string, vars map[string]any, out any) error {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/api/graphql", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			return fmt.Errorf("%w (%v)", ErrNotRunning, err)
		}
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	var r struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return fmt.Errorf("Suwayomi: HTTP %d, unreadable answer", resp.StatusCode)
	}
	if len(r.Errors) > 0 {
		var msgs []string
		for _, e := range r.Errors {
			msgs = append(msgs, e.Message)
		}
		return errors.New("Suwayomi: " + strings.Join(msgs, "; "))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(r.Data, out)
}

// Source is a manga source (from an extension, or the built-in Local source).
type Source struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	DisplayName    string `json:"displayName"`
	Lang           string `json:"lang"`
	ContentWarning string `json:"contentWarning"`
	SupportsLatest bool   `json:"supportsLatest"`
}

// LocalSource is the id of Suwayomi's built-in source for the user's own files.
const LocalSource = "0"

// Manga is a series.
type Manga struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	Author        string `json:"author"`
	Artist        string `json:"artist"`
	Description   string `json:"description"`
	Status        string `json:"status"`
	InLibrary     bool   `json:"inLibrary"`
	UnreadCount   int    `json:"unreadCount"`
	DownloadCount int    `json:"downloadCount"`
	RealURL       string `json:"realUrl"`
	Source        *struct {
		DisplayName string `json:"displayName"`
	} `json:"source"`
}

// SourceName is the manga's source for display.
func (m Manga) SourceName() string {
	if m.Source == nil {
		return ""
	}
	return m.Source.DisplayName
}

// Chapter is one chapter of a series.
type Chapter struct {
	ID            int     `json:"id"`
	MangaID       int     `json:"mangaId"`
	Name          string  `json:"name"`
	ChapterNumber float64 `json:"chapterNumber"`
	Scanlator     string  `json:"scanlator"`
	UploadDate    string  `json:"uploadDate"` // epoch milliseconds
	IsRead        bool    `json:"isRead"`
	IsDownloaded  bool    `json:"isDownloaded"`
	LastPageRead  int     `json:"lastPageRead"`
	PageCount     int     `json:"pageCount"`
	SourceOrder   int     `json:"sourceOrder"`
}

// Uploaded is the chapter's upload time (zero when unknown).
func (ch Chapter) Uploaded() time.Time {
	var ms int64
	if _, err := fmt.Sscan(ch.UploadDate, &ms); err != nil || ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// Extension is an installable source package.
type Extension struct {
	PkgName        string `json:"pkgName"`
	Name           string `json:"name"`
	Lang           string `json:"lang"`
	VersionName    string `json:"versionName"`
	IsInstalled    bool   `json:"isInstalled"`
	HasUpdate      bool   `json:"hasUpdate"`
	IsObsolete     bool   `json:"isObsolete"`
	ContentWarning string `json:"contentWarning"`
	StoreIndexURL  string `json:"storeIndexUrl"`
}

// Store is an extension repository the user added.
type Store struct {
	Name     string `json:"name"`
	IndexURL string `json:"indexUrl"`
}

// Download is one queued chapter download.
type Download struct {
	Progress float64 `json:"progress"`
	State    string  `json:"state"` // QUEUED DOWNLOADING FINISHED ERROR
	Tries    int     `json:"tries"`
	Chapter  Chapter `json:"chapter"`
	Manga    struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
	} `json:"manga"`
}

const mangaFields = `id title author artist description status inLibrary unreadCount downloadCount realUrl source { displayName }`
const chapterFields = `id mangaId name chapterNumber scanlator uploadDate isRead isDownloaded lastPageRead pageCount sourceOrder`

// Version asks the server who it is; it doubles as a ping.
func (c *Client) Version(ctx context.Context) (string, error) {
	var r struct {
		AboutServer struct {
			Version string `json:"version"`
		} `json:"aboutServer"`
	}
	err := c.do(ctx, `{ aboutServer { version } }`, nil, &r)
	return r.AboutServer.Version, err
}

// Library lists the followed series, by title.
func (c *Client) Library(ctx context.Context) ([]Manga, error) {
	var r struct {
		Mangas struct {
			Nodes []Manga `json:"nodes"`
		} `json:"mangas"`
	}
	err := c.do(ctx, `{ mangas(condition: {inLibrary: true}, order: [{by: TITLE}]) { nodes { `+mangaFields+` } } }`, nil, &r)
	return r.Mangas.Nodes, err
}

// Sources lists the installed sources (the Local source included).
func (c *Client) Sources(ctx context.Context) ([]Source, error) {
	var r struct {
		Sources struct {
			Nodes []Source `json:"nodes"`
		} `json:"sources"`
	}
	err := c.do(ctx, `{ sources { nodes { id name displayName lang contentWarning supportsLatest } } }`, nil, &r)
	sort.Slice(r.Sources.Nodes, func(i, j int) bool { return r.Sources.Nodes[i].DisplayName < r.Sources.Nodes[j].DisplayName })
	return r.Sources.Nodes, err
}

// Browse lists a source's series: kind is POPULAR, LATEST or SEARCH.
func (c *Client) Browse(ctx context.Context, source, kind, query string, page int) ([]Manga, bool, error) {
	var r struct {
		FetchSourceManga struct {
			Mangas      []Manga `json:"mangas"`
			HasNextPage bool    `json:"hasNextPage"`
		} `json:"fetchSourceManga"`
	}
	in := map[string]any{"source": source, "type": kind, "page": page}
	if kind == "SEARCH" {
		in["query"] = query
	}
	err := c.do(ctx, `mutation($in: FetchSourceMangaInput!) { fetchSourceManga(input: $in) { hasNextPage mangas { `+mangaFields+` } } }`,
		map[string]any{"in": in}, &r)
	return r.FetchSourceManga.Mangas, r.FetchSourceManga.HasNextPage, err
}

// Manga returns a series and its chapters (newest first). With refresh the
// server asks the source again first.
func (c *Client) Manga(ctx context.Context, id int, refresh bool) (Manga, []Chapter, error) {
	var chapters []Chapter
	var m Manga
	if refresh {
		var r struct {
			F struct {
				Manga    Manga     `json:"manga"`
				Chapters []Chapter `json:"chapters"`
			} `json:"fetchMangaAndChapters"`
		}
		if err := c.do(ctx, `mutation($id: Int!) { fetchMangaAndChapters(input: {id: $id, fetchManga: true, fetchChapters: true}) { manga { `+mangaFields+` } chapters { `+chapterFields+` } } }`,
			map[string]any{"id": id}, &r); err != nil {
			return m, nil, err
		}
		m, chapters = r.F.Manga, r.F.Chapters
	} else {
		var r struct {
			Manga struct {
				Manga
				Chapters struct {
					Nodes []Chapter `json:"nodes"`
				} `json:"chapters"`
			} `json:"manga"`
		}
		if err := c.do(ctx, `query($id: Int!) { manga(id: $id) { `+mangaFields+` chapters { nodes { `+chapterFields+` } } } }`,
			map[string]any{"id": id}, &r); err != nil {
			return m, nil, err
		}
		m, chapters = r.Manga.Manga, r.Manga.Chapters.Nodes
	}
	sort.SliceStable(chapters, func(i, j int) bool { return chapters[i].SourceOrder > chapters[j].SourceOrder })
	return m, chapters, nil
}

// Follow adds series to the library (or removes them).
func (c *Client) Follow(ctx context.Context, ids []int, follow bool) error {
	return c.do(ctx, `mutation($ids: [Int!]!, $in: Boolean) { updateMangas(input: {ids: $ids, patch: {inLibrary: $in}}) { mangas { id } } }`,
		map[string]any{"ids": ids, "in": follow}, nil)
}

// UpdateLibrary asks every followed series' source for new chapters. It
// returns at once; Updating reports the progress.
func (c *Client) UpdateLibrary(ctx context.Context) error {
	return c.do(ctx, `mutation { updateLibrary(input: {}) { updateStatus { jobsInfo { isRunning } } } }`, nil, nil)
}

// UpdateJobs is a library update's progress.
type UpdateJobs struct {
	IsRunning    bool `json:"isRunning"`
	TotalJobs    int  `json:"totalJobs"`
	FinishedJobs int  `json:"finishedJobs"`
}

// Updating reports the library update.
func (c *Client) Updating(ctx context.Context) (UpdateJobs, error) {
	var r struct {
		S struct {
			Jobs UpdateJobs `json:"jobsInfo"`
		} `json:"libraryUpdateStatus"`
	}
	err := c.do(ctx, `{ libraryUpdateStatus { jobsInfo { isRunning totalJobs finishedJobs } } }`, nil, &r)
	return r.S.Jobs, err
}

// Download queues chapters and starts the downloader when it is stopped.
// Chapters of the Local source are files already and are not queued.
func (c *Client) Download(ctx context.Context, chapterIDs []int) error {
	var r struct {
		E struct {
			S struct {
				State string `json:"state"`
			} `json:"downloadStatus"`
		} `json:"enqueueChapterDownloads"`
	}
	if err := c.do(ctx, `mutation($ids: [Int!]!) { enqueueChapterDownloads(input: {ids: $ids}) { downloadStatus { state } } }`,
		map[string]any{"ids": chapterIDs}, &r); err != nil {
		return err
	}
	if r.E.S.State == "STARTED" {
		return nil
	}
	return c.do(ctx, `mutation { startDownloader(input: {}) { downloadStatus { state } } }`, nil, nil)
}

// Downloads lists the download queue and whether the downloader runs.
func (c *Client) Downloads(ctx context.Context) (bool, []Download, error) {
	var r struct {
		S struct {
			State string     `json:"state"`
			Queue []Download `json:"queue"`
		} `json:"downloadStatus"`
	}
	err := c.do(ctx, `{ downloadStatus { state queue { progress state tries chapter { `+chapterFields+` } manga { id title } } } }`, nil, &r)
	return r.S.State == "STARTED", r.S.Queue, err
}

// MarkRead sets chapters read or unread.
func (c *Client) MarkRead(ctx context.Context, ids []int, read bool) error {
	return c.do(ctx, `mutation($ids: [Int!]!, $r: Boolean) { updateChapters(input: {ids: $ids, patch: {isRead: $r}}) { chapters { id } } }`,
		map[string]any{"ids": ids, "r": read}, nil)
}

// SetPage records the last page read in a chapter.
func (c *Client) SetPage(ctx context.Context, chapterID, page int) error {
	return c.do(ctx, `mutation($ids: [Int!]!, $p: Int) { updateChapters(input: {ids: $ids, patch: {lastPageRead: $p}}) { chapters { id } } }`,
		map[string]any{"ids": []int{chapterID}, "p": page}, nil)
}

// Extensions lists extensions and repositories; refresh asks the
// repositories for their current lists first.
func (c *Client) Extensions(ctx context.Context, refresh bool) ([]Extension, []Store, error) {
	const fields = `pkgName name lang versionName isInstalled hasUpdate isObsolete contentWarning storeIndexUrl`
	var r struct {
		F struct {
			Extensions []Extension `json:"extensions"`
			Stores     []Store     `json:"extensionStores"`
		} `json:"fetchExtensions"`
	}
	if refresh {
		if err := c.do(ctx, `mutation { fetchExtensions(input: {}) { extensions { `+fields+` } extensionStores { name indexUrl } } }`, nil, &r); err != nil {
			return nil, nil, err
		}
	} else {
		var q struct {
			Extensions struct {
				Nodes []Extension `json:"nodes"`
			} `json:"extensions"`
			Stores struct {
				Nodes []Store `json:"nodes"`
			} `json:"extensionStores"`
		}
		if err := c.do(ctx, `{ extensions { nodes { `+fields+` } } extensionStores { nodes { name indexUrl } } }`, nil, &q); err != nil {
			return nil, nil, err
		}
		r.F.Extensions, r.F.Stores = q.Extensions.Nodes, q.Stores.Nodes
	}
	exts := r.F.Extensions
	sort.SliceStable(exts, func(i, j int) bool {
		if exts[i].IsInstalled != exts[j].IsInstalled {
			return exts[i].IsInstalled
		}
		return exts[i].Name < exts[j].Name
	})
	return exts, r.F.Stores, nil
}

// SetExtension installs, updates or uninstalls an extension (action is
// "install", "update" or "uninstall").
func (c *Client) SetExtension(ctx context.Context, pkg, action string) error {
	switch action {
	case "install", "update", "uninstall":
	default:
		return fmt.Errorf("unknown extension action %q", action)
	}
	return c.do(ctx, `mutation($id: String!, $p: UpdateExtensionPatchInput!) { updateExtension(input: {id: $id, patch: $p}) { extension { pkgName } } }`,
		map[string]any{"id": pkg, "p": map[string]bool{action: true}}, nil)
}

// AddStore adds an extension repository by its index address; W5F never
// adds one on its own.
func (c *Client) AddStore(ctx context.Context, indexURL string) error {
	indexURL = strings.TrimSpace(indexURL)
	if !strings.HasPrefix(indexURL, "https://") && !strings.HasPrefix(indexURL, "http://") {
		return errors.New("a repository is added by its web address (https://…/index.min.json)")
	}
	return c.do(ctx, `mutation($u: String!) { addExtensionStore(input: {indexUrl: $u}) { clientMutationId } }`, map[string]any{"u": indexURL}, nil)
}

// RemoveStore removes an extension repository.
func (c *Client) RemoveStore(ctx context.Context, indexURL string) error {
	return c.do(ctx, `mutation($u: String!) { removeExtensionStore(input: {indexUrl: $u}) { clientMutationId } }`, map[string]any{"u": indexURL}, nil)
}
