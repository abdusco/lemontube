package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConcurrentSessionChecks(t *testing.T) {
	auth := newAuthentication(authenticationParams{})
	auth.sessions["valid"] = time.Now().Add(time.Hour)
	auth.sessions["expired"] = time.Now().Add(-time.Hour)
	var wg sync.WaitGroup
	for _, token := range []string{"valid", "expired", "missing"} {
		for range 20 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
				assert.Equal(t, token == "valid", auth.authenticated(req))
			}()
		}
	}
	wg.Wait()
	assert.NotContains(t, auth.sessions, "expired")
}

func TestAuthenticationLoginPage(t *testing.T) {
	users, err := ParseAuthUsers(`["alice:secret"]`)
	require.NoError(t, err)
	tests := []struct {
		name         string
		page         func() ([]byte, error)
		assertResult func(t *testing.T, response *httptest.ResponseRecorder)
	}{
		{
			name: "serves supplied page",
			page: func() ([]byte, error) { return []byte("custom login"), nil },
			assertResult: func(t *testing.T, response *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusOK, response.Code)
				assert.Equal(t, "custom login", response.Body.String())
				assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			},
		},
		{
			name: "handles page loading failure",
			page: func() ([]byte, error) { return nil, errors.New("page unavailable") },
			assertResult: func(t *testing.T, response *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusInternalServerError, response.Code)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := newAuthentication(authenticationParams{users: users, loginPage: tt.page})
			require.NotNil(t, auth.sessions)
			e := echo.New()
			auth.registerRoutes(e, rateLimit(30, time.Minute))
			response := httptest.NewRecorder()
			e.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/login", nil))
			tt.assertResult(t, response)
		})
	}
}

func TestParseAuthUsers(t *testing.T) {
	tests := []struct {
		name         string
		value        string
		assertResult func(t *testing.T, users []AuthUser, err error)
	}{
		{
			name: "unset disables authentication",
			assertResult: func(t *testing.T, users []AuthUser, err error) {
				require.NoError(t, err)
				assert.Empty(t, users)
			},
		},
		{
			name:  "multiple users and password with colon",
			value: `["user:pass:word","user2:pass2"]`,
			assertResult: func(t *testing.T, users []AuthUser, err error) {
				require.NoError(t, err)
				require.Len(t, users, 2)
				assert.True(t, users[0].MatchesCredentials("user", "pass:word"))
				assert.False(t, users[0].MatchesCredentials("user", "pass2"))
				assert.True(t, users[1].MatchesCredentials("user2", "pass2"))
				assert.False(t, users[1].MatchesCredentials("user", "pass2"))
			},
		},
		{
			name:  "empty array",
			value: `[]`,
			assertResult: func(t *testing.T, users []AuthUser, err error) {
				require.Error(t, err)
			},
		},
		{
			name:  "invalid json",
			value: `broken`,
			assertResult: func(t *testing.T, users []AuthUser, err error) {
				require.Error(t, err)
			},
		},
		{
			name:  "empty password",
			value: `["user:"]`,
			assertResult: func(t *testing.T, users []AuthUser, err error) {
				require.Error(t, err)
			},
		},
		{
			name:  "duplicate username",
			value: `["user:a","user:b"]`,
			assertResult: func(t *testing.T, users []AuthUser, err error) {
				require.Error(t, err)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users, err := ParseAuthUsers(tt.value)
			tt.assertResult(t, users, err)
		})
	}
}

