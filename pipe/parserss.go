package pipe

import (
	"regexp"
	"strconv"
)

var rssAnonRE = regexp.MustCompile(`^RssAnon:\s*(\d+)\s+kB($|\s)`)

// parseRSS parses an "RssAnon" line from /proc/*/status and returns the size.
// The entire line should be passed in, with or without the line ending. If the
// line looks like "RssAnon: 1234 kB", the byte size will be returned. If the
// line isn't parseable, (0, false) will be returned.
func parseRSS(s string) (uint64, bool) {
	m := rssAnonRE.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	kb, err := strconv.ParseUint(m[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return kb * 1024, true
}
