package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// setupOAuthProxy prepara il reverse proxy verso oauth2-proxy per le rotte
// /oauth2/* (sign_in, callback, ecc.). Preserva l'Host originale e marca
// X-Forwarded-Proto=https, così oauth2-proxy costruisce redirect e cookie
// (.<BASE_DOMAIN>) corretti — esattamente come faceva il blocco Caddy prima.
func (s *server) setupOAuthProxy() error {
	target, err := url.Parse(s.oauth2URL)
	if err != nil {
		return err
	}
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = pr.In.Host // oauth2-proxy vede l'host originale del sito
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