func TestAuthentication(t *testing.T) {
	users, err := ParseAuthUsers(`["alice:secret","bob:second"]`)
	require.NoError(t, err)
	server := newTestServerWithUsers(t, users)

	request := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, req)
		return response
	}

	for _, tt := range []struct {
		name   string
		method string
		path   string
		status int
	}{
		{
			name:   "home",
			method: http.MethodGet,
			path:   "/",
			status: http.StatusSeeOther,
		},
		{
			name:   "jobs",
			method: http.MethodGet,
			path:   "/api/jobs",
			status: http.StatusUnauthorized,
		},
		{
			name:   "download",
			method: http.MethodGet,
			path:   "/download/file.mp4",
			status: http.StatusSeeOther,
		},
		{
			name:   "health",
			method: http.MethodGet,
			path:   "/health",
			status: http.StatusNoContent,
		},
		{
			name:   "login",
			method: http.MethodGet,
			path:   "/login",
			status: http.StatusOK,
		},
		{
			name:   "login stylesheet",
			method: http.MethodGet,
			path:   "/styles.css",
			status: http.StatusOK,
		},
		{
			name:   "app script",
			method: http.MethodGet,
			path:   "/app.js",
			status: http.StatusSeeOther,
		},
		{
			name:   "login logo",
			method: http.MethodGet,
			path:   "/lemontube.svg",
			status: http.StatusOK,
		},
		{
			name:   "session",
			method: http.MethodGet,
			path:   "/api/session",
			status: http.StatusUnauthorized,
		},
		{
			name:   "logout",
			method: http.MethodPost,
			path:   "/logout",
			status: http.StatusUnauthorized,
		},
	} {
		t.Run("unauthenticated "+tt.name, func(t *testing.T) {
			assert.Equal(t, tt.status, request(tt.method, tt.path, "", nil).Code)
		})
	}
	t.Run("invalid credentials", func(t *testing.T) {
		bad := request(http.MethodPost, "/login", "username=alice&password=wrong", nil)
		assert.Equal(t, "/login?error=1", bad.Header().Get("Location"))
		assert.Equal(t, "no-store", bad.Header().Get("Cache-Control"))
		assert.Empty(t, bad.Result().Cookies())
	})

	t.Run("login session and logout", func(t *testing.T) {
		form := url.Values{"username": {"bob"}, "password": {"second"}}
		response := request(http.MethodPost, "/login", form.Encode(), nil)
		require.Equal(t, http.StatusSeeOther, response.Code)
		require.Len(t, response.Result().Cookies(), 1)
		cookie := response.Result().Cookies()[0]
		assert.True(t, cookie.HttpOnly)
		assert.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
		assert.Equal(t, http.StatusOK, request(http.MethodGet, "/api/jobs", "", cookie).Code)
		session := request(http.MethodGet, "/api/session", "", cookie)
		assert.Equal(t, http.StatusOK, session.Code)
		assert.Equal(t, "no-store", session.Header().Get("Cache-Control"))
		assert.Equal(t, http.StatusOK, request(http.MethodGet, "/app.js", "", cookie).Code)

		logout := request(http.MethodPost, "/logout", "", cookie)
		assert.Equal(t, http.StatusSeeOther, logout.Code)
		assert.Equal(t, http.StatusUnauthorized, request(http.MethodGet, "/api/jobs", "", cookie).Code)

		for _, tt := range []struct {
			name   string
			path   string
			body   string
			cookie *http.Cookie
		}{
			{
				name: "login",
				path: "/login",
				body: form.Encode(),
			},
			{
				name:   "jobs",
				path:   "/api/jobs",
				body:   "{}",
				cookie: cookie,
			},
		} {
			t.Run("cross-origin "+tt.name, func(t *testing.T) {
				crossOrigin := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
				crossOrigin.Header.Set("Origin", "https://attacker.example")
				if tt.cookie != nil {
					crossOrigin.AddCookie(tt.cookie)
				}
				response := httptest.NewRecorder()
				server.ServeHTTP(response, crossOrigin)
				assert.Equal(t, http.StatusForbidden, response.Code)
			})
		}
	})
}

func TestLoginRateLimit(t *testing.T) {
	users, err := ParseAuthUsers(`["alice:secret"]`)
	require.NoError(t, err)
	server := newTestServerWithUsers(t, users)
	for i := 0; i < 31; i++ {
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=alice&password=wrong"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, req)
		if i < 30 {
			require.Equal(t, http.StatusSeeOther, response.Code)
		} else {
			require.Equal(t, http.StatusTooManyRequests, response.Code)
			assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		}
	}
}
