package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

const sessionCookie = "lemontube_session"

type AuthUser struct {
	username [32]byte
	password [32]byte
}

// ParseAuthUsers parses a JSON array of username:password entries.
func ParseAuthUsers(value string) ([]AuthUser, error) {
	if value == "" {
		return nil, nil
	}
	var entries []string
	if err := json.Unmarshal([]byte(value), &entries); err != nil || len(entries) == 0 {
		return nil, errors.New("AUTH_USERS must be a non-empty JSON array of username:password strings")
	}
	users := make([]AuthUser, 0, len(entries))
	seen := make(map[string]bool)
	for _, entry := range entries {
		username, password, found := strings.Cut(entry, ":")
		if !found || strings.TrimSpace(username) != username || username == "" || password == "" || seen[username] {
			return nil, errors.New("AUTH_USERS contains an invalid or duplicate username")
		}
		seen[username] = true
		users = append(users, AuthUser{username: sha256.Sum256([]byte(username)), password: sha256.Sum256([]byte(password))})
	}
	return users, nil
}

type authentication struct {
	users    []AuthUser
	mutex    sync.Mutex
	sessions map[string]time.Time
	failures int
	resetAt  time.Time
}

func (a *authentication) allowed(username, password string) bool {
	userHash := sha256.Sum256([]byte(username))
	passwordHash := sha256.Sum256([]byte(password))
	match := 0
	for _, user := range a.users {
		match |= subtle.ConstantTimeCompare(userHash[:], user.username[:]) & subtle.ConstantTimeCompare(passwordHash[:], user.password[:])
	}
	return match == 1
}

func (a *authentication) login(c echo.Context) error {
	c.Response().Header().Set("Cache-Control", "no-store")
	if !sameOrigin(c.Request()) {
		return echo.ErrForbidden
	}
	a.mutex.Lock()
	defer a.mutex.Unlock()
	now := time.Now()
	if now.After(a.resetAt) {
		a.failures = 0
		a.resetAt = now.Add(time.Minute)
	}
	if a.failures >= 30 {
		return echo.NewHTTPError(http.StatusTooManyRequests, "Too many login attempts. Try again later.")
	}
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, 8192)
	if err := c.Request().ParseForm(); err != nil {
		return echo.ErrBadRequest
	}
	if !a.allowed(c.FormValue("username"), c.FormValue("password")) {
		a.failures++
		return c.Redirect(http.StatusSeeOther, "/login?error=1")
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return fmt.Errorf("generate session: %w", err)
	}
	for key, expires := range a.sessions {
		if now.After(expires) {
			delete(a.sessions, key)
		}
	}
	value := base64.RawURLEncoding.EncodeToString(token[:])
	a.sessions[value] = now.Add(24 * time.Hour)
	c.SetCookie(&http.Cookie{Name: sessionCookie, Value: value, Path: "/", HttpOnly: true, Secure: secureRequest(c.Request()), SameSite: http.SameSiteStrictMode, MaxAge: 86400})
	return c.Redirect(http.StatusSeeOther, "/")
}

func secureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && strings.EqualFold(u.Host, r.Host)
}

func (a *authentication) authenticated(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	a.mutex.Lock()
	defer a.mutex.Unlock()
	expires, ok := a.sessions[cookie.Value]
	if ok && time.Now().After(expires) {
		delete(a.sessions, cookie.Value)
		return false
	}
	return ok
}

func (a *authentication) middleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		r := c.Request()
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions && !sameOrigin(r) {
			return echo.ErrForbidden
		}
		if len(a.users) == 0 || r.URL.Path == "/health" || r.URL.Path == "/login" || r.URL.Path == "/styles.css" {
			return next(c)
		}
		if !a.authenticated(r) {
			if strings.HasPrefix(r.URL.Path, "/api/") || r.Method != http.MethodGet {
				return echo.ErrUnauthorized
			}
			return c.Redirect(http.StatusSeeOther, "/login")
		}
		return next(c)
	}
}
