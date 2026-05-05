package main

import (
	"fmt"
	"log"
)

var Version = "v1.1_26/05/05"

func main() {
	initLogger()
	logAction("startup", map[string]any{"config": "config.json"})
	cfg, err := LoadConfig("config.json")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	app := NewApp(cfg)

	log.Printf("starting %s on %s", cfg.AppName, cfg.ServerAddr)
	fmt.Printf("starting %s on %s\n", cfg.AppName, cfg.ServerAddr)
	if err := app.Run(); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}
