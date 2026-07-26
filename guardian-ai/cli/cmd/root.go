package cmd

import (
	"fmt"
	"os"
	"time"

	"guardianai/cli/internal/config"

	"github.com/spf13/cobra"
)

var cfg config.Config

var rootCmd = &cobra.Command{
	Use:   "secura",
	Short: "Secura CLI — Guardian AI Operations Center",
	Long:  "Secura CLI opera el ecosistema Guardian AI (Colsubsidio) desde la terminal: WhatsApp, voz, LLM, RAG y analytics en vivo.",
	RunE: func(c *cobra.Command, args []string) error {
		return runTUI(cfg)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfg.APIURL, "api-url", envOr("SECURA_API_URL", "http://localhost:8099"), "URL base del backend guardian-ai")
	rootCmd.PersistentFlags().BoolVar(&cfg.JSON, "json", false, "salida JSON (no interactivo)")
	rootCmd.PersistentFlags().BoolVar(&cfg.NoColor, "no-color", false, "desactivar color")
	rootCmd.PersistentFlags().DurationVar(&cfg.Timeout, "timeout", 10*time.Second, "timeout de red")
	rootCmd.PersistentFlags().BoolVar(&cfg.Demo, "demo", false, "forzar fixtures embebidos, sin red")
	rootCmd.PersistentFlags().BoolVar(&cfg.Chaos, "chaos", false, "forzar estados límite bajo demanda")
	rootCmd.PersistentFlags().StringVar(&cfg.Replay, "replay", "", "reproducir una llamada grabada por call_id")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
