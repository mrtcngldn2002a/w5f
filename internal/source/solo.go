package source

import (
	"net/url"
	"strings"

	"w5f/internal/solo"
)

// isDice reports "roll …" input that is dice (the rest stays a web search).
func isDice(s string) bool {
	expr := s
	if i := strings.Index(expr, "="); i >= 0 {
		expr = expr[:i]
	}
	_, err := solo.ParseDice(expr)
	return err == nil
}

// askTarget turns "likely Is the door locked?" (odds first, one or two
// words) into the oracle's address ("" when it does not start with odds).
func askTarget(s string) string {
	f := strings.Fields(s)
	for n := 2; n >= 1; n-- {
		if len(f) < n {
			continue
		}
		if key, _, _, ok := solo.OddsOf(strings.Join(f[:n], " ")); ok {
			return "w5f:solo/ask/" + key + "?" + url.Values{"q": {strings.Join(f[n:], " ")}}.Encode()
		}
	}
	return ""
}

func isSpark(kind string) bool {
	for _, k := range solo.SparkKinds {
		if k.Key == kind {
			return true
		}
	}
	return false
}

// isCounter reports "Health 5/5" or "Supply 3": a name and a number.
func isCounter(s string) bool { return solo.IsCounter(s) }
