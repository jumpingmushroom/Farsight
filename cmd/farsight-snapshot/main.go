// Command farsight-snapshot prints the atlas snapshot of a world save as JSON.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/save"
)

func main() {
	worlds := flag.String("worlds", ".", "worlds_local directory")
	world := flag.String("world", "", "world name (required)")
	server := flag.String("server", "local", "server id to stamp on the snapshot")
	flag.Parse()
	if *world == "" {
		flag.Usage()
		os.Exit(2)
	}
	start := time.Now()
	e := extract.New()
	w, err := save.Read(*worlds, *world, e.Add)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	s := e.Finish(w, *server, time.Now().UTC())
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "%s: %d zdos, %d markers, %d bases in %s\n",
		w.SaveID, w.ZDOCount, len(s.Markers), len(s.Bases), time.Since(start).Round(time.Millisecond))
}
