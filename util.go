package main

import (
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
