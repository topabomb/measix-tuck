package tuck

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParitySettingsDefaultSecureCookieAndHashRotation(t *testing.T) {
	_, _, _, open := securityFixture(t, false, nil)
	r := browserRequest("POST")
	info, _ := open.Info(r)
	if B(info["enabled"]) || !B(info["unlocked"]) || len(A(info["categories"])) != 6 || Contains(info["categories"], "appearance") {
		t.Fatal(info)
	}
	for _, category := range append(protectable, "protection", "appearance") {
		if e := open.Require(r, category); e != nil {
			t.Fatal(category, e)
		}
	}
	hash, _ := PasswordHash("six-ok", 6)
	c, _, _, p := securityFixture(t, false, M{"passwordHash": hash, "categories": []any{"providers"}})
	requests := []*httptest.ResponseRecorder{}
	for range 2 {
		w := httptest.NewRecorder()
		requests = append(requests, w)
		rr := httptest.NewRequest("POST", "https://localhost/api/settings/unlock", nil)
		rr.Header.Set("Origin", "https://localhost")
		w.Header().Add("Set-Cookie", "unrelated=value; Path=/")
		value, e := p.Unlock(rr, w, M{"password": "six-ok"})
		if e != nil || strings.Contains(string(JSON(value)), hash) || strings.Contains(string(JSON(value)), "passwordHash") {
			t.Fatal(value, e)
		}
		if len(w.Result().Cookies()) != 2 {
			t.Fatal(w.Header())
		}
		cookie := w.Result().Cookies()[1]
		if cookie.Name != "tuck_settings_access" || !cookie.Secure || !cookie.HttpOnly || p.grants[cookie.Value].binding != "" {
			t.Fatal(cookie)
		}
		rr.AddCookie(cookie)
		if e = p.Require(rr, "providers"); e != nil {
			t.Fatal(e)
		}
	}
	next, rev, _ := c.Read()
	replacement, _ := PasswordHash("new-secret", 10)
	O(O(next["security"])["settingsProtection"])["passwordHash"] = replacement
	c.Save(rev, next)
	for _, w := range requests {
		r := browserRequest("GET")
		r.AddCookie(w.Result().Cookies()[1])
		assertStatus(t, p.Require(r, "providers"), 403)
	}
	if len(p.grants) != 0 {
		t.Fatal("old grants retained")
	}
}

func TestParitySettingsLimitsRecoverAndPrune(t *testing.T) {
	hash, _ := PasswordHash("six-ok", 6)
	_, _, _, p := securityFixture(t, false, M{"passwordHash": hash, "categories": []any{"providers"}})
	r := browserRequest("POST")
	now := NowMS()
	p.Now = func() int64 { return now }
	for range 5 {
		_, e := p.Unlock(r, httptest.NewRecorder(), M{"password": "wrong"})
		assertStatus(t, e, 403)
	}
	_, e := p.Unlock(r, httptest.NewRecorder(), M{"password": "six-ok"})
	assertStatus(t, e, 429)
	now += 600001
	if _, e = p.Unlock(r, httptest.NewRecorder(), M{"password": "six-ok"}); e != nil || len(p.limits) != 0 {
		t.Fatal("limit did not recover", e, p.limits)
	}
	p.grants = map[string]unlockGrant{}
	for i := 0; i < 2048; i++ {
		p.limits[fmt.Sprint(i)] = M{"expiresAt": now + 600000}
	}
	_, e = p.Unlock(r, httptest.NewRecorder(), M{"password": "six-ok"})
	assertStatus(t, e, 429)
	now += 600001
	if _, e = p.Unlock(r, httptest.NewRecorder(), M{"password": "six-ok"}); e != nil || len(p.limits) != 0 {
		t.Fatal("limit expiry not pruned", e)
	}
	p.grants = map[string]unlockGrant{}
	for i := 0; i < 256; i++ {
		p.grants[fmt.Sprint(i)] = unlockGrant{"local", now + 1000}
	}
	_, e = p.Unlock(r, httptest.NewRecorder(), M{"password": "six-ok"})
	assertStatus(t, e, 429)
	now += 1001
	if _, e = p.Unlock(r, httptest.NewRecorder(), M{"password": "six-ok"}); e != nil || len(p.grants) != 1 || p.checks != 0 {
		t.Fatal("grants not pruned", e, len(p.grants))
	}
}
