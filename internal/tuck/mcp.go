package tuck

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

var mcpVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26"}

const mcpDiscoveryTimeout = 20 * time.Second

func ToolHash(tool M) (string, error) {
	var valid func(any) bool
	valid = func(v any) bool {
		switch x := v.(type) {
		case string:
			return utf8.ValidString(x)
		case map[string]any:
			for k, v := range x {
				if !utf8.ValidString(k) || !valid(v) {
					return false
				}
			}
		case []any:
			for _, v := range x {
				if !valid(v) {
					return false
				}
			}
		}
		return true
	}
	if !valid(tool) {
		return "", fmt.Errorf("invalid UTF-8 in tool contract")
	}
	encoded, e := json.Marshal(tool)
	if e != nil {
		return "", e
	}
	raw, e := jsoncanonicalizer.Transform(encoded)
	if e != nil {
		return "", e
	}
	return "sha256:" + Hash(raw), nil
}

func readMCPJSON(reader io.Reader) (M, error) {
	raw, e := io.ReadAll(io.LimitReader(reader, (4<<20)+1))
	if e != nil {
		return nil, e
	}
	return decodeMCPJSON(raw)
}
func decodeMCPJSON(raw []byte) (M, error) {
	if len(raw) > 4<<20 {
		return nil, Problem(413, "mcp_response_size", "MCP response exceeds 4 MiB")
	}
	// Validate original bytes before encoding/json can replace malformed Unicode.
	if _, e := jsoncanonicalizer.Transform(raw); e != nil {
		return nil, Problem(400, "invalid_json", "MCP JSON 不符合严格合同")
	}
	v, e := DecodeJSON(raw, 4<<20)
	if e != nil {
		return nil, e
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, Problem(400, "invalid_json", "MCP JSON 必须是对象")
	}
	return m, nil
}
func ToolAlias(name string) string {
	clean := regexp.MustCompile(`[^A-Za-z0-9_-]`).ReplaceAllString(name, "_")
	if len(clean) > 30 {
		clean = clean[:30]
	}
	return clean + "_" + Hash([]byte(name))[:8]
}

type mcpEntry struct {
	Key, Scope, Fingerprint, Namespace, Source string
	Resource                                   M
	Tools                                      map[string]M
	Catalog                                    []M
	Validated, Attempted                       bool
	Status, Error                              string
	ctx                                        context.Context
	cancel                                     context.CancelFunc
	discoverMu                                 sync.Mutex
}
type MCPConnections struct {
	owner    *Models
	mu       sync.RWMutex
	entries  map[string]*mcpEntry
	order    []string
	client   *http.Client
	OnChange func()
}

