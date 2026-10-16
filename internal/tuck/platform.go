package tuck

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed contract/client-schemas.json
var platformSchemas []byte
var schemaOnce sync.Once
var schemaError error
var schemas map[string]*jsonschema.Schema

func Check(name string, value any) error {
	schemaOnce.Do(func() {
		v, e := jsonschema.UnmarshalJSON(bytes.NewReader(platformSchemas))
		if e != nil {
			schemaError = e
			return
		}
		compiler := jsonschema.NewCompiler()
		compiler.AssertFormat()
		if e = compiler.AddResource("https://tuck.invalid/platform.json", v); e != nil {
			schemaError = e
			return
		}
		schemas = map[string]*jsonschema.Schema{}
		for key := range O(O(v)["definitions"]) {
			compiled, err := compiler.Compile("https://tuck.invalid/platform.json#/definitions/" + key)
			if err != nil {
				schemaError = err
				return
			}
			schemas[key] = compiled
		}
	})
	if schemaError != nil {
		return schemaError
	}
	schema := schemas[name]
	if schema == nil {
		return fmt.Errorf("unknown schema %s", name)
	}
	if e := schema.Validate(value); e != nil {
		return Problem(422, "invalid_platform_data", "平台数据不符合合同："+name)
	}
	return nil
}

type denySchemaLoader struct{}

func (denySchemaLoader) Load(string) (any, error) {
	return nil, fmt.Errorf("external schema references are disabled")
}
func ValidateSchema(schema M, value any) error {
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	compiler.UseLoader(denySchemaLoader{})
	if err := compiler.AddResource("https://tuck.invalid/question.json", schema); err != nil {
		return err
	}
	s, e := compiler.Compile("https://tuck.invalid/question.json")
	if e != nil {
		return e
	}
	return s.Validate(value)
}

func NormalizeOrigin(input string) (string, error) {
	u, e := url.Parse(strings.TrimSpace(input))
	if e != nil || !validURL(strings.TrimSpace(input), false) || u.RawPath != "" || (u.Path != "" && u.Path != "/") {
		return "", Problem(400, "invalid_origin", "平台地址必须是不含路径、凭据或查询参数的 HTTP/HTTPS origin")
	}
	for _, c := range u.Host {
		if c > 127 {
			return "", Problem(400, "invalid_origin", "平台主机名无效")
		}
	}
	if !strings.HasPrefix(u.Host, "[") {
		if len(u.Hostname()) > 253 {
			return "", Problem(400, "invalid_origin", "平台主机名无效")
		}
		for _, label := range strings.Split(u.Hostname(), ".") {
			if !regexp.MustCompile(`(?i)^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`).MatchString(label) {
				return "", Problem(400, "invalid_origin", "平台主机名无效")
			}
		}
	}
	host := strings.ToLower(u.Host)
	if u.Scheme == "http" && u.Port() == "80" || u.Scheme == "https" && u.Port() == "443" {
		host = strings.ToLower(u.Hostname())
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
	}
	return strings.ToLower(u.Scheme) + "://" + host, nil
}
func PathOnly(path string) (string, error) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#\\\r\n\x00") {
		return "", Problem(422, "invalid_platform_data", "平台 API 路径无效")
	}
	for _, p := range strings.Split(path, "/") {
		decoded, e := url.PathUnescape(p)
		if e != nil || decoded == "." || decoded == ".." {
			return "", Problem(422, "invalid_platform_data", "平台 API 路径无效")
		}
	}
	return strings.TrimSuffix(path, "/"), nil
}

