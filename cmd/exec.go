package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/remote"
)

func runExec(cfg *config.Config) error {
	if len(os.Args) < 3 {
		return fmt.Errorf(`usage: homebutler exec <program> [args...] [--server <name>]

Interactive (single server):
  homebutler exec /bin/bash --server myserver
  homebutler exec pwsh --server winserver

Batch (--all runs in background):
  homebutler exec --all "yum update -y"
  homebutler exec --all --arch linux "apt update && apt upgrade -y"
  homebutler exec --all --arch windows "Update-Module -Name * -Force"`)
	}

	program := os.Args[2]
	args := os.Args[3:]

	allServers := hasFlag("--all")
	serverName := getFlag("--server", "")
	archFilter := getFlag("--arch", "")

	if allServers && serverName != "" {
		return fmt.Errorf("cannot use both --server and --all")
	}

	if archFilter != "" && !allServers {
		return fmt.Errorf("--arch requires --all flag")
	}

	if allServers {
		return runExecAllServers(cfg, program, args, archFilter)
	}

	if serverName != "" {
		server := cfg.FindServer(serverName)
		if server == nil {
			return fmt.Errorf("server %q not found in config", serverName)
		}
		if server.Local {
			return fmt.Errorf("cannot use --server with local server; run directly")
		}
		return remote.ExecShell(server, program, args...)
	}

	return fmt.Errorf("exec requires --server flag for remote execution")
}

func runExecAllServers(cfg *config.Config, program string, args []string, archFilter string) error {
	if len(cfg.Servers) == 0 {
		return fmt.Errorf("no servers configured")
	}

	servers, err := remote.DetectServersArch(cfg.Servers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to detect some server architectures: %v\n", err)
	}

	for _, server := range cfg.Servers {
		if server.Local {
			continue
		}

		if archFilter != "" {
			detectedArch := servers[server.Name]
			if !strings.EqualFold(detectedArch, archFilter) {
				continue
			}
		}

		fmt.Printf("[%s] running on %s...\n", server.Name, server.Host)
		go runExecServer(&server, program, args)
	}
	return nil
}

func runExecServer(server *config.ServerConfig, program string, args []string) {
	out, err := remote.RunCommand(server, program, args...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] error: %v\n", server.Name, err)
		return
	}
	fmt.Printf("[%s] output:\n%s\n", server.Name, string(out))
}
