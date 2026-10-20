package tuck

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

var stableVersion = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
var resourceID = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
var agentID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var passwordPattern = regexp.MustCompile(`^scrypt\$32768\$8\$3\$[a-f0-9]{32}\$[a-f0-9]{64}$`)
var protectable = []string{"platform", "providers", "agents", "mcp", "orchestration", "engine"}
var reasoningEfforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}
var tuckTools = func() []string {
	names := []string{}
	for _, name := range modelContent.ToolNames() {
		if name != "deliver_result" {
			names = append(names, name)
		}
	}
	return names
}()
var runtimeFields = map[string][]string{"providers": {"providers", "defaultModelId", "modelDefaults", "platformModelSettings", "imageGenerators", "defaultImageGenerator", "defaultAttachmentInspectionModelId"}, "agents": {"agents", "workbench"}, "mcp": {"localMcp", "platformMcpTools"}}

const configVersion = 3

type Config struct {
	mu                        sync.RWMutex
	Path                      string
	current, applied          M
	revision, appliedRevision string
	onApplied                 func()
}

func LoadConfig(path string) (*Config, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	v, err := DecodeJSON(raw, 4<<20)
	if err != nil {
		return nil, err
	}
	m := O(v)
	if err = ValidateConfig(m, false); err != nil {
		return nil, err
	}
	c := &Config{Path: abs, current: m, applied: Clone(m), revision: Hash(raw), appliedRevision: Hash(raw)}
	return c, nil
}
func (c *Config) Read() (M, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, err := os.ReadFile(c.Path)
	if err != nil {
		return nil, "", err
	}
	r := Hash(raw)
	if r != c.revision {
		v, e := DecodeJSON(raw, 4<<20)
		if e != nil {
			return nil, "", e
		}
		if e = ValidateConfig(O(v), false); e != nil {
			return nil, "", e
		}
		c.current = O(v)
		c.revision = r
	}
	return Clone(c.current), c.revision, nil
}
func (c *Config) Applied() M { c.mu.RLock(); defer c.mu.RUnlock(); return Clone(c.applied) }
func (c *Config) MarkApplied() {
	c.mu.RLock()
	snapshot, revision := Clone(c.current), c.revision
	c.mu.RUnlock()
	c.MarkAppliedVersion(snapshot, revision)
}

// Mark only the snapshot that actually reached the runtime. Read may have
// discovered a newer external edit while an engine was starting.
func (c *Config) MarkAppliedVersion(snapshot M, revision string) {
	c.mu.Lock()
	c.applied = Clone(snapshot)
	c.appliedRevision = revision
	notify := c.onApplied
	c.mu.Unlock()
	if notify != nil {
		notify()
	}
}
func (c *Config) Pending() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	raw, e := os.ReadFile(c.Path)
	return e != nil || Hash(raw) != c.appliedRevision
}
func (c *Config) Save(revision string, next M) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, err := os.ReadFile(c.Path)
	if err != nil {
		return "", err
	}
	if Hash(raw) != revision {
		return "", Problem(409, "settings_changed", "配置已被其他页面或本机修改，请重新打开运行配置后编辑")
	}
	if err = ValidateConfig(next, true); err != nil {
		return "", err
	}
	b, err := marshalConfig(next)
	if err != nil {
		return "", err
	}
	if err = AtomicWrite(c.Path, b, 0600); err != nil {
		return "", err
	}
	c.current = Clone(next)
	c.revision = Hash(b)
	return c.revision, nil
}
func (c *Config) Restore(ownRevision string, previous M) error {
	_, e := c.Save(ownRevision, previous)
	return e
}
func (c *Config) Resolve(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(filepath.Dir(c.Path), path)
}
func (c *Config) DataDir() string { return c.Resolve(S(O(c.Applied()["storage"])["dataDir"])) }
func (c *Config) WorkspaceRoot() string {
	return c.Resolve(S(O(c.Applied()["storage"])["workspaceRoot"]))
}

