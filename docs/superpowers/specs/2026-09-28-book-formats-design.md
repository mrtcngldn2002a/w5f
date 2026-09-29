# More book formats — design

Date: 2026-09-28 · Status: design approved in chat, awaiting spec review
Milestone context: add-on to M3 (Library) before M5, W5F v0.4.x

## 1. Goal

Today W5F reads EPUB, TXT, HTML and Markdown books itself and hands every
other format to an external viewer. The owner wants the common e-book
formats read inside W5F, with everything the reader already offers for
EPUB: chapters, table of contents (`t`), `]`/`[`, resume where left,
"Continue reading" and `/` search.

Decisions taken with the owner:

- Formats read inside W5F: **MOBI / AZW3 (Kindle)**, **FB2 and .fb2.zip**,
  **PDF text**.
- PDF text comes from `pdftotext` (poppler) when it is installed, else from
  a pure-Go PDF library (a new Go module, downloaded only after the owner
  confirms the exact module and version at execution time).
- DJVU, CBZ and CBR stay with the external viewer (image based; comics are
  M8). PDFs always keep an "open in the external viewer" link.
- DRM is never removed or bypassed: protected files get a clear message.

Success criteria:

1. A DRM-free MOBI, AZW3 (KF8, including "combo" files) and FB2/.fb2.zip
   open in the reader with chapters, a table of contents and resume.
2. A text PDF opens as readable text, 10 pages per chapter; a scanned PDF
   says it has no text and offers the external viewer.
3. Library scans and downloads take title, author and language from inside
   these files; the search index covers their chapters.
4. DRM-protected or KFX files are recognised and refused with a message.
5. All existing EPUB behaviour and tests stay as they are.

## 2. One reader interface

```go
// Meta is what a book says about itself.
type Meta struct{ Title, Author, Lang string }

// Reader is an open book of any readable format.
type Reader interface {
	Info() Meta
	Contents() []Chapter   // reading order; Title may be ""
	ChapterDoc(i int, link func(ch int) string) (*doc.Document, error)
	Close() error
}

// Open picks the reader by the file's name and first bytes.
func Open(path string) (Reader, error)

var (
	ErrDRM         = errors.New("this book is DRM-protected; W5F cannot open it — use the reader it was bought for")
	ErrUnsupported = errors.New("this book format is not supported yet")
	ErrNoText      = errors.New("this PDF has no text layer (probably scanned)")
)
```

- EPUB keeps its type and fields; it gains `Info` and `Contents`.
- `chapterDoc`, `tocDoc`, `Scan`, `AddFile` and `index.Books` use `Open`
  instead of `OpenEPUB`. Plain text, HTML and Markdown books keep their
  current single-page path.
- Chapter titles come from the book's own table of contents when the format
  has one (EPUB, FB2), otherwise from the first heading of the chapter,
  otherwise "Part N" / "Pages 1–10".

## 3. MOBI and AZW3 (`internal/books/mobi.go`, `mobi_huff.go`)

Pure Go, no new dependency.

- **Container:** PalmDB (type/creator `BOOKMOBI`); record 0 holds the
  PalmDOC header (compression, text length, text record count, record size
  4096, encryption) and the MOBI header (text encoding 1252 or 65001,
  first non-text record, full-name offset/length, Huffman record
  offset/count, EXTH flag, extra-data flags).
- **DRM:** encryption ≠ 0 → `ErrDRM`. A KFX container (`CONT` magic, `.kfx`)
  → `ErrUnsupported` with "KFX (newer Kindle format) is not supported".
- **Text records:** trailing entries named by the extra-data flags
  (multibyte bytes, TBS indexing) are stripped from each record before
  decompression.
- **Compression:** 1 none, 2 PalmDOC (LZ77 variant), 17480 HUFF/CDIC (Huffman
  table + CDIC dictionaries, recursive dictionary entries).
- **EXTH:** 100 author, 503 title, 524 language, 121 KF8 boundary record.
  Without EXTH the full name from record 0 is the title.
- **Encoding:** 1252 is decoded as Windows-1252 (`golang.org/x/text`,
  already in the module cache), 65001 as UTF-8.
- **MOBI 6 (old Kindle):** the text is HTML; chapters split at
  `<mbp:pagebreak/>`; `filepos=` links are rewritten to the chapter holding
  that byte offset; images are dropped (alt text kept as for EPUB).
- **KF8 (AZW3 and the KF8 half of combo files):** the text records start
  after the boundary record (EXTH 121) with their own headers. The raw text
  is a sequence of parts, each a skeleton (`<?xml …` / `<html …`) followed by
  its fragments; W5F splits at each part start and converts every part as
  HTML (the parser puts fragment text after `</html>` into the body, so the
  reading order holds). `kindle:pos:fid:…` links point to the part of that
  fid when it is known; `kindle:embed:` images are dropped. The SKEL/FRAG
  and NCX indexes are not parsed in this round (titles come from headings).
