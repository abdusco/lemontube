package server

import (
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

func rateLimit(limit int, window time.Duration) echo.MiddlewareFunc {
	var mutex sync.Mutex
	var count int
	var resetAt time.Time
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			mutex.Lock()
			now := time.Now()
			if !now.Before(resetAt) {
				count = 0
				resetAt = now.Add(window)
			}
			if count >= limit {
				mutex.Unlock()
				return echo.NewHTTPError(http.StatusTooManyRequests, "Too many requests. Try again later.")
			}
			count++
			mutex.Unlock()
			return next(c)
		}
	}
}
