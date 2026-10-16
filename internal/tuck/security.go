package tuck

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/scrypt"
)

func PasswordHash(password string, minLength int) (string, error) {
	if utf8.RuneCountInString(password) < minLength || len(password) > 1024 {
		return "", Problem(422, "invalid_password", "密码长度无效")
	}
	salt := make([]byte, 16)
	if _, e := rand.Read(salt); e != nil {
		return "", e
	}
	saltText := hex.EncodeToString(salt)
	key, e := scrypt.Key([]byte(password), []byte(saltText), 32768, 8, 3, 32)
	if e != nil {
		return "", e
	}
	return "scrypt$32768$8$3$" + saltText + "$" + hex.EncodeToString(key), nil
}
func VerifyPassword(password, encoded string) bool {
	if !passwordPattern.MatchString(encoded) {
		return false
	}
	parts := strings.Split(encoded, "$")
	key, e := scrypt.Key([]byte(password), []byte(parts[4]), 32768, 8, 3, 32)
	return e == nil && Equal(hex.EncodeToString(key), parts[5])
}
func IsLoopback(address string) bool { ip := net.ParseIP(address); return ip != nil && ip.IsLoopback() }
func peerIP(r *http.Request) string {
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		return r.RemoteAddr
	}
	return host
}
func retryError(code, message string, seconds int) error {
	return &HTTPError{Status: 429, Code: code, Message: message, RetryAfter: seconds}
}

type Security struct {
	Store          *Store
	Config         *Config
	InternalToken  string
	Now            func() int64
	mu             sync.Mutex
	checks         int
	dummy          string
	watches        map[string]map[string]context.CancelFunc
	activeIdentity string
}

