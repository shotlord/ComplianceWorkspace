package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"compliance-workspace/internal/domain"
)

const (
	CSRFCookieName       = "cw_csrf_token"
	CSRFHeaderName       = "X-CSRF-Token"
	maxJSONRequestBody   = 1 << 20
	maxImportRequestBody = domain.MaxImportFileSize + (1 << 20)
)

// GenerateRandomToken membuat token hex acak 32-byte
func GenerateRandomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func validLocalAuthority(authority, expectedPort string) bool {
	host, port, err := net.SplitHostPort(strings.TrimSpace(authority))
	if err != nil || port != expectedPort {
		return false
	}
	host = strings.ToLower(host)
	return host == "127.0.0.1" || host == "localhost"
}

// HostValidatorMiddleware hanya menerima authority loopback dengan port listener yang tepat.
func HostValidatorMiddleware(expectedAddr string, next http.Handler) http.Handler {
	_, expectedPort, err := net.SplitHostPort(expectedAddr)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err != nil || !validLocalAuthority(r.Host, expectedPort) {
			http.Error(w, "Permintaan ditolak: Host tidak valid. Hanya 127.0.0.1 dan localhost yang diizinkan.", http.StatusBadRequest)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// SecurityHeadersMiddleware menambahkan header keamanan standar
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func validCSRFToken(token string) bool {
	if len(token) != 64 {
		return false
	}
	_, err := hex.DecodeString(token)
	return err == nil
}

func validLocalSource(rawSource, expectedPort string, allowPath bool) bool {
	u, err := url.Parse(rawSource)
	if err != nil || u.Scheme != "http" || u.User != nil || !validLocalAuthority(u.Host, expectedPort) {
		return false
	}
	if !allowPath && ((u.Path != "" && u.Path != "/") || u.RawQuery != "") {
		return false
	}
	return u.Fragment == ""
}

// CSRFAndOriginMiddleware memvalidasi Origin/Referer dan token CSRF untuk request state-changing.
func CSRFAndOriginMiddleware(expectedAddr string, next http.Handler) http.Handler {
	_, expectedPort, addrErr := net.SplitHostPort(expectedAddr)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Dapatkan atau buat cookie CSRF
		var token string
		cookie, err := r.Cookie(CSRFCookieName)
		if err != nil || !validCSRFToken(cookie.Value) {
			newToken, err := GenerateRandomToken()
			if err != nil {
				http.Error(w, "Kesalahan server: gagal membuat token CSRF.", http.StatusInternalServerError)
				return
			}
			token = newToken
			http.SetCookie(w, &http.Cookie{
				Name:     CSRFCookieName,
				Value:    newToken,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
			r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: newToken})
		} else {
			token = cookie.Value
		}

		// Metode pembacaan aman tidak memerlukan validasi CSRF
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		// Validasi Origin jika ada
		if addrErr != nil {
			http.Error(w, "Akses ditolak: konfigurasi listener tidak valid.", http.StatusForbidden)
			return
		}
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" {
			if !validLocalSource(origin, expectedPort, false) {
				http.Error(w, "Akses ditolak: Origin tidak diizinkan.", http.StatusForbidden)
				return
			}
		} else {
			// Jika Origin kosong, periksa Referer jika ada
			referer := strings.TrimSpace(r.Header.Get("Referer"))
			if referer != "" {
				if !validLocalSource(referer, expectedPort, true) {
					http.Error(w, "Akses ditolak: Referer tidak diizinkan.", http.StatusForbidden)
					return
				}
			}
		}

		// Validasi CSRF Token
		reqToken := strings.TrimSpace(r.Header.Get(CSRFHeaderName))
		if !validCSRFToken(reqToken) || subtle.ConstantTimeCompare([]byte(reqToken), []byte(token)) != 1 {
			http.Error(w, "Akses ditolak: token CSRF tidak valid atau tidak disertakan.", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RequestBodyLimitMiddleware membatasi payload sebelum parser JSON/multipart mengalokasikan memori.
func RequestBodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.Body != http.NoBody {
			limit := int64(maxJSONRequestBody)
			if r.URL.Path == "/api/import/preview" || r.URL.Path == "/api/import/commit" {
				limit = int64(maxImportRequestBody)
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}

// DatabaseOperationMiddleware menahan request biasa selama file database sedang diganti.
func DatabaseOperationMiddleware(gate *sync.RWMutex, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isRestore := r.Method == http.MethodPost && (r.URL.Path == "/api/backups/restore" || r.URL.Path == "/api/backup/restore")
		isServiceGatedRecurring := r.Method == http.MethodPost && r.URL.Path == "/api/recurring-schedules/trigger"
		if gate == nil || isRestore || isServiceGatedRecurring {
			next.ServeHTTP(w, r)
			return
		}
		gate.RLock()
		defer gate.RUnlock()
		next.ServeHTTP(w, r)
	})
}
