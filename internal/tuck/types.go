package tuck

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// M is the extensible public JSON projection. Owners never share mutable projections.
type M = map[string]any

type HTTPError struct {
	Status        int
	Code, Message string
	RetryAfter    int
}

func (e *HTTPError) Error() string { return e.Message }
func Problem(status int, code, message string) error {
	return &HTTPError{Status: status, Code: code, Message: message}
}
func Status(err error) int {
	var e *HTTPError
	if errors.As(err, &e) {
		return e.Status
	}
	return 500
}
func Code(err error) string {
	var e *HTTPError
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
func O(v any) M {
	if x, ok := v.(map[string]any); ok && x != nil {
		return x
	}
	return M{}
}
func A(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case []M:
		r := make([]any, len(x))
		for i := range x {
			r[i] = x[i]
		}
		return r
	case []string:
		r := make([]any, len(x))
		for i := range x {
			r[i] = x[i]
		}
		return r
	}
	return []any{}
}
func S(v any) string {
	if x, ok := v.(string); ok {
		return x
	}
	return ""
}
func N(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		n, _ := x.Float64()
		return n
	}
	return 0
}
func I(v any) int  { return int(N(v)) }
func B(v any) bool { b, _ := v.(bool); return b }
func Strings(v any) []string {
	r := []string{}
	for _, x := range A(v) {
		r = append(r, S(x))
	}
	return r
}
func Has(v any, key string) bool { _, ok := O(v)[key]; return ok }
func Contains(v any, value string) bool {
	for _, x := range A(v) {
		if S(x) == value {
			return true
		}
	}
	return false
}
func Clone(v M) M {
	if v == nil {
		return nil
	}
	raw, _ := json.Marshal(v)
	var r M
	_ = json.Unmarshal(raw, &r)
	return r
}
func Merge(values ...M) M {
	r := M{}
	for _, v := range values {
		for k, x := range v {
			r[k] = x
		}
	}
	return r
}
func NowMS() int64         { return time.Now().UnixMilli() }
func NowISO() string       { return time.Now().UTC().Format(time.RFC3339Nano) }
func Hash(v []byte) string { x := sha256.Sum256(v); return hex.EncodeToString(x[:]) }
func Token() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func ID() string {
	const chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	b := make([]byte, 16)
	for i := 0; i < len(b); {
		var x [1]byte
		if _, err := rand.Read(x[:]); err != nil {
			panic(err)
		}
		if x[0] < 248 {
			b[i] = chars[int(x[0])%62]
			i++
		}
	}
	return string(b)
}
func UUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func Equal(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
func Integer(v any, min, max int) bool {
	n, ok := v.(float64)
	if !ok {
		switch v.(type) {
		case int, int64, json.Number:
			n = N(v)
			ok = true
		}
	}
	return ok && !math.IsNaN(n) && math.Trunc(n) == n && n >= float64(min) && n <= float64(max)
}
func JSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
func DecodeJSON(raw []byte, limit int64) (any, error) {
	if int64(len(raw)) > limit {
		return nil, Problem(413, "invalid_json", "JSON 大小超限")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var visit func(int) (any, error)
	visit = func(depth int) (any, error) {
		if depth > 128 {
			return nil, Problem(400, "invalid_json", "JSON 嵌套超限")
		}
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		delim, container := t.(json.Delim)
		if !container {
			if n, ok := t.(json.Number); ok {
				return strconv.ParseFloat(string(n), 64)
			}
			return t, nil
		}
		switch delim {
		case '{':
			m := M{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return nil, e
				}
				key, ok := k.(string)
				if !ok {
					return nil, fmt.Errorf("invalid key")
				}
				if _, ok = m[key]; ok {
					return nil, Problem(400, "invalid_json", "JSON 字段重复")
				}
				v, e := visit(depth + 1)
				if e != nil {
					return nil, e
				}
				m[key] = v
			}
			end, e := d.Token()
			if e != nil || end != json.Delim('}') {
				return nil, fmt.Errorf("invalid object")
			}
			return m, nil
		case '[':
			a := []any{}
			for d.More() {
				v, e := visit(depth + 1)
				if e != nil {
					return nil, e
				}
				a = append(a, v)
			}
			end, e := d.Token()
			if e != nil || end != json.Delim(']') {
				return nil, fmt.Errorf("invalid array")
			}
			return a, nil
		}
		return nil, fmt.Errorf("invalid delimiter")
	}
	v, err := visit(0)
	if err == nil {
		_, e := d.Token()
		if e != io.EOF {
			err = fmt.Errorf("trailing JSON")
		}
	}
	if err != nil {
		var h *HTTPError
		if errors.As(err, &h) {
			return nil, err
		}
		return nil, Problem(400, "invalid_json", "JSON 格式无效")
	}
	return v, nil
}
func ReadJSON(r io.Reader, limit int64) (M, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		b = []byte("{}")
	}
	v, err := DecodeJSON(b, limit)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, Problem(400, "invalid_json", "JSON 必须为对象")
	}
	return m, nil
}
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func WriteError(w http.ResponseWriter, err error) {
	var e *HTTPError
	if errors.As(err, &e) && e.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(e.RetryAfter))
	}
	WriteJSON(w, Status(err), M{"error": err.Error(), "code": Code(err)})
}
func cleanSecret(text string, secrets ...string) string {
	for _, s := range secrets {
		if s != "" {
			text = strings.ReplaceAll(text, s, "[REDACTED]")
		}
	}
	return text
}