func NewSecurity(store *Store, config *Config) (*Security, error) {
	dummy, e := PasswordHash(Token(), 12)
	if e != nil {
		return nil, e
	}
	s := &Security{Store: store, Config: config, InternalToken: Token(), Now: NowMS, dummy: dummy, watches: map[string]map[string]context.CancelFunc{}}
	s.activeIdentity = s.identity()
	config.mu.Lock()
	config.onApplied = s.syncIdentity
	config.mu.Unlock()
	_, e = store.DB.Exec("DELETE FROM web_sessions WHERE identity<>? OR expires_at<=?", s.identity(), s.Now())
	return s, e
}
func (s *Security) syncIdentity() {
	identity := s.identity()
	s.mu.Lock()
	changed := identity != s.activeIdentity
	s.activeIdentity = identity
	if changed {
		for _, watchers := range s.watches {
			for _, cancel := range watchers {
				cancel()
			}
		}
		s.watches = map[string]map[string]context.CancelFunc{}
	}
	s.mu.Unlock()
	if changed {
		_, _ = s.Store.DB.Exec("DELETE FROM web_sessions WHERE identity<>?", identity)
	}
}
func (s *Security) auth() M { return O(O(s.Config.Applied()["security"])["auth"]) }
func (s *Security) identity() string {
	a := s.auth()
	return Hash([]byte(S(a["username"]) + "\x00" + S(a["passwordHash"])))
}
func (s *Security) Origin(r *http.Request) (*url.URL, error) {
	host := r.Host
	if host == "" || strings.ContainsAny(host, " \t\r\n/@\\,#") {
		return nil, Problem(403, "invalid_origin", "无效的访问地址")
	}
	u, e := url.Parse("http://" + host)
	if e != nil || u.Hostname() == "" {
		return nil, Problem(403, "invalid_origin", "无效的访问地址")
	}
	listen := S(O(s.Config.Applied()["server"])["listenHost"])
	if !B(s.auth()["enabled"]) && IsLoopback(listen) && !IsLoopback(u.Hostname()) && u.Hostname() != "localhost" {
		return nil, Problem(403, "invalid_origin", "无效的本机访问地址")
	}
	origin := r.Header.Get("Origin")
	if origin != "" {
		o, e := url.Parse(origin)
		if e != nil || (o.Scheme != "http" && o.Scheme != "https") || o.Host != u.Host || o.User != nil || o.Path != "" || o.RawQuery != "" || o.Fragment != "" || o.String() != origin {
			return nil, Problem(403, "invalid_origin", "拒绝跨站请求")
		}
		return o, nil
	}
	return u, nil
}
func (s *Security) Internal(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/tools/") && IsLoopback(peerIP(r)) && Equal(r.Header.Get("Authorization"), "Bearer "+s.InternalToken)
}
func cookieValue(r *http.Request, name string) string {
	c, e := r.Cookie(name)
	if e != nil {
		return ""
	}
	return c.Value
}
func (s *Security) Session(r *http.Request) (M, error) {
	token := cookieValue(r, "tuck_session")
	if len(token) != 43 {
		return nil, nil
	}
	var hash, identity, csrf string
	var expires int64
	e := s.Store.DB.QueryRow("SELECT token_hash,identity,csrf,expires_at FROM web_sessions WHERE token_hash=? AND identity=? AND expires_at>?", Hash([]byte(token)), s.identity(), s.Now()).Scan(&hash, &identity, &csrf, &expires)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	return M{"token_hash": hash, "identity": identity, "csrf": csrf, "expires_at": expires}, nil
}
func (s *Security) Authorize(r *http.Request) (M, error) {
	if s.Internal(r) {
		return nil, nil
	}
	if _, e := s.Origin(r); e != nil {
		return nil, e
	}
	if !B(s.auth()["enabled"]) {
		return nil, nil
	}
	session, e := s.Session(r)
	if e != nil {
		return nil, e
	}
	if session == nil {
		return nil, Problem(401, "login_required", "请登录工作台")
	}
	if r.Method != "GET" && r.Method != "HEAD" && !Equal(r.Header.Get("X-Tuck-CSRF"), S(session["csrf"])) {
		return nil, Problem(403, "csrf_changed", "页面凭据已变化，请刷新后重试")
	}
	return session, nil
}
func (s *Security) Info(r *http.Request) (M, error) {
	session, e := s.Session(r)
	if e != nil {
		return nil, e
	}
	enabled := B(s.auth()["enabled"])
	v := M{"enabled": enabled, "authenticated": !enabled || session != nil}
	if session != nil {
		v["username"] = s.auth()["username"]
		v["csrf"] = session["csrf"]
		v["expiresAt"] = session["expires_at"]
	}
	return v, nil
}
func loginCookie(w http.ResponseWriter, token string, seconds int, secure, remember bool) {
	cookie := &http.Cookie{Name: "tuck_session", Value: token, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode}
	if remember || token == "" {
		cookie.MaxAge = seconds
		if token == "" {
			cookie.MaxAge = -1
		}
	}
	http.SetCookie(w, cookie)
}
func (s *Security) Login(r *http.Request, w http.ResponseWriter, data M) (M, error) {
	origin, e := s.Origin(r)
	if e != nil {
		return nil, e
	}
	auth := s.auth()
	identity := Hash([]byte(S(auth["username"]) + "\x00" + S(auth["passwordHash"])))
	if !B(auth["enabled"]) {
		return nil, Problem(409, "login_disabled", "当前未启用登录")
	}
	if r.Header.Get("Origin") == "" {
		return nil, Problem(403, "origin_required", "登录需要同源 Origin")
	}
	_, uok := data["username"].(string)
	_, pok := data["password"].(string)
	if !uok || !pok || len(S(data["username"])) > 320 || len(S(data["password"])) > 1024 || !isBool(data["rememberMe"]) {
		return nil, Problem(400, "invalid_login", "登录参数无效")
	}
	keys := []string{"user:" + Hash([]byte(S(data["username"]))), "ip:" + peerIP(r)}
	s.mu.Lock()
	e = s.Store.Write(func(tx *sql.Tx) error {
		if identity != s.identity() || !B(s.auth()["enabled"]) {
			return Problem(409, "login_changed", "登录配置已变化，请重新登录")
		}
		if _, e := tx.Exec("DELETE FROM login_limits WHERE expires_at<=?", s.Now()); e != nil {
			return e
		}
		for _, key := range keys {
			var until int64
			e := tx.QueryRow("SELECT until_at FROM login_limits WHERE key=?", key).Scan(&until)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			if until > s.Now() {
				return retryError("login_limited", "登录尝试过多，请稍后重试", int((until-s.Now()+999)/1000))
			}
		}
		var count int
		if e := tx.QueryRow("SELECT COUNT(*) FROM login_limits").Scan(&count); e != nil {
			return e
		}
		if s.checks >= 2 || count >= 2048 {
			return retryError("login_busy", "登录繁忙，请稍后重试", 1)
		}
		return nil
	})
	if e == nil {
		s.checks++
	}
	s.mu.Unlock()
	if e != nil {
		return nil, e
	}
	encoded := S(auth["passwordHash"])
	if encoded == "" {
		encoded = s.dummy
	}
	valid := VerifyPassword(S(data["password"]), encoded) && Equal(S(data["username"]), S(auth["username"]))
	s.mu.Lock()
	s.checks--
	s.mu.Unlock()
	if !valid {
		e = s.Store.Write(func(tx *sql.Tx) error {
			for _, key := range keys {
				var failures int
				_ = tx.QueryRow("SELECT failures FROM login_limits WHERE key=?", key).Scan(&failures)
				failures++
				delay := 0
				if strings.HasPrefix(key, "ip:") {
					if failures >= 20 {
						delay = 600000
					}
				} else if failures >= 5 {
					delay = []int{5000, 30000, 120000, 300000}[min(failures-5, 3)]
				}
				if _, e := tx.Exec("INSERT OR REPLACE INTO login_limits VALUES(?,?,?,?)", key, failures, s.Now()+int64(delay), s.Now()+600000); e != nil {
					return e
				}
			}
			return nil
		})
		if e != nil {
			return nil, e
		}
		return nil, Problem(401, "invalid_credentials", "用户名或密码错误")
	}
	token, csrf := Token(), Token()
	seconds := 12 * 3600
	if B(data["rememberMe"]) {
		seconds = 30 * 86400
	}
	expires := s.Now() + int64(seconds)*1000
	old, e := s.Session(r)
	if e != nil {
		return nil, e
	}
	e = s.Store.Write(func(tx *sql.Tx) error {
		if identity != s.identity() || !B(s.auth()["enabled"]) {
			return Problem(409, "login_changed", "登录配置已变化，请重新登录")
		}
		if _, e := tx.Exec("DELETE FROM login_limits WHERE key=?", keys[0]); e != nil {
			return e
		}
		if _, e := tx.Exec("DELETE FROM web_sessions WHERE expires_at<=?", s.Now()); e != nil {
			return e
		}
		var count int
		if e := tx.QueryRow("SELECT COUNT(*) FROM web_sessions").Scan(&count); e != nil {
			return e
		}
		if count >= 100 {
			return retryError("session_limit", "登录设备过多，请退出其他设备后重试", 60)
		}
		if old != nil {
			if _, e := tx.Exec("DELETE FROM web_sessions WHERE token_hash=?", S(old["token_hash"])); e != nil {
				return e
			}
		}
		_, e := tx.Exec("INSERT INTO web_sessions VALUES(?,?,?,?)", Hash([]byte(token)), identity, csrf, expires)
		return e
	})
	if e != nil {
		return nil, e
	}
	if old != nil {
		s.cancelWatches(S(old["token_hash"]))
	}
	loginCookie(w, token, seconds, origin.Scheme == "https", B(data["rememberMe"]))
	return M{"enabled": true, "authenticated": true, "username": auth["username"], "csrf": csrf, "expiresAt": expires}, nil
}
func (s *Security) cancelWatches(hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cancel := range s.watches[hash] {
		cancel()
	}
	delete(s.watches, hash)
}
func (s *Security) Logout(r *http.Request, w http.ResponseWriter) error {
	session, e := s.Authorize(r)
	if e != nil {
		return e
	}
	if session != nil {
		_, e = s.Store.DB.Exec("DELETE FROM web_sessions WHERE token_hash=?", S(session["token_hash"]))
		if e != nil {
			return e
		}
		s.cancelWatches(S(session["token_hash"]))
	}
	o, e := s.Origin(r)
	if e != nil {
		return e
	}
	loginCookie(w, "", 0, o.Scheme == "https", false)
	return nil
}
func (s *Security) Watch(ctx context.Context, session M) (context.Context, context.CancelFunc) {
	child, cancel := context.WithCancel(ctx)
	if session == nil {
		return child, cancel
	}
	key := S(session["token_hash"])
	id := ID()
	s.mu.Lock()
	var valid int
	err := s.Store.DB.QueryRow("SELECT COUNT(*) FROM web_sessions WHERE token_hash=? AND identity=? AND expires_at>?", key, s.identity(), s.Now()).Scan(&valid)
	if err != nil || valid != 1 || S(session["identity"]) != s.identity() {
		s.mu.Unlock()
		cancel()
		return child, cancel
	}
	if s.watches[key] == nil {
		s.watches[key] = map[string]context.CancelFunc{}
	}
	s.watches[key][id] = cancel
	s.mu.Unlock()
	go func() {
		timer := time.NewTimer(time.Duration(max(0, N(session["expires_at"])-float64(s.Now()))) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-child.Done():
		case <-timer.C:
			cancel()
		}
		s.mu.Lock()
		delete(s.watches[key], id)
		if len(s.watches[key]) == 0 {
			delete(s.watches, key)
		}
		s.mu.Unlock()
	}()
	return child, cancel
}

