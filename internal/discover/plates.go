package discover

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"

	"w5f/internal/doc"
	"w5f/internal/fetch"
	"w5f/internal/picture"
)

func init() { families = append(families, plates{}) }

// plates: pictures to look at, a plate, a print, a painting or a photograph
// with its label (P4, the owner, 2026-10-30: one shelf; its sources were
// tried live and chosen by the owner). Famous works: the Art Institute of
// Chicago, the Met's highlights, the Cleveland Museum of Art. Plates and
// illustrations: the Wellcome Collection, the Public Domain Review's
// collections, BHL's plates, Old Book Illustrations. Photographs: Commons'
// featured pictures, the Met's early photographs (1839–1920: most of its
// later ones are not yet free) and the Art Institute's, NASA.
//
// Each page is W5F's own (w5f:discover/plate?…): the picture, who made it
// and when, and what the museum says of it. The reader draws its pictures
// in the page; z shows one large, and from there in its own colours.
type plates struct{}

var plateSources = []litSource{
	{"Art Institute of Chicago", 2, aicPick(false)},
	{"The Met", 2, metPick("isHighlight=true&hasImages=true&q=*")},
	{"Cleveland Museum of Art", 2, clevelandPick},
	{"Wellcome Collection", 2, wellcomePick},
	{"Public Domain Review", 2, pdrCollectionPick},
	{"Old Book Illustrations", 2, obiPick},
	{"BHL plates", 1, bhlPlatePick},
	{"Wikimedia Commons", 2, commonsPick},
	{"The Met photographs", 1, metPick("departmentId=19&hasImages=true&q=*&dateBegin=1839&dateEnd=1920")},
	{"Art Institute photographs", 1, aicPick(true)},
	{"NASA", 1, nasaPick},
}

func (plates) Name() string { return "plates" }
func (plates) sources() int { return len(plateSources) }

func (plates) Draw(ctx context.Context, env Env) (Draw, error) {
	return drawWeighted(ctx, env, "plates", plateSources)
}

// plateTarget is the address of a plate page.
func plateTarget(src, id string) string {
	return "w5f:discover/plate?src=" + src + "&id=" + url.QueryEscape(id)
}

// getJSON reads a JSON answer (cached like any page).
func getJSON(ctx context.Context, f *fetch.Fetcher, raw string, header http.Header, v any) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	resp, err := f.Get(ctx, u, fetch.Options{Header: header})
	if err != nil {
		return err
	}
	return json.Unmarshal(resp.Body, v)
}

// htmlParas is the paragraphs of a bit of HTML (a museum's description).
func htmlParas(s string) []string {
	if !strings.Contains(s, "<") {
		return splitParas(s)
	}
	gq, err := goquery.NewDocumentFromReader(strings.NewReader("<body>" + strings.NewReplacer("<br>", "\n", "<br/>", "\n", "<br />", "\n").Replace(s) + "</body>"))
	if err != nil {
		return nil
	}
	var out []string
	ps := gq.Find("p")
	if ps.Length() == 0 {
		return splitParas(gq.Text())
	}
	ps.Each(func(_ int, p *goquery.Selection) {
		if t := strings.Join(strings.Fields(p.Text()), " "); t != "" {
			out = append(out, t)
		}
	})
	return out
}

func splitParas(s string) []string {
	var out []string
	for _, p := range reBlankLine.Split(s, -1) {
		if t := strings.Join(strings.Fields(p), " "); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// plate is what a plate page shows.
type plate struct {
	title, byline string
	image, alt    string
	caption       string
	paras         []string
	facts         []doc.KV // medium, place, licence…
	home, homeTx  string   // the picture's page at its source
	more          []plateImage
}

type plateImage struct{ src, caption string }

func (p plate) doc(target string) *doc.Document {
	d := &doc.Document{URL: target, Title: p.title, Byline: p.byline, Lang: "en", Origin: "live", Pictures: true}
	d.Blocks = append(d.Blocks, doc.Image{Src: p.image, Alt: p.alt, Caption: p.caption})
	for _, t := range p.paras {
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: t}}})
	}
	for _, m := range p.more {
		d.Blocks = append(d.Blocks, doc.Image{Src: m.src, Caption: m.caption})
	}
	if len(p.facts) > 0 {
		var in doc.Inline
		for i, kv := range p.facts {
			if i > 0 {
				in = append(in, doc.Span{Break: true})
			}
			in = append(in, doc.Span{Text: kv.Key + ": ", Style: doc.Italic}, doc.Span{Text: kv.Value})
		}
		d.Blocks = append(d.Blocks, doc.Rule{}, doc.Paragraph{Text: in})
	}
	if p.home != "" {
		d.Links = []doc.Link{{Href: p.home, Text: p.homeTx}}
		d.Blocks = append(d.Blocks, doc.Paragraph{Text: doc.Inline{{Text: p.homeTx, Link: 1}}})
	}
	return d
}

// facts keeps the pairs whose value is not empty.
func facts(kv ...string) []doc.KV {
	var out []doc.KV
	for i := 0; i+1 < len(kv); i += 2 {
		if v := strings.Join(strings.Fields(kv[i+1]), " "); v != "" {
			out = append(out, doc.KV{Key: kv[i], Value: v})
		}
	}
	return out
}

