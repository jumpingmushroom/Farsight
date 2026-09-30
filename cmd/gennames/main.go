// Command gennames builds internal/names/names.txt from Valheim's SoftRef
// manifests: every Assets/.../<Name>.prefab basename, sorted and unique.
//
//	go run ./cmd/gennames -o internal/names/names.txt manifest manifest_extended
package main

import (
	"flag"
	"log"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
)

var prefabPath = regexp.MustCompile(`Assets/[A-Za-z0-9_/ ().-]+\.prefab`)

func main() {
	out := flag.String("o", "internal/names/names.txt", "output file")
	flag.Parse()
	seen := map[string]bool{}
	for _, f := range flag.Args() {
		b, err := os.ReadFile(f)
		if err != nil {
			log.Fatal(err)
		}
		for _, m := range prefabPath.FindAll(b, -1) {
			seen[strings.TrimSuffix(path.Base(string(m)), ".prefab")] = true
		}
	}
	list := make([]string, 0, len(seen))
	for n := range seen {
		list = append(list, n)
	}
	sort.Strings(list)
	if len(list) == 0 {
		log.Fatal("no prefab paths found; wrong input files?")
	}
	if err := os.WriteFile(*out, []byte(strings.Join(list, "\n")+"\n"), 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %d names to %s", len(list), *out)
}
