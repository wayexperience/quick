package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/zupolgec/quick/internal/quick"
)

func promptServer() string {
	fmt.Fprint(os.Stderr, "quick server URL (e.g. https://quick.example.com): ")
	return readLine()
}

func readLine() string {
	sc := bufio.NewScanner(os.Stdin)
	if sc.Scan() {
		return strings.TrimSpace(sc.Text())
	}
	return ""
}

// yesNo recognizes an affirmative answer (Italian or English).
func yesNo(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "s", "si", "sì", "y", "yes":
		return true
	}
	return false
}

type cliConfig struct {
	Server            string `json:"server"`
	OAuthClientID     string `json:"oauth_client_id"`
	OAuthClientSecret string `json:"oauth_client_secret,omitempty"`
	HostedDomain      string `json:"hosted_domain"`
	BaseDomain        string `json:"base_domain"`
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.Getenv("HOME")
	}
	return filepath.Join(dir, "quick", "config.json")
}

func loadConfig() *cliConfig {
	b, err := os.ReadFile(configPath())
	if err != nil {
		return nil
	}
	var c cliConfig
	if json.Unmarshal(b, &c) != nil {
		return nil
	}
	return &c
}

func saveConfig(c *cliConfig) {
	p := configPath()
	if os.MkdirAll(filepath.Dir(p), 0o700) != nil {
		return
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	_ = os.WriteFile(p, b, 0o600)
}

// resolveConfig picks the server (flag > env > cache) and returns the config,
// reusing the cache for the same server or refetching from /api/config.
func resolveConfig(serverFlag string) (*cliConfig, error) {
	server := serverFlag
	if server == "" {
		server = os.Getenv("QUICK_SERVER")
	}
	saved := loadConfig()
	if server == "" {
		if saved != nil && saved.OAuthClientID != "" {
			return saved, nil // no explicit server: use the remembered one
		}
		server = promptServer()
	}
	if server == "" {
		return nil, errors.New("server required (--server, QUICK_SERVER, or enter it at the prompt)")
	}

	cands := candidates(server)
	if saved != nil && saved.OAuthClientID != "" && slices.Contains(cands, saved.Server) {
		return saved, nil
	}
	var lastErr error
	for _, cand := range cands {
		c, err := fetchConfig(cand)
		if err == nil {
			c.Server = cand
			saveConfig(c)
			return c, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("server unreachable (tried: %s): %w", strings.Join(cands, ", "), lastErr)
}

// candidates normalizes the server input: accepts a bare domain or a full URL
// and adds https:// if missing. API and auth all live on the apex, so there is
// no deploy.<domain> fallback.
func candidates(input string) []string {
	input = strings.TrimRight(strings.TrimSpace(input), "/")
	if !strings.Contains(input, "://") {
		input = "https://" + input
	}
	return []string{input}
}

func fetchConfig(server string) (*cliConfig, error) {
	cli := &http.Client{Timeout: 15 * time.Second}
	resp, err := cli.Get(server + "/api/config")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("server unreachable or /api/config missing")
	}
	var r quick.ConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	return &cliConfig{
		OAuthClientID:     r.OAuthClientID,
		OAuthClientSecret: r.OAuthClientSecret,
		HostedDomain:      r.HostedDomain,
		BaseDomain:        r.BaseDomain,
	}, nil
}
