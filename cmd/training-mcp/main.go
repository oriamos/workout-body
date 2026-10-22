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
	"path/filepath"
	"syscall"
	"time"

	"github.com/fullfran/training-mcp/internal/apisrv"
	"github.com/fullfran/training-mcp/internal/config"
	"github.com/fullfran/training-mcp/internal/httpsrv"
	"github.com/fullfran/training-mcp/internal/mcpsrv"
	"github.com/fullfran/training-mcp/internal/mediastore"
	"github.com/fullfran/training-mcp/internal/sqlitestore"
	"github.com/fullfran/training-mcp/internal/training"
	"github.com/fullfran/training-mcp/internal/uisrv"
	"github.com/fullfran/training-mcp/internal/websrv"
)

// options are the parsed command line.
type options struct {
	addr  string
	stdio bool
	// command is "serve" (default), "backup" or "media-gc".
	command string
	// arg is the subcommand argument (backup destination).
	arg string
}

func parseArgs(args []string) (options, error) {
	fs := flag.NewFlagSet("training-mcp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", ":8080", "HTTP listen address")
	stdio := fs.Bool("stdio", false, "serve MCP over stdin/stdout instead of HTTP")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	o := options{addr: *addr, stdio: *stdio, command: "serve"}
	if rest := fs.Args(); len(rest) > 0 {
		o.command = rest[0]
		switch o.command {
		case "backup":
			if len(rest) != 2 {
				return o, errors.New("usage: training-mcp backup <destination.db>")
			}
			o.arg = rest[1]
		case "media-gc":
			if len(rest) != 1 {
				return o, errors.New("usage: training-mcp media-gc")
			}
		default:
			return o, fmt.Errorf("unknown command %q (commands: backup, media-gc)", o.command)
		}
	}
	return o, nil
}

func address(args []string) string {
	o, err := parseArgs(args)
	if err != nil {
		return ":8080"
	}
	return o.addr
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run(args []string) error {
	return runWithSignals(args, os.Interrupt, syscall.SIGTERM)
}

func runWithSignals(args []string, signals ...os.Signal) error {
	ctx, stop := signal.NotifyContext(context.Background(), signals...)
	defer stop()
	return runWithContext(ctx, args)
}

func runWithContext(ctx context.Context, args []string) error {
	opts, err := parseArgs(args)
	if err != nil {
		return err
	}
	cfg, err := config.LoadFor(opts.addr, opts.stdio || opts.command != "serve")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(cfg.DBPath), 0700); err != nil {
		return err
	}
	store, err := sqlitestore.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer store.Close()
	service := training.NewService(store, time.Now).WithLocation(cfg.Location).WithWeekStart(cfg.WeekStart)
	// Video storage is optional: if its directory cannot be created, workout
	// logging must still work, so degrade to "media disabled" instead of failing.
	if blobs, err := mediastore.New(cfg.MediaDir); err != nil {
		log.Printf("WARNING: video storage disabled, cannot use MEDIA_DIR %q: %v", cfg.MediaDir, err)
		service.WithMedia(store, nil, cfg.MaxVideoBytes)
	} else {
		service.WithMedia(store, blobs, cfg.MaxVideoBytes)
	}

	switch opts.command {
	case "backup":
		if err := os.MkdirAll(filepath.Dir(opts.arg), 0o700); err != nil {
			return fmt.Errorf("backup to %s: %w", opts.arg, err)
		}
		if err := service.Snapshot(ctx, opts.arg); err != nil {
			return fmt.Errorf("backup to %s: %w", opts.arg, err)
		}
		log.Printf("database snapshot written to %s (videos are not included: copy %s separately)", opts.arg, cfg.MediaDir)
		return nil
	case "media-gc":
		n, err := service.PurgeOrphanBlobs(ctx)
		if err != nil {
			return err
		}
		log.Printf("removed %d orphaned video file(s)", n)
		return nil
	}

	mcpServer := mcpsrv.New(service)
	if opts.stdio {
		// stdout carries the protocol; the standard logger writes to stderr.
		log.Printf("serving MCP over stdio (timezone %s)", cfg.Timezone)
		return mcpServer.RunStdio(ctx)
	}
	logger := log.Default()
	httpsrv.WarningIfDisabled(cfg.AuthDisabled, logger)

	var web *httpsrv.WebUI
	if cfg.WebBasePath != "" {
		webServer, err := websrv.New(service, time.Now, cfg.WebBasePath)
		if err != nil {
			return err
		}
		web = &httpsrv.WebUI{Handler: webServer.Handler(), BasePath: webServer.BasePath()}
		logger.Printf("web UI mounted under a secret base path (%d chars)", len(cfg.WebBasePath))
	}

	ui := uisrv.New(uisrv.Embedded(), cfg.Token, cfg.AuthDisabled)
	if ui.Built() {
		logger.Printf("web UI served at / (log in with the bearer token)")
	} else {
		logger.Printf("web UI not built into this binary (run scripts/build-ui.sh before go build); / shows how")
	}
	handler := httpsrv.RoutesWithUI(mcpServer.Handler(), apisrv.New(service).Handler(), ui, cfg.AuthDisabled, cfg.Token, web)
	httpServer := &http.Server{Addr: cfg.Addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	logger.Printf("listening on %s (timezone %s, database %s)", cfg.Addr, cfg.Timezone, cfg.DBPath)
	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}