func joinNonEmpty(sep string, xs ...string) string {
	var out []string
	for _, x := range xs {
		if x = strings.Join(strings.Fields(x), " "); x != "" {
			out = append(out, x)
		}
	}
	return strings.Join(out, sep)
}

// plateRoute builds the page of a plate address.
func plateRoute(ctx context.Context, env Env, target string) (*doc.Document, error) {
	q, err := url.ParseQuery(strings.TrimPrefix(target, "w5f:discover/plate?"))
	if err != nil {
		return nil, err
	}
	id := q.Get("id")
	if id == "" {
		return nil, errors.New("plate: bad address")
	}
	var p plate
	switch q.Get("src") {
	case "aic":
		p, err = aicPlate(ctx, env.Fetcher, id)
	case "met":
		p, err = metPlate(ctx, env.Fetcher, id)
	case "cma":
		p, err = clevelandPlate(ctx, env.Fetcher, id)
	case "wellcome":
		p, err = wellcomePlate(ctx, env.Fetcher, id)
	case "pdr":
		p, err = pdrPlate(ctx, env.Fetcher, id)
	case "obi":
		p, err = obiPlate(ctx, env.Fetcher, id)
	case "bhl":
		p, err = bhlPlate(ctx, env.Fetcher, id)
	case "commons":
		p, err = commonsPlate(ctx, env.Fetcher, id)
	case "nasa":
		p, err = nasaPlate(ctx, env.Fetcher, id)
	default:
		return nil, errors.New("plate: unknown source")
	}
	if err != nil {
		return nil, err
	}
	if p.image == "" {
		return nil, errors.New("plate: no picture")
	}
	return p.doc(target), nil
}

// --- The Art Institute of Chicago: CC0 data, its descriptions CC BY. Its
// API asks callers to name themselves in AIC-User-Agent.

var aicHeader = http.Header{"AIC-User-Agent": {"w5f (W5F Archive Node terminal reader)"}}

// aicMaxID is about the highest id of a public-domain work (2026-10-30).
const aicMaxID = 260000

const aicFields = "id,title,artist_display,date_display,image_id,description,short_description,medium_display,place_of_origin,credit_line,artwork_type_title"

// aicPick draws a public-domain work with a picture: one the museum
// describes, or one of its photographs.
func aicPick(photo bool) func(context.Context, Env) (string, string, error) {
	return func(ctx context.Context, env Env) (string, string, error) {
		must := []any{
			map[string]any{"term": map[string]any{"is_public_domain": true}},
			map[string]any{"exists": map[string]any{"field": "image_id"}},
		}
		if photo {
			must = append(must, map[string]any{"term": map[string]any{"artwork_type_title.keyword": "Photograph"}})
		} else {
			must = append(must, map[string]any{"exists": map[string]any{"field": "description"}})
		}
		// The API's random score is ignored: the work is the first at or
		// after a random id (works after a gap in the ids come up more).
		must = append(must, map[string]any{"range": map[string]any{"id": map[string]any{"gte": rand.IntN(aicMaxID)}}})
		body, _ := json.Marshal(map[string]any{
			"query":  map[string]any{"bool": map[string]any{"must": must}},
			"sort":   []any{map[string]any{"id": "asc"}},
			"limit":  1,
			"fields": []string{"id", "title"},
		})
		h := http.Header{"Content-Type": {"application/json"}}
		for k, v := range aicHeader {
			h[k] = v
		}
		b, _, err := rawDo(ctx, env.Fetcher, "POST", "https://api.artic.edu/api/v1/artworks/search", h, body)
		if err != nil {
			return "", "", err
		}
		var r struct {
			Data []struct {
				ID    int    `json:"id"`
				Title string `json:"title"`
			} `json:"data"`
		}
		if err := json.Unmarshal(b, &r); err != nil || len(r.Data) == 0 {
			return "", "", errors.New("no work")
		}
		return plateTarget("aic", strconv.Itoa(r.Data[0].ID)), r.Data[0].Title, nil
	}
}

func aicPlate(ctx context.Context, f *fetch.Fetcher, id string) (plate, error) {
	var r struct {
		Data struct {
			ID           int    `json:"id"`
			Title        string `json:"title"`
			Artist       string `json:"artist_display"`
			Date         string `json:"date_display"`
			ImageID      string `json:"image_id"`
			Description  string `json:"description"`
			Short        string `json:"short_description"`
			Medium       string `json:"medium_display"`
			Place        string `json:"place_of_origin"`
			Credit       string `json:"credit_line"`
			Type         string `json:"artwork_type_title"`
			ThumbnailAlt struct {
				Alt string `json:"alt_text"`
			} `json:"thumbnail"`
		} `json:"data"`
	}
	if err := getJSON(ctx, f, "https://api.artic.edu/api/v1/artworks/"+url.PathEscape(id)+"?fields="+aicFields+",thumbnail", aicHeader, &r); err != nil {
		return plate{}, err
	}
	a := r.Data
	if a.ImageID == "" {
		return plate{}, errors.New("no picture")
	}
	paras := htmlParas(a.Description)
	if len(paras) == 0 {
		paras = htmlParas(a.Short)
	}
	return plate{
		title:  a.Title,
		byline: joinNonEmpty(" · ", strings.ReplaceAll(a.Artist, "\n", ", "), a.Date, "Art Institute of Chicago"),
		image:  "https://www.artic.edu/iiif/2/" + a.ImageID + "/full/843,/0/default.jpg",
		alt:    a.ThumbnailAlt.Alt,
		paras:  paras,
		facts:  facts("Type", a.Type, "Medium", a.Medium, "Place", a.Place, "Credit", a.Credit, "Licence", "CC0 (description CC BY)"),
		home:   fmt.Sprintf("https://www.artic.edu/artworks/%d", a.ID), homeTx: "This work at the Art Institute",
	}, nil
}

