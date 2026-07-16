package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"web-search-backend/runtime/logging"
	"web-search-backend/webfetch/internal/bootstrap"
	"web-search-backend/webfetch/internal/config"
)

func main() {
	if err := run(); err != nil {
		slog.Error("webfetch API stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "config.yaml", "path to YAML configuration")
	flag.Parse()
	config, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	logger, closer, err := logging.New(config.Log)
	if err != nil {
		return err
	}
	defer closer.Close()
	slog.SetDefault(logger)
	application, err := bootstrap.New(config, logger)
	if err != nil {
		return err
	}
	defer application.Close()
	server := &http.Server{Addr: config.Address, Handler: application.Router, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("webfetch API listening", "address", config.Address)
		serverErrors <- server.ListenAndServe()
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	select {
	case signalValue := <-signals:
		logger.Info("shutdown signal received", "signal", signalValue.String())
	case serverErr := <-serverErrors:
		if !errors.Is(serverErr, http.ErrServerClosed) {
			return fmt.Errorf("listen: %w", serverErr)
		}
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}
