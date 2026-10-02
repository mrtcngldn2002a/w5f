package fetch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// anubisMaxDifficulty bounds the work W5F does for one page: 16^6 hashes on
// average is a few seconds on the W5F laptop; harder walls are left alone.
const anubisMaxDifficulty = 6

func isAnubis(body []byte) bool { return bytes.Contains(body, []byte(`id="anubis_challenge"`)) }

// anubisChallenge is the page's own description of its proof of work
// (Anubis v1.27: script#anubis_challenge and script#anubis_base_prefix).
type anubisChallenge struct {
	Rules struct {
		Algorithm  string `json:"algorithm"`
		Difficulty int    `json:"difficulty"`
	} `json:"rules"`
	Challenge struct {
		ID         string `json:"id"`
		RandomData string `json:"randomData"`
	} `json:"challenge"`
	prefix string
}

func parseAnubis(body []byte) (anubisChallenge, error) {
	var c anubisChallenge
	d, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal([]byte(d.Find("script#anubis_challenge").Text()), &c); err != nil {
		return c, fmt.Errorf("Anubis challenge: %w", err)
	}
	_ = json.Unmarshal([]byte(d.Find("script#anubis_base_prefix").Text()), &c.prefix)
	switch {
	case c.Rules.Algorithm != "fast" && c.Rules.Algorithm != "slow":
		return c, fmt.Errorf("Anubis asks for %q, which W5F does not do", c.Rules.Algorithm)
	case c.Rules.Difficulty < 1 || c.Rules.Difficulty > anubisMaxDifficulty:
		return c, fmt.Errorf("Anubis difficulty %d is beyond W5F's limit", c.Rules.Difficulty)
	case c.Challenge.RandomData == "":
		return c, errors.New("Anubis challenge has no data")
	}
	return c, nil
}

// anubisWork finds the nonce whose sha256(data + nonce) starts with
// difficulty zero hex digits, as Anubis's own worker does.
func anubisWork(ctx context.Context, data string, difficulty int) (hash string, nonce int, err error) {
	full, half := difficulty/2, difficulty%2 == 1
	buf := append(make([]byte, 0, len(data)+20), data...)
	for nonce = 0; ; nonce++ {
		if nonce&4095 == 0 && ctx.Err() != nil {
			return "", 0, ctx.Err()
		}
		sum := sha256.Sum256(strconv.AppendInt(buf[:len(data)], int64(nonce), 10))
		ok := true
		for i := 0; i < full && ok; i++ {
			ok = sum[i] == 0
		}
		if ok && half && sum[full]>>4 != 0 {
			ok = false
		}
		if ok {
			return hex.EncodeToString(sum[:]), nonce, nil
		}
	}
}

// solveAnubis does the proof of work an Anubis page asks of every visitor
// and hands the answer back the way its script would; the pass cookie lands
// in the client's jar and the page is then fetched again as usual.
func (f *Fetcher) solveAnubis(ctx context.Context, client *http.Client, userAgent string, page *url.URL, body []byte) error {
	if client.Jar == nil {
		return errors.New("Anubis needs cookies")
	}
	c, err := parseAnubis(body)
	if err != nil {
		return err
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	hash, nonce, err := anubisWork(ctx, c.Challenge.RandomData, c.Rules.Difficulty)
	if err != nil {
		return fmt.Errorf("Anubis proof of work: %w", err)
	}
	redir := page.RequestURI()
	pass := &url.URL{Scheme: page.Scheme, Host: page.Host, Path: strings.TrimRight(c.prefix, "/") + "/.within.website/x/cmd/anubis/api/pass-challenge"}
	pass.RawQuery = url.Values{"id": {c.Challenge.ID}, "response": {hash}, "nonce": {strconv.Itoa(nonce)},
		"redir": {redir}, "elapsedTime": {strconv.FormatInt(time.Since(start).Milliseconds(), 10)}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pass.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", page.String())
	f.wait(ctx, page.Host)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("Anubis refused the answer (HTTP %d)", resp.StatusCode)
	}
	return nil
}
