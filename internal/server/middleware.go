package server

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
)

const (
	CSRFCookieName = "cw_csrf_token"
	CSRFHeaderName = "X-CSRF-Token"
)

// GenerateRandomToken membuat token hex acak 32-byte
func GenerateRandomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// HostValidatorMiddleware memastikan request hanya berasal dari 127.0.0.1 atau localhost
func HostValidatorMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}

		host = strings.ToLower(strings.TrimSpace(host))
		if host != "127.0.0.1" && host != "localhost" {
			http.Error(w, "Bad Request: Host tidak valid. Hanya 127.0.0.1 dan localhost yang diizinkan.", http.StatusBadRequest)
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
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

// CSRFAndOriginMiddleware memvalidasi Origin/Referer dan token CSRF untuk request state-changing
func CSRFAndOriginMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Dapatkan atau buat cookie CSRF
		var token string
		cookie, err := r.Cookie(CSRFCookieName)
		if err != nil || cookie.Value == "" {
			newToken, err := GenerateRandomToken()
			if err == nil {
				token = newToken
				http.SetCookie(w, &http.Cookie{
					Name:     CSRFCookieName,
					Value:    newToken,
					Path:     "/",
					HttpOnly: false, // Dibaca oleh client-side JS jika dibutuhkan
					SameSite: http.SameSiteStrictMode,
				})
				r.AddCookie(&http.Cookie{
					Name:  CSRFCookieName,
					Value: newToken,
				})
			}
		} else {
			token = cookie.Value
		}

		// Metode pembacaan aman tidak memerlukan validasi CSRF
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		// Validasi Origin jika ada
		origin := r.Header.Get("Origin")
		if origin != "" {
			if !strings.HasPrefix(origin, "http://127.0.0.1:") &&
				!strings.HasPrefix(origin, "http://localhost:") &&
				origin != "http://127.0.0.1" &&
				origin != "http://localhost" {
				http.Error(w, "Forbidden: Origin tidak diizinkan", http.StatusForbidden)
				return
			}
		} else {
			// Jika Origin kosong, periksa Referer jika ada
			referer := r.Header.Get("Referer")
			if referer != "" {
				if !strings.HasPrefix(referer, "http://127.0.0.1:") &&
					!strings.HasPrefix(referer, "http://localhost:") &&
					!strings.HasPrefix(referer, "http://127.0.0.1/") &&
					!strings.HasPrefix(referer, "http://localhost/") {
					http.Error(w, "Forbidden: Referer tidak diizinkan", http.StatusForbidden)
					return
				}
			}
		}

		// Validasi CSRF Token
		reqToken := r.Header.Get(CSRFHeaderName)
		if reqToken == "" {
			reqToken = r.FormValue("csrf_token")
		}

		if reqToken == "" || reqToken != token {
			http.Error(w, "Forbidden: Token CSRF tidak valid atau tidak disertakan", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}
