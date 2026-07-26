package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"guardianai/cli/internal/api"

	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Health + capabilities + inventario en vivo",
	RunE: func(c *cobra.Command, args []string) error {
		ctx := context.Background()
		client := api.NewClient(cfg.APIURL, cfg.Timeout)

		health, latency, herr := client.Health(ctx)
		caps, cerr := client.Capabilities(ctx)
		calls, _ := client.ListCalls(ctx)

		type report struct {
			APIURL     string           `json:"api_url"`
			Health     api.Health       `json:"health"`
			LatencyMS  int64            `json:"latency_ms"`
			HealthErr  string           `json:"health_error,omitempty"`
			Caps       api.Capabilities `json:"capabilities"`
			CapsErr    string           `json:"capabilities_error,omitempty"`
			CallsCount int              `json:"calls_count"`
			AllGreen   bool             `json:"all_capabilities_ok"`
		}

		rep := report{
			APIURL:     cfg.APIURL,
			Health:     health,
			LatencyMS:  latency.Milliseconds(),
			Caps:       caps,
			CallsCount: len(calls),
			AllGreen:   caps.LLM && caps.ElevenLabs && caps.Vapi && caps.WhatsApp && caps.Colsubsidio && caps.Guardian && caps.VapiWeb,
		}
		if herr != nil {
			rep.HealthErr = herr.Error()
		}
		if cerr != nil {
			rep.CapsErr = cerr.Error()
		}

		if cfg.JSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(rep)
		}

		printHuman(rep.APIURL, health, latency, herr, caps, cerr, len(calls))
		if herr != nil || cerr != nil {
			os.Exit(1)
		}
		return nil
	},
}

func printHuman(apiURL string, h api.Health, latency time.Duration, herr error, caps api.Capabilities, cerr error, callsCount int) {
	fmt.Printf("secura doctor — %s\n\n", apiURL)

	if herr != nil {
		fmt.Printf("✗ API health: %v\n", herr)
	} else {
		fmt.Printf("✓ API health: %s (%s) — %s\n", h.Status, h.Service, latency)
	}

	if cerr != nil {
		fmt.Printf("✗ Capabilities: %v\n\n", cerr)
		return
	}

	row := func(name string, ok bool) {
		mark := "✓"
		if !ok {
			mark = "✗"
		}
		fmt.Printf("  %s %s\n", mark, name)
	}
	fmt.Println("\nCapabilities:")
	row("LLM (OpenRouter)", caps.LLM)
	row("ElevenLabs (voz)", caps.ElevenLabs)
	row("Vapi (llamadas)", caps.Vapi)
	row("Vapi Web", caps.VapiWeb)
	row("WhatsApp (Kapso)", caps.WhatsApp)
	row("Colsubsidio Protege API", caps.Colsubsidio)
	row("Guardian core", caps.Guardian)

	fmt.Printf("\nLlamadas persistidas: %d\n", callsCount)
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
