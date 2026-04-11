package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/KonjacBot/packetizer/internal/generator"
	"gopkg.in/yaml.v3"
)

func main() {
	configPath := flag.String("config", "", "generation config file")
	flag.Parse()

	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "missing required -config input")
		os.Exit(1)
	}
	if err := runConfig(*configPath); err != nil {
		fmt.Fprintln(os.Stderr, "ProtoDef generation error:", err)
		os.Exit(1)
	}
}

func runConfig(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var cfg generator.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return err
	}
	return generator.Run(cfg)
}
