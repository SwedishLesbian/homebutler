package remote

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/swedishlesbian/homebutler/internal/config"
	"github.com/swedishlesbian/homebutler/internal/util"
	"golang.org/x/crypto/ssh"
)

func ExecShell(server *config.ServerConfig, program string, args ...string) error {
	client, err := connect(server)
	if err != nil {
		return err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("[%s] failed to open SSH session: %w", server.Name, err)
	}
	defer session.Close()

	fd := int(os.Stdin.Fd())
	term := os.Getenv("TERM")
	if term == "" {
		term = "xterm-256color"
	}
	width, height, _ := getWindowSize(fd)

	modes := ssh.TerminalModes{
		ssh.ECHO:          0,
		ssh.TTY_OP_ISPEED: 115200,
		ssh.TTY_OP_OSPEED: 115200,
	}
	if err := session.RequestPty(term, height, width, modes); err != nil {
		return fmt.Errorf("[%s] failed to request PTY: %w", server.Name, err)
	}

	cmd := program
	if cmd == "" {
		cmd = defaultShell(server)
	}

	sessionStdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("[%s] failed to create stdout pipe: %w", server.Name, err)
	}

	sessionStderr, err := session.StderrPipe()
	if err != nil {
		return fmt.Errorf("[%s] failed to create stderr pipe: %w", server.Name, err)
	}

	sessionStdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("[%s] failed to create stdin pipe: %w", server.Name, err)
	}

	if err := session.Start(buildCommand(cmd, args)); err != nil {
		return fmt.Errorf("[%s] failed to start command: %w", server.Name, err)
	}

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		io.Copy(sessionStdin, os.Stdin)
		sessionStdin.Close()
		wg.Done()
	}()

	go func() {
		io.Copy(os.Stdout, sessionStdout)
		wg.Done()
	}()

	go func() {
		io.Copy(os.Stderr, sessionStderr)
		wg.Done()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGWINCH)

	go func() {
		for sig := range sigCh {
			session.Signal(sigToSSH(sig))
		}
	}()

	sessionWait := make(chan error, 1)
	go func() {
		sessionWait <- session.Wait()
	}()

	select {
	case err := <-sessionWait:
		return err
	case sig := <-sigCh:
		session.Signal(sigToSSH(sig))
		<-sessionWait
		return nil
	}
}

func defaultShell(server *config.ServerConfig) string {
	hostKey, _, err := detectRemoteArchAndOS(clientForDetection(server))
	if err != nil {
		return "/bin/sh"
	}
	if hostKey == "windows" {
		return "powershell.exe"
	}
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash"
	}
	if _, err := os.Stat("/bin/sh"); err == nil {
		return "/bin/sh"
	}
	return "/bin/sh"
}

func buildCommand(program string, args []string) string {
	if len(args) == 0 {
		return program
	}
	result := program
	for _, arg := range args {
		result += " " + util.ShellQuote(arg)
	}
	return result
}

func getWindowSize(fd int) (width, height int, err error) {
	return 80, 24, nil
}

func sigToSSH(sig os.Signal) ssh.Signal {
	switch sig {
	case syscall.SIGINT:
		return ssh.SIGINT
	case syscall.SIGTERM:
		return ssh.SIGTERM
	default:
		return ssh.SIGTERM
	}
}

func clientForDetection(server *config.ServerConfig) *ssh.Client {
	client, err := connect(server)
	if err != nil {
		return nil
	}
	return client
}

func detectRemoteArchAndOS(client *ssh.Client) (os string, arch string, err error) {
	if client == nil {
		return "", "", fmt.Errorf("no client")
	}

	session, err := client.NewSession()
	if err != nil {
		return "", "", err
	}
	defer session.Close()

	out, err := session.CombinedOutput("uname -s && uname -m")
	if err != nil {
		out, err = session.CombinedOutput("cmd /c ver")
		if err != nil {
			return "", "", fmt.Errorf("failed to detect OS: %w", err)
		}
		return "windows", "unknown", nil
	}

	output := string(out)
	switch {
	case containsLower(output, "Linux"):
		return "linux", extractArch(output), nil
	case containsLower(output, "Darwin"):
		return "darwin", extractArch(output), nil
	case containsLower(output, "MINGW") || containsLower(output, "CYGWIN"):
		return "windows", "amd64", nil
	default:
		return "linux", "amd64", nil
	}
}

func containsLower(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func extractArch(output string) string {
	switch {
	case containsLower(output, "x86_64") || containsLower(output, "amd64"):
		return "amd64"
	case containsLower(output, "aarch64") || containsLower(output, "arm64"):
		return "arm64"
	case containsLower(output, "armv7"):
		return "arm"
	default:
		return "amd64"
	}
}

func RunCommand(server *config.ServerConfig, program string, args ...string) ([]byte, error) {
	client, err := connect(server)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("[%s] failed to open SSH session: %w", server.Name, err)
	}
	defer session.Close()

	cmd := program
	if len(args) > 0 {
		cmd = program + " " + util.ShellQuoteArgs(args)
	}

	return session.CombinedOutput(cmd)
}

func DetectServersArch(servers []config.ServerConfig) (map[string]string, error) {
	result := make(map[string]string)
	var errs []string

	for _, server := range servers {
		if server.Local {
			continue
		}
		client, err := connect(&server)
		if err != nil {
			result[server.Name] = ""
			errs = append(errs, fmt.Sprintf("%s: %v", server.Name, err))
			continue
		}

		os, _, err := detectRemoteArchAndOS(client)
		client.Close()
		if err != nil {
			result[server.Name] = ""
			errs = append(errs, fmt.Sprintf("%s: %v", server.Name, err))
			continue
		}
		result[server.Name] = os
	}

	var err error
	if len(errs) > 0 {
		err = fmt.Errorf("some servers could not be detected: %v", errs)
	}
	return result, err
}
