package suwayomi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// ChapterInfo is a chapter with its series, for the viewer.
type ChapterInfo struct {
	Chapter
	Manga struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
	} `json:"manga"`
}

// ChapterInfo returns one chapter.
func (c *Client) ChapterInfo(ctx context.Context, id int) (ChapterInfo, error) {
	var r struct {
		Chapter ChapterInfo `json:"chapter"`
	}
	err := c.do(ctx, `query($id: Int!) { chapter(id: $id) { `+chapterFields+` manga { id title } } }`, map[string]any{"id": id}, &r)
	return r.Chapter, err
}

// Pages makes the server fetch a chapter's page list and returns the pages
// to read through it (downloaded chapters come from disk, others from the
// source).
func (c *Client) Pages(ctx context.Context, chapterID int) (*HTTPPages, error) {
	var r struct {
		F struct {
			Pages []string `json:"pages"`
		} `json:"fetchChapterPages"`
	}
	if err := c.do(ctx, `mutation($id: Int!) { fetchChapterPages(input: {chapterId: $id}) { pages } }`, map[string]any{"id": chapterID}, &r); err != nil {
		return nil, err
	}
	if len(r.F.Pages) == 0 {
		return nil, errors.New("this chapter has no pages")
	}
	return &HTTPPages{c: c, urls: r.F.Pages}, nil
}

// HTTPPages are a chapter's pages served by Suwayomi.
type HTTPPages struct {
	c    *Client
	urls []string
}

func (p *HTTPPages) Len() int          { return len(p.urls) }
func (p *HTTPPages) Name(i int) string { return "page " + strconv.Itoa(i+1) }
func (p *HTTPPages) Close() error      { return nil }
func (p *HTTPPages) Open(i int) (io.ReadCloser, error) {
	if i < 0 || i >= len(p.urls) {
		return nil, errors.New("no such page")
	}
	u := p.urls[i]
	if len(u) > 0 && u[0] == '/' {
		u = p.c.Base + u
	}
	resp, err := p.c.get(context.Background(), u)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("page %d: HTTP %d", i+1, resp.StatusCode)
	}
	return resp.Body, nil
}
