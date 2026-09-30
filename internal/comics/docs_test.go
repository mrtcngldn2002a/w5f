package comics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/comics/suwayomi"
	"w5f/internal/doc"
	"w5f/internal/store"
)

func text(d *doc.Document) string {
	if d == nil {
		return "(no page)"
	}
	var b strings.Builder
	var walk func([]doc.Block)
	walk = func(bs []doc.Block) {
		for _, bl := range bs {
			switch x := bl.(type) {
			case doc.Paragraph:
				for _, s := range x.Text {
					b.WriteString(s.Text)
				}
				b.WriteString("\n")
			case doc.Heading:
				for _, s := range x.Text {
					b.WriteString(s.Text)
				}
				b.WriteString("\n")
			case doc.Notice:
				b.WriteString(x.Text + "\n")
			case doc.List:
				for _, it := range x.Items {
					walk(it)
				}
			}
		}
	}
	walk(d.Blocks)
	return b.String()
}

func hasLink(d *doc.Document, href string) bool {
	for _, l := range d.Links {
		if l.Href == href {
			return true
		}
	}
	return false
}

func testEnv(t *testing.T) (Env, *[]ViewRequest) {
	db, err := store.Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	root := t.TempDir()
	writeCBZ(t, filepath.Join(root, "Berserk", "Berserk v01.cbz"), []string{"1.jpg", "2.jpg"}, "")
	var views []ViewRequest
	return Env{DB: db, Root: root, Server: suwayomi.Server{Dir: t.TempDir(), Port: 1},
		View: func(v ViewRequest) error { views = append(views, v); return nil }}, &views
}

func TestLocalPages(t *testing.T) {
	env, views := testEnv(t)
	ctx := context.Background()
	home, err := Route(ctx, "w5f:comics", env)
	if err != nil {
		t.Fatal(err)
	}
	if s := text(home); !strings.Contains(s, "Suwayomi is not installed") || !strings.Contains(s, "Berserk") {
		t.Errorf("home:\n%s", s)
	}
	series, err := Route(ctx, "w5f:comics/series?name=Berserk", env)
	if err != nil || !hasLink(series, "w5f:comics/open/1") || !strings.Contains(text(series), "new") {
		t.Fatalf("series: %v\n%s", err, text(series))
	}
	if _, err := Route(ctx, "w5f:comics/open/1", env); err != nil || len(*views) != 1 || (*views)[0].ComicID != 1 {
		t.Errorf("open: %v %+v", err, *views)
	}
	marked, _ := Route(ctx, "w5f:comics/mark/1", env)
	if !strings.Contains(text(marked), "read") || !hasLink(marked, "w5f:comics/mark/1") {
		t.Errorf("mark:\n%s", text(marked))
	}
	// Suwayomi pages without Suwayomi say so instead of hanging.
	if _, err := Route(ctx, "w5f:comics/following", env); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("following without a server: %v", err)
	}
}

// fakeSuwayomi answers the GraphQL calls the pages make.
func fakeSuwayomi(t *testing.T, calls *[]string) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Query string `json:"query"`
		}
		json.Unmarshal(b, &req)
		q := req.Query
		*calls = append(*calls, strings.Fields(strings.NewReplacer("{", " ", "(", " ").Replace(strings.TrimPrefix(q, "mutation")))[0])
		switch {
		case strings.Contains(q, "aboutServer"):
			fmt.Fprint(w, `{"data":{"aboutServer":{"version":"v2"}}}`)
		case strings.Contains(q, "mangas("):
			fmt.Fprint(w, `{"data":{"mangas":{"nodes":[{"id":5,"title":"Ra","inLibrary":true,"unreadCount":1,"source":{"displayName":"Some Source"}}]}}}`)
		case strings.Contains(q, "updateMangas"):
			fmt.Fprint(w, `{"data":{"updateMangas":{"mangas":[{"id":5}]}}}`)
		case strings.Contains(q, "manga(id"), strings.Contains(q, "fetchMangaAndChapters"):
			chapters := `[{"id":51,"name":"Chapter 1","sourceOrder":1,"isRead":true},{"id":52,"name":"Chapter 2","sourceOrder":2}]`
			if strings.Contains(q, "fetchMangaAndChapters") {
				fmt.Fprintf(w, `{"data":{"fetchMangaAndChapters":{"manga":{"id":5,"title":"Ra","inLibrary":true,"source":{"displayName":"Some Source"}},"chapters":%s}}}`, chapters)
				return
			}
			fmt.Fprintf(w, `{"data":{"manga":{"id":5,"title":"Ra","inLibrary":true,"status":"ONGOING","source":{"displayName":"Some Source"},"chapters":{"nodes":%s}}}}`, chapters)
		case strings.Contains(q, "enqueueChapterDownloads"):
			fmt.Fprint(w, `{"data":{"enqueueChapterDownloads":{"downloadStatus":{"state":"STARTED"}}}}`)
		case strings.Contains(q, "fetchExtensions"):
			fmt.Fprint(w, `{"data":{"fetchExtensions":{"extensions":[{"pkgName":"p.one","name":"One","lang":"en","isInstalled":true,"hasUpdate":true}],"extensionStores":[{"name":"Mine","indexUrl":"https://repo.example/index.min.json"}]}}}`)
		case strings.Contains(q, "extensions {"):
			fmt.Fprint(w, `{"data":{"extensions":{"nodes":[{"pkgName":"p.one","name":"One","lang":"en","isInstalled":true,"hasUpdate":true},`+
				`{"pkgName":"p.ja","name":"Nihon","lang":"ja"},{"pkgName":"p.jai","name":"Kept","lang":"ja","isInstalled":true},`+
				`{"pkgName":"p.all","name":"Everywhere","lang":"all"},{"pkgName":"p.es","name":"Uno","lang":"es"}]},`+
				`"extensionStores":{"nodes":[{"name":"Mine","indexUrl":"https://repo.example/index.min.json"}]}}}`)
		case strings.Contains(q, "addExtensionStore"), strings.Contains(q, "updateExtension"):
			fmt.Fprint(w, `{"data":{}}`)
		default:
			fmt.Fprint(w, `{"errors":[{"message":"unexpected"}]}`)
		}
	}))
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return ln.Addr().(*net.TCPAddr).Port
}

