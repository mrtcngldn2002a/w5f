package books

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"w5f/internal/config"
	"w5f/internal/store"
)

// LibraryDir is where books live: W5F_BOOKS, else [folders] books in
// config.toml, else ~/Archive/Books. Downloads go here too.
func LibraryDir() string {
	return config.Folder("W5F_BOOKS", config.Load().Folders.Books, "Archive", "Books")
}

// Formats with a reader in openers open in W5F with chapters (see
// chaptered); of the rest, readable ones open as one page and external
// ones go to an external viewer. libgen.go reads these maps too.
var (
	readable = map[string]bool{".epub": true, ".txt": true, ".html": true, ".htm": true, ".md": true}
	external = map[string]bool{".pdf": true, ".cbz": true, ".cbr": true, ".djvu": true, ".mobi": true, ".azw3": true, ".fb2": true}
)

// Scan walks the library folder and records every book it finds. It returns
// the number of books.
func Scan(db *store.DB) (int, error) {
	dir := LibraryDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	n := 0
	err := filepath.WalkDir(dir, func(p string, de os.DirEntry, err error) error {
		if err != nil || de.IsDir() {
			return nil
		}
		ext := bookExt(p)
		if !readable[ext] && !external[ext] && !chaptered(p) {
			return nil
		}
		seen[p] = true
		n++
		b := store.Book{Path: p, Format: strings.TrimPrefix(ext, ".")}
		if old, ok := findByPath(db, p); ok && old.Title != "" {
			// Metadata is read once — again only for a book listed before
			// its format had a reader (no chapters yet). PDFs are not opened
			// here: that extracts all their text.
			if old.Chapters > 0 || !chaptered(p) || ext == ".pdf" {
				return nil
			}
			b = old
		}
		if b.Title == "" || b.Chapters == 0 {
			b.Title, b.Author = titleFromName(p)
		}
		if chaptered(p) && ext != ".pdf" {
			if r, err := Open(p); err == nil {
				m := r.Info()
				if m.Title != "" {
					b.Title = m.Title
				}
				if m.Author != "" {
					b.Author = m.Author
				}
				b.Lang, b.Chapters = m.Lang, len(r.Contents())
				r.Close()
			}
		}
		_, _ = db.UpsertBook(b)
		return nil
	})
	if err != nil {
		return n, err
	}
	return n, db.MarkMissingExcept(seen)
}

func findByPath(db *store.DB, p string) (store.Book, bool) {
	books, err := db.Books("author", 0)
	if err != nil {
		return store.Book{}, false
	}
	for _, b := range books {
		if b.Path == p {
			return b, true
		}
	}
	return store.Book{}, false
}

var reAuthorTitle = regexp.MustCompile(`^(.+?)\s+-\s+(.+)$`)

// titleFromName guesses "Author - Title" from a file name.
func titleFromName(p string) (title, author string) {
	name := filepath.Base(p)
	base := name[:len(name)-len(bookExt(name))]
	base = strings.ReplaceAll(base, "_", " ")
	if m := reAuthorTitle.FindStringSubmatch(base); m != nil {
		return strings.TrimSpace(m[2]), strings.TrimSpace(m[1])
	}
	return base, ""
}

var reUnsafe = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]+`)

// FileName builds "Author - Title.ext" safe for every OS.
func FileName(author, title, ext string) string {
	name := strings.TrimSpace(title)
	if author != "" {
		name = strings.TrimSpace(author) + " - " + name
	}
	name = strings.TrimSpace(reUnsafe.ReplaceAllString(name, " "))
	if r := []rune(name); len(r) > 120 {
		name = string(r[:120])
	}
	if name == "" {
		name = "book"
	}
	return name + ext
}

// OpenExternal hands a file to the system viewer (PDF, comics…). On the W5F
// this is MuPDF/zathura when installed.
func OpenExternal(p string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", p)
	case "darwin":
		cmd = exec.Command("open", p)
	default:
		for _, viewer := range []string{"mupdf", "zathura", "xdg-open"} {
			if path, err := exec.LookPath(viewer); err == nil {
				cmd = exec.Command(path, p)
				break
			}
		}
	}
	if cmd == nil {
		return errors.New("no external viewer found (install mupdf or zathura)")
	}
	return cmd.Start()
}