// --- The Met: its open-access objects (CC0). Search 1.1 pages its answers.

var (
	searchTotals   = map[string]int{}
	searchTotalsMu sync.Mutex
)

// randomOffset is a random place among a search's answers, the total
// learned once (from a first answer at offset 0) and kept while W5F runs.
func randomOffset(key string, total func() (int, error)) (int, error) {
	searchTotalsMu.Lock()
	n, ok := searchTotals[key]
	searchTotalsMu.Unlock()
	if !ok {
		var err error
		if n, err = total(); err != nil {
			return 0, err
		}
		searchTotalsMu.Lock()
		searchTotals[key] = n
		searchTotalsMu.Unlock()
	}
	if n <= 0 {
		return 0, errors.New("nothing to draw from")
	}
	return rand.IntN(n), nil
}

func metPick(query string) func(context.Context, Env) (string, string, error) {
	base := "https://collectionapi.metmuseum.org/public/collection/v1.1/search?" + query + "&limit=1"
	draw := metDraw(base)
	return func(ctx context.Context, env Env) (target, title string, err error) {
		for range 3 {
			if target, title, err = draw(ctx, env); err == nil || ctx.Err() != nil {
				break
			}
		}
		return target, title, err
	}
}

func metDraw(base string) func(context.Context, Env) (string, string, error) {
	type answer struct {
		Total int   `json:"total"`
		IDs   []int `json:"objectIDs"`
	}
	return func(ctx context.Context, env Env) (string, string, error) {
		off, err := randomOffset(base, func() (int, error) {
			var a answer
			err := getJSON(ctx, env.Fetcher, base+"&offset=0", nil, &a)
			return min(a.Total, 10000), err // the search pages through the first 10,000
		})
		if err != nil {
			return "", "", err
		}
		u, _ := url.Parse(base + "&offset=" + strconv.Itoa(off))
		resp, err := env.Fetcher.Get(ctx, u, fetch.Options{NoStore: true})
		if err != nil {
			return "", "", err
		}
		var a answer
		if err := json.Unmarshal(resp.Body, &a); err != nil || len(a.IDs) == 0 {
			return "", "", errors.New("no object")
		}
		// Not every object with a picture is in the public domain: the
		// object's own record says (and is then at hand for the page).
		id := strconv.Itoa(a.IDs[0])
		p, err := metPlate(ctx, env.Fetcher, id)
		if err != nil {
			return "", "", err
		}
		return plateTarget("met", id), p.title, nil
	}
}

func metPlate(ctx context.Context, f *fetch.Fetcher, id string) (plate, error) {
	var o struct {
		Title      string `json:"title"`
		Artist     string `json:"artistDisplayName"`
		ArtistBio  string `json:"artistDisplayBio"`
		Date       string `json:"objectDate"`
		Medium     string `json:"medium"`
		Dimensions string `json:"dimensions"`
		Culture    string `json:"culture"`
		Period     string `json:"period"`
		Department string `json:"department"`
		Credit     string `json:"creditLine"`
		Image      string `json:"primaryImageSmall"`
		Public     bool   `json:"isPublicDomain"`
		URL        string `json:"objectURL"`
	}
	if err := getJSON(ctx, f, "https://collectionapi.metmuseum.org/public/collection/v1/objects/"+url.PathEscape(id), nil, &o); err != nil {
		return plate{}, err
	}
	if !o.Public || o.Image == "" {
		return plate{}, errors.New("no open picture")
	}
	who := o.Artist
	if o.ArtistBio != "" {
		who += " (" + o.ArtistBio + ")"
	}
	return plate{
		title:  o.Title,
		byline: joinNonEmpty(" · ", who, o.Date, "The Metropolitan Museum of Art"),
		image:  o.Image,
		facts:  facts("Medium", o.Medium, "Culture", o.Culture, "Period", o.Period, "Dimensions", o.Dimensions, "Department", o.Department, "Credit", o.Credit, "Licence", "CC0"),
		home:   o.URL, homeTx: "This work at the Met",
	}, nil
}

// --- The Cleveland Museum of Art: CC0, often with its wall label. Half the
// draws are its highlights.

