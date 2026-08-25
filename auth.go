package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// 认证相关全局状态
// ---------------------------------------------------------------------------

var (
	authUser   = "pilot"
	authPass   = "startrack"
	secretKey  string
	jwtSecret  []byte
	mu         sync.Mutex
	nonceStore = map[string]time.Time{}
)

func init() {
	buf := make([]byte, 32)
	rand.Read(buf)
	secretKey = hex.EncodeToString(buf)
	jwtSecret = []byte(secretKey)
}

// ---------------------------------------------------------------------------
// JWT 令牌
// ---------------------------------------------------------------------------

func encodeHex(src []byte) string {
	return hex.EncodeToString(src)
}

func createToken(username string) (string, error) {
	header := `{"alg":"HS256","typ":"JWT"}`
	now := time.Now().Unix()
	payload := fmt.Sprintf(`{"sub":"%s","iat":%d,"exp":%d}`, username, now, now+86400*7)

	b64 := func(b []byte) string {
		return strings.TrimRight(encodeHex(b), "=")
	}

	headerB64 := b64([]byte(header))
	payloadB64 := b64([]byte(payload))

	sig := sha256.Sum256([]byte(headerB64 + "." + payloadB64 + string(jwtSecret)))
	sigB64 := b64(sig[:])

	return headerB64 + "." + payloadB64 + "." + sigB64, nil
}

func verifyToken(token string) (string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", false
	}
	sig := sha256.Sum256([]byte(parts[0] + "." + parts[1] + string(jwtSecret)))
	expected := encodeHex(sig[:])
	if parts[2] != expected {
		return "", false
	}
	payloadHex := parts[1]
	payloadBytes, err := hex.DecodeString(payloadHex)
	if err != nil {
		return "", false
	}
	var claims struct {
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return "", false
	}
	if time.Now().Unix() > claims.Exp {
		return "", false
	}
	return claims.Sub, true
}

func extractToken(r *http.Request) string {
	t := r.Header.Get("X-CSRF-Token")
	if t != "" {
		return t
	}
	t = r.URL.Query().Get("token")
	if t != "" {
		return t
	}
	// 从 form body 取（auth 的 logout）
	t = r.FormValue("csrf_token")
	return t
}

// ---------------------------------------------------------------------------
// 中间件
// ---------------------------------------------------------------------------

func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			token := extractToken(r)
			user, ok := verifyToken(token)
			if !ok {
				http.Error(w, `{"error":"未登录"}`, http.StatusUnauthorized)
				return
			}
			r.Header.Set("X-Auth-User", user)
		}
		next(w, r)
	}
}

func requireCsrf(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "DELETE" {
			token := extractToken(r)
			_, ok := verifyToken(token)
			if !ok {
				http.Error(w, `{"error":"CSRF 校验失败"}`, http.StatusForbidden)
				return
			}
		}
		next(w, r)
	}
}

// ---------------------------------------------------------------------------
// 认证接口
// ---------------------------------------------------------------------------

func handleAuth(w http.ResponseWriter, r *http.Request) {
	action := r.FormValue("action")
	if action == "" {
		action = r.URL.Query().Get("action")
	}

	switch action {
	case "challenge":
		mu.Lock()
		nonce := make([]byte, 16)
		rand.Read(nonce)
		nonceStr := hex.EncodeToString(nonce)
		nonceStore[nonceStr] = time.Now().Add(5 * time.Minute)
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"nonce": nonceStr})

	case "login":
		user := strings.TrimSpace(r.FormValue("username"))
		clientHash := strings.ToLower(strings.TrimSpace(r.FormValue("hash")))
		nonce := r.FormValue("nonce")

		mu.Lock()
		_, nonceOK := nonceStore[nonce]
		if nonce != "" && nonceOK {
			delete(nonceStore, nonce)
		}
		mu.Unlock()

		if nonce != "" && !nonceOK {
			jsonError(w, "验证令牌无效或已过期", http.StatusBadRequest)
			return
		}

		expected := sha256.Sum256([]byte(nonce + authPass))
		expectedHash := hex.EncodeToString(expected[:])

		if user == authUser && clientHash == expectedHash {
			token, err := createToken(user)
			if err != nil {
				jsonError(w, "生成令牌失败", http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":    true,
				"csrf_token": token,
			})
		} else {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "用户名或密码不对",
			})
		}

	case "logout":
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
		})

	case "check":
		token := extractToken(r)
		_, ok := verifyToken(token)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"logged_in": ok,
		})

	default:
		jsonError(w, "未知操作", http.StatusBadRequest)
	}
}
