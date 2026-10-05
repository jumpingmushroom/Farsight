package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/jumpingmushroom/farsight/internal/auth"
	"github.com/jumpingmushroom/farsight/internal/config"
	"github.com/jumpingmushroom/farsight/internal/live"
	"github.com/jumpingmushroom/farsight/internal/server"
	"github.com/jumpingmushroom/farsight/internal/store"
	"github.com/jumpingmushroom/farsight/internal/tileset"
	"github.com/jumpingmushroom/farsight/internal/worldevents"
	"github.com/jumpingmushroom/farsight/web"
)

const (
	defaultConfigPath = "/etc/farsight/farsight.json"
	sweepInterval     = 30 * time.Second
	pruneInterval     = time.Hour
	shutdownTimeout   = 10 * time.Second
)

// setupError is a startup failure (bad flags or config, unusable data
// directory or store) that main reports with exit status 2.
type setupError struct{ err error }

func (e setupError) Error() string { return e.err.Error() }
func (e setupError) Unwrap() error { return e.err }

// parseServeFlags returns the -config path from the serve subcommand's
// arguments.
func parseServeFlags(args []string, stderr io.Writer) (string, error) {
	fs := flag.NewFlagSet("farsight serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("config", defaultConfigPath, "path to the JSON config file")
	if err := fs.Parse(args); err != nil {
		return "", setupError{err}
	}
	if fs.NArg() > 0 {
		return "", setupError{fmt.Errorf("unexpected arguments: %v", fs.Args())}
	}
	return *path, nil
}

// runServe runs the farsight central backend until ctx is cancelled, then
// shuts down gracefully: stop accepting and drain HTTP for up to
// shutdownTimeout, cancel and wait for the background loops, close the
// store. ready, if non-nil, is called once the server is accepting
// connections, with the public listen address and, when IngestListen is
// set, the ingest listen address (empty otherwise). Startup failures are
// setupErrors.
func runServe(ctx context.Context, cfgPath string, getenv func(string) string, log *slog.Logger, ready func(addr, ingestAddr string)) error {
	cfg, err := config.Load(cfgPath, getenv)
	if err != nil {
		return setupError{err}
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return setupError{fmt.Errorf("data dir: %w", err)}
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "farsight.db"))
	if err != nil {
		return setupError{err}
	}
	defer st.Close()

	tiles := tileset.NewManager(filepath.Join(cfg.DataDir, "tiles"),
		tileset.DefaultRender(max(1, runtime.GOMAXPROCS(0)-1)), log)
	applier := &live.Applier{Store: st, Now: time.Now}
	world := worldevents.NewDeriver(st, log)

	splitIngest := cfg.IngestListen != ""
	publicHandler, ingestHandler := server.NewHandlers(server.Deps{
		Config:        cfg,
		Store:         st,
		Applier:       applier,
		Tiles:         tiles,
		World:         world,
		Codec:         auth.Codec{Key: cfg.CookieKey},
		Limiter:       auth.NewLimiter(5, 5, nil),
		IngestLimiter: auth.NewLimiter(10, 10, nil),
		Now:           time.Now,
		Log:           log,
		UI:            web.Handler(),
		SplitIngest:   splitIngest,
	})

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return setupError{fmt.Errorf("listen %s: %w", cfg.Listen, err)}
	}
	srv := &http.Server{
		Handler:           publicHandler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	// The ingest listener, when configured, is a second, otherwise
	// identical *http.Server on its own port: agent traffic never crosses
	// the public ingress (Plan 5 Task 1).
	var ingestLn net.Listener
	var ingestSrv *http.Server
	if splitIngest {
		ingestLn, err = net.Listen("tcp", cfg.IngestListen)
		if err != nil {
			ln.Close()
			return setupError{fmt.Errorf("ingest listen %s: %w", cfg.IngestListen, err)}
		}
		ingestSrv = &http.Server{
			Handler:           ingestHandler,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       2 * time.Minute,
			WriteTimeout:      2 * time.Minute,
			IdleTimeout:       2 * time.Minute,
			ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
		}
	}

	// Re-queue any tile set lost to a restart; Run picks them up.
	ensureTiles(ctx, cfg, st, tiles, log)

	loopCtx, cancelLoops := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	loop := func(fn func(context.Context)) {
		wg.Add(1)
		go func() { defer wg.Done(); fn(loopCtx) }()
	}
	loop(func(ctx context.Context) {
		if err := tiles.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("tile manager stopped", "err", err)
		}
	})
	loop(func(ctx context.Context) { every(ctx, sweepInterval, false, func() { sweep(ctx, applier, log) }) })
	loop(func(ctx context.Context) { every(ctx, pruneInterval, true, func() { prune(ctx, st, log) }) })
	loop(func(ctx context.Context) { backfillWorldEvents(ctx, cfg, world, log) })

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	addr := ln.Addr().String()
	log.Info("farsight listening", "addr", addr, "dataDir", cfg.DataDir, "servers", len(cfg.Servers))
	log.Info("web ui", "embedded", web.Built)

	// ingestServeErr stays nil (and so never selectable) when the ingest
	// listener isn't split out.
	var ingestServeErr chan error
	ingestAddr := ""
	if splitIngest {
		ingestServeErr = make(chan error, 1)
		go func() { ingestServeErr <- ingestSrv.Serve(ingestLn) }()
		ingestAddr = ingestLn.Addr().String()
		log.Info("farsight ingest listening", "addr", ingestAddr)
	}

	if ready != nil {
		ready(addr, ingestAddr)
	}

	var runErr error
	select {
	case <-ctx.Done():
		log.Info("shutting down")
	case err := <-serveErr:
		runErr = fmt.Errorf("http server: %w", err)
	case err := <-ingestServeErr:
		runErr = fmt.Errorf("ingest http server: %w", err)
	}

	toShutdown := []*http.Server{srv}
	if splitIngest {
		toShutdown = append(toShutdown, ingestSrv)
	}
	if err := shutdownAll(shutdownTimeout, toShutdown...); err != nil {
		log.Warn("http shutdown", "err", err)
	}
	cancelLoops()
	wg.Wait()
	log.Info("stopped")
	return runErr
}

