package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// setupOAuthProxy sets up the reverse proxy to oauth2-proxy for /oauth2/*
// (sign_in, callback, etc.). It preserves the original Host and sets
// X-Forwarded-Proto=https so oauth2-proxy builds correct redirects and cookies
// (.<BASE_DOMAIN>), exactly as the Caddy block did before.
func (s *server) setupOAuthProxy() error {
	target, err := url.Parse(s.oauth2URL)
	if err != nil {
		return err
	}
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = pr.In.Host // oauth2-proxy sees the site's original host
			pr.SetXForwarded()
			pr.Out.Header.Set("X-Forwarded-Proto", "https")
		},
	}
	s.oauthProxy = rp
	return nil
}

func (s *server) handleOAuth2(w http.ResponseWriter, r *http.Request) {
	s.oauthProxy.ServeHTTP(w, r)
}
