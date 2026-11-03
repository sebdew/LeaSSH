package api

import (
	"net/http"
	"net/http/cookiejar"
	"net/netip"
)

func newJar() (http.CookieJar, error)  { return cookiejar.New(nil) }
func mustPrefix(s string) netip.Prefix { return netip.MustParsePrefix(s) }
