// Package config loads and validates the JSON configuration for the
// farsight central backend.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"regexp"

	"golang.org/x/crypto/bcrypt"
)

var serverIDRegexp = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,40}$`)

// Server is one Valheim server that farsight fronts.
type Server struct {
	ID             string `json:"id"` // ^[a-z0-9][a-z0-9-]{0,40}$
	Name           string `json:"name"`
	Address        string `json:"address,omitempty"`
	Crossplay      bool   `json:"crossplay"`
	DiscordHint    string `json:"discordHint,omitempty"`
	MaxPlayers     int    `json:"maxPlayers"`     // default 10
	PassphraseHash string `json:"passphraseHash"` // bcrypt
	AgentTokenHash string `json:"agentTokenHash"` // bcrypt
}

// Config is the top-level farsight configuration.
type Config struct {
	Listen       string   `json:"listen"`       // default ":8080"
	IngestListen string   `json:"ingestListen"` // default "": ingest served on Listen too
	DataDir      string   `json:"dataDir"`      // default "/data"
	CookieSecure *bool    `json:"cookieSecure"` // default true
	TrustProxy   bool     `json:"trustProxy"`   // use the rightmost X-Forwarded-For entry as client IP
	Servers      []Server `json:"servers"`
	CookieKey    []byte   `json:"-"` // from FARSIGHT_COOKIE_KEY

	byID map[string]*Server
}

// Load reads and validates the JSON config file at path, filling in
// defaults and deriving the cookie signing key from the
// FARSIGHT_COOKIE_KEY environment variable (via getenv).
func Load(path string, getenv func(string) string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	c := &Config{}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(c); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	if c.Listen == "" {
		c.Listen = ":8080"
	}
	if c.DataDir == "" {
		c.DataDir = "/data"
	}
	if c.CookieSecure == nil {
		t := true
		c.CookieSecure = &t
	}

	if c.IngestListen != "" && sameListenAddr(c.IngestListen, c.Listen) {
		return nil, fmt.Errorf("config: ingestListen must differ from listen")
	}

	if len(c.Servers) == 0 {
		return nil, fmt.Errorf("config: at least one server is required")
	}

	seen := make(map[string]bool, len(c.Servers))
	c.byID = make(map[string]*Server, len(c.Servers))
	for i := range c.Servers {
		s := &c.Servers[i]
		if !serverIDRegexp.MatchString(s.ID) {
			return nil, fmt.Errorf("config: server %d: invalid id %q", i, s.ID)
		}
		if seen[s.ID] {
			return nil, fmt.Errorf("config: duplicate server id %q", s.ID)
		}
		seen[s.ID] = true

		if s.MaxPlayers == 0 {
			s.MaxPlayers = 10
		}

		if _, err := bcrypt.Cost([]byte(s.PassphraseHash)); err != nil {
			return nil, fmt.Errorf("config: server %q: passphraseHash is not a bcrypt hash: %w", s.ID, err)
		}
		if _, err := bcrypt.Cost([]byte(s.AgentTokenHash)); err != nil {
			return nil, fmt.Errorf("config: server %q: agentTokenHash is not a bcrypt hash: %w", s.ID, err)
		}

		c.byID[s.ID] = s
	}

	rawKey := getenv("FARSIGHT_COOKIE_KEY")
	key, err := decodeCookieKey(rawKey)
	if err != nil {
		return nil, err
	}
	c.CookieKey = key

	return c, nil
}

// sameListenAddr reports whether a and b name the same listen address, for
// the ingestListen-must-differ-from-listen check. A literal ":0" port asks
// the OS for an ephemeral port, which is always distinct across two
// listeners even when the rest of the address string matches, so that case
// is never treated as a conflict.
func sameListenAddr(a, b string) bool {
	if a != b {
		return false
	}
	_, port, err := net.SplitHostPort(a)
	if err != nil {
		return true
	}
	return port != "0"
}

// decodeCookieKey turns raw (the value of FARSIGHT_COOKIE_KEY) into key
// material: the raw bytes of raw, unmodified, as long as there are at
// least 32 of them. No base64 decoding is performed.
func decodeCookieKey(raw string) ([]byte, error) {
	if len(raw) < 32 {
		return nil, fmt.Errorf("config: FARSIGHT_COOKIE_KEY must be at least 32 bytes")
	}
	return []byte(raw), nil
}

// Server looks up a server by id.
func (c *Config) Server(id string) (*Server, bool) {
	s, ok := c.byID[id]
	return s, ok
}

// Secure reports whether cookies should be marked Secure.
func (c *Config) Secure() bool {
	return c.CookieSecure == nil || *c.CookieSecure
}
