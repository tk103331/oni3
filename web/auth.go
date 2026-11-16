package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const (
	authCookieName = "oni3_web_session"
	authQueryParam = "token"
	authCookieAge  = 7 * 24 * time.Hour
)

// authCookieKey derives the cookie value from the configured token, so a
// restart with a different token invalidates cookies issued earlier.
var authCookieKey = sha256.Sum256([]byte("oni3-web-auth-cookie-v1"))

// hashToken keeps only a digest of the token in memory.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func tokenCookieValue(token string) string {
	mac := hmac.New(sha256.New, authCookieKey[:])
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

// requestToken extracts a token from the supported transports. The query
// parameter exists so the URL printed by `oni3 web` can be pasted into a
// browser; the interface immediately exchanges it for an HttpOnly cookie.
func requestToken(r *http.Request) string {
	if header := strings.TrimSpace(r.Header.Get("Authorization")); header != "" {
		if len(header) > 7 && strings.EqualFold(header[:7], "bearer ") {
			return strings.TrimSpace(header[7:])
		}
	}
	if token := strings.TrimSpace(r.Header.Get("X-Oni3-Token")); token != "" {
		return token
	}
	return strings.TrimSpace(r.URL.Query().Get(authQueryParam))
}

func (s *Server) validToken(token string) bool {
	if s.tokenHash == "" {
		return true
	}
	if token == "" {
		return false
	}
	sum := hashToken(token)
	return subtle.ConstantTimeCompare([]byte(sum), []byte(s.tokenHash)) == 1
}

func (s *Server) authorized(r *http.Request) bool {
	if s.tokenHash == "" {
		return true
	}
	if s.validToken(requestToken(r)) {
		return true
	}
	if cookie, err := r.Cookie(authCookieName); err == nil {
		return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(s.authCookie)) == 1
	}
	return false
}

func (s *Server) setAuthCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    s.authCookie,
		Path:     "/",
		MaxAge:   int(authCookieAge / time.Second),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) clearAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

type loginRequest struct {
	Token string `json:"token"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.tokenHash == "" {
		writeError(w, http.StatusBadRequest, "token authentication is not enabled")
		return
	}
	var req loginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if !s.validToken(strings.TrimSpace(req.Token)) {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}
	s.setAuthCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.clearAuthCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAuth(w http.ResponseWriter, r *http.Request) {
	if s.tokenHash == "" {
		writeError(w, http.StatusBadRequest, "token authentication is not enabled")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
}
