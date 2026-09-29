package fiction

import (
	"fmt"
	"net/url"
	"strings"

	"w5f/internal/doc"
	"w5f/internal/store"
)

type storeSerial = store.Serial

func joinDot(s []string) string { return strings.Join(s, " · ") }

// Preset is a serial offered on the home page.
type Preset struct{ Title, Author, URL string }

// presets are independent serials: a table of contents page, or a site's
// front page whose public post list gives the chapters (Twig, Unsong).
// Chosen with the owner, 2026-09-29.
var presets = []Preset{
	{"Worm", "Wildbow", "https://parahumans.wordpress.com/table-of-contents/"},
	{"Pact", "Wildbow", "https://pactwebserial.wordpress.com/table-of-contents/"},
	{"Twig", "Wildbow", "https://twigserial.wordpress.com/"},
	{"Ward", "Wildbow", "https://www.parahumans.net/table-of-contents/"},
	{"Pale", "Wildbow", "https://palewebserial.wordpress.com/table-of-contents/"},
	{"Claw", "Wildbow", "https://clawwebserial.blog/table-of-contents/"},
	{"Seek", "Wildbow", "https://seekwebserial.wordpress.com/table-of-contents/"},
	{"Unsong", "Scott Alexander", "https://unsongbook.com/"},
	{"Ra", "qntm", "https://qntm.org/ra"},
	{"A Practical Guide to Evil", "ErraticErrata", "https://practicalguidetoevil.wordpress.com/table-of-contents/"},
	{"Katalepsis", "Hungry", "https://katalepsis.net/table-of-contents/"},
	{"The Wandering Inn", "pirateaba", "https://wanderinginn.com/table-of-contents/"},
}

func openHref(u string) string { return "w5f:serial/open?" + url.Values{"u": {u}}.Encode() }

// homeDoc is the Internet Fiction page.
func homeDoc(env Env) (*doc.Document, error) {
	d := &doc.Document{Title: "Internet Fiction", URL: "w5f:fiction", Origin: "local", Lang: "en"}
	section := func(title string, items [][]doc.Block) {
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: title}}}, doc.List{Items: items})
	}
	item := func(text, href, hint string) []doc.Block {
		in := doc.Inline{{Text: text, Link: link(d, href, text)}}
		if hint != "" {
			in = append(in, plain("   "+hint, doc.Italic))
		}
		return []doc.Block{para(in...)}
	}

	followed, _ := env.DB.Serials(true)
	newTotal, _ := env.DB.NewChapterTotal()
	head := "Following"
	if newTotal > 0 {
		head = fmt.Sprintf("Following (%d new)", newTotal)
	}
	d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: head}}})
	if len(followed) == 0 {
		d.Blocks = append(d.Blocks, para(plain("Open a serial or a Reddit series and press F to follow it.", doc.Italic)))
	} else {
		if len(followed) > 6 {
			followed = followed[:6]
		}
		d.Blocks = append(d.Blocks, followedList(d, followed))
	}
	d.Blocks = append(d.Blocks, para(doc.Span{Text: "all followed serials", Link: link(d, "w5f:following", "following")},
		plain("   ", 0), doc.Span{Text: "check now", Link: link(d, "w5f:following/check", "check now")}))

	section("Weird Worlds", [][]doc.Block{
		item("SCP Foundation", "https://scp-wiki.wikidot.com/", ""),
		item("random SCP", "w5f:random/scp", "(r on any wiki page)"),
		item("Wanderers' Library", "https://wanderers-library.wikidot.com/", ""),
		item("random tale from the Library", "w5f:random/wl", ""),
		item("The Backrooms", "https://backrooms-wiki.wikidot.com/", ""),
		item("random Backrooms page", "w5f:random/backrooms", ""),
		item("There Is No Antimemetics Division — qntm", "https://scp-wiki.wikidot.com/antimemetics-division-hub", "ideas that erase themselves"),
		item("Sarkicism — flesh, gnosis and the Mekhane", "https://scp-wiki.wikidot.com/sarkicism-hub", "Yaldabaoth, Archons, a lost empire"),
		item("Alagadda — the Hanged King's masquerade", "https://scp-wiki.wikidot.com/scp-2264", "SCP-2264"),
		item("Dr. Wondertainment", "https://scp-wiki.wikidot.com/dr-wondertainment-hub", "nightmare toys"),
		item("Serpent's Hand — the Library and the Ways", "https://scp-wiki.wikidot.com/serpent-s-hand-hub", ""),
		item("Obscure & archived worlds", "w5f:worlds", "25 small fiction wikis; X visits them too"),
	})
	horror := [][]doc.Block{}
	for _, sub := range []string{"nosleep", "shortscarystories", "LibraryOfShadows", "TheCrypticCompendium", "Odd_directions", "Cryosleep"} {
		horror = append(horror, item("r/"+sub, "https://www.reddit.com/r/"+sub+"/top/?t=all", ""))
	}
	horror = append(horror, item("Creepypasta.com", "https://www.creepypasta.com/", ""))
	section("Horror", horror)

	serials := [][]doc.Block{
		item("Royal Road — best rated", "w5f:fiction/rr/best", "search: g → rr <words>"),
		item("Royal Road — latest updates", "w5f:fiction/rr/latest", ""),
	}
	for _, p := range presets {
		serials = append(serials, item(p.Title+" — "+p.Author, openHref(p.URL), ""))
	}
	serials = append(serials, item("r/redditserials", "https://www.reddit.com/r/redditserials/", ""),
		item("r/HFY", "https://www.reddit.com/r/HFY/top/?t=all", ""),
		item("any serial by address", "w5f:fiction", "g → serial <address of its contents or first chapter>"))
	section("Web Serials", serials)

	forums := [][]doc.Block{
		item("SpaceBattles — Creative Writing", "https://forums.spacebattles.com/forums/creative-writing.18/", "threads with threadmarks open as serials"),
		item("Sufficient Velocity — User Fiction", "https://forums.sufficientvelocity.com/forums/user-fiction.2/", ""),
	}
	if env.Mature {
		forums = append(forums, item("Questionable Questing — Creative Writing (adult)", "https://forum.questionablequesting.com/forums/creative-writing.19/", ""))
	}
	section("Forum Fiction", forums)

	me := "connect your account: g → ao3-login"
	if LoadAO3Session() != "" {
		me = "bookmarks, subscriptions, history"
	}
	imported, _ := ImportAO3Downloads(env)
	fan := [][]doc.Block{
		item("Archive of Our Own", "https://archiveofourown.org/", "search: g → ao3 <words>"),
		item("My AO3", "w5f:fiction/ao3/me", me),
		item("check Downloads for AO3 books now", "w5f:fiction/ao3/import", "AO3 → Download → EPUB in a browser; W5F adds it to the Library"),
		item("save any story as EPUB with FanFicFare", "w5f:fiction/ffr", "g → ffr <address>"),
	}
	if imported > 0 {
		fan = append([][]doc.Block{{doc.Notice{Kind: "info", Text: fmt.Sprintf("%d AO3 books added to the Library from your Downloads folder.", imported)}}}, fan...)
	}
	section("Fanfiction", fan)

	if all, _ := env.DB.Serials(false); len(all) > 0 {
		var items [][]doc.Block
		for i, s := range all {
			if i == 8 {
				break
			}
			items = append(items, item(s.Title, serialHref(s.ID, ""), siteName(s.Kind, s.URL)))
		}
		section("Your serials", items)
	}
	return d, nil
}
