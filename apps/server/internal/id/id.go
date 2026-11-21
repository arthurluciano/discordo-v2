// Package id generates time-ordered IDs (UUIDv7), so sorting by ID sorts by creation time.
package id

import "github.com/google/uuid"

func New() string { return uuid.Must(uuid.NewV7()).String() }

func Valid(s string) bool { return uuid.Validate(s) == nil }
