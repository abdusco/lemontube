package server

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"lemontube/internal"
)

func New(assets fs.FS, service *internal.Service, users []AuthUser) *echo.Echo {
	e := echo.New()
	auth := &authentication{users: users, sessions: make(map[string]time.Time)}
	e.Use(auth.middleware)
	e.GET("/login", func(c echo.Context) error {
		if len(users) == 0 || auth.authenticated(c.Request()) {
			return c.Redirect(http.StatusSeeOther, "/")
		}
		page, err := fs.ReadFile(assets, "login.html")
		if err != nil {
			return err
		}
		c.Response().Header().Set("Cache-Control", "no-store")
		return c.HTMLBlob(http.StatusOK, page)
	})
	e.POST("/login", auth.login)
	e.POST("/logout", func(c echo.Context) error {
		if cookie, err := c.Cookie(sessionCookie); err == nil {
			auth.mutex.Lock()
			delete(auth.sessions, cookie.Value)
			auth.mutex.Unlock()
		}
		c.SetCookie(&http.Cookie{Name: sessionCookie, Path: "/", HttpOnly: true, Secure: secureRequest(c.Request()), SameSite: http.SameSiteStrictMode, MaxAge: -1})
		return c.Redirect(http.StatusSeeOther, "/login")
	})
	e.GET("/api/session", func(c echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-store")
		return c.JSON(http.StatusOK, map[string]bool{"auth_enabled": len(users) > 0})
	})
	e.POST("/api/orphan-outputs/clear", func(c echo.Context) error {
		cleared, retained, err := service.ClearOrphanOutputs(c.Request().Context())
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, map[string]int{"cleared": cleared, "retained": retained})
	})
	e.GET("/health", func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})
	e.GET("/api/yt-dlp", func(c echo.Context) error {
		info, err := service.YTDLPVersion(c.Request().Context())
		if err != nil {
			return c.JSON(http.StatusOK, map[string]any{
				"version":    "",
				"source":     info.Source,
				"can_update": info.CanUpdate,
				"error":      "Could not run yt-dlp",
			})
		}
		return c.JSON(http.StatusOK, info)
	})
	e.POST("/api/yt-dlp/update", func(c echo.Context) error {
		info, err := service.UpdateYTDLP(c.Request().Context())
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		return c.JSON(http.StatusOK, info)
	})
	e.GET("/api/jobs", func(c echo.Context) error {
		jobs, err := service.Jobs(c.Request().Context())
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "could not list jobs")
		}
		return c.JSON(http.StatusOK, jobs)
	})
	e.POST("/api/jobs", func(c echo.Context) error {
		var request struct {
			URL          string                   `json:"url"`
			DownloadType internal.DownloadType    `json:"download_type"`
			Options      internal.DownloadOptions `json:"options"`
		}
		if err := c.Bind(&request); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
		}
		id, err := service.Enqueue(c.Request().Context(), request.URL, request.DownloadType, request.Options)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		return c.JSON(http.StatusCreated, map[string]string{"id": id})
	})
	e.DELETE("/api/jobs/:id", func(c echo.Context) error {
		if err := service.Delete(c.Request().Context(), c.Param("id")); err != nil {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		return c.NoContent(http.StatusNoContent)
	})
	e.GET("/api/jobs/:id/logs", func(c echo.Context) error {
		lines, err := service.Logs(c.Request().Context(), c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "could not read logs")
		}
		return c.JSON(http.StatusOK, lines)
	})
	e.POST("/api/jobs/:id/retry", func(c echo.Context) error {
		id, err := service.Retry(c.Request().Context(), c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		return c.JSON(http.StatusCreated, map[string]string{"id": id})
	})
	e.GET("/api/cookies", func(c echo.Context) error {
		domains, err := service.Cookies(c.Request().Context())
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError)
		}
		return c.JSON(http.StatusOK, domains)
	})
	e.POST("/api/cookies", func(c echo.Context) error {
		body := http.MaxBytesReader(c.Response(), c.Request().Body, 1_000_000)
		data, err := io.ReadAll(body)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "cookie file is too large")
		}
		if err := service.SaveCookies(c.Request().Context(), string(data)); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		return c.NoContent(http.StatusNoContent)
	})
	e.DELETE("/api/cookies", func(c echo.Context) error {
		if err := service.DeleteCookies(c.Request().Context(), ""); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError)
		}
		return c.NoContent(http.StatusNoContent)
	})
	e.DELETE("/api/cookies/:domain", func(c echo.Context) error {
		if err := service.DeleteCookies(c.Request().Context(), c.Param("domain")); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError)
		}
		return c.NoContent(http.StatusNoContent)
	})
	e.GET("/download/:name", func(c echo.Context) error {
		path, err := service.DownloadPath(c.Request().Context(), c.Param("name"))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return echo.ErrNotFound
			}
			return echo.NewHTTPError(http.StatusInternalServerError)
		}
		return c.File(path)
	})
	static := http.FileServer(http.FS(assets))
	e.GET("/*", echo.WrapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		static.ServeHTTP(w, r)
	})))
	return e
}
