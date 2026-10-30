package discover

import (
	"context"
	"errors"
	"html"
	"math/rand/v2"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/fetch"
)

func init() { families = append(families, sufism{}) }

// sufism: Islamic mysticism, which the shelves lacked beside the gnostic and
// early Christian texts of esoteric (the owner's list, 2026-10-15): the Sufi
// classics in their old English translations, Rumi translated line by line
// with notes, the Ibn Arabi Society's papers, and the Sufism entries of the
// TDV İslâm Ansiklopedisi in Turkish.
type sufism struct{}

var sufiSources = []litSource{
	{"Sacred Texts", 3, sufiBookPick},
	{"TDV İslâm Ansiklopedisi", 3, tdvPick},
	{"Dar-al-Masnavi", 2, masnaviPick},
	{"Ibn Arabi Society", 2, miasPick},
}

func (sufism) Name() string { return "sufism" }
func (sufism) sources() int { return len(sufiSources) }

func (sufism) Draw(ctx context.Context, env Env) (Draw, error) {
	return drawWeighted(ctx, env, "sufism", sufiSources)
}

// sufiBase holds the Sufi shelf of the Sacred Texts archive's Islam section.
var sufiBase = "https://archive.sacred-texts.com/isl/"

// sufiBooks are the Sufi texts of that shelf (isl/index.htm, "Sufi Texts",
// 2026-10-15). Left out: Khayyam's Rubaiyat and the Gulistan (one file each),
// Burton's Kasidah (his own poem) and Kabir (Hindu shelf).
var sufiBooks = []string{
	"masnavi/index.htm", // Rumi, the Masnavi, tr. Whinfield
	"mes/index.htm",     // Rumi, the Mesnevi book one, tr. Redhouse, with Eflaki's Acts of the Adepts
	"bp/index.htm",      // Attar, Bird Parliament, tr. FitzGerald
	"pdh/index.htm",     // Hafiz, Poems from the Divan, tr. Bell
	"taa/index.htm",     // Ibn Arabi, the Tarjuman al-Ashwaq, tr. Nicholson
	"tah/index.htm",     // Ghazali, the Alchemy of Happiness
	"mishkat/index.htm", // Ghazali, the Niche for Lights
	"egt/index.htm",     // Sanai, the Enclosed Garden of the Truth
	"srg/index.htm",     // Shabistari, the Secret Rose Garden
	"saab/index.htm",    // Jami, Salaman and Absal
	"bus/index.htm",     // Sadi, the Bustan
	"ssw/index.htm",     // Sadi's Scroll of Wisdom
	"zun/index.htm",     // the Diwan of Zeb-un-Nissa
	"spa/index.htm",     // Selections from the Poetry of the Afghans
	"omy/index.htm",     // Palmer, Oriental Mysticism
	"siim/index.htm",    // Nicholson, Studies in Islamic Mysticism
	"moi/moi.htm",       // Nicholson, the Mystics of Islam (the whole book on one page)
	"iq/index.htm",      // Iqbal, the Secrets of the Self
}

// reBackMatter: a book's contents, index, endnotes and bibliography are not
// reading on their own.
var reBackMatter = regexp.MustCompile(`(?i)(^|: )(contents|index|page index|notes|bibliography|errata)\b`)

func sufiBookPick(ctx context.Context, env Env) (string, string, error) {
	var errs []string
	for n, i := range rand.Perm(len(sufiBooks)) {
		if n == 3 {
			break
		}
		book := sufiBooks[i]
		if path.Base(book) != "index.htm" { // a book on one page is the page
			return sufiBase + book, "", nil
		}
		for range 2 {
			target, title, err := descend(ctx, env.Fetcher, sufiBase+book, 4)
			if ctx.Err() != nil {
				return "", "", ctx.Err()
			}
			if err != nil {
				errs = append(errs, err.Error())
				break
			}
			title = strings.TrimSuffix(html.UnescapeString(title), " | Internet Sacred Text Archive")
			if !reBackMatter.MatchString(title) {
				return target, title, nil
			}
			errs = append(errs, book+": back matter ("+title+")")
		}
	}
	return "", "", errors.New(strings.Join(errs, "; "))
}

