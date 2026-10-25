// Command homeview is a small, self-hosted dashboard with e-mail sign-in.
//
//	homeview                     start the web server (default)
//	homeview admin create        create the first administrator
//	homeview admin login-link    print a one-time sign-in link (recovery)
//	homeview admin list          list users
//	homeview healthcheck         exit 0 if the server answers (for Docker)
//	homeview version
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/andregoncalvespires/homeview-dash/internal/auth"
	"github.com/andregoncalvespires/homeview-dash/internal/cli"
	"github.com/andregoncalvespires/homeview-dash/internal/config"
	"github.com/andregoncalvespires/homeview-dash/internal/store"
	"github.com/andregoncalvespires/homeview-dash/internal/store/sqlstore"
	"github.com/andregoncalvespires/homeview-dash/internal/web"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `HomeView ` + "%s" + `

Usage:
  homeview                      start the web server
  homeview admin create         create the first administrator (interactive)
  homeview admin login-link     print a one-time sign-in link
  homeview admin list           list users
  homeview healthcheck          check the running server
  homeview version

Configuration is done with HOMEVIEW_* environment variables; see the README.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	cmd := "serve"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "serve":
		return serve(stderr, getenv)
	case "admin":
		return admin(args, stdin, stdout, stderr, getenv)
	case "healthcheck":
		return healthcheck(stderr, getenv)
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, "homeview", version)
		return 0
	case "help", "--help", "-h":
		fmt.Fprintf(stdout, usage, version)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", cmd)
		fmt.Fprintf(stderr, usage, version)
		return 2
	}
}

func openStore(cfg config.Config) (store.Store, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	return sqlstore.Open(filepath.Join(cfg.DataDir, "homeview.db"))
}

func admin(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	cfg, err := config.FromEnv(getenv)
	if err != nil {
		fmt.Fprintln(stderr, "configuration error:", err)
		return 1
	}
	st, err := openStore(cfg)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	defer st.Close()
	return cli.Admin(context.Background(), args, st, cfg, cli.IO{In: bufio.NewReader(stdin), Out: stdout, Err: stderr})
}

func serve(stderr io.Writer, getenv func(string) string) int {
	log := slog.New(slog.NewTextHandler(stderr, nil))
	cfg, err := config.FromEnv(getenv)
	if err == nil {
		err = cfg.ValidateForServe()
	}
	if err != nil {
		log.Error("configuration error", "err", err)
		return 1
	}
	st, err := openStore(cfg)
	if err != nil {
		log.Error("cannot open database", "err", err)
		return 1
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	secret, err := auth.LoadSecret(ctx, st)
	if err != nil {
		log.Error("cannot load secret", "err", err)
		return 1
	}
	a := auth.New(st, &cfg.SMTP, secret, auth.DefaultConfig())
	srv, err := web.New(cfg, st, a, log)
	if err != nil {
		log.Error("cannot build server", "err", err)
		return 1
	}

	if n, err := st.CountAdmins(ctx); err == nil && n == 0 {
		log.Warn("no administrator yet; create one with: docker compose exec -it <service> homeview admin create")
	}

	hs := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}
	errc := make(chan error, 1)
	go func() { errc <- hs.ListenAndServe() }()
	log.Info("HomeView started", "version", version, "addr", cfg.Addr, "base_url", cfg.BaseURL, "lang", cfg.DefaultLang)

	select {
	case err := <-errc:
		log.Error("server stopped", "err", err)
		return 1
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := hs.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("shutdown", "err", err)
		return 1
	}
	log.Info("bye")
	return 0
}

// healthcheck is used by the Docker HEALTHCHECK (the image has no curl/wget).
func healthcheck(stderr io.Writer, getenv func(string) string) int {
	cfg, err := config.FromEnv(getenv)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	_, port, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		fmt.Fprintln(stderr, "bad HOMEVIEW_ADDR:", err)
		return 1
	}
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(stderr, "unhealthy:", resp.Status)
		return 1
	}
	return 0
}
