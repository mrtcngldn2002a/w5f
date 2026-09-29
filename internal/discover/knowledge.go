package discover

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"math/rand/v2"
	"net/url"
	"path"
	"strings"

	"w5f/internal/fetch"
)

// knowledge: long-form essays, the best encyclopedia articles, primary
// documents, history and the classics (chosen with the owner, 2026-09-29).
type knowledge struct{}

type knowSource struct {
	name string
	draw func(ctx context.Context, env Env) (target, title string, err error)
}

func fromFeed(feed string) func(context.Context, Env) (string, string, error) {
	return func(ctx context.Context, env Env) (string, string, error) { return rssPick(ctx, env.Fetcher, feed) }
}

func fromRandom(raw string) func(context.Context, Env) (string, string, error) {
	return func(ctx context.Context, env Env) (string, string, error) {
		target, err := resolveRandom(ctx, env.Fetcher, raw)
		if err != nil {
			return "", "", err
		}
		u, _ := url.Parse(target)
		name, _ := url.PathUnescape(path.Base(u.Path))
		return target, strings.ReplaceAll(name, "_", " "), nil
	}
}

// fromIndex descends from an index page anywhere under scope.
func fromIndex(start, scope string, depth int) func(context.Context, Env) (string, string, error) {
	return func(ctx context.Context, env Env) (string, string, error) {
		return descendIn(ctx, env.Fetcher, start, scope, depth)
	}
}

// catholicEncyclopedia starts from a random letter's index, so the first
// step lands on an article.
func catholicEncyclopedia(ctx context.Context, env Env) (string, string, error) {
	letter := string(rune('a' + rand.IntN(26)))
	if letter == "x" {
		letter = "y"
	}
	return descendIn(ctx, env.Fetcher, "https://www.newadvent.org/cathen/"+letter+".htm", "/cathen/", 2)
}

var knowledgeSources = []knowSource{
	{"Aeon", fromFeed("https://aeon.co/feed.rss")},
	{"JSTOR Daily", fromFeed("https://daily.jstor.org/feed/")},
	{"Quanta Magazine", fromFeed("https://www.quantamagazine.org/feed/")},
	{"Wikipedia featured articles", fromRandom("https://en.wikipedia.org/wiki/Special:RandomInCategory/Featured_articles")},
	// Featured texts: plain random lands on court reporters and census pages.
	{"Wikisource", fromRandom("https://en.wikisource.org/wiki/Special:RandomInCategory/Featured_texts")},
	{"World History Encyclopedia", fromFeed("https://www.worldhistory.org/rss/articles")},
	{"Internet Classics Archive", fromIndex("https://classics.mit.edu/Browse/index.html", "/", 4)},
	{"Catholic Encyclopedia (1913)", catholicEncyclopedia},
}

func init() { families = append(families, knowledge{}) }

func (knowledge) Name() string { return "knowledge" }

// Draw tries up to three sources in random order.
func (knowledge) Draw(ctx context.Context, env Env) (Draw, error) {
	var errs []string
	for n, i := range rand.Perm(len(knowledgeSources)) {
		if n == 3 {
			break
		}
		s := knowledgeSources[i]
		target, title, err := s.draw(ctx, env)
		if err != nil {
			errs = append(errs, s.name+": "+err.Error())
			continue
		}
		why := "knowledge/" + s.name
		if title != "" {
			why += " · " + title
		}
		return Draw{Target: target, Why: why}, nil
	}
	return Draw{}, errors.New(strings.Join(errs, "; "))
}

// rssPick returns a random item (link and title) of an RSS feed.
func rssPick(ctx context.Context, f *fetch.Fetcher, feed string) (string, string, error) {
	u, err := url.Parse(feed)
	if err != nil {
		return "", "", err
	}
	resp, err := f.Get(ctx, u, fetch.Options{})
	if err != nil {
		return "", "", err
	}
	var rss struct {
		Items []struct {
			Title string `xml:"title"`
			Link  string `xml:"link"`
		} `xml:"channel>item"`
	}
	if err := xml.NewDecoder(bytes.NewReader(resp.Body)).Decode(&rss); err != nil {
		return "", "", err
	}
	var ok []int
	for i, it := range rss.Items {
		l := strings.ToLower(it.Link)
		if strings.Contains(l, "/video") || strings.Contains(l, "/podcast") {
			continue // reading matter only
		}
		if strings.TrimSpace(it.Link) != "" && strings.TrimSpace(it.Title) != "" {
			ok = append(ok, i)
		}
	}
	if len(ok) == 0 {
		return "", "", errors.New("the feed has no items")
	}
	it := rss.Items[pick(ok)]
	return strings.TrimSpace(it.Link), strings.Join(strings.Fields(it.Title), " "), nil
}
