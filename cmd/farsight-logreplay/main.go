// Command farsight-logreplay is a one-shot dev CLI: it runs a
// logwatch.Watcher against a directory holding a Valheim server's stdout
// log(s) and supervisord.log (see hack/pull-logs.sh), replays them once,
// and prints the resulting events as JSON lines to stdout. A summary
// (counts by type, distinct players, the last join code and the last
// players_now) goes to stderr.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"time"

	"github.com/jumpingmushroom/farsight/internal/logwatch"
)

func main() {
	dir := flag.String("dir", "", "directory holding the server's stdout log(s) and supervisord.log (required)")
	tz := flag.String("tz", "UTC", "the game container's TZ, which the log timestamps carry (e.g. Europe/Oslo for mulevikings)")
	flag.Parse()

	if *dir == "" {
		fmt.Fprintln(os.Stderr, "usage: farsight-logreplay -dir DIR [-tz UTC]")
		os.Exit(2)
	}

	loc, err := time.LoadLocation(*tz)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bad -tz %q: %v\n", *tz, err)
		os.Exit(2)
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	var events []logwatch.Event
	w := &logwatch.Watcher{
		Dir:  *dir,
		Loc:  loc,
		Once: true,
		Emit: func(evs []logwatch.Event) { events = append(events, evs...) },
		Log:  log,
	}

	if err := w.Run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "replay: %v\n", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	for _, e := range events {
		if err := enc.Encode(e); err != nil {
			fmt.Fprintf(os.Stderr, "encode event: %v\n", err)
			os.Exit(1)
		}
	}

	printSummary(events)
}

// printSummary writes counts by type, distinct player names, the last
// join_code and the last players_now to stderr.
func printSummary(events []logwatch.Event) {
	counts := map[string]int{}
	players := map[string]struct{}{}
	var lastCode string
	var lastPlayers *int

	for _, e := range events {
		counts[e.Type]++
		if e.Name != "" {
			players[e.Name] = struct{}{}
		}
		switch e.Type {
		case logwatch.EvJoinCode:
			lastCode = e.Code
		case logwatch.EvPlayersNow:
			lastPlayers = e.Players
		}
	}

	fmt.Fprintf(os.Stderr, "events: %d\n", len(events))

	types := make([]string, 0, len(counts))
	for t := range counts {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		fmt.Fprintf(os.Stderr, "  %s: %d\n", t, counts[t])
	}

	names := make([]string, 0, len(players))
	for n := range players {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Fprintf(os.Stderr, "distinct players (%d): %v\n", len(names), names)

	if lastCode != "" {
		fmt.Fprintf(os.Stderr, "last join code: %s\n", lastCode)
	} else {
		fmt.Fprintf(os.Stderr, "last join code: (none)\n")
	}

	if lastPlayers != nil {
		fmt.Fprintf(os.Stderr, "last players_now: %d\n", *lastPlayers)
	} else {
		fmt.Fprintf(os.Stderr, "last players_now: (none)\n")
	}
}