// shutdownAll shuts down each of servers concurrently, each with its own,
// full timeout budget (rather than sharing one deadline, under which a
// slow first shutdown could starve a later one of the time it needs), and
// joins any errors. A nil entry is skipped, so callers can pass an
// always-present server alongside one that's only sometimes started.
func shutdownAll(timeout time.Duration, servers ...*http.Server) error {
	var wg sync.WaitGroup
	errs := make([]error, len(servers))
	for i, s := range servers {
		if s == nil {
			continue
		}
		wg.Add(1)
		go func(i int, s *http.Server) {
			defer wg.Done()
			sctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			errs[i] = s.Shutdown(sctx)
		}(i, s)
	}
	wg.Wait()
	return errors.Join(errs...)
}

// every calls fn every interval (and once immediately if now is set)
// until ctx is cancelled.
func every(ctx context.Context, interval time.Duration, now bool, fn func()) {
	if now {
		fn()
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn()
		}
	}
}

func sweep(ctx context.Context, a *live.Applier, log *slog.Logger) {
	n, err := a.SweepStale(ctx)
	if err != nil {
		if ctx.Err() == nil {
			log.Error("sweep stale sessions", "err", err)
		}
		return
	}
	if n > 0 {
		log.Info("closed stale sessions", "sessions", n)
	}
}

func prune(ctx context.Context, st *store.Store, log *slog.Logger) {
	before := time.Now().Add(-live.EventRetention)
	snaps, err := st.PruneSnapshots(ctx, before)
	if err != nil {
		if ctx.Err() == nil {
			log.Error("prune snapshots", "err", err)
		}
		return
	}
	evs, err := st.PruneEvents(ctx, before)
	if err != nil {
		if ctx.Err() == nil {
			log.Error("prune events", "err", err)
		}
		return
	}
	if snaps > 0 || evs > 0 {
		log.Info("pruned", "snapshots", snaps, "events", evs)
	}
}

// backfillWorldEvents brings every configured server's world-save events
// up to date once at startup: the first run for a server replays all its
// stored snapshots, later runs only what arrived while farsight was down.
// Errors are logged and the server skipped; ingest catches up later.
func backfillWorldEvents(ctx context.Context, cfg *config.Config, world *worldevents.Deriver, log *slog.Logger) {
	for _, s := range cfg.Servers {
		if ctx.Err() != nil {
			return
		}
		if _, err := world.CatchUp(ctx, s.ID); err != nil && ctx.Err() == nil {
			log.Error("startup world events", "server", s.ID, "err", err)
		}
	}
}

// ensureTiles calls Ensure for every configured server's latest snapshot,
// so a tile set whose render was interrupted resumes after a restart.
// Errors are logged and the server skipped.
func ensureTiles(ctx context.Context, cfg *config.Config, st *store.Store, tiles *tileset.Manager, log *slog.Logger) {
	for _, s := range cfg.Servers {
		blob, _, ok, err := st.LatestSnapshot(ctx, s.ID)
		if err != nil {
			log.Error("startup tiles: read snapshot", "server", s.ID, "err", err)
			continue
		}
		if !ok {
			continue
		}
		var snap struct {
			World struct {
				Seed       int32 `json:"seed"`
				GenVersion int32 `json:"genVersion"`
			} `json:"world"`
		}
		if err := json.Unmarshal(blob, &snap); err != nil {
			log.Error("startup tiles: decode snapshot", "server", s.ID, "err", err)
			continue
		}
		stt := tiles.Ensure(snap.World.Seed, snap.World.GenVersion)
		log.Info("startup tiles", "server", s.ID, "key", tiles.Key(snap.World.Seed, snap.World.GenVersion), "state", string(stt.State))
	}
}
