package control

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const testSetupPassword = "my-private-admin-password"

func setupBody(password string) map[string]string {
	return map[string]string{"password": password, "confirmPassword": password}
}

func TestFirstSetupProtectsEveryPrivateAPI(t *testing.T) {
	a, handler := testHandler(t, "")
	session := request(handler, "GET", "/api/session", nil)
	var status map[string]bool
	if json.Unmarshal(session.Body.Bytes(), &status) != nil || status["authenticated"] || !status["passwordRequired"] || !status["setupRequired"] {
		t.Fatal("unsafe pending session response")
	}
	for _, path := range []string{"/api/state", "/api/config", "/api/core", "/api/export", "/api/settings", "/api/subscriptions", "/api/nodes"} {
		if result := request(handler, "GET", path, nil); result.Code != http.StatusUnauthorized {
			t.Fatalf("private API accessible before setup: %s", path)
		}
	}
	if result := request(handler, "POST", "/api/login", map[string]string{"password": ""}); result.Code != http.StatusConflict || len(result.Result().Cookies()) != 0 {
		t.Fatal("empty-password login bypassed setup")
	}
	if reopened, err := NewApp(a.store, ""); err != nil || !reopened.SetupRequired() {
		t.Fatal("restart did not preserve pending initialization")
	}
	data, err := os.ReadFile(filepath.Join(a.store.dir, "admin.json"))
	if err != nil || bytes.Contains(data, []byte("setupToken")) {
		t.Fatal("fresh setup still generates an initialization code")
	}
}

func TestSetupValidationPersistenceAndSingleUse(t *testing.T) {
	a, handler := testHandler(t, "")
	for _, test := range []struct {
		name string
		body map[string]string
		code int
	}{
		{"empty password", setupBody(""), http.StatusBadRequest},
		{"short password", setupBody("short"), http.StatusBadRequest},
		{"spaces", setupBody("            "), http.StatusBadRequest},
		{"confirmation", map[string]string{"password": testSetupPassword, "confirmPassword": "another-password"}, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := request(handler, "POST", "/api/setup", test.body)
			if result.Code != test.code || len(result.Result().Cookies()) != 0 || !a.auth.setupRequired() {
				t.Fatal("invalid setup accepted or issued a session")
			}
		})
	}
	result := request(handler, "POST", "/api/setup", setupBody(testSetupPassword))
	cookies := result.Result().Cookies()
	if result.Code != http.StatusOK || len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || a.SetupRequired() {
		t.Fatal("password-only setup did not issue a protected session")
	}
	if request(handler, "GET", "/api/state", nil, cookies...).Code != http.StatusOK {
		t.Fatal("setup session cannot access the panel")
	}
	if request(handler, "GET", "/api/state", nil).Code != http.StatusUnauthorized {
		t.Fatal("completed setup left anonymous access enabled")
	}
	if request(handler, "POST", "/api/setup", setupBody("attacker-new-password")).Code != http.StatusConflict {
		t.Fatal("initialization endpoint can reset an existing password")
	}
	path := filepath.Join(a.store.dir, "admin.json")
	data, err := os.ReadFile(path)
	info, statErr := os.Stat(path)
	if err != nil || statErr != nil || info.Mode().Perm() != 0600 || bytes.Contains(data, []byte(testSetupPassword)) {
		t.Fatal("unsafe persisted authentication data")
	}
	var record adminRecord
	if json.Unmarshal(data, &record) != nil || record.Iterations < passwordIterations || !matchesPassword(record, testSetupPassword) {
		t.Fatal("password was not saved with salted password hashing")
	}
	reopened, err := NewApp(a.store, "")
	if err != nil || reopened.auth.setupRequired() || !reopened.auth.verify(testSetupPassword) {
		t.Fatal("password did not survive recreation with an empty environment")
	}
	if reopened.authorized(requestForCookie(cookies[0])) {
		t.Fatal("restart retained an old process session")
	}
	login := request(handler, "POST", "/api/login", map[string]string{"password": testSetupPassword})
	if login.Code != http.StatusOK {
		t.Fatal("created password cannot log in")
	}
	if request(handler, "POST", "/api/login", map[string]string{"password": "incorrect"}).Code != http.StatusUnauthorized {
		t.Fatal("incorrect password accepted")
	}
}

