package cmd

import (
	"fmt"
	"os"
	"time"

	"guardianai/cli/internal/api"
	"guardianai/cli/internal/config"

	"github.com/spf13/cobra"
)

var cfg config.Config

// version is stamped at build time via -ldflags "-X guardianai/cli/cmd.version=...".
var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "secura",
	Short: "Secura CLI — Guardian AI Operations Center",
	Long:    "Secura CLI opera el ecosistema Guardian AI (Colsubsidio) desde la terminal: WhatsApp, voz, LLM, RAG y analytics en vivo.",
	Version: version,
	// Resolving here (not in init) means every subcommand — doctor, tail,
	// calls — gets the same endpoint without repeating the logic.
	PersistentPreRunE: func(c *cobra.Command, args []string) error {
		cfg.APIURL = api.ResolveAPIURL(
			c.Context(),
			cfg.APIURL,
			c.Flags().Changed("api-url"),
		)
		return nil
	},
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
	// No envOr here: precedence (flag > env > discovery > localhost) lives in
	// api.ResolveAPIURL, and folding the env var into the flag default would
	// make "the user passed --api-url" indistinguishable from "SECURA_API_URL
	// was set", breaking the discovery step.
	rootCmd.PersistentFlags().StringVar(&cfg.APIURL, "api-url", api.DefaultAPIURL, "URL base del backend guardian-ai (por defecto: descubierto de teamflashackaton30x.com)")
	rootCmd.PersistentFlags().BoolVar(&cfg.JSON, "json", false, "salida JSON (no interactivo)")
	rootCmd.PersistentFlags().BoolVar(&cfg.NoColor, "no-color", false, "desactivar color")
	rootCmd.PersistentFlags().DurationVar(&cfg.Timeout, "timeout", 10*time.Second, "timeout de red")
	rootCmd.PersistentFlags().BoolVar(&cfg.Demo, "demo", false, "forzar fixtures embebidos, sin red")
	rootCmd.PersistentFlags().BoolVar(&cfg.Chaos, "chaos", false, "forzar estados límite bajo demanda")
	rootCmd.PersistentFlags().StringVar(&cfg.Replay, "replay", "", "reproducir una llamada grabada por call_id")
	rootCmd.PersistentFlags().BoolVar(&cfg.ReadOnly, "read-only", false, "bloquear escrituras a producción (publish, rollback)")
}