// These resources are acquired by NewApp, not rebuilt by runtime settings.
// Never mark a different database root/key/listener/engine identity applied
// while those owners still hold the original resources and process lock.
func (c *Config) RequireRuntimeCompatible(next M) error {
	startup := func(v M) M {
		e, s := O(v["engine"]), O(v["security"])
		return M{"server": v["server"], "storage": v["storage"], "auth": s["auth"], "credentialKey": s["credentialKey"], "engine": M{"mode": e["mode"], "version": e["version"], "binary": e["binary"], "url": e["url"]}}
	}
	if string(JSON(startup(c.Applied()))) != string(JSON(startup(next))) {
		return Problem(409, "restart_required", "监听、存储目录、登录凭据、加密密钥或引擎身份已在文件中改变；请停服并核对配置与数据后重新启动")
	}
	return nil
}
func stringField(v any, min, max int) bool {
	s, ok := v.(string)
	return ok && utf8.RuneCountInString(strings.TrimSpace(s)) >= min && utf8.RuneCountInString(s) <= max && !strings.ContainsRune(s, 0)
}
func noUnknown(v M, allowed ...string) bool {
	for k := range v {
		if !slices.Contains(allowed, k) {
			return false
		}
	}
	return true
}
func isBool(v any) bool { _, ok := v.(bool); return ok }
func isArray(v any) bool {
	_, ok := v.([]any)
	if !ok {
		switch v.(type) {
		case []M, []string:
			ok = true
		}
	}
	return ok
}
func uniqueStrings(v any, allowed func(string) bool) bool {
	if !isArray(v) {
		return false
	}
	seen := map[string]bool{}
	for _, x := range A(v) {
		s, ok := x.(string)
		if !ok || seen[s] || !allowed(s) {
			return false
		}
		seen[s] = true
	}
	return true
}
func validSecret(v any) bool {
	s, ok := v.(string)
	return ok && len(s) <= 16384 && !strings.ContainsAny(s, "\r\n\x00")
}
func validURL(v any, query bool) bool {
	u, e := url.Parse(S(v))
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.Fragment == "" && (query || u.RawQuery == "")
}
func validBudget(v M, defaults M) bool {
	context := v["contextWindow"]
	if context == nil {
		context = defaults["contextWindow"]
	}
	output := v["maxOutputTokens"]
	if output == nil {
		output = defaults["maxOutputTokens"]
	}
	return Integer(context, 1, 10000000) && Integer(output, 1, 10000000) && N(output) < N(context)
}
func ValidateConfig(c M, paths bool) error {
	fail := func(s string) error { return Problem(422, "invalid_config", s) }
	if !noUnknown(c, "schemaVersion", "server", "storage", "engine", "security", "providers", "defaultModelId", "modelDefaults", "platformModelSettings", "agents", "permissions", "localMcp", "platformMcpTools", "workbench", "imageGenerators", "defaultImageGenerator", "defaultAttachmentInspectionModelId") {
		return fail("配置包含未知字段")
	}
	if !Integer(c["schemaVersion"], configVersion, configVersion) {
		return Problem(409, "config_version", "配置版本不受当前程序支持；正式 Go v1/v2 配置请停服后运行 tuck migrate --check")
	}
	server := O(c["server"])
	if !noUnknown(server, "listenHost", "port", "frontendDir") || net.ParseIP(S(server["listenHost"])) == nil || !Integer(server["port"], 0, 65535) || !stringField(server["frontendDir"], 1, 4096) {
		return fail("服务器监听或前端目录无效")
	}
	storage := O(c["storage"])
	if !noUnknown(storage, "dataDir", "workspaceRoot") || !stringField(storage["dataDir"], 1, 4096) || !stringField(storage["workspaceRoot"], 1, 4096) {
		return fail("存储目录无效")
	}
	engine := O(c["engine"])
	if !noUnknown(engine, "mode", "version", "binary", "url", "desktopProxy") || !slices.Contains([]string{"managed", "external"}, S(engine["mode"])) || !stableVersion.MatchString(S(engine["version"])) || !validURL(engine["url"], false) || !isBool(engine["desktopProxy"]) {
		return fail("引擎配置无效")
	}
	if Has(engine, "binary") && !stringField(engine["binary"], 0, 4096) {
		return fail("引擎程序路径无效")
	}
	eu, _ := url.Parse(S(engine["url"]))
	if eu.Path != "" && eu.Path != "/" {
		return fail("引擎地址不能包含路径")
	}
	if S(engine["mode"]) == "managed" && !IsLoopback(eu.Hostname()) {
		return fail("受管引擎必须监听回环地址")
	}
	security := O(c["security"])
	if !noUnknown(security, "auth", "settingsProtection", "credentialKey") {
		return fail("安全配置字段无效")
	}
	auth := O(security["auth"])
	if !noUnknown(auth, "enabled", "username", "passwordHash") || !isBool(auth["enabled"]) || !stringField(auth["username"], 1, 80) || Has(auth, "passwordHash") && (!stringField(auth["passwordHash"], 0, 200) || S(auth["passwordHash"]) != "" && !passwordPattern.MatchString(S(auth["passwordHash"]))) {
		return fail("登录配置无效")
	}
	if B(auth["enabled"]) && !passwordPattern.MatchString(S(auth["passwordHash"])) {
		return fail("启用登录前必须设置有效密码")
	}
	if !IsLoopback(S(server["listenHost"])) && (!B(auth["enabled"]) || S(auth["passwordHash"]) == "") {
		return fail("外部监听需要先设置登录密码")
	}
	{
		key := S(security["credentialKey"])
		b, e := base64.StdEncoding.DecodeString(key)
		if e != nil || len(b) != 32 {
			return fail("平台凭据加密密钥无效")
		}
	}
	protection := O(security["settingsProtection"])
	if !noUnknown(protection, "passwordHash", "categories") || Has(protection, "passwordHash") && (!stringField(protection["passwordHash"], 0, 200) || S(protection["passwordHash"]) != "" && !passwordPattern.MatchString(S(protection["passwordHash"]))) {
		return fail("配置保护格式无效")
	}
	if Has(protection, "categories") && !uniqueStrings(protection["categories"], func(s string) bool { return slices.Contains(protectable, s) }) {
		return fail("保护分类无效")
	}
	defaults := O(c["modelDefaults"])
	if !noUnknown(defaults, "contextWindow", "maxOutputTokens") || !validBudget(defaults, nil) {
		return fail("模型预算无效")
	}
	if Has(c, "defaultModelId") && (S(c["defaultModelId"]) != strings.TrimSpace(S(c["defaultModelId"])) || len(S(c["defaultModelId"])) > 256) {
		return fail("默认模型标识无效")
	}
	if Has(c, "defaultAttachmentInspectionModelId") && (!stringField(c["defaultAttachmentInspectionModelId"], 0, 256) || strings.ContainsAny(S(c["defaultAttachmentInspectionModelId"]), " \t\r\n")) {
		return fail("默认附件识别模型标识无效")
	}
	if e := validateImageConfig(c); e != nil {
		return e
	}
	modelIDs := map[string]bool{}
	providerIDs := map[string]bool{}
	if !isArray(c["providers"]) || len(A(c["providers"])) > 50 {
		return fail("提供商数量无效")
	}
	for _, pval := range A(c["providers"]) {
		p := O(pval)
		id := S(p["id"])
		if !noUnknown(p, "id", "displayName", "enabled", "protocol", "baseUrl", "apiKey", "models") || !resourceID.MatchString(id) || providerIDs[id] || !stringField(p["displayName"], 1, 80) || !isBool(p["enabled"]) || !validURL(p["baseUrl"], false) || !slices.Contains([]string{"OPENAI_CHAT_COMPLETIONS", "OPENAI_RESPONSES"}, S(p["protocol"])) || !validSecret(p["apiKey"]) {
			return fail("提供商标识、地址、协议或凭据无效")
		}
		providerIDs[id] = true
		if !isArray(p["models"]) || len(A(p["models"])) > 100 {
			return fail("每个提供商最多配置 100 个模型")
		}
		for _, mv := range A(p["models"]) {
			m := O(mv)
			id := S(m["modelId"])
			if !stringField(m["modelId"], 1, 256) || strings.ContainsAny(id, " \t\n\r") || modelIDs[id] || !stringField(m["displayName"], 1, 80) || !stringField(m["upstreamModelKey"], 1, 256) || !isBool(m["enabled"]) || !validBudget(m, nil) || !Contains(m["inputModalities"], "TEXT") || !uniqueStrings(m["inputModalities"], func(s string) bool { return s == "TEXT" || s == "IMAGE" }) || !uniqueStrings(m["capabilities"], func(s string) bool { return s == "TOOL" || s == "REASONING" }) {
				return fail("模型身份、预算或能力无效")
			}
			modelIDs[id] = true
			if e := validateModelParameters(m); e != nil {
				return e
			}
			if Has(m, "reasoningEfforts") && (len(A(m["reasoningEfforts"])) == 0 || !uniqueStrings(m["reasoningEfforts"], func(s string) bool { return slices.Contains(reasoningEfforts, s) })) {
				return fail("模型推理配置无效")
			}
		}
	}
	for _, field := range []string{"platformModelSettings", "platformMcpTools"} {
		if value, present := c[field]; present {
			if object, ok := value.(M); !ok || object == nil {
				return fail(field + " 必须是对象")
			}
		}
	}
	for key, v := range O(c["platformModelSettings"]) {
		m := O(v)
		if len(key) == 0 || len(key) > 1024 || !noUnknown(m, "contextWindow", "maxOutputTokens", "temperature", "topP") || !validBudget(m, defaults) {
			return fail("平台模型本地参数无效")
		}
		if e := validateModelParameters(m); e != nil {
			return e
		}
	}
	agents := O(c["agents"])
	for _, required := range []string{"root", "worker", "reviewer"} {
		if !Has(agents, required) {
			return fail("需要 root、worker、reviewer Agent")
		}
	}
	for name, v := range agents {
		a := O(v)
		if !agentID.MatchString(name) || !isArray(a["toolsets"]) || !stringField(a["instruction"], 0, 1000000) || !Integer(a["max_iterations"], 0, 9007199254740991) || Has(a, "model") {
			return fail("Agent 配置无效：" + name)
		}
		if Has(a, "readonly") && !isBool(a["readonly"]) {
			return fail("Agent 只读开关无效")
		}
		if Has(a, "max_consecutive_tool_calls") && !Integer(a["max_consecutive_tool_calls"], 0, 9007199254740991) {
			return fail("重复调用阈值无效")
		}
		for _, tv := range A(a["toolsets"]) {
			t := O(tv)
			if !stringField(t["type"], 1, 256) {
				return fail("原生工具配置必须包含 type")
			}
			for _, field := range []string{"allow_list", "tools", "exclude", "paths"} {
				if Has(t, field) && !uniqueStrings(t[field], func(s string) bool { return strings.TrimSpace(s) != "" }) {
					return fail("原生工具 " + field + " 必须是字符串数组")
				}
			}
		}
		if Has(a, "sub_agents") && !uniqueStrings(a["sub_agents"], func(s string) bool { return Has(agents, s) }) {
			return fail("子代理引用无效")
		}
		if Has(a, "modelId") && (!stringField(a["modelId"], 1, 256) || strings.ContainsAny(S(a["modelId"]), " \t\r\n")) {
			return fail("Agent modelId 无效")
		}
		if Has(a, "attachmentInspectionModelId") && (!stringField(a["attachmentInspectionModelId"], 0, 256) || strings.ContainsAny(S(a["attachmentInspectionModelId"]), " \t\r\n")) {
			return fail("Agent 附件识别模型标识无效")
		}
		if Has(a, "disabledTuckTools") && !uniqueStrings(a["disabledTuckTools"], func(s string) bool { return slices.Contains(tuckTools, s) }) {
			return fail("Tuck 工具选择无效")
		}
		if Has(a, "disabledEnterpriseMcp") && !uniqueStrings(a["disabledEnterpriseMcp"], func(s string) bool { return strings.HasPrefix(s, "mcp_") && len(s) <= 132 }) {
			return fail("企业 MCP 选择无效")
		}
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) bool
	visit = func(name string) bool {
		if visiting[name] {
			return false
		}
		if visited[name] {
			return true
		}
		visiting[name] = true
		for _, v := range A(O(agents[name])["sub_agents"]) {
			if !visit(S(v)) {
				return false
			}
		}
		delete(visiting, name)
		visited[name] = true
		return true
	}
	for name := range agents {
		if !visit(name) {
			return fail("子代理引用不能形成循环")
		}
	}
	wb := O(c["workbench"])
	if !noUnknown(wb, "defaultAgent", "assistants", "deliveryRetentionCount", "requireDelivery", "updatedAt") || !isArray(wb["assistants"]) || len(A(wb["assistants"])) < 1 || len(A(wb["assistants"])) > 100 {
		return fail("工作台助手配置无效")
	}
	if Has(wb, "requireDelivery") && !isBool(wb["requireDelivery"]) {
		return fail("交付要求必须是布尔值")
	}
	if Has(wb, "deliveryRetentionCount") && !Integer(wb["deliveryRetentionCount"], 0, 100) {
		return fail("交付保留必须在 0–100 之间")
	}
	exposed := map[string]bool{}
	for _, v := range A(wb["assistants"]) {
		a := O(v)
		name := S(a["agent"])
		if !Has(agents, name) || exposed[name] || !stringField(a["displayName"], 1, 80) || Has(a, "description") && !stringField(a["description"], 0, 240) {
			return fail("工作台助手引用或名称无效")
		}
		exposed[name] = true
		if Has(a, "taskConcurrency") && !Integer(a["taskConcurrency"], 1, 100000) {
			return fail("任务并发必须为正整数")
		}
		if Has(a, "deliveryTool") && !isBool(a["deliveryTool"]) {
			return fail("交付开关无效")
		}
		if Has(a, "deliveryRetentionCount") && !Integer(a["deliveryRetentionCount"], 0, 100) {
			return fail("助手交付保留无效")
		}
		if a["deliveryTool"] != false {
			for _, other := range agents {
				if Contains(O(other)["sub_agents"], name) {
					return fail("启用交付的主助手不能配置为子代理")
				}
			}
		}
		if Has(a, "workspace") {
			w := O(a["workspace"])
			if !noUnknown(w, "mode", "directory") || !slices.Contains([]string{"auto", "specified", "none"}, S(w["mode"])) || Has(w, "directory") && S(w["directory"]) != "" && !filepath.IsAbs(S(w["directory"])) {
				return fail("工作目录模式无效")
			}
			if S(w["mode"]) == "specified" {
				if S(w["directory"]) == "" {
					return fail("指定目录不能为空")
				}
				if paths {
					info, e := os.Stat(S(w["directory"]))
					if e != nil || !info.IsDir() {
						return fail("指定目录必须已存在且可访问")
					}
					f, e := os.CreateTemp(S(w["directory"]), ".tuck-access-")
					if e != nil {
						return fail("指定目录必须可读写")
					}
					f.Close()
					os.Remove(f.Name())
				}
			}
		}
	}
	if d := S(wb["defaultAgent"]); d != "" && !exposed[d] {
		return fail("默认助手不在可选助手中")
	}
	if !isArray(c["localMcp"]) || len(A(c["localMcp"])) > 100 {
		return fail("localMcp 必须为最多 100 项的数组")
	}
	names := map[string]bool{}
	for _, v := range A(c["localMcp"]) {
		m := O(v)
		name := S(m["name"])
		if Has(m, "authTokenEnv") {
			return fail("authTokenEnv 已不再支持，请改用 authToken")
		}
		if len(name) > 32 || !agentID.MatchString(name) || name == "tuck" || names[name] || !validURL(m["url"], true) || !isBool(m["enabled"]) || Has(m, "authToken") && !validSecret(m["authToken"]) || !uniqueStrings(m["agents"], func(s string) bool { return Has(agents, s) }) {
			return fail("MCP 登记、地址、认证或助手无效")
		}
		names[name] = true
		if Has(m, "tools") && !uniqueStrings(m["tools"], func(s string) bool { return resourceID.MatchString(s) }) {
			return fail("MCP 工具选择必须为精确且不重复的名称")
		}
	}
	for key, v := range O(c["platformMcpTools"]) {
		if key == "" || len(key) > 1024 || !uniqueStrings(v, func(s string) bool { return len(s) > 0 && len(s) <= 1024 && !strings.ContainsAny(s, "\x00\r\n") }) {
			return fail("平台 MCP 工具选择无效")
		}
	}
	return nil
}
func validateModelParameters(m M) error {
	for key, max := range map[string]float64{"temperature": 2, "topP": 1} {
		if Has(m, key) {
			v, ok := m[key].(float64)
			if !ok {
				switch m[key].(type) {
				case int:
					v = N(m[key])
					ok = true
				}
			}
			if !ok || v < 0 || v > max {
				return Problem(422, "invalid_config", "模型参数范围无效")
			}
		}
	}
	return nil
}
func AssistantCatalog(c M) M {
	items := []any{}
	wb := O(c["workbench"])
	for _, v := range A(wb["assistants"]) {
		a := Clone(O(v))
		if !Has(a, "taskConcurrency") {
			a["taskConcurrency"] = 2
		}
		a["deliveryTool"] = a["deliveryTool"] != false && !B(O(O(c["agents"])[S(a["agent"])])["readonly"])
		if m := S(O(O(c["agents"])[S(a["agent"])])["modelId"]); m != "" {
			a["defaultModelId"] = m
		}
		items = append(items, a)
	}
	return M{"defaultAgent": S(wb["defaultAgent"]), "items": items}
}

