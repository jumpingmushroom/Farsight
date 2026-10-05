package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func hash(t *testing.T, s string) string {
	h, err := bcrypt.GenerateFromPassword([]byte(s), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(h)
}

func write(t *testing.T, body string) string {
	p := filepath.Join(t.TempDir(), "farsight.json")
	os.WriteFile(p, []byte(body), 0o600)
	return p
}

const key = "0123456789abcdef0123456789abcdef-long-enough"

func env(k string) string {
	if k == "FARSIGHT_COOKIE_KEY" {
		return key
	}
	return ""
}

func TestLoadDefaultsAndLookup(t *testing.T) {
	p := write(t, `{"servers":[{"id":"mulevikings","name":"Mulevikings","crossplay":true,
	  "passphraseHash":"`+hash(t, "pw")+`","agentTokenHash":"`+hash(t, "tok")+`"}]}`)
	c, err := Load(p, env)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != ":8080" || c.DataDir != "/data" || !c.Secure() || string(c.CookieKey) != key {
		t.Fatalf("defaults = %+v", c)
	}
	s, ok := c.Server("mulevikings")
	if !ok || s.MaxPlayers != 10 || !s.Crossplay {
		t.Fatalf("server = %+v %v", s, ok)
	}
	if _, ok := c.Server("nope"); ok {
		t.Fatal("unknown server found")
	}
}

func TestLoadRejects(t *testing.T) {
	good := `"passphraseHash":"` + hash(t, "pw") + `","agentTokenHash":"` + hash(t, "tok") + `"`
	cases := map[string]string{
		"no servers":    `{"servers":[]}`,
		"bad id":        `{"servers":[{"id":"Bad Id","name":"x",` + good + `}]}`,
		"duplicate id":  `{"servers":[{"id":"a","name":"x",` + good + `},{"id":"a","name":"y",` + good + `}]}`,
		"plain hash":    `{"servers":[{"id":"a","name":"x","passphraseHash":"pw","agentTokenHash":"tok"}]}`,
		"unknown field": `{"servers":[{"id":"a","name":"x",` + good + `}],"extra":1}`,
	}
	for name, body := range cases {
		if _, err := Load(write(t, body), env); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	short := func(string) string { return "short" }
	if _, err := Load(write(t, `{"servers":[{"id":"a","name":"x",`+good+`}]}`), short); err == nil || !strings.Contains(err.Error(), "FARSIGHT_COOKIE_KEY") {
		t.Errorf("short cookie key: err = %v", err)
	}
}

// Plan 5 Task 1: ingestListen defaults to empty (single-listener mode) and
// must differ from listen, except when both ask for an OS-assigned port
// (":0"), which is always distinct across two listeners.
func TestIngestListenDefaultAndValidation(t *testing.T) {
	good := `"passphraseHash":"` + hash(t, "pw") + `","agentTokenHash":"` + hash(t, "tok") + `"`

	c, err := Load(write(t, `{"servers":[{"id":"a","name":"x",`+good+`}]}`), env)
	if err != nil {
		t.Fatal(err)
	}
	if c.IngestListen != "" {
		t.Errorf("ingestListen default = %q, want empty", c.IngestListen)
	}

	if _, err := Load(write(t, `{"listen":":8080","ingestListen":":8080","servers":[{"id":"a","name":"x",`+good+`}]}`), env); err == nil {
		t.Error("ingestListen == listen: want error")
	}

	c2, err := Load(write(t, `{"listen":"127.0.0.1:0","ingestListen":"127.0.0.1:0","servers":[{"id":"a","name":"x",`+good+`}]}`), env)
	if err != nil {
		t.Errorf("both :0 ports: unexpected error %v", err)
	} else if c2.IngestListen != "127.0.0.1:0" {
		t.Errorf("ingestListen = %q, want 127.0.0.1:0", c2.IngestListen)
	}

	c3, err := Load(write(t, `{"listen":":8080","ingestListen":":8081","servers":[{"id":"a","name":"x",`+good+`}]}`), env)
	if err != nil || c3.IngestListen != ":8081" {
		t.Errorf("distinct ports: %+v, err %v", c3, err)
	}
}

// Plan 7: each server's timeZone (its agent's FARSIGHT_LOG_TZ) defaults to
// UTC and must be a loadable IANA name.
func TestTimeZone(t *testing.T) {
	good := `"passphraseHash":"` + hash(t, "pw") + `","agentTokenHash":"` + hash(t, "tok") + `"`
	c, err := Load(write(t, `{"servers":[{"id":"a","name":"x",`+good+`},{"id":"b","name":"y","timeZone":"Europe/Oslo",`+good+`}]}`), env)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := c.Server("a")
	b, _ := c.Server("b")
	if a.TimeZone != "UTC" || a.Location() != time.UTC {
		t.Errorf("a: timeZone %q, location %v; want UTC", a.TimeZone, a.Location())
	}
	if b.Location().String() != "Europe/Oslo" {
		t.Errorf("b: location %v", b.Location())
	}
	if _, err := Load(write(t, `{"servers":[{"id":"a","name":"x","timeZone":"Mars/Olympus",`+good+`}]}`), env); err == nil || !strings.Contains(err.Error(), "timeZone") {
		t.Errorf("bad timeZone: err = %v", err)
	}
	// A Server built without Load (as tests do) still resolves its zone.
	if loc := (&Server{TimeZone: "Europe/Oslo"}).Location(); loc.String() != "Europe/Oslo" {
		t.Errorf("unloaded server location = %v", loc)
	}
	if loc := (&Server{}).Location(); loc != time.UTC {
		t.Errorf("zero server location = %v", loc)
	}
}
