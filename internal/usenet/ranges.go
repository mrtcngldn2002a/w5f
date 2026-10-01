package usenet

import (
	"sort"
	"strconv"
	"strings"
)

// ranges is a set of article numbers kept as sorted, non-touching
// intervals, written like a .newsrc line: "1-500,503,510-520".
type ranges [][2]int

func parseRanges(s string) ranges {
	var r ranges
	for _, part := range strings.Split(s, ",") {
		lo, hi, found := strings.Cut(strings.TrimSpace(part), "-")
		a, err := strconv.Atoi(lo)
		if err != nil {
			continue
		}
		b := a
		if found {
			if b, err = strconv.Atoi(hi); err != nil {
				continue
			}
		}
		r = r.add(a, b)
	}
	return r
}

func (r ranges) String() string {
	var b strings.Builder
	for i, iv := range r {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(iv[0]))
		if iv[1] != iv[0] {
			b.WriteByte('-')
			b.WriteString(strconv.Itoa(iv[1]))
		}
	}
	return b.String()
}

// add puts lo..hi into the set.
func (r ranges) add(lo, hi int) ranges {
	if lo > hi || hi < 1 {
		return r
	}
	lo = max(lo, 1)
	out := append(append(ranges{}, r...), [2]int{lo, hi})
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	merged := out[:1]
	for _, iv := range out[1:] {
		last := &merged[len(merged)-1]
		if iv[0] <= last[1]+1 {
			last[1] = max(last[1], iv[1])
		} else {
			merged = append(merged, iv)
		}
	}
	return merged
}

func (r ranges) has(n int) bool {
	i := sort.Search(len(r), func(i int) bool { return r[i][1] >= n })
	return i < len(r) && r[i][0] <= n
}

// countIn is how many numbers of lo..hi the set holds.
func (r ranges) countIn(lo, hi int) int {
	n := 0
	for _, iv := range r {
		a, b := max(iv[0], lo), min(iv[1], hi)
		if a <= b {
			n += b - a + 1
		}
	}
	return n
}
