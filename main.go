package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"github.com/Serebr1k-code/Writeups-MCP/internal/mcpserver"
	"github.com/Serebr1k-code/Writeups-MCP/internal/writeups"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go forceExitOnSecondSignal()

	exitCode := 0
	if err := run(ctx); err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Printf("shutdown with error: %v", err)
			exitCode = 1
		}
	}

	if ctx.Err() != nil && exitCode == 0 {
		exitCode = 130
	}

	os.Exit(exitCode)
}

func run(ctx context.Context) error {
	var (
		transport = flag.String("transport", "stdio", "stdio or http")
		host      = flag.String("host", envOrDefault("HOST", "127.0.0.1"), "HTTP bind host")
		port      = flag.String("port", envOrDefault("PORT", envOrDefault("WRITEUPS_HTTP_PORT", "9001")), "HTTP bind port")
		dbPath    = flag.String("db", writeups.DBPathFromEnv(), "Path to writeups SQLite database")
	)
	flag.Parse()

	repo, err := writeups.Open(*dbPath)
	if err != nil {
		log.Fatalf("failed to open database %q: %v", *dbPath, err)
	}
	defer repo.Close()

	mcpServer := mcpserver.New(repo)

	switch *transport {
	case "stdio":
		return serveStdio(ctx, mcpServer)
	case "http":
		return serveHTTP(ctx, mcpServer, *host, *port)
	default:
		return fmt.Errorf("unknown transport %q, expected stdio or http", *transport)
	}
	return nil
}

func serveStdio(ctx context.Context, mcpServer *server.MCPServer) error {
	stdioServer := server.NewStdioServer(mcpServer)
	errCh := make(chan error, 1)

	go func() {
		errCh <- stdioServer.Listen(ctx, os.Stdin, os.Stdout)
	}()

	select {
	case err := <-errCh:
		if err != nil && errors.Is(ctx.Err(), context.Canceled) {
			return nil
		}
		return err
	case <-ctx.Done():
		select {
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
			return nil
		case <-time.After(1500 * time.Millisecond):
			return nil
		}
	}
}

func serveHTTP(ctx context.Context, mcpServer *server.MCPServer, host, port string) error {
	addr := host + ":" + port
	mux := http.NewServeMux()
	httpServer := server.NewStreamableHTTPServer(
		mcpServer,
		server.WithStreamableHTTPServer(&http.Server{
			Addr:    addr,
			Handler: mux,
		}),
	)
	mux.Handle("/mcp", httpServer)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	errCh := make(chan error, 1)

	go func() {
		log.Printf("Writeups MCP server started over HTTP at http://%s/mcp", addr)
		if err := httpServer.Start(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-time.After(5 * time.Second):
	}

	return nil
}

func forceExitOnSecondSignal() {
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	<-sigCh
	<-sigCh

	log.Printf("received second interrupt, forcing exit")
	os.Exit(130)
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func init() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [flags]\n\n", os.Args[0])
		fmt.Fprintln(flag.CommandLine.Output(), "Flags:")
		flag.PrintDefaults()
	}
}
