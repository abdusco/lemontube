package server

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"time"

	"lemontube/internal"

	"github.com/labstack/echo/v4"
)

func New(assets fs.FS, service *internal.Service, users []AuthUser) *echo.Echo {
	e := echo.New()
	auth := newAuthentication(authenticationParams{
		users: users,
		loginPage: func() ([]byte, error) {
			return fs.ReadFile(assets, "login.html")
		},
	})
	auth.registerRoutes(e, rateLimit(30, time.Minute))

	api := e.Group("/api", auth.requireAPIAuth)
	protected := e.Group("", auth.requirePageAuth)

	api.POST("/orphan-outputs/clear", func(c echo.Context) error {
		cleared, retained, err := service.ClearOrphanOutputs(c.Request().Context())
		if err != nil {
			return err
		}

		return c.JSON(http.StatusOK, map[string]int{"cleared": cleared, "retained": retained})
	})

	e.GET("/health", func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})

	api.GET("/yt-dlp", func(c echo.Context) error {
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

	api.POST("/yt-dlp/update", func(c echo.Context) error {
		info, err := service.UpdateYTDLP(c.Request().Context())
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		return c.JSON(http.StatusOK, info)
	})

	api.GET("/jobs", func(c echo.Context) error {
		jobs, err := service.Jobs(c.Request().Context())
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "could not list jobs")
		}

		return c.JSON(http.StatusOK, jobs)
	})

	api.POST("/jobs", func(c echo.Context) error {
		var request struct {
			URL          string                   `json:"url"`
			DownloadType internal.DownloadType    `json:"download_type"`
			Options      internal.DownloadOptions `json:"options"`
		}

		if err := c.Bind(&request); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
		}

		id, err := service.Enqueue(c.Request().Context(), internal.EnqueueParams{
			URL:          request.URL,
			DownloadType: request.DownloadType,
			Options:      request.Options,
		})
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		return c.JSON(http.StatusCreated, map[string]internal.JobID{"id": id})
	})

	api.DELETE("/jobs/:id", func(c echo.Context) error {
		if err := service.Delete(c.Request().Context(), internal.JobID(c.Param("id"))); err != nil {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}

		return c.NoContent(http.StatusNoContent)
	})

	api.GET("/jobs/:id/logs", func(c echo.Context) error {
		lines, err := service.Logs(c.Request().Context(), internal.JobID(c.Param("id")))
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "could not read logs")
		}

		return c.JSON(http.StatusOK, lines)
	})

	api.POST("/jobs/:id/retry", func(c echo.Context) error {
		id, err := service.Retry(c.Request().Context(), internal.JobID(c.Param("id")))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		return c.JSON(http.StatusCreated, map[string]internal.JobID{"id": id})
	})

	api.GET("/cookies", func(c echo.Context) error {
		domains, err := service.Cookies(c.Request().Context())
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError)
		}

		return c.JSON(http.StatusOK, domains)
	})

	api.POST("/cookies", func(c echo.Context) error {
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

	api.DELETE("/cookies", func(c echo.Context) error {
		if err := service.DeleteCookies(c.Request().Context(), ""); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError)
		}

		return c.NoContent(http.StatusNoContent)
	})

	api.DELETE("/cookies/:domain", func(c echo.Context) error {
		if err := service.DeleteCookies(c.Request().Context(), c.Param("domain")); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError)
		}

		return c.NoContent(http.StatusNoContent)
	})

	protected.GET("/download/:name", func(c echo.Context) error {
		name := c.Param("name")
		if c.Request().URL.RawPath != "" {
			var err error
			name, err = url.PathUnescape(name)
			if err != nil {
				return echo.ErrNotFound
			}
		}
		path, err := service.DownloadPath(c.Request().Context(), name)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return echo.ErrNotFound
			}

			return echo.NewHTTPError(http.StatusInternalServerError)
		}

		return c.File(path)
	})
	static := http.FileServer(http.FS(assets))
	serveStatic := echo.WrapHandler(static)
	e.GET("/styles.css", serveStatic, noCache)
	e.GET("/lemontube.svg", serveStatic, noCache)
	protected.GET("/", serveStatic, noCache)
	protected.GET("/*", serveStatic, noCache)
	return e
}
