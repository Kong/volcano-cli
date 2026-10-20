package api

import (
	"errors"
	"net/url"
)

// RedactURL returns the URL verbatim, except it masks an embedded basic-auth
// password (mirroring net/http's own transport-error redaction) so a credential
// baked into the configured API URL never lands in stderr/CI logs.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	if _, hasPassword := u.User.Password(); !hasPassword {
		return raw
	}
	return u.Redacted()
}

// PublicURL returns the URL without its userinfo, query and fragment, for a
// copy that may be published, such as a frontend's public API URL. A base API
// URL needs none of them, and any of them can hold a credential. A URL that
// doesn't parse is an error, and the error leaves the URL out.
func PublicURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		if urlErr, ok := errors.AsType[*url.Error](err); ok {
			return "", urlErr.Err
		}
		return "", err
	}
	if u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" {
		return raw, nil
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return u.String(), nil
}