//go:embed contract/default-config.json
var defaultConfigJSON []byte

func defaultConfig() M {
	var result M
	if err := json.Unmarshal(defaultConfigJSON, &result); err != nil {
		panic(err)
	}
	for name, value := range O(result["agents"]) {
		O(value)["instruction"] = modelContent.DefaultInstruction(name)
		O(value)["description"] = modelContent.DefaultDescription(name)
	}
	return result
}

func InitConfig(path string) error {
	if _, e := os.Stat(path); e == nil {
		return nil
	}
	return writeInitialConfig(path, freshConfig())
}
func freshConfig() M {
	v := defaultConfig()
	key := make([]byte, 32)
	decoded, _ := base64.RawURLEncoding.DecodeString(Token())
	copy(key, decoded)
	O(v["security"])["credentialKey"] = base64.StdEncoding.EncodeToString(key)
	return v
}
func writeInitialConfig(path string, v M) error {
	if e := ValidateConfig(v, false); e != nil {
		return e
	}
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".tuck-init-*.tmp")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	raw, e := marshalConfig(v)
	if e != nil {
		f.Close()
		return e
	}
	_, e = f.Write(raw)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	// Link publishes the complete file without overwriting a concurrent init.
	if e = os.Link(f.Name(), path); os.IsExist(e) {
		return nil
	} else if e != nil {
		return e
	}
	return syncDirectory(filepath.Dir(path))
}
func AtomicWrite(path string, data []byte, mode os.FileMode) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".tuck-*.tmp")
	if e != nil {
		return e
	}
	temp := f.Name()
	defer os.Remove(temp)
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	if e = replaceFile(temp, path); e != nil {
		return e
	}
	return syncDirectory(filepath.Dir(path))
}
func configTemplate() []byte {
	b, _ := marshalConfig(defaultConfig())
	return b
}

func marshalConfig(v M) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(b)+1 > 4<<20 {
		return nil, Problem(422, "invalid_config", "配置文件不能超过 4 MiB")
	}
	return append(b, '\n'), nil
}
func configError(err error) error { return fmt.Errorf("配置无法读取或校验失败: %w", err) }