func TestSuwayomiPages(t *testing.T) {
	env, views := testEnv(t)
	var calls []string
	env.Server.Port = fakeSuwayomi(t, &calls)
	ctx := context.Background()

	home, _ := Route(ctx, "w5f:comics", env)
	if !hasLink(home, "w5f:comics/following") || !hasLink(home, "w5f:comics/extensions") {
		t.Errorf("home with a running server:\n%s", text(home))
	}
	f, err := Route(ctx, "w5f:comics/following", env)
	if err != nil || !hasLink(f, "w5f:comics/manga/5") || !strings.Contains(text(f), "1 unread") {
		t.Fatalf("following: %v\n%s", err, text(f))
	}
	m, err := Route(ctx, "w5f:comics/manga/5", env)
	if err != nil || !hasLink(m, "w5f:comics/manga/5/unfollow") || !hasLink(m, "w5f:comics/chapter/52/download?manga=5") ||
		!strings.Contains(text(m), "Chapters (2)") || m.Links[0].Href != "w5f:comics/manga/5/unfollow" {
		t.Fatalf("manga: %v\n%s", err, text(m))
	}
	if d, err := Route(ctx, "w5f:comics/manga/5/download-unread", env); err != nil || !strings.Contains(text(d), "1 chapters queued") {
		t.Errorf("download unread: %v\n%s", err, text(d))
	}
	if _, err := Route(ctx, "w5f:comics/chapter/52/read?manga=5", env); err != nil || (*views)[len(*views)-1].ChapterID != 52 {
		t.Errorf("read chapter: %v %+v", err, *views)
	}
	ext, err := Route(ctx, "w5f:comics/extensions", env)
	if err != nil || !hasLink(ext, "w5f:comics/extension/p.one/update") || !strings.Contains(text(ext), "repo.example") {
		t.Fatalf("extensions: %v\n%s", err, text(ext))
	}
	if !strings.Contains(RepoPage(), "w5f%3Acomics%2Frepo%2Fadd") {
		t.Errorf("repository input page: %s", RepoPage())
	}
	if d, _ := Route(ctx, "w5f:comics/repo/add?url=https://repo.example/index.min.json", env); !strings.Contains(text(d), "Repository list changed") {
		t.Errorf("add repo:\n%s", text(d))
	}
}

func TestExtensionsFilterByLanguage(t *testing.T) {
	env, _ := testEnv(t)
	var calls []string
	env.Server.Port = fakeSuwayomi(t, &calls)
	ctx := context.Background()
	d, err := Route(ctx, "w5f:comics/extensions", env)
	if err != nil {
		t.Fatal(err)
	}
	s := text(d)
	for _, want := range []string{"One", "Everywhere", "Kept", "2 more in other languages"} {
		if !strings.Contains(s, want) {
			t.Errorf("English view lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "Nihon") || strings.Contains(s, "Uno") {
		t.Errorf("other languages are hidden by default:\n%s", s)
	}
	if !hasLink(d, "w5f:comics/extensions?lang=ja") || !hasLink(d, "w5f:comics/extensions?lang=any") {
		t.Errorf("language links: %+v", d.Links)
	}
	ja, _ := Route(ctx, "w5f:comics/extensions?lang=ja", env)
	if s := text(ja); !strings.Contains(s, "Nihon") || strings.Contains(s, "Uno") {
		t.Errorf("Japanese view:\n%s", s)
	}
	again, _ := Route(ctx, "w5f:comics/extensions", env) // the choice is remembered
	if !strings.Contains(text(again), "Nihon") {
		t.Error("the chosen language is kept")
	}
	all, _ := Route(ctx, "w5f:comics/extensions?lang=any", env)
	if s := text(all); !strings.Contains(s, "Uno") || !strings.Contains(s, "Nihon") {
		t.Errorf("every language:\n%s", s)
	}
}
