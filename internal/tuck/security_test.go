package tuck

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testPassword = "correct horse battery staple"

func securityFixture(t *testing.T, enabled bool, protection M) (*Config, *Store, *Security, *SettingsProtection) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tuck.config.json")
	if e := InitConfig(path); e != nil {
		t.Fatal(e)
	}
	c, e := LoadConfig(path)
	if e != nil {
		t.Fatal(e)
	}
	v, rev, _ := c.Read()
	if enabled {
		h, e := PasswordHash(testPassword, 12)
		if e != nil {
			t.Fatal(e)
		}
		O(v["security"])["auth"] = M{"enabled": true, "username": "operator", "passwordHash": h}
	}
	if protection != nil {
		O(v["security"])["settingsProtection"] = protection
	}
	if _, e = c.Save(rev, v); e != nil {
		t.Fatal(e)
	}
	c.MarkApplied()
	s, e := NewStore(filepath.Join(t.TempDir(), "tuck.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	guard, e := NewSecurity(s, c)
	if e != nil {
		t.Fatal(e)
	}
	return c, s, guard, NewSettingsProtection(c, guard)
}
func browserRequest(method string) *http.Request {
	r := httptest.NewRequest(method, "http://127.0.0.1:8080/api/runtime", nil)
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("Origin", "http://127.0.0.1:8080")
	return r
}
func loginBrowser(t *testing.T, s *Security, remember bool) (*http.Request, M) {
	t.Helper()
	r := browserRequest("POST")
	w := httptest.NewRecorder()
	v, e := s.Login(r, w, M{"username": "operator", "password": testPassword, "rememberMe": remember})
	if e != nil {
		t.Fatal(e)
	}
	r = browserRequest("GET")
	for _, c := range w.Result().Cookies() {
		r.AddCookie(c)
	}
	r.Header.Set("X-Tuck-CSRF", S(v["csrf"]))
	return r, v
}
func TestSecurityConfigurationAndPasswordCommands(t *testing.T) {
	c, _, _, _ := securityFixture(t, false, nil)
	v, _, _ := c.Read()
	for _, bad := range []any{nil, true, 123, []any{}, "plaintext", M{}} {
		n := Clone(v)
		O(n["security"])["auth"] = M{"enabled": false, "username": "admin", "passwordHash": bad}
		if ValidateConfig(n, false) == nil {
			t.Fatalf("accepted hash %v", bad)
		}
	}
	n := Clone(v)
	O(n["security"])["auth"] = M{"enabled": false, "username": "admin", "unknown": 1}
	if ValidateConfig(n, false) == nil {
		t.Fatal("unknown auth field")
	}
	n = Clone(v)
	O(n["server"])["listenHost"] = "0.0.0.0"
	if ValidateConfig(n, false) == nil {
		t.Fatal("unauthenticated external listener")
	}
	var out bytes.Buffer
	if e := PasswordCommand(c.Path, []string{"--help"}, nil, &out); e != nil || !strings.Contains(out.String(), "login|settings") {
		t.Fatal(e, out.String())
	}
	for _, step := range []struct{ target, password string }{{"login", testPassword}, {"settings", "six-ok"}, {"settings", ""}, {"login", ""}} {
		if e := PasswordCommand(c.Path, []string{step.target}, strings.NewReader(step.password+"\n"), &out); e != nil {
			t.Fatal(e)
		}
		raw, _ := os.ReadFile(c.Path)
		if step.password != "" && bytes.Contains(raw, []byte(step.password)) {
			t.Fatal("plaintext password")
		}
		next, _, e := c.Read()
		if e != nil {
			t.Fatal(e)
		}
		key := "auth"
		if step.target == "settings" {
			key = "settingsProtection"
		}
		hash := S(O(O(next["security"])[key])["passwordHash"])
		if step.password == "" && hash != "" || step.password != "" && !VerifyPassword(step.password, hash) {
			t.Fatal("credential not saved")
		}
	}
	before, _ := os.ReadFile(c.Path)
	if PasswordCommand(c.Path, []string{"settings"}, strings.NewReader("short\n"), &out) == nil {
		t.Fatal("short password")
	}
	after, _ := os.ReadFile(c.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed command changed file")
	}
	if e := PasswordCommand(c.Path, []string{"settings", "--reset"}, nil, &out); e != nil {
		t.Fatal(e)
	}
	after, _ = os.ReadFile(c.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("idempotent reset changed bytes")
	}
	if e := os.WriteFile(c.Path, []byte(`{"secret":"never-echo-this",`), 0600); e != nil {
		t.Fatal(e)
	}
	e := PasswordCommand(c.Path, []string{"settings", "--reset"}, nil, &out)
	if e == nil || strings.Contains(e.Error(), "never-echo-this") {
		t.Fatal(e)
	}
}
func TestSecuritySessionsCSRFExpiryAndRevocation(t *testing.T) {
	c, s, g, _ := securityFixture(t, true, nil)
	a, e := PasswordHash(testPassword, 12)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := PasswordHash(testPassword, 12)
	if a == b || !VerifyPassword(testPassword, a) || VerifyPassword("wrong", a) {
		t.Fatal("password salt/verification")
	}
	r, info := loginBrowser(t, g, true)
	session, e := g.Authorize(r)
	if e != nil || session == nil {
		t.Fatal(e)
	}
	if bytes.Contains(JSON(info), []byte("scrypt")) {
		t.Fatal("hash exposed")
	}
	var persisted string
	if e = s.DB.QueryRow("SELECT token_hash FROM web_sessions").Scan(&persisted); e != nil || persisted == cookieValue(r, "tuck_session") {
		t.Fatal("raw session persisted", e)
	}
	r.Method = "POST"
	r.Header.Del("X-Tuck-CSRF")
	_, e = g.Authorize(r)
	assertStatus(t, e, 403)
	r.Header.Set("X-Tuck-CSRF", S(info["csrf"]))
	ctx, cancel := g.Watch(context.Background(), session)
	defer cancel()
	if e = g.Logout(r, httptest.NewRecorder()); e != nil {
		t.Fatal(e)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("logout retained stream")
	}
	_, e = g.Authorize(r)
	assertStatus(t, e, 401)
	r, info = loginBrowser(t, g, false)
	now := g.Now()
	g.Now = func() int64 { return now + 12*3600000 }
	_, e = g.Authorize(r)
	assertStatus(t, e, 401)
	g.Now = NowMS
	v, rev, _ := c.Read()
	O(O(v["security"])["auth"])["passwordHash"] = a
	if _, e = c.Save(rev, v); e != nil {
		t.Fatal(e)
	}
	c.MarkApplied()
	replacement, e := NewSecurity(s, c)
	if e != nil {
		t.Fatal(e)
	}
	_, e = replacement.Authorize(r)
	assertStatus(t, e, 401)
}
func TestSecurityOriginAndPersistentLimits(t *testing.T) {
	_, s, g, _ := securityFixture(t, true, nil)
	r := browserRequest("POST")
	r.Header.Set("X-Forwarded-For", "8.8.8.8")
	r.Header.Set("X-Forwarded-Proto", "https")
	for _, host := range []string{"evil.example", "null", "http://foreign.invalid", "http://127.0.0.1:8080/", "http://127.0.0.1:8080#x"} {
		r.Header.Set("Origin", host)
		_, e := g.Origin(r)
		assertStatus(t, e, 403)
	}
	r.Header.Set("Origin", "http://127.0.0.1:8080")
	for i := 0; i < 5; i++ {
		_, e := g.Login(r, httptest.NewRecorder(), M{"username": "operator", "password": "wrong", "rememberMe": false})
		assertStatus(t, e, 401)
	}
	_, e := g.Login(r, httptest.NewRecorder(), M{"username": "operator", "password": testPassword, "rememberMe": false})
	assertStatus(t, e, 429)
	var count int
	if e = s.DB.QueryRow("SELECT count(*) FROM login_limits WHERE key='ip:127.0.0.1'").Scan(&count); e != nil || count != 1 {
		t.Fatal("forwarded IP trusted", e)
	}
	again, e := NewSecurity(s, g.Config)
	if e != nil {
		t.Fatal(e)
	}
	_, e = again.Login(r, httptest.NewRecorder(), M{"username": "operator", "password": testPassword, "rememberMe": false})
	assertStatus(t, e, 429)
	now := NowMS()
	again.Now = func() int64 { return now + 600000 }
	_, e = again.Login(r, httptest.NewRecorder(), M{"username": "operator", "password": testPassword, "rememberMe": false})
	if e != nil {
		t.Fatal("limits did not expire", e)
	}
	again.mu.Lock()
	again.checks = 2
	again.mu.Unlock()
	_, e = again.Login(r, httptest.NewRecorder(), M{"username": "operator", "password": testPassword, "rememberMe": false})
	assertStatus(t, e, 429)
}
func TestSettingsProtectionCategoriesCookiesAndExpiry(t *testing.T) {
	h, _ := PasswordHash("six-ok", 6)
	c, _, g, p := securityFixture(t, false, M{"passwordHash": h, "categories": []any{"providers"}})
	r := browserRequest("GET")
	info, e := p.Info(r)
	if e != nil || B(info["unlocked"]) || Contains(info["categories"], "appearance") {
		t.Fatal(info, e)
	}
	if e = p.Require(r, "agents"); e != nil {
		t.Fatal(e)
	}
	assertStatus(t, p.Require(r, "providers"), 403)
	assertStatus(t, p.Require(r, "protection"), 403)
	r.Method = "POST"
	r.Header.Del("Origin")
	_, e = p.Unlock(r, httptest.NewRecorder(), M{"password": "six-ok"})
	assertStatus(t, e, 403)
	r.Header.Set("Origin", "http://127.0.0.1:8080")
	w := httptest.NewRecorder()
	w.Header().Add("Set-Cookie", "other=value; Path=/")
	now := NowMS()
	var clock atomic.Int64
	clock.Store(now)
	p.Now = clock.Load
	info, e = p.Unlock(r, w, M{"password": "six-ok"})
	if e != nil || !B(info["unlocked"]) {
		t.Fatal(info, e)
	}
	if len(w.Header().Values("Set-Cookie")) != 2 {
		t.Fatal("overwrote cookie")
	}
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "tuck_settings_access" {
			if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
				t.Fatal(cookie)
			}
			r.AddCookie(cookie)
		}
	}
	if e = p.Require(r, "providers"); e != nil {
		t.Fatal(e)
	}
	other := browserRequest("GET")
	assertStatus(t, p.Require(other, "providers"), 403)
	clock.Store(now + 3599999)
	info, _ = p.Info(r)
	if !B(info["unlocked"]) || N(info["expiresAt"]) != float64(now+3600000) {
		t.Fatal("sliding expiry")
	}
	clock.Store(now + 3600000)
	assertStatus(t, p.Require(r, "providers"), 403)
	p.Now = NowMS
	w = httptest.NewRecorder()
	_, e = p.Unlock(r, w, M{"password": "six-ok"})
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Del("Cookie")
	for _, cookie := range w.Result().Cookies() {
		r.AddCookie(cookie)
	}
	if _, e = p.Lock(r, httptest.NewRecorder()); e != nil {
		t.Fatal(e)
	}
	assertStatus(t, p.Require(r, "providers"), 403)
	w = httptest.NewRecorder()
	_, e = p.Unlock(r, w, M{"password": "six-ok"})
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Del("Cookie")
	for _, cookie := range w.Result().Cookies() {
		r.AddCookie(cookie)
	}
	restarted := NewSettingsProtection(c, g)
	assertStatus(t, restarted.Require(r, "providers"), 403)
	v, rev, _ := c.Read()
	O(O(v["security"])["settingsProtection"])["categories"] = []any{"providers", "agents"}
	if _, e = c.Save(rev, v); e != nil {
		t.Fatal(e)
	}
	assertStatus(t, p.Require(r, "agents"), 403)
}
func TestSettingsProtectionSessionBindingAndPrepare(t *testing.T) {
	h, _ := PasswordHash("six-ok", 6)
	c, _, g, p := securityFixture(t, true, M{"passwordHash": h, "categories": []any{"providers"}})
	r, info := loginBrowser(t, g, false)
	r.Method = "POST"
	w := httptest.NewRecorder()
	_, e := p.Unlock(r, w, M{"password": "six-ok"})
	if e != nil {
		t.Fatal(e)
	}
	for _, cookie := range w.Result().Cookies() {
		r.AddCookie(cookie)
	}
	second, _ := loginBrowser(t, g, false)
	for _, cookie := range w.Result().Cookies() {
		second.AddCookie(cookie)
	}
	assertStatus(t, p.Require(second, "providers"), 403)
	if e = g.Logout(r, httptest.NewRecorder()); e != nil {
		t.Fatal(e)
	}
	assertStatus(t, p.Require(r, "providers"), 403)
	_ = info
	_, rev, _ := c.Read()
	next, revision, e := p.Prepare(M{"revision": rev, "categories": []any{"agents"}})
	if e != nil || revision != rev || S(O(O(next["security"])["settingsProtection"])["passwordHash"]) != h {
		t.Fatal(e)
	}
	next, _, e = p.Prepare(M{"revision": rev, "categories": []any{}, "password": ""})
	if e != nil || Has(O(O(next["security"])["settingsProtection"]), "passwordHash") {
		t.Fatal(e)
	}
	for _, bad := range []M{{"revision": "stale", "categories": []any{}}, {"revision": rev, "categories": []any{"appearance"}}, {"revision": rev, "categories": []any{"agents", "agents"}}, {"revision": rev, "categories": []any{}, "password": 123}, {"revision": rev, "categories": []any{}, "password": "short"}} {
		if _, _, e = p.Prepare(bad); e == nil {
			t.Fatal("accepted", bad)
		}
	}
	next, _, e = p.Prepare(M{"revision": rev, "categories": []any{"agents"}, "password": "six-ok"})
	if e != nil || !VerifyPassword("six-ok", S(O(O(next["security"])["settingsProtection"])["passwordHash"])) {
		t.Fatal(e)
	}
}
func TestSettingsProtectionLimitsAndConcurrentInvalidation(t *testing.T) {
	h, _ := PasswordHash("six-ok", 6)
	_, _, _, p := securityFixture(t, false, M{"passwordHash": h, "categories": []any{"providers"}})
	r := browserRequest("POST")
	for range 5 {
		_, e := p.Unlock(r, httptest.NewRecorder(), M{"password": "wrong"})
		assertStatus(t, e, 403)
	}
	_, e := p.Unlock(r, httptest.NewRecorder(), M{"password": "six-ok"})
	assertStatus(t, e, 429)
	now := NowMS()
	p.Now = func() int64 { return now + 600001 }
	p.mu.Lock()
	p.checks = 2
	p.mu.Unlock()
	_, e = p.Unlock(r, httptest.NewRecorder(), M{"password": "six-ok"})
	assertStatus(t, e, 429)
	p.mu.Lock()
	p.checks = 0
	p.mu.Unlock()
	done := make(chan error, 1)
	go func() { _, e := p.Unlock(r, httptest.NewRecorder(), M{"password": "six-ok"}); done <- e }()
	deadline := time.Now().Add(time.Second)
	for {
		p.mu.Lock()
		checking := p.checks > 0
		p.mu.Unlock()
		if checking {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("hash did not start")
		}
		time.Sleep(time.Millisecond)
	}
	p.Lock(r, httptest.NewRecorder())
	e = <-done
	assertStatus(t, e, 409)
	p.mu.Lock()
	if len(p.grants) != 0 || p.checks != 0 || len(p.limits) != 0 {
		t.Fatal("unbounded or retained state")
	}
	p.mu.Unlock()
}
func TestPublicIdentifiers(t *testing.T) {
	seen := map[string]bool{}
	for range 10000 {
		id := ID()
		if !publicID.MatchString(id) || seen[id] {
			t.Fatal(id)
		}
		seen[id] = true
	}
}
