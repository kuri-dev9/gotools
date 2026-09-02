package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type config struct {
	OutputDir    string
	Notification bool
	path         string
}

func loadConfig(executable string) config {
	base := filepath.Dir(executable)
	configPath := filepath.Join(base, "b64drop.ini")
	cfg := config{OutputDir: filepath.Join(base, "B64Drop"), Notification: true, path: configPath}
	f, err := os.Open(configPath)
	if err != nil {
		return cfg
	}
	defer f.Close()
	section := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		if section != "general" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(parts[0])) {
		case "output_dir":
			if value := strings.TrimSpace(parts[1]); value != "" {
				if !filepath.IsAbs(value) {
					value = filepath.Join(base, value)
				}
				cfg.OutputDir = filepath.Clean(value)
			}
		case "notification":
			cfg.Notification = strings.EqualFold(strings.TrimSpace(parts[1]), "true") || strings.TrimSpace(parts[1]) == "1"
		}
	}
	return cfg
}

func saveConfig(cfg config) error {
	file, err := os.OpenFile(cfg.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, writeErr := fmt.Fprintf(file, "[general]\r\noutput_dir=%s\r\nnotification=%t\r\n", cfg.OutputDir, cfg.Notification)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
