package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

func nowString() string {
	return time.Now().Format(time.RFC3339)
}

func portLabel(port int) string {
	if name, ok := defaultKnownPorts()[port]; ok {
		return name
	}
	return strconv.Itoa(port)
}

func normalizeProtocol(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "udp":
		return "udp"
	default:
		return "tcp"
	}
}

func recordKey(port int, protocol string) string {
	return strconv.Itoa(port) + ":" + normalizeProtocol(protocol)
}

func initLogger() {
	file, err := os.OpenFile("log.txt", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("failed to open log file: %v", err)
		return
	}
	log.SetOutput(file)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("logger initialized")
}

func logAction(action string, fields map[string]any) {
	if len(fields) == 0 {
		log.Printf("%s", action)
		return
	}
	log.Printf("%s %s", action, formatLogFields(fields))
}

func formatLogFields(fields map[string]any) string {
	if len(fields) == 0 {
		return "{}"
	}
	parts := make([]string, 0, len(fields))
	for k, v := range fields {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	sortStrings(parts)
	return "{" + strings.Join(parts, ", ") + "}"
}

func sortStrings(values []string) {
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}
