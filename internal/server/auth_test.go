package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			a := authentication{users: users}
			assert.True(t, a.allowed("user", "pass:word"))
			assert.True(t, a.allowed("user2", "pass2"))
			assert.False(t, a.allowed("user", "pass2"))
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
	bad := request(http.MethodPost, "/login", "username=alice&password=wrong", nil)
	assert.Equal(t, "/login?error=1", bad.Header().Get("Location"))
	assert.Empty(t, bad.Result().Cookies())

	form := url.Values{"username": {"bob"}, "password": {"second"}}
	response := request(http.MethodPost, "/login", form.Encode(), nil)
	require.Equal(t, http.StatusSeeOther, response.Code)
	require.Len(t, response.Result().Cookies(), 1)
	cookie := response.Result().Cookies()[0]
	assert.True(t, cookie.HttpOnly)
	assert.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
	assert.Equal(t, http.StatusOK, request(http.MethodGet, "/api/jobs", "", cookie).Code)

	logout := request(http.MethodPost, "/logout", "", cookie)
	assert.Equal(t, http.StatusSeeOther, logout.Code)
	assert.Equal(t, http.StatusUnauthorized, request(http.MethodGet, "/api/jobs", "", cookie).Code)

	crossOrigin := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	crossOrigin.Header.Set("Origin", "https://attacker.example")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, crossOrigin)
	assert.Equal(t, http.StatusForbidden, response.Code)
}