// tdvBase is the TDV İslâm Ansiklopedisi. An author's page lists every entry
// (or part of one) they wrote; these authors wrote its Sufism entries
// (counted 2026-10-15: Uludağ alone about 230).
var tdvBase = "https://islamansiklopedisi.org.tr"

var tdvAuthors = []string{"suleyman-uludag", "mustafa-kara", "resat-ongoren", "nihat-azamat", "necdet-tosun",
	"semih-ceyhan", "hasan-kamil-yilmaz", "mehmet-demirci", "ahmet-yasar-ocak", "mahmut-erol-kilic",
	"mustafa-tatci", "ethem-cebecioglu"}

func tdvPick(ctx context.Context, env Env) (string, string, error) {
	author := pick(tdvAuthors)
	return listPick(ctx, env.Fetcher, tdvBase+"/muellif/"+author, ".madde_liste_satir a[href]", func(u *url.URL) bool {
		return strings.Count(u.Path, "/") == 1 && len(u.Path) > 1
	})
}

// masnaviBase is Dar-al-Masnavi, Ibrahim Gamard's school of Masnavi studies
// (Mevlevi): passages of the Masnavi and odes of the Divan-e Shams translated
// from the Persian, each with its notes.
var masnaviBase = "https://www.dar-al-masnavi.org/"

var masnaviIndexes = []string{"book1.html", "book2.html", "book3.html", "book4.html", "book5.html", "book6.html", "odes_trans.html"}

var reMasnaviPage = regexp.MustCompile(`(?i)^/(n-[ivx]+-\d+|gh-\d+)[a-z]?\.html$`)

func masnaviPick(ctx context.Context, env Env) (string, string, error) {
	return listPick(ctx, env.Fetcher, masnaviBase+pick(masnaviIndexes), "a[href]", func(u *url.URL) bool {
		return reMasnaviPage.MatchString(u.Path)
	})
}

// miasArticles is the Ibn Arabi Society's list of papers, most of them from
// its Journal.
var miasArticles = "https://ibnarabisociety.org/articles/"

// reMiasNotPaper: the list also links the society's news, its courses'
// pages, event reports and the Journal's own index (a short course's own text stays:
// "ibn-arabi-short-course-on-love-james-morris").
var reMiasNotPaper = regexp.MustCompile(`(?i)news|programme|education-|overview|events?/|report|symposium|^/journal/$`)

func miasPick(ctx context.Context, env Env) (string, string, error) {
	return listPick(ctx, env.Fetcher, miasArticles, ".entry-content a[href]", func(u *url.URL) bool {
		return strings.Count(strings.Trim(u.Path, "/"), "/") == 0 && u.Path != "/" &&
			!skipExt[strings.ToLower(path.Ext(u.Path))] && !reNotContent.MatchString(u.Path) && !reMiasNotPaper.MatchString(u.Path)
	})
}

// listPick opens an index page and picks one of the links that sel finds
// there on the same site and keep accepts; it returns the link and its text.
func listPick(ctx context.Context, f *fetch.Fetcher, index, sel string, keep func(*url.URL) bool) (string, string, error) {
	gq, base, err := get(ctx, f, index)
	if err != nil {
		return "", "", err
	}
	type entry struct{ href, text string }
	var entries []entry
	seen := map[string]bool{}
	gq.Find(sel).Each(func(_ int, a *goquery.Selection) {
		u, err := base.Parse(strings.TrimSpace(a.AttrOr("href", "")))
		if err != nil || strings.TrimPrefix(u.Hostname(), "www.") != strings.TrimPrefix(base.Hostname(), "www.") || u.RawQuery != "" {
			return
		}
		u.Fragment = ""
		if !keep(u) || seen[u.String()] {
			return
		}
		seen[u.String()] = true
		entries = append(entries, entry{u.String(), strings.Join(strings.Fields(a.Text()), " ")})
	})
	if len(entries) == 0 {
		return "", "", errors.New("no entries on " + index)
	}
	e := pick(entries)
	return e.href, e.text, nil
}
