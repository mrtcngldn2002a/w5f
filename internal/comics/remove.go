package comics

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"w5f/internal/comics/suwayomi"
)

// SuwayomiDir is where Suwayomi keeps its downloads inside the comics
// folder; what lies there is removed through Suwayomi, never by hand, so
// it knows the chapter is gone.
func SuwayomiDir(root string) string { return filepath.Join(root, "Suwayomi") }

// RemoveDownloaded removes a chapter Suwayomi downloaded (its file under
// Comics/Suwayomi/mangas/<source>/<series>/), asking Suwayomi to do it.
func (e Env) RemoveDownloaded(ctx context.Context, path string) error {
	if !e.Server.Running(ctx) {
		return errors.New("Suwayomi is not running: open the Picture Vault (5) once so it starts, then remove the chapter again")
	}
	chs, err := e.client().DownloadedChapters(ctx)
	if err != nil {
		return err
	}
	id, ok := matchDownload(path, chs)
	if !ok {
		return fmt.Errorf("Suwayomi lists no downloaded chapter for %s", filepath.Base(path))
	}
	if err := e.client().DeleteDownload(ctx, id); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return errors.New("Suwayomi said the chapter is removed, but its file is still there")
	}
	return nil
}

// matchDownload finds the chapter a downloaded file belongs to: the series
// is the file's folder, the file is the chapter's name (with the
// scanlator's in front when it has one), as Suwayomi names them.
func matchDownload(path string, chs []suwayomi.DownloadedChapter) (int, bool) {
	series := plain(filepath.Base(filepath.Dir(path)))
	file := plain(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	for _, c := range chs {
		if plain(c.Manga) != series {
			continue
		}
		name := plain(c.Name)
		if name != "" && (file == name || file == plain(c.Scanlator)+name) {
			return c.ID, true
		}
	}
	return 0, false
}

// plain keeps letters and digits, lower case: Suwayomi replaces characters
// a file name cannot hold.
func plain(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