func clevelandPick(ctx context.Context, env Env) (string, string, error) {
	base := "https://openaccess-api.clevelandart.org/api/artworks/?cc0=1&has_image=1"
	if rand.IntN(2) == 0 {
		base += "&highlight=1"
	}
	type answer struct {
		Info struct {
			Total int `json:"total"`
		} `json:"info"`
		Data []struct {
			ID    int    `json:"id"`
			Title string `json:"title"`
			Desc  string `json:"description"`
		} `json:"data"`
	}
	for range 4 {
		off, err := randomOffset(base, func() (int, error) {
			var a answer
			err := getJSON(ctx, env.Fetcher, base+"&limit=0", nil, &a)
			return a.Info.Total, err
		})
		if err != nil {
			return "", "", err
		}
		var a answer
		if err := getJSON(ctx, env.Fetcher, fmt.Sprintf("%s&limit=1&skip=%d", base, off), nil, &a); err != nil {
			return "", "", err
		}
		// A work without its label is a line of data: draw another.
		if len(a.Data) > 0 && len(strings.Fields(a.Data[0].Desc)) >= 20 {
			return plateTarget("cma", strconv.Itoa(a.Data[0].ID)), a.Data[0].Title, nil
		}
	}
	return "", "", errors.New("no described work")
}

func clevelandPlate(ctx context.Context, f *fetch.Fetcher, id string) (plate, error) {
	var r struct {
		Data struct {
			Title     string   `json:"title"`
			Date      string   `json:"creation_date"`
			Desc      string   `json:"description"`
			Fun       string   `json:"did_you_know"`
			Tombstone string   `json:"tombstone"`
			Technique string   `json:"technique"`
			Type      string   `json:"type"`
			Culture   []string `json:"culture"`
			Credit    string   `json:"creditline"`
			URL       string   `json:"url"`
			Creators  []struct {
				Description string `json:"description"`
			} `json:"creators"`
			Images struct {
				Web struct {
					URL string `json:"url"`
				} `json:"web"`
			} `json:"images"`
		} `json:"data"`
	}
	if err := getJSON(ctx, f, "https://openaccess-api.clevelandart.org/api/artworks/"+url.PathEscape(id), nil, &r); err != nil {
		return plate{}, err
	}
	a := r.Data
	var who []string
	for _, c := range a.Creators {
		who = append(who, c.Description)
	}
	paras := htmlParas(a.Desc)
	if a.Fun != "" {
		paras = append(paras, htmlParas(a.Fun)...)
	}
	return plate{
		title:  a.Title,
		byline: joinNonEmpty(" · ", strings.Join(who, "; "), a.Date, "Cleveland Museum of Art"),
		image:  a.Images.Web.URL,
		paras:  paras,
		facts:  facts("Type", a.Type, "Technique", a.Technique, "Culture", strings.Join(a.Culture, "; "), "Credit", a.Credit, "Licence", "CC0"),
		home:   a.URL, homeTx: "This work at the Cleveland Museum of Art",
	}, nil
}

// --- The Wellcome Collection: its pictures (the iconographic collection)
// and its digital images, public domain or CC BY. Its digital images of
// today's laboratories (B numbers) are left out: the shelf is old pictures.

var reWellcomeImage = regexp.MustCompile(`/image/([A-Z])\d`)

type wellcomeWork struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Lettering   string `json:"lettering"`
	Thumbnail   struct {
		URL     string `json:"url"`
		License struct {
			Label string `json:"label"`
		} `json:"license"`
	} `json:"thumbnail"`
	Contributors []struct {
		Agent struct {
			Label string `json:"label"`
		} `json:"agent"`
	} `json:"contributors"`
	Production []struct {
		Label string `json:"label"`
	} `json:"production"`
	Subjects []struct {
		Label string `json:"label"`
	} `json:"subjects"`
	Notes []struct {
		Contents []string `json:"contents"`
	} `json:"notes"`
}

func wellcomeOld(w wellcomeWork) bool {
	m := reWellcomeImage.FindStringSubmatch(w.Thumbnail.URL)
	return m != nil && m[1] != "B" && len(strings.Fields(w.Title)) >= 4
}

func wellcomePick(ctx context.Context, env Env) (string, string, error) {
	kind := "k" // pictures
	if rand.IntN(3) == 0 {
		kind = "q" // digital images
	}
	base := "https://api.wellcomecollection.org/catalogue/v2/works?workType=" + kind +
		"&availabilities=online&items.locations.license=pdm,cc-0,cc-by&pageSize=100"
	type answer struct {
		Pages   int            `json:"totalPages"`
		Results []wellcomeWork `json:"results"`
	}
	off, err := randomOffset(base, func() (int, error) {
		var a answer
		err := getJSON(ctx, env.Fetcher, base+"&page=1", nil, &a)
		return min(a.Pages, 100), err // the API pages through the first 10,000 only
	})
	if err != nil {
		return "", "", err
	}
	var a answer
	if err := getJSON(ctx, env.Fetcher, fmt.Sprintf("%s&page=%d", base, off+1), nil, &a); err != nil {
		return "", "", err
	}
	// Described pictures first, then any with a telling title.
	var described, titled []wellcomeWork
	for _, w := range a.Results {
		if !wellcomeOld(w) {
			continue
		}
		if len(strings.Fields(w.Description)) >= 20 {
			described = append(described, w)
		} else {
			titled = append(titled, w)
		}
	}
	for _, ws := range [][]wellcomeWork{described, titled} {
		if len(ws) > 0 {
			w := pick(ws)
			return plateTarget("wellcome", w.ID), w.Title, nil
		}
	}
	return "", "", errors.New("no old picture on that page")
}