var platformMessages = map[string]string{"enrollment_expired": "接入码已过期，请重新获取接入码", "enrollment_already_used": "接入码已使用，请重新获取接入码", "installation_user_conflict": "此安装已绑定其他企业用户，请联系管理员", "user_disabled": "企业账号已停用，请联系管理员", "device_revoked": "此设备的企业访问已撤销，请联系管理员重新接入", "session_revoked": "企业会话已撤销，请重新接入企业账号", "enterprise_identity_deleted": "企业身份已删除，请联系管理员", "invalid_credential": "企业凭据已失效，请重新接入企业账号", "session_expired": "企业会话已过期，请重新接入企业账号", "refresh_conflict": "企业会话续期冲突，请重新接入企业账号", "managed_snapshot_required": "企业配置已变化，请同步配置后继续", "applied_release_mismatch": "企业配置不一致，请重新同步配置", "applied_generation_regression": "企业配置已更新，请重新同步配置", "snapshot_not_found": "企业配置已不可用，请同步配置或联系管理员", "platform_unreachable": "无法连接企业服务，请检查网络后重试", "file_not_found": "文件已不存在，请刷新目录", "file_conflict": "目标名称已存在，请更换名称或刷新目录", "file_version_conflict": "文件已变化，请重新读取后再保存", "file_locked": "文件正被占用，请稍后重试", "file_listing_limit": "目录内容过多，请拆分目录后重试", "file_storage_full": "企业工作区空间不足，请清理文件或联系管理员", "file_transfer_limit": "文件传输过多，请等待当前传输完成", "file_range_invalid": "文件读取范围已无效，请重新打开文件", "workspace_space_mismatch": "企业工作区已变化，请刷新后重新打开", "workspace_revision_conflict": "企业工作区状态已变化，请刷新后重试", "workspace_operation_pending": "企业工作区正在处理其他操作，请稍后刷新", "workspace_not_found": "企业工作区已不存在，请联系管理员", "workspace_unavailable": "企业工作区暂不可用，请稍后刷新或联系管理员", "dav_credential_unavailable": "企业文件服务凭据不可用，请联系管理员", "workspace_result_unknown": "操作结果未知，请刷新核对后再操作", "invalid_file_path": "文件路径无效，请重新选择文件", "invalid_file_destination": "目标路径无效，请重新选择目录", "file_condition_required": "缺少文件版本，请刷新后重试", "invalid_file_condition": "文件版本条件无效，请刷新后重试", "recursive_confirmation_required": "删除目录前请确认包含的文件", "directory_overwrite_forbidden": "不能覆盖整个目录，请更换目标名称"}

