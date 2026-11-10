// Command inventory serves the inventory API and the embedded web app.
//
//	inventory                        run the server
//	inventory reset-password <email> print a one-time password reset link
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db"
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

	database, err := db.Open(ctx, cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.Handler(version, buildTime, web.Dist(), database, cfg),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown", "err", err)
		}
	}()

	slog.Info("listening", "addr", cfg.Addr, "version", version)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func runCommand(cfg config.Config, args []string) int {
	switch args[0] {
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
		fmt.Fprintf(os.Stderr, "unknown command %q\nusage: inventory [reset-password <email>]\n", args[0])
		return 2
	}
}
