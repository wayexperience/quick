package main

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/wayexperience/quick/internal/quick"
	"github.com/wayexperience/quick/internal/storage"
)

// handleConfig espone (pubblicamente) ciò che serve alla CLI per auto-configurarsi:
// client OAuth, hosted domain, dominio dei siti. Nessun segreto.
func (s *server) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(quick.ConfigResponse{
		OAuthClientID:     s.clientID,
		OAuthClientSecret: s.clientSecret,
		HostedDomain:      s.domain,
		BaseDomain:        s.baseDomain,
	})
}

func storageConfigFromEnv() storage.Config {
	return storage.Config{
		Kind:     quick.Env("QUICK_STORAGE", "local"),
		SitesDir: quick.Env("QUICK_SITES_DIR", "./sites"),
		MetaDir:  quick.Env("QUICK_META_DIR", "./meta"),
		S3: storage.S3Config{
			Endpoint:  os.Getenv("QUICK_S3_ENDPOINT"),
			Region:    os.Getenv("QUICK_S3_REGION"),
			Bucket:    os.Getenv("QUICK_S3_BUCKET"),
			AccessKey: os.Getenv("QUICK_S3_ACCESS_KEY"),
			SecretKey: os.Getenv("QUICK_S3_SECRET_KEY"),
			Prefix:    os.Getenv("QUICK_S3_PREFIX"),
			UseSSL:    quick.Env("QUICK_S3_USE_SSL", "true") == "true",
		},
	}
}

func reservedSet() map[string]bool {
	list := quick.SplitList(os.Getenv("QUICK_RESERVED_SUBS"))
	if len(list) == 0 {
		list = quick.DefaultReservedSubs
	}
	m := make(map[string]bool, len(list))
	for _, x := range list {
		m[x] = true
	}
	return m
}
