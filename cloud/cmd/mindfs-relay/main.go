package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mindfs-cloud/app"
	"mindfs-cloud/internal/assetsync"
	"mindfs-cloud/internal/config"
	"mindfs-cloud/internal/ops"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(args []string, stdout io.Writer) error {
	command, rest, err := parseCommand(args)
	if err != nil {
		return err
	}
	if command == "sync-assets" {
		result, err := assetsync.Sync(context.Background(), assetsync.Options{
			SourceDir: rest[0],
			TargetDir: rest[1],
		})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(
			stdout,
			"asset sync complete current_files=%d releases_imported=%d releases_present=%d assets_added=%d assets_reused=%d\n",
			result.CurrentFiles,
			result.ReleasesImported,
			result.ReleasesPresent,
			result.AssetsAdded,
			result.AssetsReused,
		)
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	switch command {
	case "validate":
		_, err := fmt.Fprintln(stdout, "configuration valid")
		return err
	case "migrate":
		if err := ops.Migrate(cfg); err != nil {
			return err
		}
		_, err := fmt.Fprintln(stdout, "migration complete")
		return err
	case "backup":
		result, err := ops.Backup(context.Background(), cfg, rest[0])
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "backup complete path=%s size_bytes=%d\n", result.Destination, result.SizeBytes)
		return err
	case "healthcheck":
		return ops.Healthcheck(context.Background(), cfg.Addr, nil)
	case "serve":
		return serve(cfg)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func parseCommand(args []string) (string, []string, error) {
	if len(args) == 0 {
		return "serve", nil, nil
	}
	command := args[0]
	switch command {
	case "serve", "validate", "migrate", "healthcheck":
		if len(args) != 1 {
			return "", nil, fmt.Errorf("%s does not accept arguments", command)
		}
		return command, nil, nil
	case "backup":
		if len(args) != 2 {
			return "", nil, errors.New("usage: mindfs-relay backup <destination>")
		}
		return command, args[1:], nil
	case "sync-assets":
		if len(args) != 3 {
			return "", nil, errors.New("usage: mindfs-relay sync-assets <source-dir> <target-dir>")
		}
		return command, args[1:], nil
	default:
		return "", nil, fmt.Errorf("unknown command %q", command)
	}
}

func serve(cfg config.Config) error {
	application, err := app.New(cfg)
	if err != nil {
		return err
	}
	defer application.Close()
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           application.Handler(),
		ReadHeaderTimeout: cfg.HeaderTimeout,
	}
	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-runCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown failed: %v", err)
		}
	}()
	log.Printf("mindfs cloud relay listening on %s public_url=%s", cfg.Addr, cfg.PublicURL)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
