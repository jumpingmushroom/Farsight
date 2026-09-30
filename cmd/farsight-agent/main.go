// Command farsight-agent runs beside a Valheim server, reads each new world
// save and pushes an atlas snapshot to Farsight, and (unless disabled)
// watches the server's logs for player and lifecycle events.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata" // embed the tzdata database: the distroless runtime image has none

	"github.com/jumpingmushroom/farsight/internal/agent"
	"github.com/jumpingmushroom/farsight/internal/ingest"
	"github.com/jumpingmushroom/farsight/internal/logwatch"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	level := slog.LevelInfo
	if env("LOG_LEVEL", "info") == "debug" {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	poll, err := time.ParseDuration(env("FARSIGHT_POLL", "15s"))
	if err != nil {
		log.Error("bad FARSIGHT_POLL", "err", err)
		os.Exit(2)
	}
	cfg := agent.Config{
		WorldsDir: env("FARSIGHT_WORLDS_DIR", "/worlds/worlds_local"),
		WorldName: os.Getenv("WORLD_NAME"),
		ServerID:  os.Getenv("FARSIGHT_SERVER_ID"),
		URL:       os.Getenv("FARSIGHT_URL"),
		Token:     os.Getenv("FARSIGHT_TOKEN"),
		Poll:      poll,
	}
	for k, v := range map[string]string{"WORLD_NAME": cfg.WorldName, "FARSIGHT_SERVER_ID": cfg.ServerID, "FARSIGHT_URL": cfg.URL, "FARSIGHT_TOKEN": cfg.Token} {
		if v == "" {
			log.Error("missing required env", "var", k)
			os.Exit(2)
		}
	}

	logDir := env("FARSIGHT_LOG_DIR", "/var/log/supervisor")
	logWatcherEnabled := logDir != "off"
	var loc *time.Location
	if logWatcherEnabled {
		// FARSIGHT_LOG_TZ must equal the game container's TZ: the logs
		// carry its local wall-clock time with no offset. Unset TZ in the
		// game container means UTC, hence the default.
		tz := env("FARSIGHT_LOG_TZ", "UTC")
		loc, err = time.LoadLocation(tz)
		if err != nil {
			log.Error("bad FARSIGHT_LOG_TZ", "tz", tz, "err", err)
			os.Exit(2)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	log.Info("farsight-agent starting", "world", cfg.WorldName, "server", cfg.ServerID, "poll", cfg.Poll, "logWatcher", logWatcherEnabled)

	cfg.Client = ingest.New(cfg.URL, cfg.ServerID, cfg.Token)
	saveAgent := agent.New(cfg, log)
	sink := agent.NewSink(cfg.Client, agent.SinkConfig{}, log)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	var firstErr error
	var mu sync.Mutex
	run := func(name string, fn func(context.Context) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := fn(ctx)
			if err != nil && ctx.Err() == nil {
				log.Error("component stopped", "component", name, "err", err)
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				cancel()
			}
		}()
	}

	// The event sink (and its heartbeat) always runs, even with the log
	// watcher disabled: central treats an agent as offline after a few
	// minutes without a heartbeat, so heartbeats must keep flowing
	// regardless of FARSIGHT_LOG_DIR. Only the logwatch.Watcher itself is
	// gated by it.
	run("save-agent", saveAgent.Run)
	run("event-sink", sink.Run)
	if logWatcherEnabled {
		watcher := &logwatch.Watcher{Dir: logDir, Loc: loc, Emit: sink.Add, Log: log}
		run("log-watcher", watcher.Run)
	}

	wg.Wait()

	if firstErr != nil {
		os.Exit(1)
	}
}
