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
	"strings"
	"syscall"
	"time"

	"github.com/weftdesk/weft/internal/agent"
	"github.com/weftdesk/weft/internal/config"
	"github.com/weftdesk/weft/internal/provider"
	"github.com/weftdesk/weft/internal/server"
	"github.com/weftdesk/weft/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}
	switch os.Args[1] {
	case "--version", "-v", "version":
		fmt.Printf("weft %s\n", config.Version)
	case "server", "serve":
		if err := runServer(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Print(`Weft - an agentic coding workbench

Usage:
  weft --version
  weft server [flags]

Server flags:
  --local               store data in ./.weft-data instead of ~/.weft
  --data-dir <dir>      data directory
  --listen <addr>       listen address (default 127.0.0.1:47123)
  --port <port>         listen port on 127.0.0.1
  --permission <mode>   ask | acceptEdits | yolo (default ask)
  --provider <kind>     mock | openai | openai-compatible | anthropic
  --base-url <url>      provider base URL
  --api-key <key>       provider API key
  --model <name>        model name
  --max-turns <n>       agent loop turn limit (default 24)
`)
}

func runServer(args []string) error {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	var (
		local      = fs.Bool("local", false, "store data in ./.weft-data")
		dataDir    = fs.String("data-dir", "", "data directory")
		listen     = fs.String("listen", "", "listen address")
		port       = fs.Int("port", 0, "listen port")
		permission = fs.String("permission", "", "permission mode")
		provKind   = fs.String("provider", "", "provider kind")
		baseURL    = fs.String("base-url", "", "provider base URL")
		apiKey     = fs.String("api-key", "", "provider API key")
		model      = fs.String("model", "", "model name")
		maxTurns   = fs.Int("max-turns", 0, "agent loop turn limit")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	dir := *dataDir
	if dir == "" && *local {
		dir = ".weft-data"
	}
	if dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		dir = abs
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if *listen != "" {
		cfg.Listen = *listen
	}
	if *port > 0 {
		cfg.Listen = fmt.Sprintf("127.0.0.1:%d", *port)
	}
	if *permission != "" {
		cfg.PermissionMode = *permission
	}
	if *provKind != "" {
		cfg.Provider.Kind = *provKind
	}
	if *baseURL != "" {
		cfg.Provider.BaseURL = *baseURL
	}
	if *apiKey != "" {
		cfg.Provider.APIKey = *apiKey
	}
	if *model != "" {
		cfg.Provider.Model = *model
	}
	if *maxTurns > 0 {
		cfg.MaxTurns = *maxTurns
	}
	config.Normalize(&cfg)

	prov, err := provider.New(cfg.Provider)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	ag := agent.New(st, cfg, prov)
	srv := server.New(st, ag, cfg)

	banner := []string{
		fmt.Sprintf("Weft %s", config.Version),
		fmt.Sprintf("data    %s", cfg.DataDir),
		fmt.Sprintf("listen  http://%s", cfg.Listen),
		fmt.Sprintf("model   %s (%s)", cfg.Provider.Model, prov.Name()),
	}
	if cfg.Provider.Kind != "mock" && cfg.Provider.APIKey == "" {
		banner = append(banner, "warning provider API key is not set")
	}
	fmt.Println(strings.Join(banner, "\n"))
	fmt.Println()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-stop:
		fmt.Println("\nshutting down")
	}

	ag.CancelAll()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}
