package smallweb

import "w5f/internal/doc"

// Search addresses: Wiby (?q=) and Marginalia's text-friendly interface
// (?query=), which Marginalia itself recommends to text-based browsers.
const (
	wibySearch       = "https://wiby.me/"
	marginaliaSearch = "https://old-search.marginalia.nu/search"
)

// menuDoc is the Small Web page.
func menuDoc() *doc.Document {
	d := &doc.Document{Title: "Small Web", URL: "w5f:smallweb", Origin: "local", Lang: "en"}
	section := func(title string, items [][2]string) {
		var list [][]doc.Block
		for _, it := range items {
			d.Links = append(d.Links, doc.Link{Href: it[1], Text: it[0]})
			list = append(list, []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: it[0], Link: len(d.Links)}}}})
		}
		d.Blocks = append(d.Blocks, doc.Heading{Level: 2, Text: doc.Inline{{Text: title}}}, doc.List{Items: list})
	}
	d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: "Gemini capsules and Gopher holes: the quiet, text-only internet. Type any gemini:// or gopher:// address at g; on pages that ask for input, press enter and type your answer.", Style: doc.Italic}}})
	section("Gemini", [][2]string{
		{"Project Gemini", "gemini://geminiprotocol.net/"},
		{"Cosmos — what capsules posted lately", "gemini://skyjake.fi/~Cosmos/"},
		{"Kennedy — search Geminispace", "gemini://kennedy.gemi.dev/search"},
		{"TLGS — another Gemini search", "gemini://tlgs.one/"},
		{"BBS — Geminispace forums", "gemini://bbs.geminispace.org/"},
	})
	section("Gopher", [][2]string{
		{"Floodgap — the Gopher gateway", "gopher://gopher.floodgap.com/1/"},
		{"Veronica-2 — search Gopherspace", "gopher://gopher.floodgap.com/7/v2/vs"},
		{"SDF Public Access UNIX", "gopher://sdf.org/1/"},
	})
	section("Small web search (web)", [][2]string{
		{"Wiby — the old-style web, surprise me", "w5f:discover/wiby"},
		{"Wiby — search the old-style web", WebSearchPage(wibySearch, "q", "Search Wiby: hand-picked, lightweight pages of the old web.")},
		{"Marginalia — random small sites (explore)", "w5f:smallweb/marginalia-random"},
		{"Marginalia Search — the non-commercial web", WebSearchPage(marginaliaSearch, "query", "Search the independent, non-commercial web with Marginalia.")},
	})
	return d
}
