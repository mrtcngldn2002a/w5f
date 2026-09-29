package fetch

import "strings"

// Use the same transport when discovering a page and when reopening it from
// a packet, saved link, or ordinary navigation. Match exact owned hosts only.
func browserPageHost(host string) bool {
	switch strings.ToLower(host) {
	case "sacred-texts.com", "www.sacred-texts.com", "archive.sacred-texts.com",
		"hermetic.com", "www.hermetic.com", "britannica.com", "www.britannica.com":
		return true
	}
	return false
}
