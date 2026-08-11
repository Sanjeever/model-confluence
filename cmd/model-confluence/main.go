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

	"github.com/Sanjeever/model-confluence/internal/app"
	"github.com/Sanjeever/model-confluence/internal/config"
	"github.com/Sanjeever/model-confluence/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("model-confluence stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		return err
	}

	db, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	if cfg.Command == config.CommandResetPassword {
		if err := db.ResetAdminPassword(cfg.AdminPassword); err != nil {
			return fmt.Errorf("reset admin password: %w", err)
		}
		fmt.Println("管理员密码已重置，现有会话已撤销。")
		return nil
	}

	if err := db.BootstrapAdmin(cfg.AdminPassword); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}

	handler := app.New(cfg, db)
	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("model-confluence listening", "address", cfg.ListenAddress, "database", cfg.DatabasePath)
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}
