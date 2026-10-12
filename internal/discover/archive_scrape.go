package discover

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/url"
	"strconv"
	"strings"

	"w5f/internal/fetch"
)

// The Internet Archive's scrape API (/services/search/v1/scrape) is the bulk
// interface to the same catalogue as advancedsearch, and it kept answering
// while advancedsearch failed with backend errors (2026-10-12). It pages by
// a cursor, and with sorts=identifier the cursor is only the last identifier
// seen, so a cursor made up of two random characters starts a hundred items
// at a random place in the alphabet: random access without an offset.

const archiveCursorChars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func randomArchiveCursor() string {
	b := []byte{archiveCursorChars[rand.IntN(len(archiveCursorChars))], archiveCursorChars[rand.IntN(len(archiveCursorChars))]}
	j, _ := json.Marshal([]map[string]string{{"identifier": string(b)}})
	return base64.StdEncoding.EncodeToString(j)
}

// archiveScrape reads a hundred items of the query from the cursor on.
func archiveScrape(ctx context.Context, f *fetch.Fetcher, q, cursor string) ([]archiveDoc, error) {
	v := url.Values{"q": {q}, "fields": {"identifier,title,year"}, "count": {"100"}, "sorts": {"identifier"}, "cursor": {cursor}}
	u, _ := url.Parse(archiveAPI + "/services/search/v1/scrape?" + v.Encode())
	resp, err := f.Get(ctx, u, fetch.Options{NoStore: true})
	if err != nil {
		return nil, err
	}
	var r struct {
		Error string       `json:"error"`
		Items []archiveDoc `json:"items"`
	}
	if err := json.Unmarshal(resp.Body, &r); err != nil {
		return nil, errors.New("unexpected answer from the Internet Archive's scrape API")
	}
	if r.Error != "" {
		return nil, errors.New("the Internet Archive's scrape API is failing: " + r.Error)
	}
	return r.Items, nil
}

// archivePick takes an item of the query at random: from the scrape API, and
// when that gives nothing, from the offset search.
func archivePick(ctx context.Context, f *fetch.Fetcher, q string) (archiveDoc, error) {
	var errs []string
	for range 3 {
		docs, err := archiveScrape(ctx, f, q, randomArchiveCursor())
		if err != nil {
			if ctx.Err() != nil {
				return archiveDoc{}, ctx.Err()
			}
			// A made-up cursor is now and then refused ("Bad cursor"), a
			// busy server answers 502: another cursor, then the other search.
			errs = append(errs, err.Error())
			if strings.Contains(err.Error(), "is failing") {
				break
			}
			continue
		}
		if len(docs) > 0 {
			return docs[rand.IntN(len(docs))], nil
		}
		// A cursor past the last identifier: another one.
	}
	n, _, err := archiveSearch(ctx, f, q, 1, 1)
	if err == nil && n == 0 {
		err = errors.New("nothing found")
	}
	if err == nil {
		var docs []archiveDoc
		if _, docs, err = archiveSearch(ctx, f, q, 1, 1+rand.IntN(min(n, 10000))); err == nil && len(docs) > 0 {
			return docs[0], nil
		}
		if err == nil {
			err = errors.New("no item on that page")
		}
	}
	errs = append(errs, err.Error())
	return archiveDoc{}, errors.New(strings.Join(errs, "; "))
}

// archiveTextFile is the name of an item's text (the OCR the Archive keeps
// as DjVuTXT): not always "<identifier>_djvu.txt", the files keep the names
// they were uploaded with. The largest is taken (a supplement is another,
// shorter file).
func archiveTextFile(ctx context.Context, f *fetch.Fetcher, id string) (string, error) {
	u, _ := url.Parse(archiveAPI + "/metadata/" + url.PathEscape(id) + "/files")
	resp, err := f.Get(ctx, u, fetch.Options{NoStore: true})
	if err != nil {
		return "", err
	}
	var r struct {
		Result []struct {
			Name   string `json:"name"`
			Format string `json:"format"`
			Size   string `json:"size"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp.Body, &r); err != nil {
		return "", errors.New("unexpected answer from the Internet Archive's metadata")
	}
	best, bestSize := "", -1
	for _, file := range r.Result {
		if file.Format != "DjVuTXT" {
			continue
		}
		if n, _ := strconv.Atoi(file.Size); n > bestSize {
			best, bestSize = file.Name, n
		}
	}
	if best == "" {
		return "", errors.New("no text file")
	}
	return best, nil
}