func PlatformHTTPError(status int, code string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`).MatchString(code) {
		code = "platform_error"
	}
	message := platformMessages[code]
	if message == "" {
		message = map[int]string{400: "请求内容无效，请检查后重试", 401: "企业会话不可用，请重新接入企业账号", 403: "当前企业账号无权执行此操作，请联系管理员", 404: "请求的资源已不存在，请刷新后重试", 408: "请求超时，请稍后重试", 409: "当前状态有冲突，请刷新后重试", 412: "文件状态已变化，请刷新后重试", 413: "内容超过服务限制，请减小文件后重试", 416: "读取范围无效，请重新打开文件", 422: "请求内容不符合服务要求，请检查后重试", 428: "企业配置需要更新，请同步配置后重试", 429: "请求过于频繁，请稍后重试"}[status]
	}
	if message == "" {
		if status >= 500 {
			message = "企业服务暂时不可用，请稍后重试"
		} else {
			message = fmt.Sprintf("请求未完成，请稍后重试（HTTP %d）", status)
		}
	}
	return Problem(status, code, message)
}

// Platform owns only enterprise credentials and public control-plane calls.
type Platform struct {
	Store             *Store
	Config            *Config
	Client            *http.Client
	mu                sync.RWMutex
	refreshMu, syncMu sync.Mutex
	data, state       M
	lastError         string
	ctx               context.Context
	cancel            context.CancelFunc
}

func NewPlatform(store *Store, config *Config) (*Platform, error) {
	ctx, cancel := context.WithCancel(context.Background())
	p := &Platform{Store: store, Config: config, Client: &http.Client{Transport: newTransport(15 * time.Second), CheckRedirect: noRedirect}, ctx: ctx, cancel: cancel}
	var installation string
	var encrypted []byte
	var version int
	e := store.DB.QueryRow("SELECT installation_id,encrypted_payload,format_version FROM platform_session WHERE id=1").Scan(&installation, &encrypted, &version)
	if errors.Is(e, sql.ErrNoRows) {
		p.data = M{"installationId": "ins_" + UUID(), "binding": nil}
		if e = p.persist(p.data); e != nil {
			cancel()
			return nil, e
		}
	} else if e != nil {
		cancel()
		return nil, e
	} else {
		if version != platformFormatVersion {
			cancel()
			return nil, Problem(409, "platform_version", "平台持久格式不受支持")
		}
		_, value, e := decryptPlatformState(config.Applied(), installation, version, encrypted)
		if e != nil {
			cancel()
			return nil, e
		}
		p.data = value
	}
	return p, nil
}
func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
func newTransport(headerTimeout time.Duration) *http.Transport {
	return &http.Transport{Proxy: http.ProxyFromEnvironment, MaxIdleConns: 64, MaxIdleConnsPerHost: 16, IdleConnTimeout: 90 * time.Second, ResponseHeaderTimeout: headerTimeout, ForceAttemptHTTP2: true}
}

const platformFormatVersion = 2

func decryptPlatformState(config M, installation string, version int, encrypted []byte) ([]byte, M, error) {
	plain, err := platformCrypt(config, installation, version, encrypted, true)
	if err != nil {
		return nil, nil, Problem(409, "credential_key", "平台凭据无法解密，请恢复匹配的配置与数据库备份")
	}
	v, err := DecodeJSON(plain, 4<<20)
	_, bindingObject := O(v)["binding"].(M)
	if err != nil || installation == "" || S(O(v)["installationId"]) != installation || O(v)["binding"] != nil && !bindingObject {
		return nil, nil, Problem(409, "credential_identity", "平台持久身份或内容不一致")
	}
	return plain, O(v), nil
}

// v1 used a constant AAD. v2 authenticates both the format and installation
// column; moving ciphertext between installations no longer authenticates.
// Only the offline migration reader accepts v1. Runtime always writes v2.
func platformCrypt(config M, installation string, version int, raw []byte, decrypt bool) ([]byte, error) {
	if version != 1 && version != platformFormatVersion {
		return nil, Problem(409, "platform_version", "平台持久格式不受支持")
	}
	key, e := base64.StdEncoding.DecodeString(S(O(config["security"])["credentialKey"]))
	if e != nil || len(key) != 32 {
		return nil, Problem(409, "credential_key", "平台凭据加密密钥缺失或无效")
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	gcm, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	aad := []byte("Measix Tuck platform v1")
	if version == platformFormatVersion {
		aad = JSON([]any{"Measix Tuck platform", version, installation})
	}
	if decrypt {
		if len(raw) < gcm.NonceSize()+gcm.Overhead() {
			return nil, fmt.Errorf("short encrypted state")
		}
		return gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], aad)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	return gcm.Seal(nonce, nonce, raw, aad), nil
}
func (p *Platform) persist(value M) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.persistLocked(value)
}
func (p *Platform) mutate(fn func(M) error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	value := Clone(p.data)
	if err := fn(value); err != nil {
		return err
	}
	return p.persistLocked(value)
}
func (p *Platform) persistLocked(value M) error {
	value = Clone(value)
	plain := JSON(value)
	if len(plain) > 4<<20 || S(value["installationId"]) == "" {
		return Problem(422, "platform_state", "平台持久内容超限或身份缺失")
	}
	encrypted, e := platformCrypt(p.Config.Applied(), S(value["installationId"]), platformFormatVersion, plain, false)
	if e != nil {
		return e
	}
	if e = p.Store.Write(func(tx *sql.Tx) error {
		_, e := tx.Exec("INSERT INTO platform_session(id,installation_id,encrypted_payload,format_version) VALUES(1,?,?,?) ON CONFLICT(id) DO UPDATE SET installation_id=excluded.installation_id,encrypted_payload=excluded.encrypted_payload,format_version=excluded.format_version", S(value["installationId"]), encrypted, platformFormatVersion)
		return e
	}); e != nil {
		return e
	}
	p.data = value
	return nil
}
func (p *Platform) Data() M { p.mu.RLock(); defer p.mu.RUnlock(); return Clone(p.data) }
func (p *Platform) Binding() M {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.data["binding"] == nil {
		return nil
	}
	return Clone(O(p.data["binding"]))
}
func (p *Platform) State() M      { p.mu.RLock(); defer p.mu.RUnlock(); return Clone(p.state) }
func (p *Platform) Error() string { p.mu.RLock(); defer p.mu.RUnlock(); return p.lastError }
func (p *Platform) setError(e error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastError = ""
	if e != nil {
		p.lastError = e.Error()
	}
}
func (p *Platform) HTTP(ctx context.Context, origin, path, method string, body any, token string, headers http.Header) (*http.Response, error) {
	path, e := PathOnly(path)
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	stop := context.AfterFunc(p.ctx, cancel)
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(JSON(body))
	}
	req, e := http.NewRequestWithContext(ctx, method, origin+path, r)
	if e != nil {
		cancel()
		stop()
		return nil, e
	}
	// Idempotency-Key is the persisted recovery identity, not permission for
	// net/http to replay a POST after a lost response. The next explicit caller
	// reuses the durable key; a single operation makes one attempt.
	req.GetBody = nil
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header[k] = v
	}
	response, e := p.Client.Do(req)
	if e != nil {
		cancel()
		stop()
		return nil, Problem(502, "platform_unreachable", "平台网络请求失败，请检查连接地址")
	}
	response.Body = &cancelBody{ReadCloser: response.Body, cancel: func() { stop(); cancel() }}
	if response.StatusCode < 200 || response.StatusCode >= 300 && response.StatusCode != 304 {
		data, _ := ReadJSON(response.Body, 4<<20)
		response.Body.Close()
		return nil, PlatformHTTPError(response.StatusCode, S(data["code"]))
	}
	return response, nil
}

type cancelBody struct {
	io.ReadCloser
	cancel func()
}

func (b *cancelBody) Close() error { e := b.ReadCloser.Close(); b.cancel(); return e }
func responseJSON(response *http.Response, schema string) (M, error) {
	defer response.Body.Close()
	v, e := ReadJSON(response.Body, 4<<20)
	if e != nil {
		return nil, e
	}
	if schema != "" {
		if e = Check(schema, v); e != nil {
			return nil, e
		}
	}
	return v, nil
}
func (p *Platform) discovery(ctx context.Context, origin string) (M, error) {
	r, e := p.HTTP(ctx, origin, "/.well-known/measix", "GET", nil, "", nil)
	if e != nil {
		return nil, e
	}
	v, e := responseJSON(r, "Discovery")
	if e != nil {
		return nil, e
	}
	if _, e = PathOnly(S(v["clientApiBase"])); e != nil {
		return nil, e
	}
	if _, e = PathOnly(S(v["runtimeApiBase"])); e != nil {
		return nil, e
	}
	supported := false
	for _, n := range A(v["supportedSnapshotSchemaVersions"]) {
		supported = supported || I(n) == 4 || I(n) == 5
	}
	if !supported {
		return nil, Problem(422, "unsupported_snapshot", "平台没有受支持的 Snapshot 版本")
	}
	return v, nil
}
func (p *Platform) Enroll(ctx context.Context, text string) error {
	v, e := DecodeJSON([]byte(text), 16<<10)
	if e != nil {
		return e
	}
	material := O(v)
	if e = Check("PlatformEnrollmentMaterial", material); e != nil {
		return e
	}
	origin, e := NormalizeOrigin(S(material["platformUrl"]))
	if e != nil {
		return e
	}
	expires, e := time.Parse(time.RFC3339Nano, S(material["expiresAt"]))
	if e != nil || !expires.After(time.Now()) {
		return Problem(401, "enrollment_expired", "接入码已过期，请重新获取 JSON")
	}
	discovery, e := p.discovery(ctx, origin)
	if e != nil {
		return e
	}
	data := p.Data()
	response, e := p.HTTP(ctx, origin, S(discovery["clientApiBase"])+"/enrollments/exchange", "POST", M{"code": material["code"], "installationId": data["installationId"], "deviceName": "Measix Tuck", "appVersion": "1.0.0", "platform": "ANDROID"}, "", nil)
	if e != nil {
		return e
	}
	credentials, e := responseJSON(response, "EnrollmentExchangeResponse")
	if e != nil {
		return e
	}
	if credentials["deploymentId"] != discovery["deploymentId"] {
		return Problem(422, "identity_mismatch", "接入响应与 Discovery 身份不一致")
	}
	data["binding"] = Merge(credentials, M{"origin": origin, "discovery": discovery, "snapshot": nil, "pendingRefresh": nil, "appliedReported": false})
	if e = p.persist(data); e != nil {
		return e
	}
	p.setError(nil)
	if e = p.Sync(ctx); e != nil {
		p.setError(e)
	}
	return nil
}
func (p *Platform) Access(ctx context.Context) (string, error) {
	binding := p.Binding()
	if binding == nil {
		return "", Problem(401, "not_enrolled", "请导入平台接入 JSON")
	}
	expires, _ := time.Parse(time.RFC3339Nano, S(binding["accessTokenExpiresAt"]))
	if binding["pendingRefresh"] != nil || !expires.After(time.Now().Add(30*time.Second)) {
		if e := p.Refresh(ctx); e != nil {
			return "", e
		}
	}
	return S(p.Binding()["accessToken"]), nil
}
func (p *Platform) Refresh(ctx context.Context) error {
	p.refreshMu.Lock()
	defer p.refreshMu.Unlock()
	data := p.Data()
	binding := O(data["binding"])
	if len(binding) == 0 {
		return Problem(401, "not_enrolled", "请导入平台接入 JSON")
	}
	expires, _ := time.Parse(time.RFC3339Nano, S(binding["accessTokenExpiresAt"]))
	if binding["pendingRefresh"] == nil && expires.After(time.Now().Add(30*time.Second)) {
		return nil
	}
	if binding["pendingRefresh"] == nil {
		pending := M{"key": "idem_" + UUID(), "refreshToken": binding["refreshToken"]}
		if e := p.mutate(func(current M) error {
			live := O(current["binding"])
			if live["sessionId"] != binding["sessionId"] || live["refreshToken"] != binding["refreshToken"] {
				return Problem(409, "refresh_conflict", "平台会话已变化，请重新执行当前操作")
			}
			live["pendingRefresh"] = pending
			return nil
		}); e != nil {
			return e
		}
		binding["pendingRefresh"] = pending
	}
	pending := O(binding["pendingRefresh"])
	r, e := p.HTTP(ctx, S(binding["origin"]), S(O(binding["discovery"])["clientApiBase"])+"/sessions/refresh", "POST", M{"refreshToken": pending["refreshToken"]}, "", http.Header{"Idempotency-Key": []string{S(pending["key"])}})
	if e != nil {
		return e
	}
	credentials, e := responseJSON(r, "RefreshResponse")
	if e != nil {
		return e
	}
	return p.mutate(func(current M) error {
		live := O(current["binding"])
		if live["sessionId"] != binding["sessionId"] || O(live["pendingRefresh"])["key"] != pending["key"] {
			return Problem(409, "refresh_conflict", "平台会话已变化，请重新执行当前操作")
		}
		current["binding"] = Merge(live, credentials, M{"pendingRefresh": nil})
		return nil
	})
}
func (p *Platform) ClientCall(ctx context.Context, path, method string, body any, headers http.Header) (*http.Response, error) {
	token, e := p.Access(ctx)
	if e != nil {
		p.setError(e)
		return nil, e
	}
	b := p.Binding()
	r, e := p.HTTP(ctx, S(b["origin"]), S(O(b["discovery"])["clientApiBase"])+path, method, body, token, headers)
	if e != nil {
		p.setError(e)
	}
	return r, e
}
func (p *Platform) ManagedState(ctx context.Context) (M, error) {
	b := p.Binding()
	headers := http.Header{}
	if snapshot := O(b["snapshot"]); len(snapshot) > 0 {
		headers.Set("X-Measix-Applied-Managed-Generation", fmt.Sprint(I(snapshot["managedGeneration"])))
	}
	r, e := p.ClientCall(ctx, "/managed/state", "GET", nil, headers)
	if e != nil {
		return nil, e
	}
	state, e := responseJSON(r, "ManagedState")
	if e == nil {
		p.mu.Lock()
		p.state = state
		p.mu.Unlock()
	}
	return state, e
}
func snapshotClosure(snapshot M) error {
	maps := map[string]map[string]M{}
	groups := map[string]string{"providers": "providerId", "models": "modelId", "tts": "ttsId", "asr": "asrId", "mcp": "mcpServerId", "assistants": "assistantDefinitionId", "starters": "starterId", "imageGenerators": "imageId"}
	for group, key := range groups {
		entries := map[string]M{}
		for _, v := range A(snapshot[group]) {
			m := O(v)
			id := S(m[key])
			if entries[id] != nil {
				return Problem(422, "invalid_platform_data", "平台目录存在重复 ID")
			}
			entries[id] = m
		}
		maps[group] = entries
	}
	ref := func(group, id string, enabled bool) error {
		item := maps[group][id]
		if item == nil || enabled && !B(item["enabled"]) {
			return Problem(422, "invalid_platform_data", "平台资源引用无效")
		}
		return nil
	}
	for _, v := range A(snapshot["models"]) {
		m := O(v)
		if e := ref("providers", S(m["providerId"]), B(m["enabled"])); e != nil {
			return e
		}
		if _, e := PathOnly(S(m["runtimePath"])); e != nil {
			return e
		}
	}
	for _, v := range A(snapshot["imageGenerators"]) {
		if _, e := PathOnly(S(O(v)["runtimePath"])); e != nil {
			return e
		}
	}
	for _, v := range A(snapshot["mcp"]) {
		m := O(v)
		if _, e := PathOnly(S(m["runtimePath"])); e != nil {
			return e
		}
		if I(snapshot["schemaVersion"]) == 5 {
			names := map[string]bool{}
			for _, g := range A(m["allowedTools"]) {
				name := S(O(g)["name"])
				if names[name] {
					return Problem(422, "invalid_platform_data", "MCP 工具许可名称重复")
				}
				names[name] = true
			}
			if S(m["toolAccessMode"]) == "ALL" && len(names) > 0 || S(m["toolAccessMode"]) == "ALLOWLIST" && len(names) == 0 {
				return Problem(422, "invalid_platform_data", "MCP 工具许可模式与列表不一致")
			}
		}
	}
	for key, id := range O(snapshot["policy"]) {
		if !strings.HasPrefix(key, "default") {
			continue
		}
		group := map[string]string{"defaultAssistantId": "assistants", "defaultImageGenerationId": "imageGenerators", "defaultTtsId": "tts", "defaultAsrId": "asr"}[key]
		if group == "" {
			group = "models"
		}
		if e := ref(group, S(id), true); e != nil {
			return e
		}
		if key == "defaultAttachmentInspectionModelId" && !Contains(maps[group][S(id)]["inputModalities"], "IMAGE") {
			return Problem(422, "invalid_platform_data", "附件检查模型未声明 IMAGE 输入")
		}
	}
	for _, v := range A(snapshot["assistants"]) {
		a := O(v)
		if e := ref("models", S(a["modelId"]), B(a["enabled"])); e != nil {
			return e
		}
		if I(snapshot["schemaVersion"]) == 4 {
			for _, id := range A(a["mcpServerIds"]) {
				if e := ref("mcp", S(id), B(a["enabled"])); e != nil {
					return e
				}
			}
		} else {
			for _, binding := range A(a["mcpBindings"]) {
				if e := ref("mcp", S(O(binding)["mcpServerId"]), B(a["enabled"])); e != nil {
					return e
				}
			}
		}
	}
	for _, v := range A(snapshot["starters"]) {
		m := O(v)
		if e := ref("assistants", S(m["assistantDefinitionId"]), B(m["enabled"])); e != nil {
			return e
		}
	}
	return nil
}
func (p *Platform) Sync(ctx context.Context) (err error) {
	p.syncMu.Lock()
	defer p.syncMu.Unlock()
	defer func() { p.setError(err) }()
	r, e := p.ClientCall(ctx, "/bootstrap", "GET", nil, nil)
	if e != nil {
		return e
	}
	bootstrap, e := responseJSON(r, "Bootstrap")
	if e != nil {
		return e
	}
	binding := p.Binding()
	if O(bootstrap["deployment"])["deploymentId"] != binding["deploymentId"] || O(bootstrap["user"])["userId"] != binding["userId"] || O(bootstrap["device"])["deviceId"] != binding["deviceId"] || O(bootstrap["session"])["sessionId"] != binding["sessionId"] {
		return Problem(422, "identity_mismatch", "平台会话身份不一致")
	}
	if e = p.mutate(func(data M) error {
		O(data["binding"])["employeeName"] = O(bootstrap["user"])["displayName"]
		return nil
	}); e != nil {
		return e
	}
	state, e := p.ManagedState(ctx)
	if e != nil {
		return e
	}
	generation := I(state["targetManagedGeneration"])
	if generation == 0 {
		generation = I(state["activeManagedGeneration"])
	}
	if generation == 0 {
		return Problem(409, "snapshot_not_found", "平台尚未发布配置")
	}
	binding = p.Binding()
	cached := O(binding["snapshot"])
	if I(cached["managedGeneration"]) != generation {
		cached = nil
	}
	headers := http.Header{}
	if cached != nil {
		headers.Set("If-None-Match", "\""+S(cached["snapshotHash"])+"\"")
	}
	r, e = p.ClientCall(ctx, fmt.Sprintf("/managed/snapshots/%d", generation), "GET", nil, headers)
	if e != nil {
		return e
	}
	snapshot := cached
	if r.StatusCode == 304 {
		r.Body.Close()
		if cached == nil || r.Header.Get("ETag") != "\""+S(cached["snapshotHash"])+"\"" {
			return Problem(422, "invalid_platform_data", "平台返回 304，但缓存或 ETag 不匹配")
		}
	} else {
		etag := r.Header.Get("ETag")
		snapshot, e = responseJSON(r, "")
		if e != nil {
			return e
		}
		schema := "ManagedSnapshot"
		if I(snapshot["schemaVersion"]) == 4 {
			schema = "ManagedSnapshotV4"
		} else if I(snapshot["schemaVersion"]) != 5 {
			return Problem(422, "unsupported_snapshot", "平台 Snapshot 版本不兼容")
		}
		if e = Check(schema, snapshot); e != nil {
			return e
		}
		if snapshot["deploymentId"] != binding["deploymentId"] || I(snapshot["managedGeneration"]) != generation || etag != "\""+S(snapshot["snapshotHash"])+"\"" {
			return Problem(422, "invalid_platform_data", "平台 Snapshot 身份、generation 或 ETag 不一致")
		}
		if e = snapshotClosure(snapshot); e != nil {
			return e
		}
		if e = p.mutate(func(data M) error {
			O(data["binding"])["snapshot"] = snapshot
			O(data["binding"])["appliedReported"] = false
			return nil
		}); e != nil {
			return e
		}
	}
	if !B(p.Binding()["appliedReported"]) {
		r, e = p.ClientCall(ctx, "/managed/applied", "PUT", M{"managedGeneration": snapshot["managedGeneration"], "snapshotHash": snapshot["snapshotHash"]}, nil)
		if e != nil {
			return e
		}
		r.Body.Close()
		if e = p.mutate(func(data M) error { O(data["binding"])["appliedReported"] = true; return nil }); e != nil {
			return e
		}
	}
	_, e = p.ManagedState(ctx)
	return e
}
func (p *Platform) Admit(ctx context.Context, generation int) error {
	state, e := p.ManagedState(ctx)
	if e != nil {
		return e
	}
	if B(state["runtimeBlocked"]) || B(state["syncRequired"]) || S(state["runtimeStatus"]) != "READY" || I(state["activeManagedGeneration"]) != generation {
		return Problem(428, "managed_snapshot_required", "平台配置未就绪或已变化，请同步后继续")
	}
	return nil
}
func (p *Platform) ChangeOrigin(ctx context.Context, input string) error {
	binding := p.Binding()
	if binding == nil {
		return Problem(409, "not_enrolled", "请先接入企业平台")
	}
	origin, e := NormalizeOrigin(input)
	if e != nil {
		return e
	}
	discovery, e := p.discovery(ctx, origin)
	if e != nil {
		return e
	}
	if discovery["deploymentId"] != binding["deploymentId"] {
		return Problem(422, "identity_mismatch", "新地址不属于当前企业平台；如需切换企业，请使用更换平台接入")
	}
	p.syncMu.Lock()
	p.refreshMu.Lock()
	data := p.Data()
	O(data["binding"])["origin"] = origin
	O(data["binding"])["discovery"] = discovery
	e = p.persist(data)
	p.refreshMu.Unlock()
	p.syncMu.Unlock()
	if e != nil {
		return e
	}
	if e = p.Sync(ctx); e != nil {
		p.setError(Problem(Status(e), Code(e), "连接地址已保存，但同步未完成："+e.Error()))
	}
	return nil
}
func (p *Platform) Logout(ctx context.Context) error {
	b := p.Binding()
	if b != nil {
		r, e := p.HTTP(ctx, S(b["origin"]), S(O(b["discovery"])["clientApiBase"])+"/sessions/logout", "POST", M{"refreshToken": b["refreshToken"]}, "", nil)
		if e != nil {
			return e
		}
		r.Body.Close()
	}
	data := p.Data()
	data["binding"] = nil
	if e := p.persist(data); e != nil {
		return e
	}
	p.mu.Lock()
	p.state = nil
	p.lastError = ""
	p.mu.Unlock()
	return nil
}
func (p *Platform) Close() { p.cancel(); p.Client.CloseIdleConnections() }
