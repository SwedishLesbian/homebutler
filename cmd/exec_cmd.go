package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/swedishlesbian/homebutler/internal/config"
	"github.com/swedishlesbian/homebutler/internal/remote"
	"github.com/spf13/cobra"
)

func newExecCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "exec <program> [args...]",
		Short: "Run a program on remote server(s)",
		Long: `Run a program on remote server(s).

Interactive (single server):
  homebutler exec /bin/bash --server myserver
  homebutler exec pwsh --server winserver

Batch (--all runs in background):
  homebutler exec --all "yum update -y"
  homebutler exec --all --arch linux "apt update && apt upgrade -y"
  homebutler exec --all --arch windows "Update-Module -Name * -Force"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(); err != nil {
				return err
			}

			allServers, _ := cmd.Flags().GetBool("all")
			serverName, _ := cmd.Flags().GetString("server")
			archFilter, _ := cmd.Flags().GetString("arch")

			if allServers && serverName != "" {
				return fmt.Errorf("cannot use both --server and --all")
			}

			if archFilter != "" && !allServers {
				return fmt.Errorf("--arch requires --all flag")
			}

			if len(args) < 1 {
				return fmt.Errorf("usage: homebutler exec <program> [args...]")
			}

			program := args[0]
			programArgs := args[1:]

			if allServers {
				return runExecAllServers(cfg, program, programArgs, archFilter)
			}

			if serverName != "" {
				server := cfg.FindServer(serverName)
				if server == nil {
					return fmt.Errorf("server %q not found in config", serverName)
				}
				if server.Local {
					return fmt.Errorf("cannot use --server with local server; run directly")
				}
				return remote.ExecShell(server, program, programArgs...)
			}

			return fmt.Errorf("exec requires --server flag for remote execution")
		},
	}
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
