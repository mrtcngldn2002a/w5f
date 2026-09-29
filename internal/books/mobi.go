package books

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"html"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/charmap"

	"w5f/internal/doc"
	"w5f/internal/htmlconv"
)

var (
	errMOBIDamaged = errors.New("the MOBI file is damaged")
	errKFX         = fmt.Errorf("%w: KFX (the newer Kindle format) is not supported", ErrUnsupported)
)

// maxMOBI caps the size of a MOBI read into memory.
const maxMOBI = 100 << 20

// mobiBook is an opened MOBI, AZW or AZW3 book, held as HTML chapters.
type mobiBook struct {
	meta     Meta
	chapters []Chapter
	parts    []string
}

func init() {
	// A .kfx file is listed so the owner sees it, and refused with a message.
	openers[".kfx"] = func(string) (Reader, error) { return nil, errKFX }
	for _, e := range []string{".mobi", ".azw", ".azw3", ".prc"} {
		openers[e] = func(p string) (Reader, error) {
			st, err := os.Stat(p)
			if err != nil {
				return nil, err
			}
			if st.Size() > maxMOBI {
				return nil, errors.New("the MOBI file is larger than 100 MB")
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return nil, err
			}
			m, err := parseMOBI(data)
			if err != nil {
				return nil, err
			}
			return m, nil
		}
	}
}

func (m *mobiBook) Info() Meta          { return m.meta }
func (m *mobiBook) Contents() []Chapter { return m.chapters }
func (m *mobiBook) Close() error        { return nil }

// ChapterDoc converts one chapter; links to other chapters ("mobi:/ch/N")
// go through link.
func (m *mobiBook) ChapterDoc(i int, link func(ch int) string) (*doc.Document, error) {
	if i < 0 || i >= len(m.parts) {
		return nil, errors.New("no such chapter")
	}
	d, err := htmlconv.XHTML([]byte(m.parts[i]), "mobi:/")
	if err != nil {
		return nil, err
	}
	for li, l := range d.Links {
		u, err := url.Parse(l.Href)
		if err != nil || u.Scheme != "mobi" || !strings.HasPrefix(u.Path, "/ch/") {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimPrefix(u.Path, "/ch/")); err == nil && n >= 0 && n < len(m.parts) {
			d.Links[li].Href = link(n)
		}
	}
	return d, nil
}

// palmDB is the record container of MOBI files.
type palmDB struct {
	data []byte
	offs []int
}

func openPalmDB(data []byte) (*palmDB, error) {
	if len(data) < 78 {
		return nil, errMOBIDamaged
	}
	if t := string(data[60:68]); t != "BOOKMOBI" && t != "TEXtREAd" {
		return nil, errors.New("not a MOBI file")
	}
	n := int(binary.BigEndian.Uint16(data[76:78]))
	if n == 0 || 78+8*n > len(data) {
		return nil, errMOBIDamaged
	}
	p := &palmDB{data: data}
	for i := 0; i < n; i++ {
		off := int(binary.BigEndian.Uint32(data[78+8*i:]))
		if off > len(data) || (i > 0 && off < p.offs[i-1]) {
			return nil, errMOBIDamaged
		}
		p.offs = append(p.offs, off)
	}
	return p, nil
}

func (p *palmDB) rec(i int) ([]byte, error) {
	if i < 0 || i >= len(p.offs) {
		return nil, errMOBIDamaged
	}
	start, end := p.offs[i], len(p.data)
	if i+1 < len(p.offs) {
		end = p.offs[i+1]
	}
	if start > end {
		return nil, errMOBIDamaged
	}
	return p.data[start:end], nil
}

func mu16(b []byte, off int) int {
	if off < 0 || off+2 > len(b) {
		return 0
	}
	return int(binary.BigEndian.Uint16(b[off:]))
}

func mu32(b []byte, off int) int {
	if off < 0 || off+4 > len(b) {
		return 0
	}
	return int(binary.BigEndian.Uint32(b[off:]))
}