func NewMCPConnections(owner *Models) *MCPConnections {
	return &MCPConnections{owner: owner, entries: map[string]*mcpEntry{}, client: &http.Client{Transport: newTransport(0), CheckRedirect: noRedirect}}
}
func (c *MCPConnections) scope() string {
	b := c.owner.Platform.Binding()
	if b == nil {
		return "direct"
	}
	return fmt.Sprintf("platform:%s:%s:%s:%s:%s:%d", S(b["origin"]), S(O(b["discovery"])["runtimeApiBase"]), S(b["deploymentId"]), S(b["userId"]), S(b["sessionId"]), I(O(b["snapshot"])["managedGeneration"]))
}
func (c *MCPConnections) Rebuild() error {
	config := c.owner.Config()
	binding := c.owner.Platform.Binding()
	registrations := []M{}
	for _, v := range A(O(binding["snapshot"])["mcp"]) {
		registrations = append(registrations, M{"key": O(v)["mcpServerId"], "source": "platform", "resource": v})
	}
	for _, v := range A(config["localMcp"]) {
		registrations = append(registrations, M{"key": "local:" + S(O(v)["name"]), "source": "local", "resource": v})
	}
	wanted := map[string]bool{}
	cacheWanted := map[string]string{}
	scope := c.scope()
	for _, r := range registrations {
		key := S(r["key"])
		resource := Clone(O(r["resource"]))
		source := S(r["source"])
		wanted[key] = true
		data := M{"scope": scope, "resource": resource}
		if source == "local" {
			data = M{"name": resource["name"], "url": resource["url"], "authToken": S(resource["authToken"]), "enabled": resource["enabled"]}
		}
		canonical, e := jsoncanonicalizer.Transform(JSON(data))
		if e != nil {
			return e
		}
		fingerprint := Hash(canonical)
		if B(resource["enabled"]) {
			cacheWanted[key] = fingerprint
		}
		c.mu.Lock()
		old := c.entries[key]
		if old != nil && old.Fingerprint == fingerprint {
			old.Resource = resource
			c.mu.Unlock()
			continue
		}
		if old != nil {
			old.cancel()
		}
		ctx, cancel := context.WithCancel(c.owner.ctx)
		entry := &mcpEntry{Key: key, Source: source, Resource: resource, Scope: scope, Fingerprint: fingerprint, Namespace: "e" + Hash([]byte(key))[:12], Status: "pending", Tools: map[string]M{}, ctx: ctx, cancel: cancel}
		if source == "local" {
			entry.Scope = "local:" + fingerprint[:24]
			entry.Namespace = S(resource["name"])
		}
		if !B(resource["enabled"]) {
			entry.Status = "disabled"
		}
		c.entries[key] = entry
		c.mu.Unlock()
		if B(resource["enabled"]) {
			catalog, _, err := c.owner.Store.Catalog(key, fingerprint)
			if err != nil {
				return err
			}
			if catalog != nil {
				tools, message, err := c.project(entry, catalog)
				if err == nil {
					c.mu.Lock()
					entry.Catalog = catalog
					entry.Tools = tools
					entry.Validated = true
					entry.Status = "ready"
					entry.Error = message
					c.mu.Unlock()
				}
			}
		}
	}
	c.mu.Lock()
	c.order = nil
	for _, registration := range registrations {
		c.order = append(c.order, S(registration["key"]))
	}
	for key, entry := range c.entries {
		if !wanted[key] {
			entry.cancel()
			delete(c.entries, key)
		}
	}
	c.mu.Unlock()
	return c.owner.Store.Write(func(tx *sql.Tx) error {
		rows, e := tx.Query("SELECT resource_key,fingerprint FROM mcp_catalog")
		if e != nil {
			return e
		}
		keys := []string{}
		for rows.Next() {
			var key, fingerprint string
			if e = rows.Scan(&key, &fingerprint); e != nil {
				rows.Close()
				return e
			}
			if cacheWanted[key] != fingerprint {
				keys = append(keys, key)
			}
		}
		if e = rows.Close(); e != nil {
			return e
		}
		for _, key := range keys {
			if _, e = tx.Exec("DELETE FROM mcp_catalog WHERE resource_key=?", key); e != nil {
				return e
			}
		}
		return nil
	})
}
func validTool(tool M) bool {
	return S(tool["name"]) != "" && len(S(tool["name"])) <= 1024 && S(O(tool["inputSchema"])["type"]) == "object"
}
func (c *MCPConnections) project(entry *mcpEntry, catalog []M) (map[string]M, string, error) {
	resource := c.resource(entry)
	grants := map[string]M{}
	for _, v := range A(resource["allowedTools"]) {
		grants[S(O(v)["name"])] = O(v)
	}
	tools, seen := map[string]M{}, map[string]bool{}
	drift := []string{}
	for _, raw := range catalog {
		name := S(raw["name"])
		if !validTool(raw) || seen[name] || len(seen) >= 4096 {
			return nil, "", Problem(502, "mcp_catalog", "MCP 工具目录无效")
		}
		seen[name] = true
		hash, e := ToolHash(raw)
		if e != nil {
			return nil, "", e
		}
		grant := grants[name]
		if entry.Source == "platform" && S(resource["toolAccessMode"]) == "ALLOWLIST" && (grant == nil || S(grant["contractHash"]) != hash) {
			if grant != nil {
				drift = append(drift, name)
			}
			continue
		}
		alias := name
		if entry.Source == "platform" {
			alias = ToolAlias(name)
		}
		if tools[alias] != nil {
			return nil, "", Problem(502, "mcp_catalog", "MCP 工具别名冲突")
		}
		tools[alias] = M{"raw": Clone(raw), "hash": hash, "grant": Clone(grant)}
	}
	for name := range grants {
		if !seen[name] {
			drift = append(drift, name)
		}
	}
	message := ""
	if len(drift) > 0 {
		message = fmt.Sprintf("%d 个审核工具缺失或合同已变化，已阻止使用：%s", len(drift), strings.Join(drift[:min(3, len(drift))], "、"))
	}
	return tools, message, nil
}
func (c *MCPConnections) resource(entry *mcpEntry) M {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Clone(entry.Resource)
}
func (c *MCPConnections) selected(entry *mcpEntry, name string) bool {
	selection, defined := c.resource(entry)["tools"]
	if entry.Source == "platform" {
		selection, defined = O(c.owner.Config()["platformMcpTools"])[c.owner.DefaultsKey(entry.Key, "platform")]
	}
	return !defined || Contains(selection, name)
}
func (c *MCPConnections) current(entry *mcpEntry) error {
	c.mu.RLock()
	same := c.entries[entry.Key] == entry
	enabled := B(entry.Resource["enabled"])
	c.mu.RUnlock()
	if !same || !enabled || entry.ctx.Err() != nil || entry.Source == "platform" && entry.Scope != c.scope() {
		return Problem(428, "mcp_scope_changed", "MCP 配置或身份已变化，请刷新配置")
	}
	return nil
}
func (c *MCPConnections) gate(ctx context.Context, entry *mcpEntry) error {
	if e := c.current(entry); e != nil {
		return e
	}
	if entry.Source == "platform" {
		if e := c.owner.Platform.Admit(ctx, I(O(c.owner.Platform.Binding()["snapshot"])["managedGeneration"])); e != nil {
			return e
		}
	}
	return c.current(entry)
}
func (c *MCPConnections) target(entry *mcpEntry) string {
	resource := c.resource(entry)
	if entry.Source == "local" {
		return S(resource["url"])
	}
	b := c.owner.Platform.Binding()
	return S(b["origin"]) + strings.TrimSuffix(S(O(b["discovery"])["runtimeApiBase"]), "/") + "/resources/" + url.PathEscape(entry.Key) + S(resource["runtimePath"])
}
func (c *MCPConnections) headers(ctx context.Context, entry *mcpEntry, initial http.Header) (http.Header, error) {
	if e := c.gate(ctx, entry); e != nil {
		return nil, e
	}
	headers := initial.Clone()
	if headers == nil {
		headers = http.Header{}
	}
	token := S(c.resource(entry)["authToken"])
	if entry.Source == "platform" {
		var e error
		token, e = c.owner.Platform.Access(ctx)
		if e != nil {
			return nil, e
		}
		headers.Set("X-Measix-Managed-Generation", fmt.Sprint(I(O(c.owner.Platform.Binding()["snapshot"])["managedGeneration"])))
		headers.Set("X-Measix-Interaction-Id", "int_"+UUID())
	}
	if e := c.current(entry); e != nil {
		return nil, e
	}
	if token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	return headers, nil
}
func (c *MCPConnections) Summary(local bool) []M {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := []M{}
	for _, key := range c.order {
		entry := c.entries[key]
		if (entry.Source == "local") != local {
			continue
		}
		v := M{"displayName": entry.Resource["displayName"], "enabled": entry.Resource["enabled"], "status": entry.Status, "validated": entry.Validated, "toolCount": len(entry.Tools), "error": nil}
		if entry.Error != "" {
			v["error"] = entry.Error
		}
		if local {
			v["name"] = entry.Resource["name"]
			v["url"] = entry.Resource["url"]
			if S(v["displayName"]) == "" {
				v["displayName"] = entry.Resource["name"]
			}
		} else {
			v["mcpServerId"] = entry.Key
		}
		result = append(result, v)
	}
	return result
}
func (c *MCPConnections) AssistantSummary(agent string) []M {
	config := c.owner.Config()
	a := O(O(config["agents"])[agent])
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := []M{}
	for _, key := range c.order {
		entry := c.entries[key]
		if entry.Source == "local" && !Contains(entry.Resource["agents"], agent) || entry.Source == "platform" && Contains(a["disabledEnterpriseMcp"], entry.Key) {
			continue
		}
		enabled := B(entry.Resource["enabled"])
		selection := entry.Resource["tools"]
		if entry.Source == "platform" {
			selection = O(config["platformMcpTools"])[c.owner.DefaultsKey(entry.Key, "platform")]
		}
		if selection != nil && len(A(selection)) == 0 {
			enabled = false
		}
		count := 0
		for _, tool := range entry.Tools {
			if selection == nil || Contains(selection, S(O(tool["raw"])["name"])) {
				count++
			}
		}
		v := M{"id": entry.Key, "displayName": entry.Resource["displayName"], "source": entry.Source, "enabled": enabled, "status": entry.Status, "validated": entry.Validated, "toolCount": nil}
		if enabled && entry.Validated {
			v["toolCount"] = count
		}
		if !enabled {
			v["status"] = "disabled"
		}
		result = append(result, v)
	}
	return result
}
func (c *MCPConnections) Toolsets(agent string, readonly bool) []any {
	config := c.owner.Config()
	a := O(O(config["agents"])[agent])
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := []any{}
	for _, key := range c.order {
		entry := c.entries[key]
		if !B(entry.Resource["enabled"]) {
			continue
		}
		if entry.Source == "local" && !Contains(entry.Resource["agents"], agent) || entry.Source == "platform" && Contains(a["disabledEnterpriseMcp"], entry.Key) {
			continue
		}
		selection := entry.Resource["tools"]
		if entry.Source == "platform" {
			selection = O(config["platformMcpTools"])[c.owner.DefaultsKey(entry.Key, "platform")]
		}
		if selection != nil && len(A(selection)) == 0 {
			continue
		}
		target := S(entry.Resource["url"])
		token := ""
		if entry.Source == "platform" {
			target = c.owner.URL + "/mcp/" + url.PathEscape(entry.Scope) + "/" + url.PathEscape(entry.Key)
			token = "TUCK_GATEWAY_TOKEN"
		} else if S(entry.Resource["authToken"]) != "" {
			token = "TUCK_MCP_" + strings.ToUpper(Hash([]byte(entry.Key))[:16])
		}
		remote := M{"url": target, "transport_type": "streamable-http"}
		if token != "" {
			remote["headers"] = M{"Authorization": "Bearer ${env." + token + "}"}
		}
		toolset := M{"type": "mcp", "name": entry.Namespace, "allow_private_ips": true, "remote": remote}
		if readonly {
			toolset["readonly"] = true
		}
		if entry.Source == "local" && selection != nil {
			names := []any{}
			for _, name := range A(selection) {
				names = append(names, entry.Namespace+"_"+S(name))
			}
			toolset["tools"] = names
		}
		result = append(result, toolset)
	}
	return result
}
func (c *MCPConnections) Environment() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	env := map[string]string{}
	for _, entry := range c.entries {
		if entry.Source == "local" && B(entry.Resource["enabled"]) && S(entry.Resource["authToken"]) != "" {
			env["TUCK_MCP_"+strings.ToUpper(Hash([]byte(entry.Key))[:16])] = S(entry.Resource["authToken"])
		}
	}
	return env
}
func (c *MCPConnections) ApprovalTools() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	tools := []string{}
	for _, entry := range c.entries {
		if entry.Source != "platform" || !B(entry.Resource["enabled"]) {
			continue
		}
		for _, g := range A(entry.Resource["allowedTools"]) {
			if S(O(g)["approvalPolicy"]) == "REQUIRE_CONFIRMATION" {
				tools = append(tools, entry.Namespace+"_"+ToolAlias(S(O(g)["name"])))
			}
		}
	}
	return tools
}

