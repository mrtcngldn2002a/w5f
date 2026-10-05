package fiction

import (
	"context"
	"fmt"

	"w5f/internal/doc"
)

// Report sums up an update check of followed serials.
type Report struct{ Checked, New, Errors int }

func (r Report) String() string {
	s := fmt.Sprintf("serials: %d checked, %d new chapters", r.Checked, r.New)
	if r.Errors > 0 {
		s += fmt.Sprintf(" (%d errors)", r.Errors)
	}
	return s
}

// SyncFollowed checks every followed serial for new chapters, one after
// another; a failing site is recorded on its serial and the rest go on.
func SyncFollowed(ctx context.Context, env Env, progress func(done, total int)) Report {
	var r Report
	ss, err := env.DB.Serials(true)
	if err != nil {
		r.Errors++
		return r
	}
	for i, s := range ss {
		if ctx.Err() != nil {
			break
		}
		n, err := Refresh(ctx, env, s.ID)
		r.Checked++
		if err != nil {
			r.Errors++
		} else {
			r.New += n
		}
		if progress != nil {
			progress(i+1, len(ss))
		}
	}
	return r
}

// followingDoc lists followed serials with their new chapters.
func followingDoc(env Env, status string) (*doc.Document, error) {
	d := &doc.Document{Title: "Following", URL: "w5f:following", Origin: "local", Lang: "en"}
	ss, err := env.DB.Serials(true)
	if err != nil {
		return nil, err
	}
	if status != "" {
		d.Blocks = append(d.Blocks, doc.Notice{Kind: "info", Text: status})
	}
	d.Blocks = append(d.Blocks, para(doc.Span{Text: "check now", Link: link(d, "w5f:following/check", "check now")},
		plain("   (w5f sync checks too)", doc.Italic)))
	if len(ss) == 0 {
		d.Blocks = append(d.Blocks, para(plain("Nothing followed yet. Open a serial or a Reddit series and press u.", doc.Italic)))
		return d, nil
	}
	d.Blocks = append(d.Blocks, followedList(d, ss))
	return d, nil
}

// followedList is the list of followed serials used on several pages.
func followedList(d *doc.Document, ss []storeSerial) doc.Block {
	var items [][]doc.Block
	for _, s := range ss {
		in := doc.Inline{{Text: s.Title, Style: doc.Bold, Link: link(d, serialHref(s.ID, ""), s.Title)}}
		sub := []string{}
		if s.Author != "" {
			sub = append(sub, s.Author)
		}
		sub = append(sub, siteName(s.Kind, s.URL))
		if n := s.Chapters - s.Seen; n > 0 {
			sub = append(sub, fmt.Sprintf("%d new", n))
		}
		sub = append(sub, "checked "+ago(s.Checked))
		in = append(in, plain("  · "+joinDot(sub), doc.Italic))
		blocks := []doc.Block{para(in...)}
		if s.CheckError != "" {
			blocks = append(blocks, para(plain("  ✗ "+s.CheckError, doc.Italic)))
		}
		if !s.Opened.IsZero() && s.Chapter < s.Chapters {
			t := fmt.Sprintf("continue chapter %d", s.Chapter+1)
			blocks = append(blocks, para(plain("  ", 0), doc.Span{Text: t, Link: link(d, serialHref(s.ID, "continue"), t)}))
		}
		items = append(items, blocks)
	}
	return doc.List{Items: items}
}