// mobiHeader is record 0 of a MOBI section.
type mobiHeader struct {
	compression, textLength, textRecords, encryption int
	encoding, version                                int
	huffOffset, huffCount                            int
	extraFlags                                       int
	title                                            []byte
	exth                                             map[int][]byte
}

func readMOBIHeader(r []byte) (*mobiHeader, error) {
	if len(r) < 16 {
		return nil, errMOBIDamaged
	}
	h := &mobiHeader{compression: mu16(r, 0), textLength: mu32(r, 4), textRecords: mu16(r, 8),
		encryption: mu16(r, 12), encoding: 1252, exth: map[int][]byte{}}
	if len(r) < 24 || string(r[16:20]) != "MOBI" {
		return h, nil // a plain PalmDOC book
	}
	end := 16 + mu32(r, 20)
	field := func(off int) int {
		if off+4 <= end {
			return mu32(r, off)
		}
		return 0
	}
	if e := field(28); e != 0 {
		h.encoding = e
	}
	h.version = field(36)
	h.huffOffset, h.huffCount = field(112), field(116)
	if 244 <= end {
		h.extraFlags = mu16(r, 242)
	}
	if no, nl := field(84), field(88); no > 0 && nl > 0 && no+nl <= len(r) {
		h.title = r[no : no+nl]
	}
	if field(128)&0x40 != 0 && end+12 <= len(r) && string(r[end:end+4]) == "EXTH" {
		count, p := mu32(r, end+8), end+12
		for i := 0; i < count && p+8 <= len(r); i++ {
			typ, l := mu32(r, p), mu32(r, p+4)
			if l < 8 || p+l > len(r) {
				break
			}
			h.exth[typ] = r[p+8 : p+l]
			p += l
		}
	}
	return h, nil
}

// trailingSize is the size of the trailing entries the extra-data flags
// add to a text record (TBS indexing entries, multibyte bytes).
func trailingSize(rec []byte, flags int) int {
	n := 0
	for f := flags >> 1; f != 0; f >>= 1 {
		if f&1 != 0 && n < len(rec) {
			n += trailingEntry(rec[:len(rec)-n])
		}
	}
	if flags&1 != 0 && len(rec)-n-1 >= 0 {
		n += int(rec[len(rec)-n-1]&0x3) + 1
	}
	return n
}

// trailingEntry reads an entry size stored backwards as a varint.
func trailingEntry(b []byte) int {
	res, bit := 0, 0
	for i := len(b) - 1; i >= 0; i-- {
		v := b[i]
		res |= int(v&0x7F) << bit
		bit += 7
		if v&0x80 != 0 || bit >= 28 || i == 0 {
			return res
		}
	}
	return res
}

// palmdoc decompresses PalmDOC (an LZ77 variant).
func palmdoc(in []byte) []byte {
	out := make([]byte, 0, 4096)
	for i := 0; i < len(in); i++ {
		c := in[i]
		switch {
		case c == 0 || (c >= 0x09 && c <= 0x7F):
			out = append(out, c)
		case c >= 0x01 && c <= 0x08:
			n := min(int(c), len(in)-1-i)
			out = append(out, in[i+1:i+1+n]...)
			i += n
		case c >= 0x80 && c <= 0xBF:
			if i+1 >= len(in) {
				return out
			}
			i++
			v := int(c)<<8 | int(in[i])
			dist, length := (v>>3)&0x7FF, (v&0x7)+3
			if dist == 0 || dist > len(out) {
				continue // damaged back-reference
			}
			for j := 0; j < length; j++ {
				out = append(out, out[len(out)-dist])
			}
		default: // 0xC0–0xFF: a space and a character
			out = append(out, ' ', c^0x80)
		}
	}
	return out
}