// mapSSE validates data frames without changing upstream event IDs or notifications.
func mapSSE(reader io.Reader, writer io.Writer, transform func(M) (M, error)) error {
	r := bufio.NewReaderSize(reader, 32<<10)
	packet := []byte{}
	partialLine := false
	for {
		line, e := r.ReadSlice('\n')
		packet = append(packet, line...)
		if len(packet) > 4<<20 {
			return Problem(413, "mcp_response_size", "MCP response exceeds 4 MiB")
		}
		if e == bufio.ErrBufferFull {
			partialLine = true
			continue
		}
		blank := !partialLine && (bytes.Equal(line, []byte("\n")) || bytes.Equal(line, []byte("\r\n")))
		partialLine = false
		if blank {
			lines := strings.Split(string(packet), "\n")
			data := []string{}
			for _, l := range lines {
				l = strings.TrimSuffix(l, "\r")
				if strings.HasPrefix(l, "data:") {
					data = append(data, strings.TrimLeft(strings.TrimPrefix(l, "data:"), " \t"))
				}
			}
			// MCP priming events carry a replay ID with an empty data buffer.
			// Preserve those frames without treating them as JSON-RPC messages.
			payload := strings.Join(data, "\n")
			if len(data) > 0 && payload != "" {
				value, err := decodeMCPJSON([]byte(payload))
				if err != nil {
					return err
				}
				mapped, err := transform(O(value))
				if err != nil {
					return err
				}
				out := []string{}
				written := false
				for _, l := range lines {
					l = strings.TrimSuffix(l, "\r")
					if strings.HasPrefix(l, "data:") {
						if !written {
							out = append(out, "data: "+string(JSON(mapped)))
							written = true
						}
					} else {
						out = append(out, l)
					}
				}
				packet = []byte(strings.Join(out, "\n"))
			}
			if _, err := writer.Write(packet); err != nil {
				return err
			}
			if f, ok := writer.(http.Flusher); ok {
				f.Flush()
			}
			packet = nil
		}
		if e == io.EOF {
			if len(bytes.TrimSpace(packet)) > 0 {
				return fmt.Errorf("incomplete SSE frame")
			}
			return nil
		}
		if e != nil {
			return e
		}
	}
}
func readRPC(response *http.Response, wanted any) (M, error) {
	defer response.Body.Close()
	if strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		var found M
		e := mapSSE(response.Body, io.Discard, func(v M) (M, error) {
			if reflect.DeepEqual(v["id"], wanted) {
				found = v
				return v, io.EOF
			}
			return v, nil
		})
		if e != nil && e != io.EOF {
			return nil, e
		}
		if found == nil {
			return nil, fmt.Errorf("missing RPC result")
		}
		return found, nil
	}
	v, e := readMCPJSON(response.Body)
	if e == nil && response.StatusCode >= 200 && response.StatusCode < 300 && (v["jsonrpc"] != "2.0" || !reflect.DeepEqual(v["id"], wanted)) {
		return nil, fmt.Errorf("invalid MCP JSON-RPC response identity")
	}
	return v, e
}
func verifyRemote(ctx context.Context, client *http.Client, target string, headers http.Header) ([]M, error) {
	session := ""
	protocol := mcpVersions[0]
	rpc := func(method string, params M, id any) (M, error) {
		body := M{"jsonrpc": "2.0", "method": method}
		if params != nil {
			body["params"] = params
		}
		if id != nil {
			body["id"] = id
		}
		req, e := http.NewRequestWithContext(ctx, "POST", target, bytes.NewReader(JSON(body)))
		if e != nil {
			return nil, e
		}
		req.Header = headers.Clone()
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
			req.Header.Set("Mcp-Protocol-Version", protocol)
		}
		r, e := client.Do(req)
		if e != nil {
			return nil, e
		}
		if sid := r.Header.Get("Mcp-Session-Id"); sid != "" {
			session = sid
		}
		if r.StatusCode < 200 || r.StatusCode >= 300 {
			r.Body.Close()
			return nil, PlatformHTTPError(r.StatusCode, "mcp_verification_failed")
		}
		if id == nil {
			r.Body.Close()
			return nil, nil
		}
		v, e := readRPC(r, id)
		if e != nil {
			return nil, e
		}
		if v["error"] != nil {
			return nil, Problem(502, "mcp_error", "MCP 验证请求失败")
		}
		return O(v["result"]), nil
	}
	defer func() {
		if session != "" {
			closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(closeCtx, "DELETE", target, nil)
			req.Header = headers.Clone()
			req.Header.Set("Mcp-Session-Id", session)
			req.Header.Set("Mcp-Protocol-Version", protocol)
			if r, e := client.Do(req); e == nil {
				r.Body.Close()
			}
		}
	}()
	initialized, e := rpc("initialize", M{"protocolVersion": protocol, "capabilities": M{}, "clientInfo": M{"name": "measix-tuck-verification", "version": "1.0.0"}}, float64(1))
	if e != nil {
		return nil, e
	}
	protocol = S(initialized["protocolVersion"])
	if !Contains(mcpVersions, protocol) || !Has(O(initialized["capabilities"]), "tools") {
		return nil, Problem(422, "mcp_protocol", "MCP 协议版本或工具能力不受支持")
	}
	if _, e = rpc("notifications/initialized", nil, nil); e != nil {
		return nil, e
	}
	catalog := []M{}
	seen, cursors := map[string]bool{}, map[string]bool{}
	cursor := ""
	size := 0
	for page := 0; page < 64; page++ {
		params := M{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		result, e := rpc("tools/list", params, float64(page+2))
		if e != nil {
			return nil, e
		}
		size += len(JSON(result))
		if size > 4<<20 || !isArray(result["tools"]) {
			return nil, Problem(413, "mcp_catalog", "MCP 工具目录超限或无效")
		}
		for _, v := range A(result["tools"]) {
			tool := O(v)
			name := S(tool["name"])
			if !validTool(tool) || seen[name] || len(catalog) >= 4096 {
				return nil, Problem(502, "mcp_catalog", "MCP 工具目录无效")
			}
			seen[name] = true
			catalog = append(catalog, tool)
		}
		if !Has(result, "nextCursor") {
			return catalog, nil
		}
		cursor = S(result["nextCursor"])
		if cursor == "" || cursors[cursor] {
			return nil, Problem(502, "mcp_catalog", "MCP 分页游标无效")
		}
		cursors[cursor] = true
	}
	return nil, Problem(502, "mcp_catalog", "MCP 工具目录不完整")
}
func (c *MCPConnections) Discover(ctx context.Context, key string) error {
	c.mu.RLock()
	entry := c.entries[key]
	c.mu.RUnlock()
	if entry == nil {
		return Problem(422, "mcp_unavailable", "MCP 登记不存在")
	}
	entry.discoverMu.Lock()
	defer entry.discoverMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, mcpDiscoveryTimeout)
	defer cancel()
	stop := context.AfterFunc(entry.ctx, cancel)
	defer stop()
	headers, e := c.headers(ctx, entry, http.Header{})
	if e != nil {
		return e
	}
	c.mu.Lock()
	entry.Attempted = true
	c.mu.Unlock()
	catalog, e := verifyRemote(ctx, c.client, c.target(entry), headers)
	if e == nil {
		var tools map[string]M
		var message string
		tools, message, e = c.project(entry, catalog)
		if e == nil {
			c.mu.Lock()
			if c.entries[entry.Key] != entry || entry.ctx.Err() != nil {
				e = Problem(428, "mcp_scope_changed", "MCP 配置或身份已变化，请刷新配置")
			}
			if e == nil {
				e = c.owner.Store.SaveCatalog(entry.Key, entry.Fingerprint, catalog)
			}
			if e == nil {
				entry.Tools = tools
				entry.Catalog = catalog
				entry.Error = message
				entry.Validated = true
				entry.Status = "ready"
			}
			c.mu.Unlock()
		}
	}
	if e != nil {
		c.mu.Lock()
		if c.entries[key] == entry && entry.ctx.Err() == nil {
			entry.Status = "pending"
			if entry.Validated {
				entry.Status = "unavailable"
			}
			entry.Error = "MCP 验证失败，请检查服务后重新验证"
			if Code(e) == "mcp_protocol" {
				entry.Error = e.Error()
			}
		}
		c.mu.Unlock()
	}
	if c.OnChange != nil {
		c.OnChange()
	}
	return e
}
func (c *MCPConnections) Prepare(ctx context.Context, refresh bool) {
	c.mu.RLock()
	keys := []string{}
	for key, entry := range c.entries {
		if B(entry.Resource["enabled"]) && (refresh || !entry.Validated && !entry.Attempted) {
			keys = append(keys, key)
		}
	}
	c.mu.RUnlock()
	for _, key := range keys {
		if ctx.Err() != nil {
			return
		}
		_ = c.Discover(ctx, key)
	}
}
func (c *MCPConnections) secrets(entry *mcpEntry) []string {
	b := c.owner.Platform.Binding()
	return []string{c.owner.Secret, S(c.resource(entry)["authToken"]), S(b["accessToken"]), S(b["refreshToken"])}
}
func redactMCP(value any, secrets []string) any {
	switch x := value.(type) {
	case string:
		x = cleanSecret(x, secrets...)
		x = regexp.MustCompile(`(?i)Bearer\s+[^\s"'<>]+`).ReplaceAllString(x, "Bearer [REDACTED]")
		x = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f]`).ReplaceAllString(x, "")
		if len(x) > 2400 {
			x = x[:2400]
		}
		return x
	case map[string]any:
		r := M{}
		for k, v := range x {
			r[k] = redactMCP(v, secrets)
		}
		return r
	case []any:
		r := []any{}
		for _, v := range x {
			r = append(r, redactMCP(v, secrets))
		}
		return r
	}
	return value
}
func (c *MCPConnections) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.EscapedPath(), "/")
	if len(parts) != 4 {
		WriteError(w, Problem(428, "mcp_scope_changed", "MCP 配置已失效"))
		return
	}
	scope, scopeErr := url.PathUnescape(parts[2])
	key, keyErr := url.PathUnescape(parts[3])
	if scopeErr != nil || keyErr != nil {
		WriteError(w, Problem(400, "mcp_path", "MCP 路径无效"))
		return
	}
	c.mu.RLock()
	entry := c.entries[key]
	c.mu.RUnlock()
	var body M
	forwarding := false
	fail := func(e error) {
		if forwarding {
			panic(http.ErrAbortHandler)
		}
		if body != nil && S(body["method"]) == "tools/call" {
			WriteJSON(w, 200, M{"jsonrpc": "2.0", "id": body["id"], "result": M{"isError": true, "content": []any{M{"type": "text", "text": e.Error()}}}})
		} else {
			WriteJSON(w, Status(e), M{"code": Code(e), "message": e.Error()})
		}
	}
	if entry == nil || entry.Source != "platform" || entry.Scope != scope {
		fail(Problem(428, "mcp_scope_changed", "MCP 配置已失效"))
		return
	}
	if r.Method != "GET" && r.Method != "POST" && r.Method != "DELETE" {
		fail(Problem(405, "mcp_method", "MCP 请求方法不受支持"))
		return
	}
	if r.Method == "POST" {
		var e error
		body, e = readMCPJSON(r.Body)
		if e != nil {
			fail(e)
			return
		}
		if S(body["jsonrpc"]) != "2.0" {
			fail(Problem(400, "mcp_request", "MCP 请求格式无效"))
			return
		}
		method := S(body["method"])
		if method != "" && !Contains([]string{"initialize", "ping", "tools/list", "tools/call", "prompts/list", "resources/list", "resources/templates/list", "notifications/initialized", "notifications/cancelled", "notifications/progress"}, method) {
			fail(Problem(403, "mcp_method", "此资源只开放 MCP 工具能力"))
			return
		}
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stop := context.AfterFunc(entry.ctx, cancel)
	defer stop()
	initial := http.Header{}
	for _, name := range []string{"Content-Type", "Accept", "Mcp-Session-Id", "Mcp-Protocol-Version", "Last-Event-ID"} {
		if value := r.Header.Values(name); len(value) > 0 {
			initial[name] = value
		}
	}
	headers, e := c.headers(ctx, entry, initial)
	if e != nil {
		fail(e)
		return
	}
	if empty := map[string]string{"prompts/list": "prompts", "resources/list": "resources", "resources/templates/list": "resourceTemplates"}[S(body["method"])]; empty != "" {
		WriteJSON(w, 200, M{"jsonrpc": "2.0", "id": body["id"], "result": M{empty: []any{}}})
		return
	}
	if S(body["method"]) == "tools/call" {
		name := S(O(body["params"])["name"])
		c.mu.RLock()
		tool := Clone(entry.Tools[name])
		c.mu.RUnlock()
		if tool == nil || !c.selected(entry, S(O(tool["raw"])["name"])) {
			fail(Problem(403, "mcp_tool_unavailable", "工具未获许可、合同已变化或已在本机停用"))
			return
		}
		body["params"] = Merge(O(body["params"]), M{"name": O(tool["raw"])["name"]})
	}
	var input io.Reader
	if body != nil {
		input = bytes.NewReader(JSON(body))
	}
	request, e := http.NewRequestWithContext(ctx, r.Method, c.target(entry), input)
	if e != nil {
		fail(e)
		return
	}
	request.Header = headers
	forwarding = true
	upstream, e := c.client.Do(request)
	if e != nil {
		fail(e)
		return
	}
	defer upstream.Body.Close()
	if e = c.current(entry); e != nil {
		fail(e)
		return
	}
	transform := func(message M) (M, error) {
		if e := c.current(entry); e != nil {
			return nil, e
		}
		if S(body["method"]) == "initialize" && reflect.DeepEqual(message["id"], body["id"]) && Has(O(message["result"]), "capabilities") {
			result := Clone(O(message["result"]))
			caps := M{}
			if tools, ok := O(result["capabilities"])["tools"]; ok {
				caps["tools"] = tools
			}
			result["capabilities"] = caps
			message = Merge(message, M{"result": result})
		}
		if upstream.StatusCode < 200 || upstream.StatusCode >= 300 {
			message = O(redactMCP(message, c.secrets(entry)))
		} else if Has(message, "error") {
			message = Merge(message, M{"error": redactMCP(message["error"], c.secrets(entry))})
		}
		if S(body["method"]) == "tools/list" && reflect.DeepEqual(message["id"], body["id"]) && message["result"] != nil {
			result := O(message["result"])
			if !isArray(result["tools"]) {
				return nil, Problem(502, "mcp_catalog", "MCP 工具目录无效")
			}
			catalog := []M{}
			for _, t := range A(result["tools"]) {
				catalog = append(catalog, O(t))
			}
			tools, _, e := c.project(entry, catalog)
			if e != nil {
				return nil, e
			}
			c.mu.Lock()
			if S(O(body["params"])["cursor"]) == "" {
				entry.Tools = map[string]M{}
			}
			for name, t := range tools {
				entry.Tools[name] = t
			}
			c.mu.Unlock()
			visible := []any{}
			for alias, tool := range tools {
				if c.selected(entry, S(O(tool["raw"])["name"])) {
					visible = append(visible, Merge(O(tool["raw"]), M{"name": alias}))
				}
			}
			message = Merge(message, M{"result": Merge(result, M{"tools": visible})})
		}
		return message, nil
	}
	contentType := upstream.Header.Get("Content-Type")
	var raw []byte
	// Notifications and session deletion may acknowledge without a JSON-RPC
	// body, even when the upstream keeps its default JSON content type.
	acknowledgment := r.Method == "POST" && upstream.StatusCode == http.StatusAccepted && !Has(body, "id") || r.Method == "DELETE" && upstream.StatusCode == http.StatusNoContent
	if strings.Contains(contentType, "application/json") {
		var value M
		var e error
		if acknowledgment {
			payload, readErr := io.ReadAll(io.LimitReader(upstream.Body, (4<<20)+1))
			e = readErr
			if e == nil && len(payload) == 0 {
				raw = []byte{}
			} else if e == nil {
				value, e = decodeMCPJSON(payload)
			}
		} else {
			value, e = readMCPJSON(upstream.Body)
		}
		if e != nil {
			fail(e)
			return
		}
		if raw == nil {
			value, e = transform(value)
			if e != nil {
				fail(e)
				return
			}
			raw = JSON(value)
		}
	} else if !strings.Contains(contentType, "text/event-stream") && upstream.StatusCode >= 300 {
		value, e := io.ReadAll(io.LimitReader(upstream.Body, (4<<20)+1))
		if e != nil || len(value) > 4<<20 {
			fail(fmt.Errorf("MCP response exceeds limit"))
			return
		}
		raw = []byte(S(redactMCP(string(value), c.secrets(entry))))
	}
	for _, name := range []string{"Content-Type", "Mcp-Session-Id", "Mcp-Protocol-Version", "Cache-Control", "Retry-After", "WWW-Authenticate"} {
		if value := upstream.Header.Get(name); value != "" {
			if name == "WWW-Authenticate" {
				value = cleanSecret(value, c.secrets(entry)...)
			}
			w.Header().Set(name, value)
		}
	}
	w.WriteHeader(upstream.StatusCode)
	if raw != nil {
		w.Write(raw)
		return
	}
	if strings.Contains(contentType, "text/event-stream") {
		if e = mapSSE(upstream.Body, w, transform); e != nil {
			panic(http.ErrAbortHandler)
		}
	} else {
		if _, e = io.Copy(w, upstream.Body); e != nil {
			panic(http.ErrAbortHandler)
		}
	}
}
func (c *MCPConnections) Close() {
	c.mu.Lock()
	for _, entry := range c.entries {
		entry.cancel()
	}
	c.entries = map[string]*mcpEntry{}
	c.mu.Unlock()
	c.client.CloseIdleConnections()
}