- Combo files: the KF8 half is used when present, else MOBI 6.

## 4. FB2 (`internal/books/fb2.go`)

- `.fb2` or `.fb2.zip` (first `.fb2` entry inside the zip).
- XML with the declared encoding: UTF-8, Windows-1251, Windows-1252,
  KOI8-R and ISO-8859-5 via `golang.org/x/text/encoding/charmap`.
- Meta: `description/title-info` — `book-title`, `author` (first, middle,
  last name or nickname), `lang`.
- Chapters: the top-level `section`s of the main `body` (a body with no
  sections is one chapter). Nested sections become headings.
- Elements: `title` → heading; `p` → paragraph; `emphasis` → italic;
  `strong` → bold; `strikethrough`; `sub`/`sup`; `code`; `epigraph` and
  `cite` → quote; `poem`/`stanza`/`v` → lines with breaks; `text-author` →
  italic line; `empty-line` → blank; `subtitle` → small heading;
  `table` → table; `image` → image placeholder.
- Notes: the `body name="notes"` sections are footnotes; `a l:href="#n1"`
  links become footnote references (`[1]`), shown at the end of the chapter
  that cites them.

## 5. PDF text (`internal/books/pdf.go`)

- Extraction order: `pdftotext -enc UTF-8 <file> -` when `pdftotext` is on
  the PATH (pages separated by form feeds); otherwise the Go library,
  page by page.
- Pages are grouped 10 per chapter ("Pages 1–10"); `Info` from the PDF's
  document info (Title, Author) when present, else the file name.
- Text to paragraphs: blank lines split paragraphs; a hyphen at a line end
  joins the word; other single line breaks join with a space; lines that
  look like headings (short, no final punctuation, followed by a blank
  line) become headings; repeated page headers/footers (same line on most
  pages) are dropped.
- Fewer than 50 characters of text in the whole file → `ErrNoText`.
- The book page shows "→ open in the external viewer" for every PDF; the
  `ErrNoText` / extraction-error page offers the same.
- Extraction runs once per book and is cached as text in the W5F cache
  folder (`<cache>/pdftext/<book id>.txt`), so chapters open quickly.

## 6. Library changes

- Readable: `.epub .mobi .azw .azw3 .prc .fb2 .fb2.zip .pdf .txt .html .htm .md`.
  External: `.djvu .cbz .cbr`.
- `Scan` and `AddFile` read `Info` and the chapter count through `Open`;
  a DRM/KFX file is still listed (so the owner sees it) with a note on its
  book page.
- `index.Books` indexes every readable format through `Open`.
- Site catalogs and downloads need no change (they already accept these
  formats).

## 7. Error handling

- `ErrDRM`, `ErrUnsupported`, `ErrNoText`: the book page shows the message
  and an "open in the external viewer" link; nothing crashes, the book stays
  in the library.
- Corrupt files (bad offsets, truncated records): every read is bounds
  checked; an error names the format ("the MOBI file is damaged").
- `pdftotext` failing falls back to the Go library; both failing gives the
  error page.

## 8. Testing

Fixtures are generated by the tests (no downloaded samples in the repo):

- MOBI: uncompressed; PalmDOC-compressed (test encoder); HUFF/CDIC with a
  small synthetic table; trailing-entry stripping; EXTH meta; DRM flag;
  pagebreak chapters; filepos links; Windows-1252 text.
- KF8: a combo file with a boundary record; two parts → two chapters.
- FB2: UTF-8 and Windows-1251 files; `.fb2.zip`; poems, epigraphs, notes.
- PDF: a minimal text PDF written by the test for the Go path; the
  `pdftotext` path through a stub command on the PATH; the paragraph
  heuristics on sample text; a text-less PDF → `ErrNoText`.
- Library: `Scan` and `AddFile` read meta from each format; `index.Books`
  indexes a MOBI and an FB2; the EPUB tests stay green.

Live smoke (manual): Project Gutenberg's own "Kindle" (AZW3) and "older
Kindle" (MOBI) downloads of a public-domain book, a Standard Ebooks AZW3,
and an open-access PDF — each downloaded only after the owner agrees.

## 9. Out of scope

DRM removal of any kind, KFX, images inside books, the SKEL/FRAG/NCX
indexes of KF8, PDF layout (columns, tables, figures), DJVU/CBZ/CBR text,
DOCX/ODT/RTF, legacy TXT encodings.
