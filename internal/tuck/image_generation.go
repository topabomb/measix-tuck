package tuck

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/netip"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const imageProtocol = "OPENAI_IMAGES_GENERATIONS"

// IsGlobalUnicast includes reserved/documentation networks. Provider-returned
// URLs must not reach those networks, shared address space or translation tunnels.
var imageReservedNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
}

func publicImageIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	if addr.Is6() && addr.As16()[0]&224 != 32 {
		return false
	}
	for _, prefix := range imageReservedNetworks {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

func validateImageConfig(c M) error {
	fail := func() error {
		return Problem(422, "invalid_config", "生图资源的标识、连接、协议或返回格式无效")
	}
	if Has(c, "imageGenerators") && (!isArray(c["imageGenerators"]) || len(A(c["imageGenerators"])) > 100) {
		return fail()
	}
	seen := map[string]bool{}
	for _, value := range A(c["imageGenerators"]) {
		r := O(value)
		id := S(r["imageId"])
		if !noUnknown(r, "imageId", "providerId", "displayName", "enabled", "protocol", "upstreamModelKey", "responseFormat") || !resourceID.MatchString(id) || seen[id] || !resourceID.MatchString(S(r["providerId"])) || !stringField(r["displayName"], 1, 80) || !stringField(r["upstreamModelKey"], 1, 256) || !isBool(r["enabled"]) || S(r["protocol"]) != imageProtocol || Has(r, "responseFormat") && (!stringField(r["responseFormat"], 0, 16) || !Contains([]string{"", "b64_json", "url"}, S(r["responseFormat"]))) {
			return fail()
		}
		seen[id] = true
		found := false
		for _, p := range A(c["providers"]) {
			if O(p)["id"] == r["providerId"] {
				found = true
			}
		}
		if !found {
			return Problem(422, "invalid_config", "生图资源引用的提供商不存在")
		}
	}
	if Has(c, "defaultImageGenerator") {
		if !validImageSelection(c["defaultImageGenerator"]) {
			return fail()
		}
	}
	for _, agent := range O(c["agents"]) {
		if a := O(agent); Has(a, "imageGenerator") && !validImageSelection(a["imageGenerator"]) {
			return fail()
		}
	}
	return nil
}

func validImageSelection(value any) bool {
	r, ok := value.(map[string]any)
	return ok && noUnknown(r, "source", "imageId") && (len(r) == 0 || Contains([]string{"local", "platform"}, S(r["source"])) && resourceID.MatchString(S(r["imageId"])))
}

func agentImageSelector(config M, agent string) string {
	ref := O(O(O(config["agents"])[agent])["imageGenerator"])
	if len(ref) == 0 {
		return ""
	}
	return S(ref["source"]) + ":" + S(ref["imageId"])
}

// The catalog contains no connection credentials. Unavailable resources stay visible.
func imageCatalog(config, binding M) []M {
	list := []M{}
	for _, value := range A(config["imageGenerators"]) {
		r := Merge(O(value), M{"source": "local"})
		r["providerEnabled"] = false
		for _, p := range A(config["providers"]) {
			if O(p)["id"] == r["providerId"] {
				r["providerEnabled"] = O(p)["enabled"]
				r["providerName"] = O(p)["displayName"]
			}
		}
		list = append(list, r)
	}
	for _, value := range A(O(binding["snapshot"])["imageGenerators"]) {
		r := O(value)
		list = append(list, Merge(r, M{"source": "platform", "protocol": r["clientProtocol"]}))
	}
	for _, r := range list {
		reason := ""
		if !B(r["enabled"]) || r["providerEnabled"] == false {
			reason = "已停用或连接不可用"
		} else if S(r["protocol"]) != imageProtocol {
			reason = "尚未支持该生图协议"
		}
		r["selectable"], r["reason"] = reason == "", reason
		r["selector"] = S(r["source"]) + ":" + S(r["imageId"])
	}
	return list
}

type ImageGenerationRequest struct {
	Resource, Prompt string
	Count            int
	References       []ImageReference
}
type ImageReference struct {
	Bytes []byte
	MIME  string
}
type GeneratedImage struct {
	Bytes, ModelBytes []byte
	MIME, ModelMIME   string
}
type ImageGenerationResult struct {
	Images   []GeneratedImage
	Resource M
	Usage    M
}

func resolveImage(config, binding M, selector string) (M, error) {
	if selector == "" {
		ref := O(config["defaultImageGenerator"])
		if len(ref) > 0 {
			selector = S(ref["source"]) + ":" + S(ref["imageId"])
		} else if id := S(O(O(binding["snapshot"])["policy"])["defaultImageGenerationId"]); id != "" {
			selector = "platform:" + id
		}
	}
	for _, r := range imageCatalog(config, binding) {
		if S(r["selector"]) == selector && B(r["selectable"]) {
			return r, nil
		}
	}
	return nil, Problem(428, "image_resource_unavailable", "请选择可用的生图资源；未设置默认资源或原选择已不可用")
}

// GenerateImage is a bounded provider operation, independent of MCP and files.
// It never retries or falls back to a different resource/protocol.
func (m *Models) GenerateImage(ctx context.Context, config, binding M, input ImageGenerationRequest) (*ImageGenerationResult, error) {
	r, e := resolveImage(config, binding, input.Resource)
	if e != nil {
		return nil, e
	}
	if !stringField(input.Prompt, 1, 32000) || input.Count < 1 || input.Count > 4 || len(input.References) > 4 {
		return nil, Problem(422, "invalid_arguments", "提示词无效；生成数量为 1–4 张，参考图最多 4 张")
	}
	if S(r["source"]) == "platform" {
		if input.Count > I(r["maxImagesPerRequest"]) {
			return nil, Problem(422, "invalid_arguments", "生成数量超过平台资源许可上限")
		}
		// The published platform contract grants only runtimePath. Do not infer
		// an edits route or send uncontracted reference fields to that route.
		if len(input.References) > 0 {
			return nil, Problem(422, "image_references_unsupported", "平台合同尚未提供参考图编辑路由，请使用支持 OpenAI Images Edits 的本地配置资源")
		}
	}
	resource := Merge(r, M{"resourceId": r["imageId"]})
	if S(r["source"]) == "local" {
		resource["runtimePath"] = "/images/generations"
		if len(input.References) > 0 {
			resource["runtimePath"] = "/images/edits"
		}
	}
	target, headers, e := m.resourceAccess(ctx, config, binding, resource)
	if e != nil {
		return nil, e
	}
	body := M{"model": r["upstreamModelKey"], "prompt": input.Prompt, "n": input.Count}
	// The platform contract requires an explicit automatic size. Local Images
	// requests continue to omit dimensions; this is not an Agent parameter.
	if S(r["source"]) == "platform" {
		body["size"] = "auto"
	}
	// GPT image models reject response_format. Only send an explicit resource setting.
	if S(r["responseFormat"]) != "" {
		body["response_format"] = r["responseFormat"]
	}
	encoded := JSON(body)
	if len(input.References) > 0 {
		var contentType string
		encoded, contentType, e = imageEditBody(body, input.References)
		if e != nil {
			return nil, e
		}
		headers.Set("Content-Type", contentType)
	}
	req, e := http.NewRequestWithContext(ctx, "POST", target, bytes.NewReader(encoded))
	if e != nil {
		return nil, e
	}
	req.Header = headers
	resp, e := m.client.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctxError(ctx)
		}
		return nil, modelTransportError(e, "image_unreachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		payload, _ := ReadJSON(resp.Body, 1<<20)
		if ctx.Err() != nil {
			return nil, ctxError(ctx)
		}
		return nil, modelToolError(resp.StatusCode, payload, m.Secret, strings.TrimPrefix(headers.Get("Authorization"), "Bearer "))
	}
	payload, e := ReadJSON(resp.Body, 72<<20)
	if ctx.Err() != nil {
		return nil, ctxError(ctx)
	}
	if e != nil {
		return nil, modelTransportError(e, "invalid_image_response")
	}
	items := A(payload["data"])
	if len(items) != input.Count {
		return nil, Problem(502, "invalid_image_response", "生图服务返回的图片数量与请求不符；本次未自动重试")
	}
	usage := M{}
	for _, key := range []string{"input_tokens", "output_tokens", "total_tokens"} {
		if value := O(payload["usage"])[key]; Integer(value, 0, 1000000000000) {
			usage[key] = value
		}
	}
	result := &ImageGenerationResult{Resource: r, Usage: usage}
	total, modelTotal := 0, 0
	for _, item := range items {
		if e = ctxError(ctx); e != nil {
			return nil, e
		}
		v := O(item)
		var raw []byte
		if encoded := S(v["b64_json"]); encoded != "" {
			if len(encoded) > (MaxUploadBytes+2)/3*4 {
				return nil, Problem(502, "image_limit", "生成的单张图片超过 16 MiB")
			}
			raw, e = base64.StdEncoding.Strict().DecodeString(encoded)
		} else {
			raw, e = downloadGeneratedImage(ctx, S(v["url"]))
		}
		if ctx.Err() != nil {
			return nil, ctxError(ctx)
		}
		if e != nil {
			return nil, modelTransportError(e, "invalid_image_response")
		}
		total += len(raw)
		if len(raw) > MaxUploadBytes || total > 48<<20 {
			return nil, Problem(502, "image_limit", "生成图片超过大小限制")
		}
		model, mime, e := PrepareModelImage(raw)
		if e != nil {
			return nil, Problem(502, "invalid_image", "生图服务返回了无效或不支持的图片")
		}
		modelTotal += len(model)
		if modelTotal > 16<<20 {
			return nil, Problem(502, "image_limit", "图片模型表示超过 16 MiB")
		}
		result.Images = append(result.Images, GeneratedImage{Bytes: raw, MIME: ImageType(raw), ModelBytes: model, ModelMIME: mime})
	}
	return result, nil
}

