package comics

import (
	"archive/tar"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/bodgit/sevenzip"
	"github.com/nwaples/rardecode/v2"
)

// entry is a page inside an archive.
type entry struct {
	name   string
	index  int   // position among the archive's entries (RAR, 7z)
	offset int64 // where the data starts (tar)
	size   int64
}

func pageEntry(name string) bool {
	base := filepath.Base(name)
	return IsImage(base) && !strings.HasPrefix(base, ".") && !strings.Contains(name, "__MACOSX")
}

func sortEntries(es []entry) {
	sort.Slice(es, func(i, j int) bool { return natLess(es[i].name, es[j].name) })
}

func readInfo(r io.Reader, info *Info) {
	b, _ := io.ReadAll(io.LimitReader(r, 1<<20))
	_ = xml.Unmarshal(b, info)
}

// --- CBT (tar): data offsets are recorded once, pages are read in place.

type tarPages struct {
	f     *os.File
	pages []entry
}

func openTar(path string) (Pages, Info, error) {
	var info Info
	f, err := os.Open(path)
	if err != nil {
		return nil, info, err
	}
	t := &tarPages{f: f}
	tr := tar.NewReader(f)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			f.Close()
			return nil, info, fmt.Errorf("%s: damaged or not a CBT (%v)", filepath.Base(path), err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		switch {
		case pageEntry(h.Name):
			off, _ := f.Seek(0, io.SeekCurrent)
			t.pages = append(t.pages, entry{name: h.Name, offset: off, size: h.Size})
		case strings.EqualFold(filepath.Base(h.Name), "ComicInfo.xml"):
			readInfo(tr, &info)
		}
	}
	if len(t.pages) == 0 {
		f.Close()
		return nil, info, fmt.Errorf("%s: no pages inside", filepath.Base(path))
	}
	sortEntries(t.pages)
	return t, info, nil
}

func (t *tarPages) Len() int          { return len(t.pages) }
func (t *tarPages) Name(i int) string { return t.pages[i].name }
func (t *tarPages) Close() error      { return t.f.Close() }
func (t *tarPages) Open(i int) (io.ReadCloser, error) {
	if i < 0 || i >= len(t.pages) {
		return nil, errors.New("no such page")
	}
	return io.NopCloser(io.NewSectionReader(t.f, t.pages[i].offset, t.pages[i].size)), nil
}

// --- CB7 (7z): the library reads any file of the archive.

// Most CB7 files are solid: a page is decompressed only after everything
// before it. Pages are read in archive order by one cursor (the library then
// reuses its decompressor), and only going back starts over.
type sevenPages struct {
	r     *sevenzip.ReadCloser
	pages []entry
	mu    sync.Mutex
	next  int // archive index the cursor reads next
}

func openSeven(path string) (Pages, Info, error) {
	var info Info
	r, err := sevenzip.OpenReader(path)
	if err != nil {
		return nil, info, fmt.Errorf("%s: damaged or not a CB7 (%v)", filepath.Base(path), err)
	}
	s := &sevenPages{r: r}
	for i, f := range r.File {
		switch {
		case f.FileInfo().IsDir():
		case pageEntry(f.Name):
			s.pages = append(s.pages, entry{name: f.Name, index: i})
		case strings.EqualFold(filepath.Base(f.Name), "ComicInfo.xml"):
			if rc, err := f.Open(); err == nil {
				readInfo(rc, &info)
				rc.Close()
			}
		}
	}
	if len(s.pages) == 0 {
		r.Close()
		return nil, info, fmt.Errorf("%s: no pages inside", filepath.Base(path))
	}
	sortEntries(s.pages)
	return s, info, nil
}

func (s *sevenPages) Len() int          { return len(s.pages) }
func (s *sevenPages) Name(i int) string { return s.pages[i].name }
func (s *sevenPages) Close() error      { return s.r.Close() }
func (s *sevenPages) Open(i int) (io.ReadCloser, error) {
	if i < 0 || i >= len(s.pages) {
		return nil, errors.New("no such page")
	}
	want := s.pages[i].index
	s.mu.Lock()
	defer s.mu.Unlock()
	if want < s.next {
		s.next = 0 // going back: start over
	}
	for ; s.next < want; s.next++ {
		f := s.r.File[s.next]
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		_, err = io.Copy(io.Discard, rc) // read to the end so the decompressor is reused
		rc.Close()
		if err != nil {
			return nil, err
		}
	}
	rc, err := s.r.File[want].Open()
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(rc, 64<<20))
	rc.Close()
	if err != nil {
		return nil, fmt.Errorf("%s: %v", s.pages[i].name, err)
	}
	s.next = want + 1
	return io.NopCloser(bytes.NewReader(b)), nil
}

// --- CBR (RAR): read front to back. A cursor walks forward from the last
// page read, and starts over only to go back; solid archives stay usable.

type rarPages struct {
	path  string
	pages []entry
	mu    sync.Mutex
	rc    *rardecode.ReadCloser
	at    int // index of the next entry the cursor would read
}

func openRar(path string) (Pages, Info, error) {
	var info Info
	rc, err := rardecode.OpenReader(path)
	if err != nil {
		return nil, info, fmt.Errorf("%s: damaged or not a CBR (%v)", filepath.Base(path), err)
	}
	defer rc.Close()
	r := &rarPages{path: path}
	for i := 0; ; i++ {
		h, err := rc.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, info, fmt.Errorf("%s: damaged CBR (%v)", filepath.Base(path), err)
		}
		switch {
		case h.IsDir:
		case pageEntry(h.Name):
			r.pages = append(r.pages, entry{name: h.Name, index: i})
		case strings.EqualFold(filepath.Base(h.Name), "ComicInfo.xml"):
			readInfo(rc, &info)
		}
	}
	if len(r.pages) == 0 {
		return nil, info, fmt.Errorf("%s: no pages inside", filepath.Base(path))
	}
	sortEntries(r.pages)
	return r, info, nil
}

func (r *rarPages) Len() int          { return len(r.pages) }
func (r *rarPages) Name(i int) string { return r.pages[i].name }
func (r *rarPages) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rc != nil {
		r.rc.Close()
		r.rc = nil
	}
	return nil
}

func (r *rarPages) Open(i int) (io.ReadCloser, error) {
	if i < 0 || i >= len(r.pages) {
		return nil, errors.New("no such page")
	}
	want := r.pages[i].index
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rc == nil || want < r.at {
		if r.rc != nil {
			r.rc.Close()
		}
		rc, err := rardecode.OpenReader(r.path)
		if err != nil {
			r.rc = nil
			return nil, err
		}
		r.rc, r.at = rc, 0
	}
	for {
		_, err := r.rc.Next()
		if err != nil {
			r.rc.Close()
			r.rc = nil
			return nil, fmt.Errorf("%s: %v", r.pages[i].name, err)
		}
		r.at++
		if r.at-1 == want {
			// The entry is only readable until the next Next: copy it.
			b, err := io.ReadAll(io.LimitReader(r.rc, 64<<20))
			if err != nil {
				return nil, err
			}
			return io.NopCloser(bytes.NewReader(b)), nil
		}
	}
}
