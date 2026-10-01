package usenet

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html/charset"
)

var wordDecoder = &mime.WordDecoder{CharsetReader: charset.NewReaderLabel}

// decodeHeader undoes RFC 2047 encoded words ("=?UTF-8?B?…?=").
func decodeHeader(s string) string {
	s = strings.TrimSpace(s)
	if d, err := wordDecoder.DecodeHeader(s); err == nil {
		s = d
	}
	return strings.Join(strings.Fields(toUTF8(s, "")), " ")
}

func parseDate(s string) time.Time {
	t, err := mail.ParseDate(strings.TrimSpace(s))
	if err != nil {
		return time.Time{}
	}
	return t
}

// toUTF8 converts text in a charset (default: UTF-8 if it is, else Latin-1
// as old posts are).
func toUTF8(s, cs string) string {
	if cs == "" || strings.EqualFold(cs, "us-ascii") || strings.EqualFold(cs, "utf-8") {
		if utf8.ValidString(s) {
			return s
		}
		cs = "windows-1252"
	}
	r, err := charset.NewReaderLabel(cs, strings.NewReader(s))
	if err != nil {
		return strings.ToValidUTF8(s, "?")
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return strings.ToValidUTF8(s, "?")
	}
	return string(b)
}

// Article is a decoded article.
type Article struct {
	Num        int
	Subject    string
	From       string
	Date       time.Time
	MessageID  string
	References []string
	Body       string // plain text, UTF-8, lines separated by \n
}

// parseArticle decodes an article's lines (headers, blank line, body):
// transfer encodings, charsets and the first text part of a multipart post.
func parseArticle(num int, lines []string) (Article, error) {
	msg, err := mail.ReadMessage(strings.NewReader(strings.Join(lines, "\r\n") + "\r\n"))
	if err != nil {
		return Article{}, err
	}
	a := Article{Num: num, Subject: decodeHeader(msg.Header.Get("Subject")), From: decodeHeader(msg.Header.Get("From")),
		MessageID: strings.TrimSpace(msg.Header.Get("Message-ID")), References: strings.Fields(msg.Header.Get("References"))}
	a.Date = parseDate(msg.Header.Get("Date"))
	body, _ := io.ReadAll(io.LimitReader(msg.Body, 2<<20))
	a.Body = decodeBody(msg.Header.Get("Content-Type"), msg.Header.Get("Content-Transfer-Encoding"), body, 0)
	return a, nil
}

func decodeBody(ctype, cte string, body []byte, depth int) string {
	mt, params, err := mime.ParseMediaType(ctype)
	if err != nil {
		mt, params = "text/plain", map[string]string{}
	}
	switch strings.ToLower(strings.TrimSpace(cte)) {
	case "quoted-printable":
		if b, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body))); err == nil {
			body = b
		}
	case "base64":
		if b, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewReader(bytes.ReplaceAll(body, []byte("\r\n"), nil)))); err == nil {
			body = b
		}
	}
	if strings.HasPrefix(mt, "multipart/") && depth < 3 {
		mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
		for {
			p, err := mr.NextRawPart()
			if err != nil {
				break
			}
			pt := p.Header.Get("Content-Type")
			if pt == "" || strings.HasPrefix(strings.ToLower(pt), "text/plain") || strings.HasPrefix(strings.ToLower(pt), "multipart/") {
				b, _ := io.ReadAll(io.LimitReader(p, 2<<20))
				return decodeBody(pt, p.Header.Get("Content-Transfer-Encoding"), b, depth+1)
			}
		}
		return "(this post has no plain-text part)"
	}
	text := toUTF8(string(body), params["charset"])
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if strings.EqualFold(params["format"], "flowed") {
		text = unflow(text, strings.EqualFold(params["delsp"], "yes"))
	}
	return text
}

// unflow joins format=flowed soft line breaks (a line ending in a space).
func unflow(text string, delsp bool) string {
	var b strings.Builder
	for _, l := range strings.Split(text, "\n") {
		if strings.HasSuffix(l, " ") && l != "-- " {
			if delsp {
				l = strings.TrimSuffix(l, " ")
			}
			b.WriteString(l)
			continue
		}
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// posterName is the name part of a From header.
func posterName(from string) string {
	if a, err := mail.ParseAddress(from); err == nil {
		if a.Name != "" {
			return a.Name
		}
		return a.Address
	}
	// "user@host (Real Name)"
	if i := strings.Index(from, "("); i > 0 && strings.HasSuffix(from, ")") {
		return strings.TrimSpace(from[i+1 : len(from)-1])
	}
	return from
}