// One official wire format, independent of provider host or model name. No
// size/resolution fields and no fallback to text-only generation on rejection.
func imageEditBody(fields M, references []ImageReference) ([]byte, string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for _, key := range []string{"model", "prompt", "n", "response_format"} {
		if value, ok := fields[key]; ok {
			if e := w.WriteField(key, fmt.Sprint(value)); e != nil {
				return nil, "", e
			}
		}
	}
	total := 0
	for i, ref := range references {
		total += len(ref.Bytes)
		ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}[ref.MIME]
		if ext == "" || ImageType(ref.Bytes) != ref.MIME || len(ref.Bytes) == 0 || len(ref.Bytes) > MaxUploadBytes || total > 48<<20 {
			return nil, "", Problem(422, "invalid_image_reference", "参考图须为 PNG、JPEG 或 WebP；每张最多 16 MiB，合计最多 48 MiB")
		}
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="image[]"; filename="reference_`+strconv.Itoa(i+1)+ext+`"`)
		header.Set("Content-Type", ref.MIME)
		part, e := w.CreatePart(header)
		if e != nil {
			return nil, "", e
		}
		if _, e = part.Write(ref.Bytes); e != nil {
			return nil, "", e
		}
	}
	if e := w.Close(); e != nil {
		return nil, "", e
	}
	return body.Bytes(), w.FormDataContentType(), nil
}

// Provider-returned URLs are untrusted. Fetch public HTTPS only, without credentials,
// redirects, cookies or proxy routing that could bypass the address check.
func downloadGeneratedImage(ctx context.Context, address string) ([]byte, error) {
	u, e := url.Parse(address)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, Problem(502, "image_url", "图片地址必须为公开 HTTPS 地址")
	}
	transport := newTransport(60 * time.Second)
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(ctx, httpConnectTimeout)
		defer cancel()
		host, port, e := net.SplitHostPort(addr)
		if e != nil {
			return nil, e
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil {
			return nil, e
		}
		for _, ip := range ips {
			if !publicImageIP(ip.IP) {
				return nil, Problem(502, "image_url", "图片地址不允许访问本地网络")
			}
		}
		if len(ips) == 0 {
			return nil, Problem(502, "image_url", "图片地址无法解析")
		}
		return (&net.Dialer{Timeout: httpConnectTimeout}).DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "GET", address, nil)
	if e != nil {
		return nil, e
	}
	resp, e := (&http.Client{Transport: transport, CheckRedirect: noRedirect}).Do(req)
	if e != nil {
		return nil, e
	}
	resp.Body = watchIdle(resp.Body, fileTransferIdleTimeout, cancel)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, Problem(502, "image_download", "图片下载失败")
	}
	return io.ReadAll(io.LimitReader(resp.Body, MaxUploadBytes+1))
}
