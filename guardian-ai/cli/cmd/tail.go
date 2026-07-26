package cmd

import (
	"fmt"
	"strings"
	"time"

	"guardianai/cli/internal/api"

	"github.com/spf13/cobra"
)

var (
	tailCallFilter string
	tailTypeFilter string
)

var tailCmd = &cobra.Command{
	Use:   "tail",
	Short: "tail -f del WebSocket /ws — el cerebro del sistema en vivo",
	RunE: func(c *cobra.Command, args []string) error {
		stream := api.NewEventStream(cfg.APIURL)
		stream.Start()
		defer stream.Stop()

		var typeSet map[string]bool
		if tailTypeFilter != "" {
			typeSet = map[string]bool{}
			for _, t := range strings.Split(tailTypeFilter, ",") {
				typeSet[strings.TrimSpace(strings.ToUpper(t))] = true
			}
		}

		fmt.Printf("conectando a %s ...\n", stream.State())
		lastState := stream.State()

		for ev := range stream.Out() {
			if s := stream.State(); s != lastState {
				fmt.Printf("[stream: %s]\n", s)
				lastState = s
			}
			if tailCallFilter != "" && ev.CallID != tailCallFilter {
				continue
			}
			if typeSet != nil && !typeSet[ev.Type] {
				continue
			}
			printEvent(ev)
		}
		return nil
	},
}

func printEvent(ev api.Event) {
	ts := ev.Timestamp
	if t, err := time.Parse(time.RFC3339, ev.Timestamp); err == nil {
		ts = t.Local().Format("15:04:05.000")
	}
	fmt.Printf("%s  %-26s  call=%s  seq=%d\n", ts, ev.Type, shortID(ev.CallID), ev.Sequence)
	if len(ev.Payload) > 0 {
		for k, v := range ev.Payload {
			fmt.Printf("    %s: %v\n", k, v)
		}
	}
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func init() {
	tailCmd.Flags().StringVar(&tailCallFilter, "call", "", "filtrar por call_id")
	tailCmd.Flags().StringVar(&tailTypeFilter, "type", "", "filtrar por tipos de evento, separados por coma")
	rootCmd.AddCommand(tailCmd)
}
