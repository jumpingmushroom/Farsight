package main

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestRunHash(t *testing.T) {
	var out bytes.Buffer
	if err := runHash(strings.NewReader("secret\r\n"), &out); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("output = %q, want a single line", out.String())
	}
	line := lines[0]

	cost, err := bcrypt.Cost([]byte(line))
	if err != nil {
		t.Fatalf("output is not a bcrypt hash: %v", err)
	}
	if cost != hashCost {
		t.Fatalf("cost = %d, want %d", cost, hashCost)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(line), []byte("secret")); err != nil {
		t.Fatalf("hash does not verify against %q: %v", "secret", err)
	}
}

func TestRunHashEmptyInput(t *testing.T) {
	var out bytes.Buffer
	if err := runHash(strings.NewReader(""), &out); err == nil {
		t.Fatal("want error for empty input")
	}
}
