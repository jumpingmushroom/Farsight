// Command farsight is the central Farsight backend. It serves the web UI
// and API that front one or more Valheim servers, and provides operator
// utilities such as generating bcrypt password hashes for the config file.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	_ "time/tzdata" // embed the tzdata database: the distroless runtime image has none

	"golang.org/x/crypto/bcrypt"
)

const hashCost = 12

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "hash":
		if err := runHash(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "farsight hash:", err)
			os.Exit(1)
		}
	case "serve":
		os.Exit(serveMain(os.Args[2:]))
	default:
		usage()
		os.Exit(2)
	}
}

// serveMain runs "farsight serve" and returns the process exit status:
// 2 for a startup error (bad flags, config or store), 1 for a runtime
// failure, 0 after a clean shutdown on SIGTERM or SIGINT.
func serveMain(args []string) int {
	cfgPath, err := parseServeFlags(args, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "farsight serve:", err)
		return 2
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := runServe(ctx, cfgPath, os.Getenv, log, nil); err != nil {
		fmt.Fprintln(os.Stderr, "farsight serve:", err)
		var se setupError
		if errors.As(err, &se) {
			return 2
		}
		return 1
	}
	return 0
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: farsight hash | farsight serve [-config PATH]")
}

// runHash reads a single line from in, trims its trailing \r\n, and writes
// its bcrypt hash (cost hashCost) to out. It fails on empty input.
func runHash(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return err
		}
		return fmt.Errorf("empty input")
	}
	line := strings.TrimRight(scanner.Text(), "\r\n")
	if line == "" {
		return fmt.Errorf("empty input")
	}

	h, err := bcrypt.GenerateFromPassword([]byte(line), hashCost)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, string(h))
	return nil
}
