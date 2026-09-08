package main

import (
	"github.com/joho/godotenv"

	"github.com/metruzanca/spacefinder/cmd"
	"github.com/metruzanca/spacefinder/internal/logging"
	// Uncomment to use a TOML config file, persisted on first use (see
	// internal/config). Remove if this CLI has no config.
	// "github.com/metruzanca/spacefinder/internal/config"
)

func main() {
	// Load dev-time env overrides from a repo-local .env (see AGENTS.md).
	_ = godotenv.Load()

	logging.Init(cmd.Version)
	defer logging.Close()

	// Uncomment to load (and create on first use) the config file:
	//
	// cfg, err := config.LoadOrCreate()
	// if err != nil {
	// 	fmt.Fprintf(os.Stderr, "spacefinder: config: %v\n", err)
	// 	os.Exit(1)
	// }
	// _ = cfg

	cmd.Execute()
}
