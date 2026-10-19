package control

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBootstrapCreatesPrivateEmptyInstallation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	options := BootstrapOptions{PublicHost: "proxy.example.com", ControllerURL: "http://127.0.0.1:19092"}
	if err := Bootstrap(dir, options); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	st := s.Snapshot()
	if st.Settings.PublicHost != options.PublicHost || st.Settings.ControllerURL != options.ControllerURL || len(st.Settings.Password) < 24 || len(st.Settings.ControllerSecret) < 32 {
		t.Fatal("first boot settings or generated credentials are invalid")
	}
	var doc struct {
		Controller string           `yaml:"external-controller"`
		Secret     string           `yaml:"secret"`
		Listeners  []map[string]any `yaml:"listeners"`
		Rules      []string         `yaml:"rules"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "mihomo.yaml"))
	if err != nil || yaml.Unmarshal(data, &doc) != nil {
		t.Fatal("bootstrap config is missing or invalid")
	}
	if len(doc.Listeners) != 0 || doc.Controller != "127.0.0.1:19092" || doc.Secret != st.Settings.ControllerSecret || len(doc.Rules) != 1 || doc.Rules[0] != "MATCH,REJECT" {
		t.Fatal("empty installation must not expose a proxy listener")
	}
	for _, name := range []string{"state.json", "mihomo.yaml"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("%s must be private", name)
		}
	}
}

func TestBootstrapPreservesAppliedConfigAndPendingCredentials(t *testing.T) {
	s := newStore(t)
	if err := s.Update(func(st *State) error { return reconcile(st, "source", fixtureProxies(t)) }); err != nil {
		t.Fatal(err)
	}
	applied, err := GenerateConfig(s.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err = atomicWrite(filepath.Join(s.dir, "mihomo.yaml"), applied); err != nil {
		t.Fatal(err)
	}
	if err = s.Update(func(st *State) error { st.Settings.Password = "pending-new-password"; st.Revision++; return nil }); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(s.dir, "state.json"))
	if err = Bootstrap(s.dir, BootstrapOptions{Username: "different", Password: "different-password", ControllerURL: "http://127.0.0.1:19999"}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(s.dir, "state.json"))
	config, _ := os.ReadFile(filepath.Join(s.dir, "mihomo.yaml"))
	if !bytes.Equal(before, after) || !bytes.Equal(applied, config) {
		t.Fatal("reinitialization changed data or applied pending settings")
	}
}

func TestBootstrapRefusesIncompleteRestoreAndInvalidFirstBoot(t *testing.T) {
	t.Run("nodes without applied file", func(t *testing.T) {
		s := newStore(t)
		_ = s.Update(func(st *State) error { return reconcile(st, "source", fixtureProxies(t)) })
		before, _ := os.ReadFile(filepath.Join(s.dir, "state.json"))
		if Bootstrap(s.dir, BootstrapOptions{}) == nil {
			t.Fatal("must not silently apply node settings after a missing config")
		}
		after, _ := os.ReadFile(filepath.Join(s.dir, "state.json"))
		if !bytes.Equal(before, after) {
			t.Fatal("incomplete restore modified state")
		}
	})
	t.Run("applied file without state", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "mihomo.yaml"), []byte("secret: existing-secret"), 0600)
		if Bootstrap(dir, BootstrapOptions{}) == nil {
			t.Fatal("must not create unrelated credentials beside an existing config")
		}
		if _, err := os.Stat(filepath.Join(dir, "state.json")); !os.IsNotExist(err) {
			t.Fatal("orphaned config must not create state")
		}
	})
	t.Run("short initial password", func(t *testing.T) {
		dir := t.TempDir()
		if Bootstrap(dir, BootstrapOptions{Password: "short"}) == nil {
			t.Fatal("invalid credentials must fail initialization")
		}
		if _, err := os.Stat(filepath.Join(dir, "state.json")); !os.IsNotExist(err) {
			t.Fatal("invalid first boot must not persist default credentials")
		}
	})
}

func TestEveryListenerRequiresCredentials(t *testing.T) {
	st := initialState()
	_ = reconcile(&st, "source", fixtureProxies(t))
	data, err := GenerateConfig(st)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Listeners []struct {
			Users []struct{ Username, Password string }
		}
	}
	if err = yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Listeners) != 2 {
		t.Fatal("expected both proxy listeners")
	}
	for _, listener := range doc.Listeners {
		if len(listener.Users) != 1 || listener.Users[0].Username != st.Settings.Username || listener.Users[0].Password != st.Settings.Password {
			t.Fatal("each mixed listener must have explicit nonempty credentials")
		}
	}
	for _, settings := range []Settings{
		func() Settings { s := st.Settings; s.Username = ""; return s }(),
		func() Settings { s := st.Settings; s.Password = ""; return s }(),
		func() Settings { s := st.Settings; s.Password = "short"; return s }(),
	} {
		st.Settings = settings
		if _, err := GenerateConfig(st); err == nil {
			t.Fatal("invalid credentials must never produce a config")
		}
	}
}
