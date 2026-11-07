package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"lemontube/internal"
	"lemontube/internal/server"
	"lemontube/web"
)

func main() {
	authUsers, configured := os.LookupEnv("AUTH_USERS")
	if configured && authUsers == "" {
		log.Fatal("AUTH_USERS must not be empty; unset it to disable authentication")
	}
	users, err := server.ParseAuthUsers(authUsers)
	if err != nil {
		log.Fatal(err)
	}
	stateDir := os.Getenv("STATE_DIR")
	if stateDir == "" {
		stateDir = "."
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		log.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(stateDir, ".lemontube.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		log.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		log.Fatal("another LemonTube instance owns this state directory")
	}
	repo, err := internal.OpenRepository(filepath.Join(stateDir, "lemontube.sqlite3"))
	if err != nil {
		log.Fatal(err)
	}
	defer repo.Close()
	address := os.Getenv("LISTEN_ADDR")
	if address == "" {
		address = ":8081"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	downloadDir := os.Getenv("DOWNLOAD_DIR")
	if downloadDir == "" {
		downloadDir = "downloads"
	}
	tempDir := os.Getenv("TEMP_DIR")
	if tempDir == "" {
		tempDir = downloadDir
	}
	concurrency, _ := strconv.Atoi(os.Getenv("MAX_CONCURRENT_DOWNLOADS"))
	clearSeconds, _ := strconv.Atoi(os.Getenv("CLEAR_COMPLETED_AFTER"))
	deleteOutputs := os.Getenv("DELETE_FILE_ON_TRASHCAN") == "true"
	service, err := internal.NewService(repo, internal.Config{
		StateDir:      stateDir,
		DownloadDir:   downloadDir,
		TempDir:       tempDir,
		YTDLPPath:     os.Getenv("YTDLP_PATH"),
		Concurrency:   concurrency,
		DeleteOutputs: deleteOutputs,
		ClearAfter:    time.Duration(clearSeconds) * time.Second,
	}, slog.Default())
	if err != nil {
		log.Fatal(err)
	}
	if err := service.Start(ctx); err != nil {
		log.Fatal(err)
	}
	defer service.Wait()
	e := server.New(web.Assets(), service, users)
	e.HideBanner = true
	e.HidePort = true
	e.Listener, err = net.Listen("tcp", address)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Listening at %s", browserURL(e.Listener.Addr()))
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := e.Shutdown(shutdown); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()
	if err := e.Start(address); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func browserURL(addr net.Addr) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "http://" + addr.String()
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return (&url.URL{Scheme: "http", Host: net.JoinHostPort(host, port)}).String()
}
