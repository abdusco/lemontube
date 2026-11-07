package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
)

func TestRateLimit(t *testing.T) {
	tests := []struct {
		name   string
		window time.Duration
		want   []int
	}{
		{
			name:   "limits requests",
			window: time.Minute,
			want:   []int{http.StatusNoContent, http.StatusNoContent, http.StatusTooManyRequests},
		},
		{
			name:   "resets expired window",
			window: 0,
			want:   []int{http.StatusNoContent, http.StatusNoContent, http.StatusNoContent},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			e.GET("/", func(c *echo.Context) error {
				return c.NoContent(http.StatusNoContent)
			}, rateLimit(2, tt.window))
			for _, want := range tt.want {
				response := httptest.NewRecorder()
				e.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
				assert.Equal(t, want, response.Code)
			}
		})
	}
}
