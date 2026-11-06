package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		{"unset disables authentication", "", func(t *testing.T, users []AuthUser, err error) {
			require.NoError(t, err)
			assert.Empty(t, users)
		}},
		{"multiple users and password with colon", `["user:pass:word","user2:pass2"]`, func(t *testing.T, users []AuthUser, err error) {
			require.NoError(t, err)
			require.Len(t, users, 2)
			assert.True(t, users[0].MatchesCredentials("user", "pass:word"))
			assert.False(t, users[0].MatchesCredentials("user", "pass2"))
			assert.True(t, users[1].MatchesCredentials("user2", "pass2"))
			assert.False(t, users[1].MatchesCredentials("user", "pass2"))
		}},
		{"empty array", `[]`, func(t *testing.T, users []AuthUser, err error) { require.Error(t, err) }},
		{"invalid json", `broken`, func(t *testing.T, users []AuthUser, err error) { require.Error(t, err) }},
		{"empty password", `["user:"]`, func(t *testing.T, users []AuthUser, err error) { require.Error(t, err) }},
		{"duplicate username", `["user:a","user:b"]`, func(t *testing.T, users []AuthUser, err error) { require.Error(t, err) }},
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

	assert.Equal(t, http.StatusSeeOther, request(http.MethodGet, "/", "", nil).Code)
	assert.Equal(t, http.StatusUnauthorized, request(http.MethodGet, "/api/jobs", "", nil).Code)
	assert.Equal(t, http.StatusSeeOther, request(http.MethodGet, "/download/file.mp4", "", nil).Code)
	assert.Equal(t, http.StatusNoContent, request(http.MethodGet, "/health", "", nil).Code)
	assert.Equal(t, http.StatusOK, request(http.MethodGet, "/login", "", nil).Code)
	assert.Equal(t, http.StatusOK, request(http.MethodGet, "/styles.css", "", nil).Code)
	assert.Equal(t, http.StatusSeeOther, request(http.MethodGet, "/app.js", "", nil).Code)
	assert.Equal(t, http.StatusUnauthorized, request(http.MethodGet, "/api/session", "", nil).Code)
	assert.Equal(t, http.StatusUnauthorized, request(http.MethodPost, "/logout", "", nil).Code)
	bad := request(http.MethodPost, "/login", "username=alice&password=wrong", nil)
	assert.Equal(t, "/login?error=1", bad.Header().Get("Location"))
	assert.Equal(t, "no-store", bad.Header().Get("Cache-Control"))
	assert.Empty(t, bad.Result().Cookies())

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

	crossOrigin := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	crossOrigin.Header.Set("Origin", "https://attacker.example")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, crossOrigin)
	assert.Equal(t, http.StatusForbidden, response.Code)
	crossOrigin = httptest.NewRequest(http.MethodPost, "/api/jobs", strings.NewReader("{}"))
	crossOrigin.Header.Set("Origin", "https://attacker.example")
	crossOrigin.AddCookie(cookie)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, crossOrigin)
	assert.Equal(t, http.StatusForbidden, response.Code)
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
