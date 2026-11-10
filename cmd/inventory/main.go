// Command inventory serves the inventory API and the embedded web app.
//
//	inventory                        run the server
//	inventory healthcheck            exit 0 when the server on the configured address answers
//	inventory reindex                rebuild the search index
//	inventory reset-password <email> print a one-time password reset link
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/search"
	"github.com/kaanemec/things-items-inventory/internal/server"
	"github.com/kaanemec/things-items-inventory/web"
)

// Set with -ldflags at build time.
var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	cfg := config.Load()
	if len(os.Args) > 1 {
		os.Exit(runCommand(cfg, os.Args[1:]))
	}
	if err := run(cfg); err != nil {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}

func run(cfg config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := checkDataDir(cfg.DataDir); err != nil {
		return err
	}
	database, err := db.Open(ctx, cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	app := server.New(version, buildTime, web.Dist(), database, cfg)
	if err := app.Start(ctx); err != nil {
		return fmt.Errorf("start job queue: %w", err)
	}
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           app.Handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		app.Hub.Close() // open streams would hold Shutdown up
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown", "err", err)
		}
		if err := app.Stop(); err != nil {
			slog.Error("stop job queue", "err", err)
		}
	}()

	if cfg.SetupToken == "" && strings.HasPrefix(cfg.PublicURL, "https://") {
		slog.Warn("first-run setup is open: anyone who reaches this address first can create the household; set INVENTORY_SETUP_TOKEN (inventory setup-token makes one)")
	}
	slog.Info("listening", "addr", cfg.Addr, "version", version)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		stop()
		<-done // stop the queue before closing the database
		return err
	}
	<-done
	return nil
}

// checkDataDir creates dir if needed and proves it is writable, so a bad
// INVENTORY_DATA fails with one clear line instead of a database error.
func checkDataDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("data directory %q cannot be created: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, ".write-check-*")
	if err != nil {
		return fmt.Errorf("data directory %q is not writable: %w", dir, err)
	}
	f.Close()
	os.Remove(f.Name())
	return nil
}

// healthcheck asks the server on the configured address for /api/health. The
// Docker image has no shell or curl, so the binary probes itself.
func healthcheck(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("bad INVENTORY_ADDR %q: %w", addr, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/api/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check answered %s", resp.Status)
	}
	return nil
}

func runCommand(cfg config.Config, args []string) int {
	switch args[0] {
	case "healthcheck":
		if err := healthcheck(cfg.Addr); err != nil {
			fmt.Fprintln(os.Stderr, "unhealthy:", err)
			return 1
		}
		return 0
	case "setup-token":
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		fmt.Println(hex.EncodeToString(b))
		return 0
	case "reindex":
		ctx := context.Background()
		database, err := db.Open(ctx, cfg.DataDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		defer database.Close()
		n, err := search.Rebuild(ctx, database, "")
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		fmt.Printf("indexed %d rows\n", n)
		return 0
	case "reset-password":
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "usage: inventory reset-password <email>")
			return 2
		}
		ctx := context.Background()
		database, err := db.Open(ctx, cfg.DataDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		defer database.Close()
		link, expires, err := auth.ResetLinkForEmail(ctx, database, cfg, args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		fmt.Printf("One-time password reset link (valid until %s):\n%s\n", expires.Format(time.RFC1123), link)
		if cfg.PublicURL == "" {
			fmt.Println("Open it on the address you use for the app; set INVENTORY_PUBLIC_URL to print full links.")
		}
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\nusage: inventory [healthcheck | setup-token | reindex | reset-password <email>]\n", args[0])
		return 2
	}
}
