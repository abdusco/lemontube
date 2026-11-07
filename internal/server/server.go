package server

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"time"

	"lemontube/internal"

	"github.com/labstack/echo/v5"
)

func New(assets fs.FS, service *internal.Service, users []AuthUser) *echo.Echo {
	e := echo.NewWithConfig(echo.Config{
		HTTPErrorHandler: handleHTTPError,
		Router: echo.NewRouter(echo.RouterConfig{
			UnescapePathParamValues: true,
		}),
	})
	auth := newAuthentication(authenticationParams{
		users: users,
		loginPage: func() ([]byte, error) {
			return fs.ReadFile(assets, "login.html")
		},
	})
	auth.registerRoutes(e, rateLimit(30, time.Minute))

	api := e.Group("/api", auth.requireAPIAuth)
	protected := e.Group("", auth.requirePageAuth)

	api.POST("/orphan-outputs/clear", func(c *echo.Context) error {
		cleared, retained, err := service.ClearOrphanOutputs(c.Request().Context())
		if err != nil {
			return fmt.Errorf("clear orphan outputs: %w", err)
		}

		return c.JSON(http.StatusOK, map[string]int{"cleared": cleared, "retained": retained})
	})

	e.GET("/health", func(c *echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})

	api.GET("/yt-dlp", func(c *echo.Context) error {
		info, err := service.YTDLPVersion(c.Request().Context())
		if err != nil {
			return fmt.Errorf("get yt-dlp version: %w", err)
		}

		return c.JSON(http.StatusOK, info)
	})

	api.POST("/yt-dlp/update", func(c *echo.Context) error {
		info, err := service.UpdateYTDLP(c.Request().Context())
		if err != nil {
			return fmt.Errorf("update yt-dlp: %w", err)
		}

		return c.JSON(http.StatusOK, info)
	})

	api.GET("/jobs", func(c *echo.Context) error {
		jobs, err := service.Jobs(c.Request().Context())
		if err != nil {
			return fmt.Errorf("list jobs: %w", err)
		}

		return c.JSON(http.StatusOK, jobs)
	})

	api.POST("/jobs", func(c *echo.Context) error {
		var request struct {
			URL          string                   `json:"url"`
			DownloadType internal.DownloadType    `json:"download_type"`
			Options      internal.DownloadOptions `json:"options"`
		}

		if err := c.Bind(&request); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid request").Wrap(err)
		}

		id, err := service.Enqueue(c.Request().Context(), internal.EnqueueParams{
			URL:          request.URL,
			DownloadType: request.DownloadType,
			Options:      request.Options,
		})
		if err != nil {
			return operationError("enqueue job", err)
		}

		return c.JSON(http.StatusCreated, map[string]internal.JobID{"id": id})
	})

	api.DELETE("/jobs/:id", func(c *echo.Context) error {
		if err := service.Delete(c.Request().Context(), internal.JobID(c.Param("id"))); err != nil {
			return operationError("delete job", err)
		}

		return c.NoContent(http.StatusNoContent)
	})

	api.GET("/jobs/:id/logs", func(c *echo.Context) error {
		lines, err := service.Logs(c.Request().Context(), internal.JobID(c.Param("id")))
		if err != nil {
			return fmt.Errorf("read job logs: %w", err)
		}

		return c.JSON(http.StatusOK, lines)
	})

	api.POST("/jobs/:id/retry", func(c *echo.Context) error {
		id, err := service.Retry(c.Request().Context(), internal.JobID(c.Param("id")))
		if err != nil {
			return operationError("retry job", err)
		}

		return c.JSON(http.StatusCreated, map[string]internal.JobID{"id": id})
	})

	api.GET("/cookies", func(c *echo.Context) error {
		domains, err := service.Cookies(c.Request().Context())
		if err != nil {
			return fmt.Errorf("list cookies: %w", err)
		}

		return c.JSON(http.StatusOK, domains)
	})

	api.POST("/cookies", func(c *echo.Context) error {
		body := http.MaxBytesReader(c.Response(), c.Request().Body, 1_000_000)
		data, err := io.ReadAll(body)
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "cookie file is too large").Wrap(err)
			}
			return fmt.Errorf("read cookie file: %w", err)
		}

		if err := service.SaveCookies(c.Request().Context(), string(data)); err != nil {
			return operationError("save cookies", err)
		}

		return c.NoContent(http.StatusNoContent)
	})

	api.DELETE("/cookies", func(c *echo.Context) error {
		if err := service.DeleteCookies(c.Request().Context(), ""); err != nil {
			return fmt.Errorf("delete cookies: %w", err)
		}

		return c.NoContent(http.StatusNoContent)
	})

	api.DELETE("/cookies/:domain", func(c *echo.Context) error {
		if err := service.DeleteCookies(c.Request().Context(), c.Param("domain")); err != nil {
			return fmt.Errorf("delete domain cookies: %w", err)
		}

		return c.NoContent(http.StatusNoContent)
	})

	protected.GET("/download/:name", func(c *echo.Context) error {
		name := c.Param("name")
		path, err := service.DownloadPath(c.Request().Context(), name)
		if err != nil {
			return operationError("find download", err)
		}

		http.ServeFile(c.Response(), c.Request(), path)
		return nil
	})
	static := http.FileServer(http.FS(assets))
	serveStatic := echo.WrapHandler(static)
	e.GET("/styles.css", serveStatic, noCache)
	e.GET("/lemontube.svg", serveStatic, noCache)
	protected.GET("/", serveStatic, noCache)
	protected.GET("/*", serveStatic, noCache)
	return e
}
