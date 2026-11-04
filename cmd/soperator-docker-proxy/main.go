package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	defaultListenSocket    = "/var/run/soperator-docker.sock"
	defaultUpstreamSocket  = "/var/run/docker.sock"
	defaultCgroupBaseFile  = "/run/soperator-docker-cgroup-base"
	defaultCgroupWait      = 2 * time.Minute
	defaultShutdownTimeout = 5 * time.Second
)

type config struct {
	mode                string
	listenSocket        string
	upstreamSocket      string
	cgroupBaseFile      string
	cgroupWaitTimeout   time.Duration
	procRoot            string
	logCgroupResolution bool
}

func main() {
	logger := log.New(os.Stdout, "Soperator Docker proxy: ", log.LstdFlags|log.Lmicroseconds)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, parseConfig(), logger); err != nil {
		logger.Fatalf("Run: %v", err)
	}
}

func parseConfig() config {
	var cfg config
	flag.StringVar(&cfg.mode, "mode", "", "cgroup policy mode: login or worker")
	flag.StringVar(&cfg.listenSocket, "listen", defaultListenSocket, "Unix socket path to listen on")
	flag.StringVar(&cfg.upstreamSocket, "upstream", defaultUpstreamSocket, "dockerd Unix socket path")
	flag.StringVar(
		&cfg.cgroupBaseFile,
		"login-cgroup-base-file",
		defaultCgroupBaseFile,
		"file containing the login container cgroup path",
	)
	flag.DurationVar(
		&cfg.cgroupWaitTimeout,
		"login-cgroup-wait-timeout",
		defaultCgroupWait,
		"maximum time to wait for the login cgroup base file",
	)
	flag.StringVar(&cfg.procRoot, "proc-root", "/proc", "procfs root used by worker peer discovery")
	flag.BoolVar(
		&cfg.logCgroupResolution,
		"log-cgroup-resolution",
		false,
		"log peer and cgroup resolution details for diagnostics",
	)
	flag.Parse()
	return cfg
}

func run(ctx context.Context, cfg config, logger *log.Logger) error {
	if err := validateSocketPath("listen", cfg.listenSocket); err != nil {
		return err
	}
	if err := validateSocketPath("upstream", cfg.upstreamSocket); err != nil {
		return err
	}

	resolver, err := newCgroupResolver(ctx, cfg)
	if err != nil {
		return fmt.Errorf("configure cgroup resolver: %w", err)
	}

	listener, err := listenUnix(ctx, cfg.listenSocket)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.listenSocket, err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(cfg.listenSocket)
	}()

	handler := newDockerProxy(
		resolver,
		unixTransport(cfg.upstreamSocket),
		logger,
		cfg.logCgroupResolution,
	)
	server := newHTTPServer(handler)
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	logger.Printf("Ready on %s in %s mode", cfg.listenSocket, cfg.mode)
	select {
	case err := <-serveErr:
		if !errors.Is(err, errServerClosed) {
			return fmt.Errorf("serve proxy: %w", err)
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shut down proxy: %w", err)
		}
		return nil
	}
}
