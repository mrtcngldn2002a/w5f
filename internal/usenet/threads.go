package usenet

import (
	"regexp"
	"sort"
	"strings"
	"time"
)

// Thread is a conversation: the articles hanging from one root.
type Thread struct {
	Root    Overview
	Posts   []Post // in reading order: root first, replies depth-first
	Last    time.Time
	Subject string
}

// Post is an article in a thread with its depth.
type Post struct {
	Overview
	Depth int
}

var reRe = regexp.MustCompile(`(?i)^((re|aw|sv|antw)\s*(\[\d+\])?\s*:\s*)+`)

// baseSubject is a subject without its "Re:" prefixes.
func baseSubject(s string) string {
	return strings.ToLower(strings.TrimSpace(reRe.ReplaceAllString(strings.TrimSpace(s), "")))
}

// buildThreads joins articles by their References; an article whose
// parents are not in the list goes under the thread with the same subject
// if there is one, else it starts its own. Threads are ordered by their
// latest article, newest first.
func buildThreads(ovs []Overview) []Thread {
	byID := map[string]int{}
	for i, o := range ovs {
		if o.MessageID != "" {
			byID[o.MessageID] = i
		}
	}
	parent := make([]int, len(ovs))
	children := map[int][]int{}
	var roots []int
	rootBySubject := map[string]int{}
	sorted := make([]int, len(ovs))
	for i := range sorted {
		sorted[i] = i
	}
	sort.SliceStable(sorted, func(a, b int) bool { return ovs[sorted[a]].Num < ovs[sorted[b]].Num })
	for _, i := range sorted {
		o := ovs[i]
		parent[i] = -1
		for r := len(o.References) - 1; r >= 0; r-- {
			if p, ok := byID[o.References[r]]; ok && p != i {
				parent[i] = p
				break
			}
		}
		if parent[i] < 0 && len(o.References) > 0 {
			if p, ok := rootBySubject[baseSubject(o.Subject)]; ok {
				parent[i] = p
			}
		}
		if parent[i] < 0 {
			roots = append(roots, i)
			if _, ok := rootBySubject[baseSubject(o.Subject)]; !ok {
				rootBySubject[baseSubject(o.Subject)] = i
			}
			continue
		}
		children[parent[i]] = append(children[parent[i]], i)
	}
	var threads []Thread
	for _, r := range roots {
		t := Thread{Root: ovs[r], Subject: ovs[r].Subject}
		var walk func(i, depth int)
		walk = func(i, depth int) {
			t.Posts = append(t.Posts, Post{Overview: ovs[i], Depth: depth})
			if ovs[i].Date.After(t.Last) {
				t.Last = ovs[i].Date
			}
			for _, c := range children[i] {
				walk(c, min(depth+1, 8))
			}
		}
		walk(r, 0)
		threads = append(threads, t)
	}
	sort.SliceStable(threads, func(a, b int) bool { return threads[a].Last.After(threads[b].Last) })
	return threads
}

// Kill is the kill file: posts whose subject or poster contains one of
// these (case does not matter) are not shown.
type Kill struct {
	Subjects []string `toml:"subjects"`
	From     []string `toml:"from"`
}

func (k Kill) match(o Overview) bool {
	subj, from := strings.ToLower(o.Subject), strings.ToLower(o.From)
	for _, s := range k.Subjects {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" && strings.Contains(subj, s) {
			return true
		}
	}
	for _, s := range k.From {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" && strings.Contains(from, s) {
			return true
		}
	}
	return false
}

// filter drops killed articles and says how many.
func (k Kill) filter(ovs []Overview) ([]Overview, int) {
	out := ovs[:0:0]
	for _, o := range ovs {
		if !k.match(o) {
			out = append(out, o)
		}
	}
	return out, len(ovs) - len(out)
}