func wellcomePlate(ctx context.Context, f *fetch.Fetcher, id string) (plate, error) {
	var w wellcomeWork
	if err := getJSON(ctx, f, "https://api.wellcomecollection.org/catalogue/v2/works/"+url.PathEscape(id)+"?include=notes,contributors,production,subjects", nil, &w); err != nil {
		return plate{}, err
	}
	if w.Thumbnail.URL == "" {
		return plate{}, errors.New("no picture")
	}
	var who, made, subj []string
	for _, c := range w.Contributors {
		who = append(who, c.Agent.Label)
	}
	for _, p := range w.Production {
		made = append(made, p.Label)
	}
	for _, s := range w.Subjects {
		subj = append(subj, s.Label)
	}
	paras := htmlParas(w.Description)
	for _, n := range w.Notes {
		for _, c := range n.Contents {
			paras = append(paras, htmlParas(c)...)
		}
	}
	return plate{
		title:  w.Title,
		byline: joinNonEmpty(" · ", strings.Join(who, "; "), strings.Join(made, "; "), "Wellcome Collection"),
		image:  strings.Replace(w.Thumbnail.URL, "/full/300,/", "/full/1000,/", 1),
		paras:  paras,
		facts:  facts("Lettering", w.Lettering, "Subjects", strings.Join(subj, "; "), "Licence", w.Thumbnail.License.Label),
		home:   "https://wellcomecollection.org/works/" + w.ID, homeTx: "This picture at the Wellcome Collection",
	}, nil
}

// --- The Public Domain Review's collections: an introduction, then the
// pictures with their captions.

var pdrCollections = sitemapPick("https://publicdomainreview.org/sitemap-index.xml", `sitemap-\d+\.xml$`, `publicdomainreview\.org/collection/[^/]+/?$`, ``)

// pdrCollectionPick takes a collection of pictures (some are a film or a
// recording: the page is read to tell, and is then at hand).
func pdrCollectionPick(ctx context.Context, env Env) (string, string, error) {
	var err error
	for range 3 {
		var target string
		if target, _, err = pdrCollections(ctx, env); err != nil {
			return "", "", err
		}
		var p plate
		if p, err = pdrPlate(ctx, env.Fetcher, target); err == nil && p.image != "" {
			return plateTarget("pdr", target), p.title, nil
		}
		if err == nil {
			err = errors.New("no pictures in " + target)
		}
	}
	return "", "", err
}

// pdrSized asks the image CDN for a picture about the size the screen shows.
func pdrSized(src string) string {
	if i := strings.Index(src, "?"); i >= 0 {
		src = src[:i]
	}
	return src + "?width=1000"
}

func pdrPlate(ctx context.Context, f *fetch.Fetcher, page string) (plate, error) {
	if !strings.HasPrefix(page, "https://publicdomainreview.org/collection/") {
		return plate{}, errors.New("not a collection")
	}
	gq, _, err := get(ctx, f, page)
	if err != nil {
		return plate{}, err
	}
	p := plate{title: strings.Join(strings.Fields(gq.Find("h1").First().Text()), " "), home: page, homeTx: "This collection at the Public Domain Review"}
	gq.Find(".collection-preamble p").Each(func(_ int, s *goquery.Selection) {
		if t := strings.Join(strings.Fields(s.Text()), " "); t != "" {
			p.paras = append(p.paras, t)
		}
	})
	by := strings.Join(strings.Fields(gq.Find(".collection-byline").First().Text()), " ")
	p.byline = joinNonEmpty(" · ", by, "The Public Domain Review")
	gq.Find(".collection__gallery__cell").Each(func(_ int, c *goquery.Selection) {
		img := c.Find("img").First()
		src := img.AttrOr("data-src", img.AttrOr("src", ""))
		if !strings.Contains(src, "/collections/") {
			return
		}
		cap := strings.Join(strings.Fields(c.Find(".collection__gallery__caption").Text()), " ")
		if p.image == "" {
			p.image, p.caption = pdrSized(src), cap
			return
		}
		if len(p.more) < 11 { // a dozen pictures is a page; the rest are on the site
			p.more = append(p.more, plateImage{pdrSized(src), cap})
		}
	})
	return p, nil
}

// --- Old Book Illustrations: one illustration, its description, and the
// book it comes from.

var reOBIPage = regexp.MustCompile(`oldbookillustrations\.com/illustrations/[^/]+/$`)

func obiPick(ctx context.Context, env Env) (string, string, error) {
	u, _ := url.Parse("https://www.oldbookillustrations.com/sitemap.xml")
	resp, err := env.Fetcher.Get(ctx, u, fetch.Options{})
	if err != nil {
		return "", "", err
	}
	var x sitemapXML
	if err := xml.Unmarshal(resp.Body, &x); err != nil {
		return "", "", err
	}
	var ok []string
	for _, l := range x.URLs {
		if reOBIPage.MatchString(l) {
			ok = append(ok, l)
		}
	}
	if len(ok) == 0 {
		return "", "", errors.New("no illustration in the sitemap")
	}
	l := pick(ok)
	return plateTarget("obi", l), slugTitle(l), nil
}

