package tuck

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPlatformSyncAndAdmission(t *testing.T) {
	for _, version := range []int{4, 5} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			f := newPlatformFixture(t, version)
			f.enroll()
			if S(f.models.Summary()["status"]) != "ready" {
				t.Fatal(f.models.Summary())
			}
			model := f.models.Catalog()[0]
			status, _, raw := f.send("/v1/chat/completions", M{"model": model["key"], "messages": []any{M{"role": "user", "content": "fixture"}}, "stream": true, "unknown_provider_field": M{"preserve": true}})
			if status != 200 {
				t.Fatal(status, raw)
			}
			found, exchange, applied := false, false, false
			for _, r := range f.requestList() {
				path := S(r["path"])
				if strings.HasSuffix(path, "/exchange") {
					exchange = O(r["body"])["platform"] == "ANDROID"
				}
				if strings.HasSuffix(path, "/managed/applied") {
					applied = r["method"] == "PUT"
				}
				if strings.HasPrefix(path, "/runtime/") {
					found = true
					b, h := O(r["body"]), O(r["headers"])
					if path != "/runtime/v1/resources/"+S(model["modelId"])+S(model["runtimePath"]) || b["model"] != "public-chat-alias" || !B(O(b["unknown_provider_field"])["preserve"]) || h["x-measix-managed-generation"] != fmt.Sprint(I(f.snapshot["managedGeneration"])) || !strings.HasPrefix(S(h["x-measix-interaction-id"]), "int_") || h["authorization"] != "Bearer synthetic.access.token" {
						t.Fatalf("forward contract: %s", JSON(r))
					}
				}
			}
			if !found || !exchange || !applied {
				t.Fatal("missing control/runtime requests")
			}
			if strings.Contains(string(JSON(f.models.Summary())), "synthetic.access") {
				t.Fatal("public credential")
			}
			var encrypted []byte
			if e := f.store.DB.QueryRow("SELECT encrypted_payload FROM platform_session WHERE id=1").Scan(&encrypted); e != nil {
				t.Fatal(e)
			}
			if bytes.Contains(encrypted, []byte("synthetic.access")) {
				t.Fatal("plaintext credential")
			}
			cached := Clone(O(f.platform.Binding()["snapshot"]))
			f.change(func() { f.controls["employeeName"] = "更新后的员工" })
			if e := f.platform.Sync(context.Background()); e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(cached, O(f.platform.Binding()["snapshot"])) || f.platform.Binding()["employeeName"] != "更新后的员工" {
				t.Fatal("304 changed verified snapshot or missed employee")
			}
			restored, e := NewPlatform(f.store, f.config)
			if e != nil {
				t.Fatal(e)
			}
			defer restored.Close()
			if restored.Binding()["employeeName"] != "更新后的员工" {
				t.Fatal("employee not persisted")
			}
			f.change(func() { f.controls["badResponse"] = true; f.controls["etag"] = "\"changed\"" })
			if e := f.platform.Sync(context.Background()); Code(e) != "invalid_platform_data" {
				t.Fatalf("invalid snapshot: %v", e)
			}
			if !reflect.DeepEqual(cached, O(f.platform.Binding()["snapshot"])) {
				t.Fatal("invalid snapshot replaced cache")
			}
			f.change(func() { O(f.controls["state"])["activeManagedGeneration"] = I(f.snapshot["managedGeneration"]) + 1 })
			assertStatus(t, f.platform.Admit(context.Background(), I(cached["managedGeneration"])), 428)
		})
	}
}
func TestPlatformRefreshPersistence(t *testing.T) {
	f := newPlatformFixture(t, 5)
	f.enroll()
	d := f.platform.Data()
	O(d["binding"])["accessTokenExpiresAt"] = time.Unix(0, 0).UTC().Format(time.RFC3339)
	if e := f.platform.persist(d); e != nil {
		t.Fatal(e)
	}
	f.change(func() { f.controls["refreshDrop"] = true })
	if _, e := f.platform.Access(context.Background()); Code(e) != "platform_unreachable" {
		t.Fatalf("refresh %v requests=%s", e, JSON(f.requestList()))
	}
	pending := Clone(O(f.platform.Binding()["pendingRefresh"]))
	if S(pending["key"]) == "" {
		t.Fatal("refresh key not persisted before send")
	}
	restored, e := NewPlatform(f.store, f.config)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	if !reflect.DeepEqual(pending, O(restored.Binding()["pendingRefresh"])) {
		t.Fatal("pending lost on restart")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for range 3 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := restored.Access(context.Background()); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	attempts := []M{}
	for _, r := range f.requestList() {
		if strings.HasSuffix(S(r["path"]), "/sessions/refresh") {
			attempts = append(attempts, r)
		}
	}
	if len(attempts) != 2 || O(attempts[0]["headers"])["idempotency-key"] != O(attempts[1]["headers"])["idempotency-key"] || O(attempts[0]["body"])["refreshToken"] != O(attempts[1]["body"])["refreshToken"] {
		t.Fatalf("refresh replay: %s", JSON(attempts))
	}
	if restored.Binding()["pendingRefresh"] != nil || restored.Binding()["accessToken"] != "synthetic-rotated-access" {
		t.Fatal(restored.Binding())
	}
	reload, e := NewPlatform(f.store, f.config)
	if e != nil {
		t.Fatal(e)
	}
	defer reload.Close()
	if reload.Binding()["accessToken"] != "synthetic-rotated-access" {
		t.Fatal("rotated credential not committed")
	}
}
func TestPlatformOriginIdentity(t *testing.T) {
	f := newPlatformFixture(t, 5)
	f.enroll()
	before := f.platform.Binding()
	var mu sync.Mutex
	requests := []M{}
	mismatch := true
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, M{"path": r.URL.Path, "authorization": r.Header.Get("Authorization")})
		bad := mismatch
		mu.Unlock()
		req, _ := http.NewRequestWithContext(r.Context(), r.Method, f.server.URL+r.URL.RequestURI(), r.Body)
		req.Header = r.Header.Clone()
		response, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Error(e)
			return
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		if bad && r.URL.Path == "/.well-known/measix" {
			v, _ := DecodeJSON(raw, 4<<20)
			O(v)["deploymentId"] = "dep_00000000-0000-4000-8000-000000000099"
			raw = JSON(v)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", response.Header.Get("ETag"))
		w.WriteHeader(response.StatusCode)
		w.Write(raw)
	}))
	defer proxy.Close()
	assertStatus(t, f.platform.ChangeOrigin(context.Background(), proxy.URL), 422)
	if !reflect.DeepEqual(before, f.platform.Binding()) {
		t.Fatal("mismatch altered binding")
	}
	mu.Lock()
	if len(requests) != 1 || requests[0]["authorization"] != "" {
		t.Error("credentials sent before identity verification")
	}
	mismatch = false
	mu.Unlock()
	assertStatus(t, f.platform.ChangeOrigin(context.Background(), proxy.URL+"/path"), 400)
	if e := f.platform.ChangeOrigin(context.Background(), " "+proxy.URL+"/ "); e != nil {
		t.Fatal(e)
	}
	for _, k := range []string{"userId", "deviceId", "sessionId", "accessToken", "refreshToken"} {
		if f.platform.Binding()[k] != before[k] {
			t.Fatal("origin changed identity", k)
		}
	}
	restored, e := NewPlatform(f.store, f.config)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	if restored.Binding()["origin"] != proxy.URL {
		t.Fatal("origin not persisted")
	}
	f.change(func() { f.controls["badResponse"] = true; f.controls["etag"] = "\"changed\"" })
	if e := f.platform.ChangeOrigin(context.Background(), f.server.URL); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(f.platform.Error(), "连接地址已保存，但同步未完成") {
		t.Fatal(f.platform.Error())
	}
}
func TestPlatformJSONOriginAndErrors(t *testing.T) {
	for _, text := range []string{`{"code":"a","code":"b"}`, `{"x":{"code":1,"c\u006fde":2}}`, strings.Repeat("[", 130) + "0" + strings.Repeat("]", 130)} {
		if _, e := DecodeJSON([]byte(text), 10<<20); e == nil {
			t.Fatal("accepted malformed JSON")
		}
	}
	large := JSON(M{"messages": []any{M{"content": []any{M{"type": "image_url", "image_url": M{"url": "data:image/png;base64," + strings.Repeat("A", 8<<20)}}}}}, "escaped": "\\\"\n", "nested": []any{true, nil, -1.5, M{}, []any{}}})
	if _, e := DecodeJSON(large, 10<<20); e != nil {
		t.Fatal(e)
	}
	if _, e := DecodeJSON(large, 4<<20); e == nil {
		t.Fatal("unbounded JSON")
	}
	if _, e := DecodeJSON(append(append([]byte(`{"messages":[`), large...), []byte(`],"x":1,"\u0078":2}`)...), 10<<20); e == nil {
		t.Fatal("nested duplicate accepted")
	}
	origin, e := NormalizeOrigin("HTTP://127.0.0.1:80/")
	if e != nil || origin != "http://127.0.0.1" {
		t.Fatalf("canonical origin %q %v", origin, e)
	}
	for _, origin := range []string{"http://a.example/path", "http://user:password@a.example", "http://a.example/?code=x", "http://a.example/#x", "http://evil_host", "http://a.example.", "http://中文.example"} {
		if _, e := NormalizeOrigin(origin); e == nil {
			t.Fatal("untrusted origin", origin)
		}
	}
	f := newPlatformFixture(t, 5)
	status := 409
	var code any = "refresh_conflict"
	malformed := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if malformed {
			w.WriteHeader(status)
			w.Write([]byte("<h1>upstream diagnostic</h1>"))
			return
		}
		WriteJSON(w, status, M{"code": code, "message": "unsafe upstream diagnostic"})
	}))
	defer upstream.Close()
	cases := []struct {
		status        int
		code, message string
	}{{409, "refresh_conflict", "企业会话续期冲突，请重新接入企业账号"}, {403, "user_disabled", "企业账号已停用，请联系管理员"}, {403, "device_revoked", "此设备的企业访问已撤销，请联系管理员重新接入"}, {403, "session_revoked", "企业会话已撤销，请重新接入企业账号"}, {401, "enterprise_identity_deleted", "企业身份已删除，请联系管理员"}, {401, "invalid_credential", "企业凭据已失效，请重新接入企业账号"}, {401, "session_expired", "企业会话已过期，请重新接入企业账号"}, {422, "applied_release_mismatch", "企业配置不一致，请重新同步配置"}, {409, "applied_generation_regression", "企业配置已更新，请重新同步配置"}, {404, "snapshot_not_found", "企业配置已不可用，请同步配置或联系管理员"}, {409, "unknown_conflict", "当前状态有冲突，请刷新后重试"}, {401, "unknown_auth", "企业会话不可用，请重新接入企业账号"}, {403, "unknown_permission", "当前企业账号无权执行此操作，请联系管理员"}, {429, "unknown_limit", "请求过于频繁，请稍后重试"}, {502, "unknown_service", "企业服务暂时不可用，请稍后重试"}, {409, "constructor", "当前状态有冲突，请刷新后重试"}}
	for _, tc := range cases {
		status = tc.status
		code = tc.code
		_, e := f.platform.HTTP(context.Background(), upstream.URL, "/failure", "GET", nil, "", nil)
		if Status(e) != status || Code(e) != tc.code || e.Error() != tc.message {
			t.Fatalf("%s: %v", tc.code, e)
		}
	}
	status = 503
	malformed = true
	_, e = f.platform.HTTP(context.Background(), upstream.URL, "/failure", "GET", nil, "", nil)
	if Status(e) != 503 || Code(e) != "platform_error" {
		t.Fatal(e)
	}
	malformed = false
	status = 409
	code = M{"unexpected": "object"}
	_, e = f.platform.HTTP(context.Background(), upstream.URL, "/failure", "GET", nil, "", nil)
	if Code(e) != "platform_error" {
		t.Fatal(e)
	}
}
func TestPlatformResponsesAndReasoningErrors(t *testing.T) {
	f := newPlatformFixture(t, 5)
	f.enroll()
	models := f.models.Catalog()
	var model M
	for _, v := range models {
		if S(v["protocol"]) == "OPENAI_RESPONSES" {
			model = v
		}
	}
	body := M{"model": model["key"], "input": []any{M{"role": "user", "content": "test"}}, "stream": false, "store": true}
	status, _, _ := f.send("/v1/responses", body)
	if status != 422 {
		t.Fatal(status)
	}
	body["store"] = false
	body["previous_response_id"] = "resp-old"
	status, _, _ = f.send("/v1/responses", body)
	if status != 422 {
		t.Fatal(status)
	}
	delete(body, "previous_response_id")
	status, _, raw := f.send("/v1/responses", body)
	if status != 200 {
		t.Fatal(status, raw)
	}
	count := 0
	for _, r := range f.requestList() {
		if strings.HasPrefix(S(r["path"]), "/runtime/") {
			count++
			if S(r["path"]) != "/runtime/v1/resources/"+S(model["modelId"])+S(model["runtimePath"]) || O(r["body"])["model"] != "public-responses-alias" || O(r["body"])["store"] != false {
				t.Fatal(r)
			}
		}
	}
	if count != 1 {
		t.Fatal(count)
	}
	d := f.platform.Data()
	for _, v := range A(O(d["binding"])["snapshot"].(M)["models"]) {
		O(v)["capabilities"] = append(A(O(v)["capabilities"]), "REASONING")
	}
	if e := f.platform.persist(d); e != nil {
		t.Fatal(e)
	}
	if e := f.models.Rebuild(); e != nil {
		t.Fatal(e)
	}
	f.change(func() {
		f.controls["runtimeRejection"] = M{"status": 422, "body": M{"error": M{"code": "unsupported_parameter", "param": "reasoning.effort"}}}
	})
	for _, m := range f.models.Catalog() {
		for _, effort := range []string{"default", "low", "medium", "high"} {
			key := S(m["key"])
			if effort != "default" {
				key += "--reasoning-" + effort
			}
			path := "/v1/chat/completions"
			if S(m["protocol"]) == "OPENAI_RESPONSES" {
				path = "/v1/responses"
			}
			before := len(f.requestList())
			status, v, raw := f.send(path, M{"model": key, "messages": []any{}, "input": []any{}})
			if status != 422 || O(v["error"])["code"] != "reasoning_effort_rejected" || !strings.Contains(raw, "（"+effort+"）") || !strings.Contains(raw, "未自动降级") {
				t.Fatal(status, raw)
			}
			calls := 0
			for _, r := range f.requestList()[before:] {
				if strings.HasPrefix(S(r["path"]), "/runtime/") {
					calls++
				}
			}
			if calls != 1 {
				t.Fatal("model request retried", calls)
			}
		}
	}
}
func TestModelErrorDetail(t *testing.T) {
	_, code := ModelRequestError(400, M{"error": M{"message": "Unsupported thinking level"}}, "low", "M", nil)
	if code != "reasoning_effort_rejected" {
		t.Fatal(code)
	}
	for _, status := range []int{401, 403, 429, 500} {
		_, code = ModelRequestError(status, M{"error": M{"message": "reasoning service unavailable"}}, "high", "M", nil)
		if code == "reasoning_effort_rejected" {
			t.Fatal(status)
		}
	}
	_, code = ModelRequestError(400, M{"error": M{"param": "temperature"}}, "medium", "M", nil)
	if code == "reasoning_effort_rejected" {
		t.Fatal(code)
	}
	message, code := ModelRequestError(400, M{"error": M{"code": "invalid_request_error", "message": "Missing reasoning_content in assistant message; key=fixture-secret Bearer opaque-token data:image/png;base64,ABCD"}}, "default", "M", []string{"fixture-secret"})
	if code != "invalid_request_error" || !strings.Contains(message, "Missing reasoning_content") || strings.Contains(message, "fixture-secret") || strings.Contains(message, "opaque-token") || strings.Contains(message, "ABCD") {
		t.Fatal(message, code)
	}
	message, _ = ModelRequestError(400, M{"error": M{"message": strings.Repeat("x", 5000)}}, "default", "M", nil)
	if len([]rune(message)) >= 900 {
		t.Fatal("unbounded model diagnostic")
	}
}
