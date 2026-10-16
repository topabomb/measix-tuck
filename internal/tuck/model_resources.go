package tuck

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// resourceAccess prepares one request from a captured resource/configuration.
// Platform remains the sole credential owner. Callers own protocol serialization.
func (m *Models) resourceAccess(ctx context.Context, config, binding, resource M) (string, http.Header, error) {
	headers := http.Header{"Content-Type": {"application/json"}}
	if S(resource["source"]) == "platform" {
		if _, e := PathOnly(S(resource["runtimePath"])); e != nil {
			return "", nil, e
		}
		identity := platformIdentity(binding)
		if identity == "" || identity != platformIdentity(m.Platform.Binding()) {
			return "", nil, Problem(409, "resource_changed", "平台资源已改变，请重新调用")
		}
		generation := I(O(binding["snapshot"])["managedGeneration"])
		if e := m.Platform.Admit(ctx, generation); e != nil {
			return "", nil, e
		}
		token, e := m.Platform.Access(ctx)
		if e != nil {
			return "", nil, e
		}
		if identity != platformIdentity(m.Platform.Binding()) {
			return "", nil, Problem(409, "resource_changed", "平台资源已改变，请重新调用")
		}
		headers.Set("Authorization", "Bearer "+token)
		headers.Set("X-Measix-Managed-Generation", fmt.Sprint(generation))
		headers.Set("X-Measix-Interaction-Id", "int_"+UUID())
		return S(binding["origin"]) + strings.TrimSuffix(S(O(binding["discovery"])["runtimeApiBase"]), "/") + "/resources/" + url.PathEscape(S(resource["resourceId"])) + S(resource["runtimePath"]), headers, nil
	}
	for _, value := range A(config["providers"]) {
		p := O(value)
		if p["id"] == resource["providerId"] && B(p["enabled"]) {
			if S(p["apiKey"]) != "" {
				headers.Set("Authorization", "Bearer "+S(p["apiKey"]))
			}
			return strings.TrimRight(S(p["baseUrl"]), "/") + S(resource["runtimePath"]), headers, nil
		}
	}
	return "", nil, Problem(428, "resource_unavailable", "资源连接不可用")
}