func obiPlate(ctx context.Context, f *fetch.Fetcher, page string) (plate, error) {
	if !reOBIPage.MatchString(page) {
		return plate{}, errors.New("not an illustration")
	}
	gq, base, err := get(ctx, f, page)
	if err != nil {
		return plate{}, err
	}
	p := plate{title: strings.Join(strings.Fields(gq.Find("h1#title").First().Text()), " "), home: page, homeTx: "This illustration at Old Book Illustrations"}
	if img, ok := gq.Find(`meta[property="og:image"]`).Attr("content"); ok && !strings.Contains(img, "/high-res/-") {
		p.image = img
	} else if src, ok := gq.Find("figure img").First().Attr("src"); ok {
		if u, err := base.Parse(src); err == nil {
			p.image = u.String()
		}
	}
	p.alt = gq.Find("figure img").First().AttrOr("alt", "")
	p.caption = strings.Join(strings.Fields(gq.Find("figcaption p").First().Text()), " ")
	gq.Find(".description p").Each(func(_ int, s *goquery.Selection) {
		if t := strings.Join(strings.Fields(s.Text()), " "); t != "" {
			p.paras = append(p.paras, t)
		}
	})
	var kv []string
	gq.Find("dl dt").Each(func(_ int, dt *goquery.Selection) {
		k := strings.TrimSuffix(strings.TrimSpace(dt.Text()), ":")
		switch k {
		case "Name", "Dates", "Technique", "Title", "Author", "Publisher":
			kv = append(kv, k, dt.NextFiltered("dd").Text())
		}
	})
	p.facts = facts(kv...)
	var artist, book string
	for _, f := range p.facts {
		switch f.Key {
		case "Name":
			artist = f.Value
		case "Title":
			book = f.Value
		}
	}
	p.byline = joinNonEmpty(" · ", artist, book, "Old Book Illustrations")
	return p, nil
}

// --- BHL's plates: a leaf of an illustrated book (the curious books'
// list) whose few words name a plate.

var rePlateLabel = regexp.MustCompile(`(?i)\b(plate|pl\.|tab\.|tafel|planche|taf\.)\s*[ivxlc\d]+\b`)

// reIllustrated: titles of books made for their pictures.
var reIllustrated = regexp.MustCompile(`(?i)illustr|plates|icones|figures|atlas|pictur|drawings|tabulae|abbildungen|planches`)

// bhlIllustrated is the books of the BHL list made for their pictures (all
// of them, should the list name none).
func bhlIllustrated() []bhlItem {
	bhlOnce.Do(loadBHL)
	var all, ill []bhlItem
	for _, tier := range bhlItems {
		for _, b := range tier {
			all = append(all, b)
			if reIllustrated.MatchString(b.title) {
				ill = append(ill, b)
			}
		}
	}
	if len(ill) > 0 {
		return ill
	}
	return all
}

func bhlPlatePick(ctx context.Context, env Env) (string, string, error) {
	books := bhlIllustrated()
	if len(books) == 0 {
		return "", "", errors.New("no BHL books")
	}
	for range 3 {
		b := pick(books)
		leaves, err := bhlLeaves(ctx, env.Fetcher, b.id)
		if err != nil {
			if ctx.Err() != nil {
				return "", "", err
			}
			continue
		}
		// A plate has no words of its own (its OCR is blank or a line), and
		// it is inside the book, not a cover or an endpaper.
		var bare []bhlLeaf
		for i, l := range leaves {
			if l.size <= 64 && i > len(leaves)/10 && i < len(leaves)*9/10 {
				bare = append(bare, l)
			}
		}
		rand.Shuffle(len(bare), func(i, j int) { bare[i], bare[j] = bare[j], bare[i] })
		// A blank page is bare too: the picture tells, by its tones.
		for _, l := range bare[:min(len(bare), 2)] {
			u, _ := url.Parse(fmt.Sprintf("https://www.biodiversitylibrary.org/pageimage/%d", l.page))
			resp, err := env.Fetcher.Get(ctx, u, fetch.Options{})
			if err != nil {
				continue
			}
			if s, err := picture.Spread(resp.Body); err == nil && s >= plateSpread {
				return plateTarget("bhl", fmt.Sprintf("%d/%d", l.item, l.page)), b.title, nil
			}
		}
	}
	return "", "", errors.New("no plate found")
}

// plateSpread is the least spread of tones a plate has; a blank leaf,
// paper with a little show-through, has less.
const plateSpread = 30