// sectionText decompresses the text records of the section whose header is
// record hi.
func sectionText(db *palmDB, hi int, h *mobiHeader) ([]byte, error) {
	if h.encryption != 0 {
		return nil, ErrDRM
	}
	var huff *huffReader
	if h.compression == 17480 {
		var err error
		if huff, err = loadHuff(db, hi+h.huffOffset, h.huffCount); err != nil {
			return nil, err
		}
	}
	var out []byte
	for i := 1; i <= h.textRecords; i++ {
		rec, err := db.rec(hi + i)
		if err != nil {
			return nil, err
		}
		if n := trailingSize(rec, h.extraFlags); n <= len(rec) {
			rec = rec[:len(rec)-n]
		}
		switch h.compression {
		case 1:
			out = append(out, rec...)
		case 2:
			out = append(out, palmdoc(rec)...)
		case 17480:
			t, err := huff.unpack(rec, 0)
			if err != nil {
				return nil, err
			}
			out = append(out, t...)
		default:
			return nil, fmt.Errorf("%w: MOBI compression %d", ErrUnsupported, h.compression)
		}
	}
	if h.textLength > 0 && h.textLength < len(out) {
		out = out[:h.textLength]
	}
	return out, nil
}

// mobiDecode turns MOBI text into UTF-8.
func mobiDecode(b []byte, encoding int) string {
	if encoding == 1252 {
		if s, err := charmap.Windows1252.NewDecoder().Bytes(b); err == nil {
			return string(s)
		}
	}
	return strings.ToValidUTF8(string(b), "�")
}

func exthMeta(h *mobiHeader) Meta {
	m := Meta{Title: strings.TrimSpace(mobiDecode(h.title, h.encoding))}
	if t := h.exth[503]; len(t) > 0 {
		m.Title = strings.TrimSpace(mobiDecode(t, h.encoding))
	}
	if a := h.exth[100]; len(a) > 0 {
		m.Author = strings.TrimSpace(mobiDecode(a, h.encoding))
	}
	if l := h.exth[524]; len(l) > 0 {
		m.Lang = strings.TrimSpace(string(l))
	}
	return m
}

// parseMOBI reads a MOBI, AZW or AZW3 file held in memory.
func parseMOBI(data []byte) (*mobiBook, error) {
	switch {
	case bytes.HasPrefix(data, []byte("\xeaDRMION\xee")):
		return nil, ErrDRM
	case bytes.HasPrefix(data, []byte("CONT")):
		return nil, errKFX
	}
	db, err := openPalmDB(data)
	if err != nil {
		return nil, err
	}
	r0, err := db.rec(0)
	if err != nil {
		return nil, err
	}
	h, err := readMOBIHeader(r0)
	if err != nil {
		return nil, err
	}
	if h.encryption != 0 {
		return nil, ErrDRM
	}
	m := &mobiBook{meta: exthMeta(h)}
	if kf := kf8Header(db, h); kf >= 0 {
		kh := h
		if kf != 0 {
			r, err := db.rec(kf)
			if err != nil {
				return nil, err
			}
			if kh, err = readMOBIHeader(r); err != nil {
				return nil, err
			}
			if kh.encryption != 0 {
				return nil, ErrDRM
			}
		}
		if raw, err := sectionText(db, kf, kh); err == nil {
			m.splitKF8(raw)
			if len(m.parts) > 0 {
				return m, nil
			}
		}
		m.parts, m.chapters = nil, nil // fall back to the MOBI 6 half
	}
	raw, err := sectionText(db, 0, h)
	if err != nil {
		return nil, err
	}
	m.splitMOBI6(raw, h.encoding)
	if len(m.parts) == 0 {
		return nil, errors.New("the MOBI book has no text")
	}
	return m, nil
}

// kf8Header finds the KF8 header record: the one EXTH 121 names (combo
// files, after a BOUNDARY record) or record 0 of a standalone KF8 book.
// -1 means the book is MOBI 6 only.
func kf8Header(db *palmDB, h *mobiHeader) int {
	if v, ok := h.exth[121]; ok && len(v) == 4 {
		i := int(binary.BigEndian.Uint32(v))
		if r, err := db.rec(i); err == nil && bytes.HasPrefix(r, []byte("BOUNDARY")) {
			i++
		}
		if r, err := db.rec(i); err == nil && len(r) >= 20 && string(r[16:20]) == "MOBI" {
			return i
		}
	}
	if h.version >= 8 {
		return 0
	}
	return -1
}