func requestForCookie(cookie *http.Cookie) *http.Request {
	request, _ := http.NewRequest("GET", "http://localhost/api/state", nil)
	request.AddCookie(cookie)
	return request
}

func TestConfiguredPasswordSkipsSetupAndSupportsOwnerOverride(t *testing.T) {
	a, handler := testHandler(t, testSetupPassword)
	if a.SetupRequired() {
		t.Fatal("explicit password did not skip initialization")
	}
	if request(handler, "POST", "/api/setup", setupBody("attacker-new-password")).Code != http.StatusConflict {
		t.Fatal("configured password can be overwritten through setup")
	}
	reopened, err := NewApp(a.store, "")
	if err != nil || !reopened.auth.verify(testSetupPassword) {
		t.Fatal("removing the environment variable reset authentication")
	}
	overridden, err := NewApp(a.store, "owner-recovery-password")
	if err != nil || !overridden.auth.verify("owner-recovery-password") || overridden.auth.verify(testSetupPassword) {
		t.Fatal("explicit owner password override failed")
	}
	if _, err := NewApp(a.store, "short"); err == nil {
		t.Fatal("short environment password accepted")
	}
}

func TestAuthenticationFileCorruptionFailsClosed(t *testing.T) {
	for _, content := range []string{"", "{}", "invalid-json", `{"version":1,"setupToken":"short"}`, `{"version":1,"hash":""}`} {
		store := newStore(t)
		path := filepath.Join(store.dir, "admin.json")
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewApp(store, ""); err == nil {
			t.Fatal("damaged authentication data silently reopened initialization")
		}
		actual, _ := os.ReadFile(path)
		if string(actual) != content {
			t.Fatal("damaged authentication data was overwritten")
		}
	}
}

func TestSetupWriteFailureDoesNotUnlockPanel(t *testing.T) {
	a, handler := testHandler(t, "")
	path := filepath.Join(a.store.dir, "admin.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, fs.FileMode(0700)); err != nil {
		t.Fatal(err)
	}
	response := request(handler, "POST", "/api/setup", setupBody(testSetupPassword))
	if response.Code != http.StatusInternalServerError || len(response.Result().Cookies()) != 0 || !a.auth.setupRequired() {
		t.Fatal("failed persistence unlocked the panel")
	}
}

func TestConcurrentSetupHasExactlyOneWinner(t *testing.T) {
	_, handler := testHandler(t, "")
	results := make(chan int, 8)
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			results <- request(handler, "POST", "/api/setup", setupBody(testSetupPassword)).Code
		}()
	}
	workers.Wait()
	close(results)
	winners := 0
	for result := range results {
		if result == http.StatusOK {
			winners++
		} else if result != http.StatusConflict {
			t.Fatalf("unexpected concurrent setup status: %d", result)
		}
	}
	if winners != 1 {
		t.Fatal("setup succeeded more or less than once")
	}
}

func TestLegacyPendingSetupMigratesWithoutCode(t *testing.T) {
	store := newStore(t)
	path := filepath.Join(store.dir, "admin.json")
	legacy := `{"version":1,"setupToken":"` + strings.Repeat("a", 64) + `"}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	auth, err := NewApp(store, "")
	if err != nil || !auth.SetupRequired() {
		t.Fatal("legacy pending setup could not migrate")
	}
	data, _ := os.ReadFile(path)
	if bytes.Contains(data, []byte("setupToken")) {
		t.Fatal("migration retained the old setup code")
	}
	if err := auth.auth.initialize(testSetupPassword); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewApp(store, "")
	if err != nil || reopened.SetupRequired() || !reopened.auth.verify(testSetupPassword) {
		t.Fatal("migrated password was not preserved")
	}
}
