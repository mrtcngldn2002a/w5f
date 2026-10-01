// Package dict is W5F's pop-up dictionary. It reads StarDict dictionaries
// (.ifo/.idx/.dict/.syn): only the index and the synonym table live in
// memory; definitions are read from disk on demand, which suits a 2 GB
// machine even for a 250 000-entry dictionary.
//
// The default dictionary is the English–Turkish StarDict package from
// github.com/Okbaydere/ereader-en-tr-dictionary (data compiled from Tureng).
package dict

import (
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// DefaultURL is the English–Turkish package installed by `dict-install`.
const DefaultURL = "https://github.com/Okbaydere/ereader-en-tr-dictionary/releases/download/1.1/stardict-en-tr.zip"

// ErrNotInstalled means no dictionary has been installed yet.
var ErrNotInstalled = errors.New("dictionary not installed")

// Dict is an opened StarDict dictionary.
type Dict struct {
	Name  string
	Count int
	dir   string
	base  string
	idx   []byte   // raw .idx
	words []uint32 // start offset of each headword in idx
	syns  []syn    // sorted synonym entries
	synB  []byte
	dict  *os.File
}

type syn struct {
	word  uint32 // offset into synB
	entry uint32
}

// Entry is one dictionary article.
type Entry struct {
	Word string
	HTML string
}

// Dir returns where dictionaries are installed.
func Dir(dataDir string) string { return filepath.Join(dataDir, "dict", "en-tr") }

var (
	mu     sync.Mutex
	opened *Dict
)

// Shared opens the dictionary in dir once and keeps it open.
func Shared(dir string) (*Dict, error) {
	mu.Lock()
	defer mu.Unlock()
	if opened != nil && opened.dir == dir {
		return opened, nil
	}
	d, err := Open(dir)
	if err != nil {
		return nil, err
	}
	opened = d
	return d, nil
}

// Open loads the dictionary found in dir.
func Open(dir string) (*Dict, error) {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.ifo"))
	if len(matches) == 0 {
		return nil, ErrNotInstalled
	}
	base := strings.TrimSuffix(matches[0], ".ifo")
	d := &Dict{dir: dir, base: base}
	if err := d.readIfo(base + ".ifo"); err != nil {
		return nil, err
	}
	idx, err := os.ReadFile(base + ".idx")
	if err != nil {
		return nil, err
	}
	d.idx = idx
	for p := 0; p < len(idx); {
		d.words = append(d.words, uint32(p))
		z := indexByte(idx[p:], 0)
		if z < 0 {
			break
		}
		p += z + 1 + 8
	}
	if b, err := os.ReadFile(base + ".syn"); err == nil {
		d.synB = b
		for p := 0; p < len(b); {
			z := indexByte(b[p:], 0)
			if z < 0 || p+z+5 > len(b) {
				break
			}
			d.syns = append(d.syns, syn{word: uint32(p), entry: binary.BigEndian.Uint32(b[p+z+1:])})
			p += z + 1 + 4
		}
	}
	f, err := os.Open(base + ".dict")
	if err != nil {
		return nil, fmt.Errorf("%w (run dict-install again)", err)
	}
	d.dict = f
	if d.Count == 0 {
		d.Count = len(d.words)
	}
	return d, nil
}

func (d *Dict) readIfo(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		switch k {
		case "bookname":
			d.Name = v
		case "sametypesequence":
			if v != "h" && v != "m" && v != "x" && v != "g" {
				return fmt.Errorf("unsupported dictionary type %q", v)
			}
		case "idxoffsetbits":
			if v != "32" {
				return errors.New("64-bit dictionaries are not supported")
			}
		}
	}
	return sc.Err()
}

func (d *Dict) word(i int) string {
	p := d.words[i]
	z := indexByte(d.idx[p:], 0)
	return string(d.idx[p : int(p)+z])
}

// Len is how many headwords the dictionary has.
func (d *Dict) Len() int { return len(d.words) }

// Word is headword i (0 ≤ i < Len), in the dictionary's order.
func (d *Dict) Word(i int) string { return d.word(i) }

func (d *Dict) synWord(i int) string {
	p := d.syns[i].word
	z := indexByte(d.synB[p:], 0)
	return string(d.synB[p : int(p)+z])
}

// Close releases the dictionary file.
func (d *Dict) Close() error { return d.dict.Close() }

// Entry reads article i.
func (d *Dict) Entry(i int) (Entry, error) {
	p := int(d.words[i])
	z := indexByte(d.idx[p:], 0)
	off := binary.BigEndian.Uint32(d.idx[p+z+1:])
	size := binary.BigEndian.Uint32(d.idx[p+z+5:])
	buf := make([]byte, size)
	if _, err := d.dict.ReadAt(buf, int64(off)); err != nil && !errors.Is(err, io.EOF) {
		return Entry{}, err
	}
	return Entry{Word: d.word(i), HTML: string(buf)}, nil
}