func bhlPlate(ctx context.Context, f *fetch.Fetcher, id string) (plate, error) {
	itemS, pageS, _ := strings.Cut(id, "/")
	item, err1 := strconv.Atoi(itemS)
	page, err2 := strconv.Atoi(pageS)
	if err1 != nil || err2 != nil {
		return plate{}, errors.New("bhl: bad plate")
	}
	leaves, err := bhlLeaves(ctx, f, item)
	if err != nil {
		return plate{}, err
	}
	var label string
	for _, l := range leaves {
		if l.page == page {
			u, _ := url.Parse(bhlBucket + "/" + l.key)
			if resp, err := f.Get(ctx, u, fetch.Options{}); err == nil {
				label = strings.Join(strings.Fields(string(resp.Body)), " ")
			}
		}
	}
	title, year := "", ""
	bhlOnce.Do(loadBHL)
	for _, tier := range bhlItems {
		for _, b := range tier {
			if b.id == item {
				title, year = strings.TrimSpace(strings.TrimRight(b.title, " :/;,")), strconv.Itoa(b.year)
			}
		}
	}
	if title == "" {
		title = fmt.Sprintf("BHL item %d", item)
	}
	head := title
	if m := rePlateLabel.FindString(label); m != "" {
		head = strings.ToUpper(m[:1]) + m[1:] + " · " + title
	}
	return plate{
		title:   head,
		byline:  joinNonEmpty(" · ", year, "Biodiversity Heritage Library"),
		image:   fmt.Sprintf("https://www.biodiversitylibrary.org/pageimage/%d", page),
		caption: label,
		facts:   facts("Book", title, "Year", year, "Licence", "public domain (BHL)"),
		home:    fmt.Sprintf("https://www.biodiversitylibrary.org/page/%d", page), homeTx: "This plate on BHL",
	}, nil
}

// --- Wikimedia Commons' featured pictures, drawn at a random place in
// the category's order.

const commonsAPI = "https://commons.wikimedia.org/w/api.php"

func commonsPick(ctx context.Context, env Env) (string, string, error) {
	letters := "ABCDEFGHIJKLMNOPRSTUVW"
	prefix := string(letters[rand.IntN(len(letters))]) + string("aeiourln"[rand.IntN(8)])
	q := url.Values{"action": {"query"}, "list": {"categorymembers"}, "format": {"json"},
		"cmtitle": {"Category:Featured_pictures_on_Wikimedia_Commons"}, "cmtype": {"file"},
		"cmlimit": {"50"}, "cmstartsortkeyprefix": {prefix}}
	var r struct {
		Query struct {
			Members []struct {
				Title string `json:"title"`
			} `json:"categorymembers"`
		} `json:"query"`
	}
	if err := getJSON(ctx, env.Fetcher, commonsAPI+"?"+q.Encode(), nil, &r); err != nil {
		return "", "", err
	}
	var ok []string
	for _, m := range r.Query.Members {
		low := strings.ToLower(m.Title)
		if strings.HasSuffix(low, ".jpg") || strings.HasSuffix(low, ".jpeg") || strings.HasSuffix(low, ".png") {
			ok = append(ok, m.Title)
		}
	}
	if len(ok) == 0 {
		return "", "", errors.New("no picture at " + prefix)
	}
	t := pick(ok)
	return plateTarget("commons", t), commonsName(t), nil
}

// commonsName: "File:Sturnus vulgaris at Bodega Head-1209.jpg" is
// "Sturnus vulgaris at Bodega Head-1209".
func commonsName(t string) string {
	t = strings.TrimPrefix(t, "File:")
	if i := strings.LastIndex(t, "."); i > 0 {
		t = t[:i]
	}
	return strings.ReplaceAll(t, "_", " ")
}