type unlockGrant struct {
	binding string
	expires int64
}
type SettingsProtection struct {
	Config      *Config
	Security    *Security
	Now         func() int64
	mu          sync.Mutex
	epoch       uint64
	fingerprint string
	grants      map[string]unlockGrant
	limits      map[string]M
	checks      int
}

func NewSettingsProtection(config *Config, security *Security) *SettingsProtection {
	return &SettingsProtection{Config: config, Security: security, Now: NowMS, grants: map[string]unlockGrant{}, limits: map[string]M{}}
}
func (p *SettingsProtection) config() (M, error) {
	c, _, e := p.Config.Read()
	if e != nil {
		return nil, e
	}
	v := O(O(c["security"])["settingsProtection"])
	fingerprint := Hash(JSON(v))
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fingerprint != fingerprint {
		p.grants = map[string]unlockGrant{}
		p.epoch++
		p.fingerprint = fingerprint
	}
	now := p.Now()
	for k, g := range p.grants {
		if g.expires <= now {
			delete(p.grants, k)
		}
	}
	for k, l := range p.limits {
		if N(l["expiresAt"]) <= float64(now) {
			delete(p.limits, k)
		}
	}
	return v, nil
}
func (p *SettingsProtection) Invalidate() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.grants = map[string]unlockGrant{}
	p.epoch++
}
func (p *SettingsProtection) binding(r *http.Request) string {
	if !B(p.Security.auth()["enabled"]) {
		return "local"
	}
	session, e := p.Security.Session(r)
	if e != nil || session == nil {
		return ""
	}
	return S(session["token_hash"])
}
func (p *SettingsProtection) Info(r *http.Request) (M, error) {
	c, e := p.config()
	if e != nil {
		return nil, e
	}
	categories := Strings(c["categories"])
	if len(categories) == 0 {
		categories = protectable
	}
	binding := p.binding(r)
	p.mu.Lock()
	g, ok := p.grants[Hash([]byte(cookieValue(r, "tuck_settings_access")))]
	p.mu.Unlock()
	enabled := S(c["passwordHash"]) != ""
	unlocked := !enabled || binding != "" && ok && g.binding == binding
	locked := []string{}
	if enabled && !unlocked {
		locked = categories
	}
	v := M{"enabled": enabled, "unlocked": unlocked, "categories": categories, "lockedCategories": locked}
	if enabled && unlocked {
		v["expiresAt"] = g.expires
	}
	return v, nil
}
func (p *SettingsProtection) sameOrigin(r *http.Request) error {
	if _, e := p.Security.Origin(r); e != nil {
		return e
	}
	if r.Header.Get("Origin") == "" {
		return Problem(403, "settings_origin_required", "配置保护操作需要同源页面请求")
	}
	return nil
}
func (p *SettingsProtection) Require(r *http.Request, category string) error {
	info, e := p.Info(r)
	if e != nil {
		return e
	}
	if !B(info["enabled"]) || category != "protection" && !Contains(info["categories"], category) {
		return nil
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		if e = p.sameOrigin(r); e != nil {
			return e
		}
	}
	if !B(info["unlocked"]) {
		return Problem(403, "settings_locked", "请先解锁设置")
	}
	return nil
}
func (p *SettingsProtection) cookie(r *http.Request, w http.ResponseWriter, value string) {
	o, _ := p.Security.Origin(r)
	c := &http.Cookie{Name: "tuck_settings_access", Value: value, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: o != nil && o.Scheme == "https", MaxAge: 3600}
	if value == "" {
		c.MaxAge = -1
	}
	http.SetCookie(w, c)
}
func (p *SettingsProtection) Unlock(r *http.Request, w http.ResponseWriter, data M) (M, error) {
	if e := p.sameOrigin(r); e != nil {
		return nil, e
	}
	c, e := p.config()
	if e != nil {
		return nil, e
	}
	if S(c["passwordHash"]) == "" {
		return p.Info(r)
	}
	if !noUnknown(data, "password") || S(data["password"]) == "" || len(S(data["password"])) > 1024 {
		return nil, Problem(422, "settings_password_invalid", "请输入有效的配置保护密码")
	}
	binding := p.binding(r)
	if binding == "" {
		return nil, Problem(403, "settings_login_required", "请先登录后解锁设置")
	}
	key := peerIP(r)
	p.mu.Lock()
	limit := p.limits[key]
	if N(limit["until"]) > float64(p.Now()) {
		p.mu.Unlock()
		return nil, retryError("settings_unlock_limited", "密码尝试过多，请稍后重试", int((N(limit["until"])-float64(p.Now())+999)/1000))
	}
	if p.checks >= 2 || len(p.grants) >= 256 || limit == nil && len(p.limits) >= 2048 {
		p.mu.Unlock()
		return nil, retryError("settings_unlock_limited", "解锁请求繁忙，请稍后重试", 1)
	}
	epoch := p.epoch
	p.checks++
	p.mu.Unlock()
	valid := VerifyPassword(S(data["password"]), S(c["passwordHash"]))
	_, e = p.config()
	afterBinding := p.binding(r)
	p.mu.Lock()
	p.checks--
	defer p.mu.Unlock()
	if e != nil {
		return nil, e
	}
	if epoch != p.epoch || binding != afterBinding {
		return nil, Problem(409, "settings_access_changed", "配置保护或登录状态已变化，请重新解锁")
	}
	if !valid {
		failures := I(p.limits[key]["failures"]) + 1
		delay := 0
		if failures >= 5 {
			delay = []int{5000, 30000, 120000, 300000}[min(failures-5, 3)]
		}
		p.limits[key] = M{"failures": failures, "until": p.Now() + int64(delay), "expiresAt": p.Now() + 600000}
		return nil, Problem(403, "settings_password_invalid", "配置保护密码不正确")
	}
	delete(p.limits, key)
	delete(p.grants, Hash([]byte(cookieValue(r, "tuck_settings_access"))))
	token := Token()
	expires := p.Now() + 3600000
	p.grants[Hash([]byte(token))] = unlockGrant{binding, expires}
	p.cookie(r, w, token)
	categories := Strings(c["categories"])
	if len(categories) == 0 {
		categories = protectable
	}
	return M{"enabled": true, "unlocked": true, "categories": categories, "lockedCategories": []string{}, "expiresAt": expires}, nil
}
func (p *SettingsProtection) Lock(r *http.Request, w http.ResponseWriter) (M, error) {
	if e := p.sameOrigin(r); e != nil {
		return nil, e
	}
	p.mu.Lock()
	delete(p.grants, Hash([]byte(cookieValue(r, "tuck_settings_access"))))
	p.epoch++
	p.mu.Unlock()
	p.cookie(r, w, "")
	return p.Info(r)
}
func (p *SettingsProtection) Prepare(data M) (M, string, error) {
	if !noUnknown(data, "revision", "password", "categories") {
		return nil, "", Problem(422, "settings_protection_invalid", "配置保护内容无效")
	}
	c, revision, e := p.Config.Read()
	if e != nil {
		return nil, "", e
	}
	if S(data["revision"]) != revision {
		return nil, "", Problem(409, "settings_changed", "配置已改变，请重新读取后保存")
	}
	if !uniqueStrings(data["categories"], func(s string) bool { return Contains(protectable, s) }) {
		return nil, "", Problem(422, "settings_protection_invalid", "保护分类无效")
	}
	v := Clone(O(O(c["security"])["settingsProtection"]))
	v["categories"] = data["categories"]
	if Has(data, "password") {
		password, ok := data["password"].(string)
		if !ok {
			return nil, "", Problem(422, "settings_protection_invalid", "配置保护密码格式无效")
		}
		if password == "" {
			delete(v, "passwordHash")
		} else {
			p.mu.Lock()
			if p.checks >= 2 {
				p.mu.Unlock()
				return nil, "", retryError("settings_unlock_limited", "密码处理繁忙，请稍后重试", 1)
			}
			p.checks++
			p.mu.Unlock()
			encoded, err := PasswordHash(password, 6)
			p.mu.Lock()
			p.checks--
			p.mu.Unlock()
			if err != nil {
				return nil, "", err
			}
			v["passwordHash"] = encoded
		}
	}
	O(c["security"])["settingsProtection"] = v
	_, latest, err := p.Config.Read()
	if err != nil {
		return nil, "", err
	}
	if latest != revision {
		return nil, "", Problem(409, "settings_changed", "配置已改变，请重新读取后保存")
	}
	return c, revision, nil
}
