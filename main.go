package main

import (
	"log"
)

func main() {
	cfg, err := LoadConfig("config.json")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	app := NewApp(cfg)

	log.Printf("starting %s on %s", cfg.AppName, cfg.ServerAddr)
	if err := app.Run(); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}