func commonsPlate(ctx context.Context, f *fetch.Fetcher, title string) (plate, error) {
	q := url.Values{"action": {"query"}, "format": {"json"}, "titles": {title}, "prop": {"imageinfo"},
		"iiprop": {"url|extmetadata"}, "iiurlwidth": {"1024"},
		"iiextmetadatafilter": {"ImageDescription|Artist|DateTimeOriginal|LicenseShortName|ObjectName|Credit"}}
	var r struct {
		Query struct {
			Pages map[string]struct {
				ImageInfo []struct {
					Thumb string `json:"thumburl"`
					Page  string `json:"descriptionurl"`
					Meta  map[string]struct {
						Value any `json:"value"`
					} `json:"extmetadata"`
				} `json:"imageinfo"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := getJSON(ctx, f, commonsAPI+"?"+q.Encode(), nil, &r); err != nil {
		return plate{}, err
	}
	for _, pg := range r.Query.Pages {
		if len(pg.ImageInfo) == 0 {
			break
		}
		ii := pg.ImageInfo[0]
		meta := func(k string) string {
			s, _ := ii.Meta[k].Value.(string)
			return strings.Join(htmlParas(s), " ")
		}
		name := meta("ObjectName")
		if name == "" {
			name = commonsName(title)
		}
		img := ii.Thumb
		if i := strings.Index(img, "?"); i >= 0 {
			img = img[:i]
		}
		var paras []string
		if s, _ := ii.Meta["ImageDescription"].Value.(string); s != "" {
			paras = htmlParas(s)
		}
		return plate{
			title:  name,
			byline: joinNonEmpty(" · ", meta("Artist"), meta("DateTimeOriginal"), "Wikimedia Commons, featured picture"),
			image:  img,
			paras:  paras,
			facts:  facts("Credit", meta("Credit"), "Licence", meta("LicenseShortName")),
			home:   ii.Page, homeTx: "This picture on Wikimedia Commons",
		}, nil
	}
	return plate{}, errors.New("no such picture")
}

// --- NASA's image library (public domain): space, drawn by a subject.

// reSentenceStop: the end of a first sentence.
var reSentenceStop = regexp.MustCompile(`[.!?]\s`)

// reDateline: a press caption's place and date line ("TITUSVILLE, Fla. -- ",
// "CAPE CANAVERAL, Fla. – ").
var reDateline = regexp.MustCompile(`^[A-Z][A-Z .'-]{2,}, [A-Z][a-z]+\.?\s*(--|—|–|-)\s*`)

var nasaSubjects = []string{"nebula", "galaxy", "supernova", "star cluster", "jupiter", "saturn", "mars surface",
	"earth from orbit", "aurora", "solar flare", "moon surface", "apollo", "gemini", "mercury program",
	"comet", "eclipse", "hubble", "voyager", "pluto", "titan", "europa", "venus"}

func nasaPick(ctx context.Context, env Env) (string, string, error) {
	subject := pick(nasaSubjects)
	base := "https://images-api.nasa.gov/search?media_type=image&page_size=100&q=" + url.QueryEscape(subject)
	type answer struct {
		Collection struct {
			Meta struct {
				Hits int `json:"total_hits"`
			} `json:"metadata"`
			Items []struct {
				Data []struct {
					ID          string `json:"nasa_id"`
					Title       string `json:"title"`
					Description string `json:"description"`
				} `json:"data"`
			} `json:"items"`
		} `json:"collection"`
	}
	off, err := randomOffset(base, func() (int, error) {
		var a answer
		err := getJSON(ctx, env.Fetcher, base+"&page=1", nil, &a)
		return min((a.Collection.Meta.Hits+99)/100, 100), err
	})
	if err != nil {
		return "", "", err
	}
	var a answer
	if err := getJSON(ctx, env.Fetcher, fmt.Sprintf("%s&page=%d", base, off+1), nil, &a); err != nil {
		return "", "", err
	}
	var ok []int
	for i, it := range a.Collection.Items {
		if len(it.Data) > 0 && len(strings.Fields(it.Data[0].Description)) >= 25 {
			ok = append(ok, i)
		}
	}
	if len(ok) == 0 {
		return "", "", errors.New("no described picture of " + subject)
	}
	d := a.Collection.Items[pick(ok)].Data[0]
	return plateTarget("nasa", d.ID), d.Title, nil
}

func nasaPlate(ctx context.Context, f *fetch.Fetcher, id string) (plate, error) {
	var a struct {
		Collection struct {
			Items []struct {
				Data []struct {
					ID           string `json:"nasa_id"`
					Title        string `json:"title"`
					Description  string `json:"description"`
					Date         string `json:"date_created"`
					Center       string `json:"center"`
					Photographer string `json:"photographer"`
				} `json:"data"`
			} `json:"items"`
		} `json:"collection"`
	}
	if err := getJSON(ctx, f, "https://images-api.nasa.gov/search?nasa_id="+url.QueryEscape(id), nil, &a); err != nil {
		return plate{}, err
	}
	if len(a.Collection.Items) == 0 || len(a.Collection.Items[0].Data) == 0 {
		return plate{}, errors.New("no such picture")
	}
	d := a.Collection.Items[0].Data[0]
	date, _, _ := strings.Cut(d.Date, "T")
	esc := url.PathEscape(d.ID)
	// Not every picture has every size: the asset list says which it has.
	var assets struct {
		Collection struct {
			Items []struct {
				Href string `json:"href"`
			} `json:"items"`
		} `json:"collection"`
	}
	img := ""
	if err := getJSON(ctx, f, "https://images-api.nasa.gov/asset/"+esc, nil, &assets); err == nil {
	sizes:
		for _, size := range []string{"~medium.jpg", "~large.jpg", "~small.jpg", "~orig.jpg"} {
			for _, it := range assets.Collection.Items {
				if strings.HasSuffix(it.Href, size) {
					img = strings.Replace(it.Href, "http://", "https://", 1)
					break sizes
				}
			}
		}
	}
	if img == "" {
		return plate{}, errors.New("no picture")
	}
	title := d.Title
	if !strings.Contains(title, " ") { // a picture's number ("KSC-64C-1868"): its caption's first sentence
		if paras := htmlParas(d.Description); len(paras) > 0 {
			first := reDateline.ReplaceAllString(paras[0], "")
			if i := reSentenceStop.FindStringIndex(first); i != nil {
				first = first[:i[0]+1]
			}
			if len(first) > 120 {
				first = first[:strings.LastIndex(first[:120], " ")] + " …"
			}
			title = first
		}
	}
	return plate{
		title:  title,
		byline: joinNonEmpty(" · ", d.Photographer, date, joinNonEmpty(" ", "NASA", d.Center)),
		image:  img,
		paras:  htmlParas(d.Description),
		facts:  facts("NASA id", d.ID, "Licence", "public domain (NASA)"),
		home:   "https://images.nasa.gov/details/" + esc, homeTx: "This picture at NASA",
	}, nil
}

// PictureHeader is what a picture's host asks to be sent with it (the Art
// Institute asks callers to name themselves).
func PictureHeader(src string) http.Header {
	if strings.Contains(src, "artic.edu/") {
		return aicHeader
	}
	return nil
}