// Lookup returns the articles for a word: exact headword matches first, then
// the base forms an inflected word points to ("ran" → "run").
func (d *Dict) Lookup(q string) []Entry {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil
	}
	seen := map[int]bool{}
	var out []Entry
	add := func(i int) {
		if seen[i] {
			return
		}
		seen[i] = true
		if e, err := d.Entry(i); err == nil {
			out = append(out, e)
		}
	}
	for i := d.lower(q); i < len(d.words) && foldEq(d.word(i), q); i++ {
		add(i)
	}
	for i := d.lowerSyn(q); i < len(d.syns) && foldEq(d.synWord(i), q); i++ {
		add(int(d.syns[i].entry))
	}
	return out
}

// Suggest lists up to n headwords starting with prefix.
func (d *Dict) Suggest(prefix string, n int) []string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil
	}
	var out []string
	for i := d.lower(prefix); i < len(d.words) && len(out) < n; i++ {
		w := d.word(i)
		if !foldPrefix(w, prefix) {
			break
		}
		out = append(out, w)
	}
	if len(out) == 0 {
		// Inflected forms: show the base words they lead to.
		for i := d.lowerSyn(prefix); i < len(d.syns) && len(out) < n; i++ {
			if !foldPrefix(d.synWord(i), prefix) {
				break
			}
			out = append(out, d.word(int(d.syns[i].entry)))
		}
	}
	return dedupe(out)
}

func (d *Dict) lower(q string) int {
	return sort.Search(len(d.words), func(i int) bool { return cmp(d.word(i), q) >= 0 })
}

func (d *Dict) lowerSyn(q string) int {
	return sort.Search(len(d.syns), func(i int) bool { return cmp(d.synWord(i), q) >= 0 })
}

// cmp orders like StarDict: ASCII case-insensitive, then byte order.
func cmp(a, b string) int {
	if c := asciiCaseCmp(a, b); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

func asciiCaseCmp(a, b string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		ca, cb := asciiLower(a[i]), asciiLower(b[i])
		if ca != cb {
			if ca < cb {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

func asciiLower(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 32
	}
	return c
}

func foldEq(a, b string) bool { return asciiCaseCmp(a, b) == 0 }

func foldPrefix(w, p string) bool {
	return len(w) >= len(p) && asciiCaseCmp(w[:len(p)], p) == 0
}

func dedupe(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

// WordAt extracts a likely English word from text (for pre-filling).
func WordAt(s string) string {
	s = strings.TrimFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && r != '-' && r != '\'' })
	if i := strings.IndexFunc(s, unicode.IsSpace); i > 0 {
		s = s[:i]
	}
	if !utf8.ValidString(s) {
		return ""
	}
	return s
}

// Install installs a StarDict zip (local path or URL) into dir. Compressed
// dictionaries (.dict.dz) are expanded once so articles can be read directly.
func Install(ctx context.Context, src, dir, userAgent string) (string, error) {
	path := src
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		tmp, err := download(ctx, src, userAgent)
		if err != nil {
			return "", err
		}
		defer os.Remove(tmp)
		path = tmp
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("not a dictionary zip: %w", err)
	}
	defer zr.Close()
	staging := dir + ".new"
	os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return "", err
	}
	var name string
	for _, f := range zr.File {
		base := filepath.Base(f.Name)
		ext := strings.ToLower(filepath.Ext(base))
		if f.FileInfo().IsDir() || !(ext == ".ifo" || ext == ".idx" || ext == ".syn" || ext == ".dz" || ext == ".dict") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		var r io.Reader = rc
		out := filepath.Join(staging, base)
		if strings.HasSuffix(base, ".dict.dz") {
			gz, err := gzip.NewReader(rc)
			if err != nil {
				rc.Close()
				return "", err
			}
			r, out = gz, filepath.Join(staging, strings.TrimSuffix(base, ".dz"))
		}
		if err := writeFile(out, r); err != nil {
			rc.Close()
			return "", err
		}
		rc.Close()
		if ext == ".ifo" {
			name = strings.TrimSuffix(base, ".ifo")
		}
	}
	if name == "" {
		return "", errors.New("the zip contains no StarDict .ifo file")
	}
	d, err := Open(staging)
	if err != nil {
		return "", err
	}
	title := fmt.Sprintf("%s (%d entries)", d.Name, d.Count)
	d.dict.Close()
	mu.Lock()
	if opened != nil && opened.dir == dir {
		opened.dict.Close()
		opened = nil
	}
	mu.Unlock()
	os.RemoveAll(dir)
	if err := os.Rename(staging, dir); err != nil {
		return "", err
	}
	return title, nil
}

func writeFile(path string, r io.Reader) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func download(ctx context.Context, url, ua string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", ua)
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	f, err := os.CreateTemp("", "w5f-dict-*.zip")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 200<<20)); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	f.Close()
	return f.Name(), nil
}
