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
}
