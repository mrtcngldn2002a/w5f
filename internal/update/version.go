package update

import (
	"strconv"
	"strings"
)

// Newer reports whether version a is newer than b ("v0.7.0" > "0.6.4").
// Pre-release and build suffixes are ignored; "0.0.0-dev" is the oldest.
func Newer(a, b string) bool {
	pa, pb := parts(a), parts(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parts(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	for i, s := range strings.SplitN(v, ".", 3) {
		n, err := strconv.Atoi(s)
		if err != nil {
			return [3]int{}
		}
		out[i] = n
	}
	return out
}