var (
	reXMLStart   = regexp.MustCompile(`(?i)<\?xml`)
	reHTMLStart  = regexp.MustCompile(`(?i)<html[\s>]`)
	reKindleLink = regexp.MustCompile(`(?i)\shref=["']kindle:[^"']*["']`)
)

// splitKF8 cuts KF8 text into its parts, one per XHTML file of the book.
// Fragments follow their skeleton, so each part reads in order.
func (m *mobiBook) splitKF8(raw []byte) {
	s := strings.ToValidUTF8(string(raw), "�")
	s = reKindleLink.ReplaceAllString(s, "") // kindle:pos/embed targets are unknown without the FRAG index
	idx := reXMLStart.FindAllStringIndex(s, -1)
	if len(idx) == 0 {
		idx = reHTMLStart.FindAllStringIndex(s, -1)
	}
	if len(idx) == 0 {
		idx = [][]int{{0, 0}}
	}
	idx[0][0] = 0 // text before the first part joins it
	for i, x := range idx {
		end := len(s)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		part := s[x[0]:end]
		if plainOf(part) == "" {
			continue
		}
		m.parts = append(m.parts, part)
		m.chapters = append(m.chapters, Chapter{Title: firstHeading(part)})
	}
}

var (
	rePagebreak = regexp.MustCompile(`(?i)<mbp:pagebreak[^>]*>`)
	reFilepos   = regexp.MustCompile(`(?i)filepos=["']?0*(\d+)["']?`)
	reHeadingTx = regexp.MustCompile(`(?is)<h[1-6][^>]*>(.*?)</h[1-6]>`)
	reParaTx    = regexp.MustCompile(`(?is)<p(?:\s[^>]*)?>(.*?)</p>`)
	reAnyTag    = regexp.MustCompile(`(?s)<[^>]*>`)
)

// plainOf is the visible text of an HTML fragment.
func plainOf(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(reAnyTag.ReplaceAllString(s, " "))), " ")
}

// firstHeading is a chapter's title: its first heading, at most 80 runes;
// without headings (MOBI 6 books style titles with <font> and <b>) a short
// first paragraph.
func firstHeading(s string) string {
	if h := reHeadingTx.FindStringSubmatch(s); h != nil {
		t := plainOf(h[1])
		if r := []rune(t); len(r) > 80 {
			t = string(r[:80])
		}
		return t
	}
	for _, p := range reParaTx.FindAllStringSubmatch(s, 8) {
		if t := plainOf(p[1]); t != "" {
			if len([]rune(t)) <= 80 {
				return t
			}
			return ""
		}
	}
	return ""
}

// splitMOBI6 cuts MOBI 6 text at page breaks into chapters; filepos links
// (byte offsets into the text) become links to the chapter holding them.
func (m *mobiBook) splitMOBI6(raw []byte, encoding int) {
	var chunks [][]byte
	var starts []int
	prev := 0
	for _, l := range rePagebreak.FindAllIndex(raw, -1) {
		chunks, starts = append(chunks, raw[prev:l[0]]), append(starts, prev)
		prev = l[1]
	}
	chunks, starts = append(chunks, raw[prev:]), append(starts, prev)
	var keep [][]byte
	var keepStarts []int
	for i, c := range chunks {
		if plainOf(string(c)) == "" {
			continue
		}
		keep, keepStarts = append(keep, c), append(keepStarts, starts[i])
	}
	chapterAt := func(pos int) int {
		n := 0
		for i, s := range keepStarts {
			if s <= pos {
				n = i
			}
		}
		return n
	}
	for _, c := range keep {
		c = reFilepos.ReplaceAllFunc(c, func(a []byte) []byte {
			pos, _ := strconv.Atoi(string(reFilepos.FindSubmatch(a)[1]))
			return []byte(fmt.Sprintf(`href="mobi:/ch/%d"`, chapterAt(pos)))
		})
		s := mobiDecode(c, encoding)
		m.parts = append(m.parts, s)
		m.chapters = append(m.chapters, Chapter{Title: firstHeading(s)})
	}
}
