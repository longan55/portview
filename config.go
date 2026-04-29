package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	AppName          string `json:"app_name"`
	ServerAddr       string `json:"server_addr"`
	DataFile         string `json:"data_file"`
	TcpPortRange     string `json:"tcp_port_range"`
	UdpPortRange     string `json:"udp_port_range"`
	RefreshOnGet     bool   `json:"refresh_on_get"`
	EnableRangeInput bool   `json:"enable_range_input"`
}

func LoadConfig(path string) (Config, error) {
	cfg := Config{
		AppName:          "portview",
		ServerAddr:       "127.0.0.1:8079",
		DataFile:         "ports.json",
		TcpPortRange:     "9100-10000",
		UdpPortRange:     "9100-10000",
		RefreshOnGet:     true,
		EnableRangeInput: true,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return Config{}, err
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	if cfg.AppName == "" {
		cfg.AppName = "portview"
	}
	if cfg.ServerAddr == "" {
		cfg.ServerAddr = "127.0.0.1:8079"
	}
	if cfg.DataFile == "" {
		cfg.DataFile = "ports.json"
	}
	if cfg.TcpPortRange == "" {
		cfg.TcpPortRange = "9100-10000"
	}
	if cfg.UdpPortRange == "" {
		cfg.UdpPortRange = "9100-10000"
	}
	if !cfg.EnableRangeInput {
		cfg.EnableRangeInput = true
	}

	return cfg, nil
}
