package tuck

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
)

type workspaceLease struct {
	identity string
	cancel   context.CancelFunc
}
type EnterpriseWorkspace struct {
	Platform *Platform
	client   *http.Client
	mu       sync.Mutex
	leases   map[string]workspaceLease
}

func NewEnterpriseWorkspace(p *Platform) *EnterpriseWorkspace {
	return &EnterpriseWorkspace{Platform: p, client: &http.Client{Transport: newTransport(15 * time.Second), CheckRedirect: noRedirect}, leases: map[string]workspaceLease{}}
}
func workspaceIdentity(b M) string {
	if b == nil {
		return ""
	}
	return strings.Join([]string{S(b["origin"]), S(b["deploymentId"]), S(b["userId"]), S(b["sessionId"])}, "\x00")
}
func workspaceSubject(b M) string {
	return Hash([]byte(strings.Join([]string{S(b["origin"]), S(b["deploymentId"]), S(b["userId"])}, "\x00")))
}
func (e *EnterpriseWorkspace) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, lease := range e.leases {
		lease.cancel()
	}
	e.client.CloseIdleConnections()
}
func (e *EnterpriseWorkspace) Invalidate() {
	identity := workspaceIdentity(e.Platform.Binding())
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, lease := range e.leases {
		if lease.identity != identity {
			lease.cancel()
		}
	}
}
func (e *EnterpriseWorkspace) Handle(w http.ResponseWriter, r *http.Request) error {
	route := strings.TrimPrefix(r.URL.Path, "/api/enterprise-workspace")
	allowed := map[string][]string{"": {"GET"}, "/files": {"GET", "POST"}, "/content": {"GET", "HEAD", "PUT"}, "/archive": {"GET"}}
	if !Contains(allowed[route], r.Method) {
		return Problem(404, "workspace_route", "文件操作不存在")
	}
	binding := e.Platform.Binding()
	if binding == nil {
		return Problem(409, "workspace_not_enrolled", "请先接入企业账号")
	}
	identity := workspaceIdentity(binding)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	id := ID()
	e.mu.Lock()
	e.leases[id] = workspaceLease{identity, cancel}
	e.mu.Unlock()
	defer func() { e.mu.Lock(); delete(e.leases, id); e.mu.Unlock() }()
	assertIdentity := func() error {
		if workspaceIdentity(e.Platform.Binding()) != identity {
			return Problem(409, "workspace_identity_changed", "企业账号已变化，请重新打开工作区")
		}
		return ctxError(ctx)
	}
	call := func(resource string, query url.Values, method string, body io.Reader, headers http.Header) (*http.Response, error) {
		if er := assertIdentity(); er != nil {
			return nil, er
		}
		token, er := e.Platform.Access(ctx)
		if er != nil {
			status := Status(er)
			if status == 401 {
				status = 409
			}
			return nil, Problem(status, Code(er), PlatformHTTPError(Status(er), Code(er)).Error())
		}
		if er = assertIdentity(); er != nil {
			return nil, er
		}
		target := S(binding["origin"]) + strings.TrimSuffix(S(O(binding["discovery"])["clientApiBase"]), "/") + "/workspace" + resource
		if len(query) > 0 {
			target += "?" + query.Encode()
		}
		operation, cancelOperation := context.WithCancel(ctx)
		if method == "PUT" {
			body = watchRequestIdle(io.NopCloser(body), 120*time.Second, cancelOperation)
		}
		request, er := http.NewRequestWithContext(operation, method, target, body)
		if er != nil {
			cancelOperation()
			return nil, er
		}
		request.Header = headers.Clone()
		if request.Header == nil {
			request.Header = http.Header{}
		}
		request.Header.Set("Authorization", "Bearer "+token)
		if method == "PUT" && r.ContentLength >= 0 {
			request.ContentLength = r.ContentLength
		}
		upstream, er := e.client.Do(request)
		if er != nil {
			cancelOperation()
			if method == "GET" || method == "HEAD" {
				return nil, Problem(502, "workspace_unreachable", "无法连接企业文件服务")
			}
			return nil, Problem(502, "workspace_result_unknown", "连接中断，操作结果未知，请刷新核对")
		}
		upstream.Body = watchIdle(upstream.Body, 120*time.Second, cancelOperation)
		if er = assertIdentity(); er != nil {
			upstream.Body.Close()
			return nil, er
		}
		if upstream.StatusCode >= 300 && upstream.StatusCode != 304 {
			value, _ := ReadJSON(upstream.Body, 4<<20)
			upstream.Body.Close()
			status := upstream.StatusCode
			if status == 401 {
				status = 409
			}
			if status >= 300 && status < 400 {
				status = 502
			}
			if upstream.StatusCode == 416 && strings.HasPrefix(upstream.Header.Get("Content-Range"), "bytes */") {
				w.Header().Set("Content-Range", upstream.Header.Get("Content-Range"))
			}
			failure := PlatformHTTPError(upstream.StatusCode, S(value["code"]))
			return nil, Problem(status, Code(failure), failure.Error())
		}
		return upstream, nil
	}
	if route == "" {
		upstream, er := call("", nil, "GET", nil, nil)
		if er != nil {
			return er
		}
		value, er := responseJSON(upstream, "WorkspaceProjection")
		if er != nil {
			return er
		}
		WriteJSON(w, 200, Merge(value, M{"subject": workspaceSubject(binding), "employeeName": S(binding["employeeName"])}))
		return nil
	}
	space := r.URL.Query().Get("agentSpaceId")
	if !spaceIDPattern.MatchString(space) || r.URL.Query().Get("subject") != workspaceSubject(binding) {
		return Problem(409, "workspace_identity_changed", "工作区身份已变化，请刷新")
	}
	filePath, er := WorkspacePath(r.URL.Query().Get("path"), true)
	if er != nil {
		return er
	}
	query := url.Values{"agentSpaceId": {space}, "path": {filePath}}
	list := func(directory string) (M, error) {
		upstream, er := call("/files", url.Values{"agentSpaceId": {space}, "path": {directory}}, "GET", nil, nil)
		if er != nil {
			return nil, er
		}
		value, er := responseJSON(upstream, "WorkspaceFileList")
		if er != nil {
			return nil, er
		}
		for _, v := range A(value["entries"]) {
			entry := O(v)
			entryPath, er := WorkspacePath(S(entry["path"]), false)
			if er != nil {
				return nil, er
			}
			parent := path.Dir(entryPath)
			if parent == "." {
				parent = ""
			}
			if parent != directory {
				return nil, Problem(422, "invalid_platform_data", "目录响应包含无效路径")
			}
		}
		return value, nil
	}
	if route == "/files" {
		if r.Method == "GET" {
			value, er := list(filePath)
			if er != nil {
				return er
			}
			WriteJSON(w, 200, value)
			return nil
		}
		value, er := ReadJSON(r.Body, 4<<20)
		if er != nil {
			return er
		}
		if er = Check("WorkspaceFileMutation", value); er != nil {
			return er
		}
		if _, er = WorkspacePath(S(value["path"]), false); er != nil {
			return er
		}
		if Has(value, "destination") {
			if _, er = WorkspacePath(S(value["destination"]), false); er != nil {
				return er
			}
		}
		upstream, er := call("/files", url.Values{"agentSpaceId": {space}}, "POST", bytes.NewReader(JSON(value)), http.Header{"Content-Type": {"application/json"}})
		if er != nil {
			return er
		}
		out, er := responseJSON(upstream, "WorkspaceFileResult")
		if er != nil {
			return er
		}
		WriteJSON(w, 200, out)
		return nil
	}
	if route == "/archive" {
		raw, er := DecodeJSON([]byte(r.URL.Query().Get("paths")), 16384)
		if er != nil {
			return er
		}
		paths := Strings(raw)
		if len(paths) < 1 || len(paths) > 100 {
			return Problem(400, "invalid_workspace_path", "请选择 1–100 个文件或目录")
		}
		roots := []string{}
		for _, p := range paths {
			if _, er = WorkspacePath(p, false); er != nil {
				return er
			}
			redundant := false
			for _, other := range paths {
				if other != p && strings.HasPrefix(p, other+"/") {
					redundant = true
				}
			}
			if !redundant && !Contains(roots, p) {
				roots = append(roots, p)
			}
		}
		entries := []M{}
		seen := map[string]bool{}
		var visit func(M, int) error
		visit = func(entry M, depth int) error {
			if depth > 32 || len(entries) >= 10000 {
				return Problem(413, "workspace_archive_limit", "目录过大，请分批下载")
			}
			p := S(entry["path"])
			if seen[p] {
				return nil
			}
			seen[p] = true
			entries = append(entries, entry)
			if S(entry["kind"]) == "DIRECTORY" {
				directory, er := list(p)
				if er != nil {
					return er
				}
				for _, child := range A(directory["entries"]) {
					if er = visit(O(child), depth+1); er != nil {
						return er
					}
				}
			}
			return nil
		}
		for _, root := range roots {
			parent := path.Dir(root)
			if parent == "." {
				parent = ""
			}
			directory, er := list(parent)
			if er != nil {
				return er
			}
			found := false
			for _, v := range A(directory["entries"]) {
				entry := O(v)
				if S(entry["path"]) == root {
					found = true
					if er = visit(entry, 0); er != nil {
						return er
					}
					break
				}
			}
			if !found {
				return Problem(404, "workspace_missing", "文件已不存在，请刷新")
			}
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="workspace.zip"`)
		w.WriteHeader(200)
		archive := zip.NewWriter(w)
		for _, entry := range entries {
			name := "workspace/" + S(entry["path"])
			header := &zip.FileHeader{Name: name, Method: zip.Deflate}
			if S(entry["kind"]) == "DIRECTORY" {
				header.Name += "/"
				header.Method = zip.Store
			}
			output, er := archive.CreateHeader(header)
			if er != nil {
				panic(http.ErrAbortHandler)
			}
			if S(entry["kind"]) != "DIRECTORY" {
				headers := http.Header{}
				if S(entry["etag"]) != "" {
					headers.Set("If-Match", S(entry["etag"]))
				}
				upstream, er := call("/content", url.Values{"agentSpaceId": {space}, "path": {S(entry["path"])}}, "GET", nil, headers)
				if er != nil {
					panic(http.ErrAbortHandler)
				}
				_, er = io.CopyBuffer(output, upstream.Body, make([]byte, 64<<10))
				upstream.Body.Close()
				if er != nil {
					panic(http.ErrAbortHandler)
				}
			}
		}
		if er = archive.Close(); er != nil {
			panic(http.ErrAbortHandler)
		}
		return nil
	}
	if _, er = WorkspacePath(filePath, false); er != nil {
		return er
	}
	headers := http.Header{}
	for _, name := range []string{"Range", "If-Match", "If-None-Match", "Content-Type", "Content-Length"} {
		if value := r.Header.Get(name); value != "" {
			headers.Set(name, value)
		}
	}
	if version := r.URL.Query().Get("version"); version != "" {
		if !strongETag(version) {
			return Problem(400, "invalid_version", "文件版本无效")
		}
		headers.Set("If-Match", version)
	}
	var body io.Reader
	if r.Method == "PUT" {
		_ = http.NewResponseController(w).EnableFullDuplex()
		body = struct{ io.Reader }{r.Body}
	}
	upstream, er := call("/content", query, r.Method, body, headers)
	if er != nil {
		return er
	}
	defer upstream.Body.Close()
	if r.Method == "PUT" {
		value, er := responseJSON(upstream, "WorkspaceFileResult")
		if er != nil {
			return er
		}
		WriteJSON(w, 200, value)
		return nil
	}
	for _, name := range []string{"ETag", "Last-Modified", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if value := upstream.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
	mimeType := "application/octet-stream"
	disposition := "attachment"
	if r.URL.Query().Get("preview") == "1" {
		candidate := MIMEType(filePath)
		if strings.HasPrefix(candidate, "image/") && candidate != "image/svg+xml" || strings.HasPrefix(candidate, "video/") || strings.HasPrefix(candidate, "audio/") || candidate == "application/pdf" {
			mimeType = candidate
			disposition = "inline"
		}
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", disposition+"; filename*=UTF-8''"+url.PathEscape(path.Base(filePath)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(upstream.StatusCode)
	if r.Method != "HEAD" {
		if _, er = io.CopyBuffer(w, upstream.Body, make([]byte, 64<<10)); er != nil {
			panic(http.ErrAbortHandler)
		}
	}
	return nil
}

var spaceIDPattern = regexpSpaceID()

func regexpSpaceID() *regexp.Regexp { return regexp.MustCompile(`^spc_[0-9a-f-]{36}$`) }
