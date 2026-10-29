package deploylink

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var linkIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{12}$`)

// ParseLink returns the deploy link id a link names: a deploy link URL such as
// https://volcano.dev/deploy/k3Vn8TqPz0aX, or the bare 12-character id.
//
// Only the id is taken from a URL. The host is ignored, because the CLI sends
// the id to the API it is configured for rather than to whatever host a link
// happens to name.
func ParseLink(value string) (string, error) {
	value = strings.TrimSpace(value)
	if linkIDPattern.MatchString(value) {
		return value, nil
	}
	if !strings.Contains(value, "://") {
		return "", fmt.Errorf("invalid deploy link %q: pass a link such as https://volcano.dev/deploy/<id> or its 12-character id", value)
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return "", fmt.Errorf("invalid deploy link %q: pass a link such as https://volcano.dev/deploy/<id> or its 12-character id", value)
	}
	id, ok := strings.CutPrefix(strings.TrimSuffix(parsed.Path, "/"), "/deploy/")
	if !ok || !linkIDPattern.MatchString(id) {
		return "", fmt.Errorf("invalid deploy link %q: the address must look like https://volcano.dev/deploy/<id>, "+
			"where the id is 12 letters and digits", value)
	}
	return id, nil
}
