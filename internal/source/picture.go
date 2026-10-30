package source

import (
	"context"
	"errors"
	"net/url"

	"w5f/internal/discover"
	"w5f/internal/fetch"
)

// Picture fetches a picture a page shows (cached like any page).
func Picture(ctx context.Context, src string) ([]byte, error) {
	if Fetcher == nil {
		return nil, errors.New("no network")
	}
	u, err := url.Parse(src)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("not a picture address: " + src)
	}
	resp, err := Fetcher.Get(ctx, u, fetch.Options{Header: discover.PictureHeader(src)})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}
