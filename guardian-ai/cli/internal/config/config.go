package config

import "time"

type Config struct {
	APIURL  string
	JSON    bool
	NoColor bool
	Timeout time.Duration
	Demo    bool
	Chaos   bool
	Replay  string

	// ReadOnly blocks every mutating call (studio draft/publish/rollback).
	// Set for the public web-terminal session so a juror cannot roll back the
	// production prompt mid-pitch.
	ReadOnly bool
}
