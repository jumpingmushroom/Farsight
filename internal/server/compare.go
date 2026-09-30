package server

import (
	"context"

	"golang.org/x/crypto/bcrypt"
)

// maxConcurrentCompares bounds how many bcrypt compares run at once
// across the whole process (unlock, the unlock dummy compare, and ingest
// token checks), so a flood of guesses queues rather than eating every
// CPU.
const maxConcurrentCompares = 2

// bcryptSlots is the process-wide semaphore for bcrypt compares.
var bcryptSlots = make(chan struct{}, maxConcurrentCompares)

// bcryptCompare is the password-hash compare every handler uses; a var so
// tests can observe how many run at once.
var bcryptCompare = bcrypt.CompareHashAndPassword

// compareHash reports whether pw matches the bcrypt hash, waiting for one
// of the bcryptSlots first. It returns ctx's error, without comparing, if
// ctx is done before a slot frees up.
func compareHash(ctx context.Context, hash, pw []byte) (bool, error) {
	select {
	case bcryptSlots <- struct{}{}:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	defer func() { <-bcryptSlots }()
	return bcryptCompare(hash, pw) == nil, nil
}
