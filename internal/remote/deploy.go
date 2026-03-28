package remote

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/util"
	"golang.org/x/crypto/ssh"
)

const (
	releaseURL = "https://github.com/Higangssh/homebutler/releases/latest/download"
)

// DeployResult holds the result of a deploy operation.
type DeployResult struct {
	Server  string `json:"server"`
	Arch    string `json:"arch"`
	Source  string `json:"source"` // "github" or "local"
	Status  string `json:"status"` // "ok" or "error"
	Message string `json:"message,omitempty"`
}

// Deploy installs homebutler on a remote server.
// If localBin is set, it copies that file directly (air-gapped mode).
// Otherwise, it downloads the correct binary from GitHub Releases.
func Deploy(server *config.ServerConfig, localBin string) (*DeployResult, error) {
	result := &DeployResult{Server: server.Name}

	client, err := connect(server)
	if err != nil {
		return nil, fmt.Errorf("ssh connect to %s: %w", server.Name, err)
	}
	defer client.Close()

	remoteOS, remoteArch, err := detectRemoteArch(client)
	if err != nil {
		return nil, fmt.Errorf("detect arch on %s: %w", server.Name, err)
	}
	result.Arch = remoteOS + "/" + remoteArch

	installDir, err := detectInstallDir(client)
	if err != nil {
		return nil, fmt.Errorf("detect install dir on %s: %w", server.Name, err)
	}

	isWindows := remoteOS == "windows"
	binaryName := "homebutler"
	if isWindows {
		binaryName = "homebutler.exe"
	}
	remotePath := installDir + string(os.PathSeparator) + binaryName

	if localBin != "" {
		result.Source = "local"
		data, err := os.ReadFile(localBin)
		if err != nil {
			return nil, fmt.Errorf("read local binary: %w", err)
		}
		if err := scpUpload(client, data, remotePath, 0755); err != nil {
			return nil, fmt.Errorf("upload to %s: %w", server.Name, err)
		}
	} else {
		result.Source = "github"
		data, err := downloadRelease(remoteOS, remoteArch)
		if err != nil {
			return nil, fmt.Errorf("download for %s/%s: %w\n\nFor air-gapped environments, use:\n  homebutler deploy --server %s --local ./homebutler-%s-%s",
				remoteOS, remoteArch, err, server.Name, remoteOS, remoteArch)
		}
		if err := scpUpload(client, data, remotePath, 0755); err != nil {
			return nil, fmt.Errorf("upload to %s: %w", server.Name, err)
		}
	}

	// Set executable permissions
	if isWindows {
		runSession(client, fmt.Sprintf(`icacls "%s" /grant Everyone:RX 2>nul`, remotePath))
	} else {
		runSession(client, "chmod +x "+remotePath)
	}

	// Verify installation
	var verifyCmd string
	if isWindows {
		verifyCmd = fmt.Sprintf(`powershell -NoProfile -Command "& '%s' version"`, remotePath)
	} else {
		verifyCmd = fmt.Sprintf("export PATH=$PATH:%s; %s version", installDir, binaryName)
	}

	if err := runSession(client, verifyCmd); err != nil {
		result.Status = "error"
		result.Message = "uploaded but verification failed: " + err.Error()
		return result, nil
	}

	if !isWindows {
		ensurePath(client, installDir)
	}

	result.Status = "ok"
	result.Message = fmt.Sprintf("installed to %s (%s/%s)", remotePath, remoteOS, remoteArch)
	return result, nil
}

// DeployLocal validates architecture match when deploying current binary without --local flag.
func ValidateLocalArch(remoteOS, remoteArch string) error {
	localOS := runtime.GOOS
	localArch := runtime.GOARCH
	if localOS != remoteOS || localArch != remoteArch {
		return fmt.Errorf("local binary is %s/%s but remote is %s/%s\n\n"+
			"To deploy to a different architecture in air-gapped environments:\n"+
			"  1. Cross-compile: CGO_ENABLED=0 GOOS=%s GOARCH=%s go build -o homebutler-%s-%s\n"+
			"  2. Deploy: homebutler deploy --server <name> --local ./homebutler-%s-%s",
			localOS, localArch, remoteOS, remoteArch,
			remoteOS, remoteArch, remoteOS, remoteArch,
			remoteOS, remoteArch)
	}
	return nil
}

// detectInstallDir finds the best install location on the remote server.
// Windows: %LOCALAPPDATA%\homebutler > %USERPROFILE%\bin
// Unix: /usr/local/bin (writable or via sudo) > ~/.local/bin
func detectInstallDir(client *ssh.Client) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	// Check if Windows
	out, _ := session.CombinedOutput("TERM=dumb uname -s 2>/dev/null")
	osName := strings.ToLower(strings.TrimSpace(string(out)))

	if osName == "windows" || strings.Contains(osName, "win") {
		// Windows paths
		if err := runSession(client, `if exist "C:\Program Files\homebutler" echo exists`); err == nil {
			return `C:\Program Files\homebutler`, nil
		}
		if err := runSession(client, `if exist "%LOCALAPPDATA%\homebutler" echo exists`); err == nil {
			runSession(client, `if not exist "%LOCALAPPDATA%\homebutler" mkdir "%LOCALAPPDATA%\homebutler"`)
			return `%LOCALAPPDATA%\homebutler`, nil
		}
		runSession(client, `mkdir "%USERPROFILE%\bin" 2>nul`)
		return `%USERPROFILE%\bin`, nil
	}

	// Unix paths
	if err := runSession(client, "test -w /usr/local/bin"); err == nil {
		return "/usr/local/bin", nil
	}
	if err := runSession(client, "sudo -n test -w /usr/local/bin 2>/dev/null"); err == nil {
		runSession(client, "sudo mkdir -p /usr/local/bin")
		return "/usr/local/bin", nil
	}
	runSession(client, "mkdir -p $HOME/.local/bin")
	return "$HOME/.local/bin", nil
}

// ensurePath adds installDir to PATH in shell rc files if not already present.
// Covers .profile, .bashrc, and .zshrc for broad compatibility.
func ensurePath(client *ssh.Client, installDir string) {
	if installDir == "/usr/local/bin" {
		return // already in PATH on most systems
	}

	exportLine := fmt.Sprintf(`export PATH="$PATH:%s"`, installDir)
	rcFiles := []string{"$HOME/.profile", "$HOME/.bashrc", "$HOME/.zshrc"}

	for _, rc := range rcFiles {
		// Only patch files that exist
		checkExist := fmt.Sprintf(`test -f %s`, rc)
		if err := runSession(client, checkExist); err != nil {
			continue
		}
		// Skip if already present
		checkCmd := fmt.Sprintf(`grep -qF '%s' %s 2>/dev/null`, installDir, rc)
		if err := runSession(client, checkCmd); err != nil {
			addCmd := fmt.Sprintf(`echo '%s' >> %s`, exportLine, rc)
			runSession(client, addCmd)
		}
	}
}

func detectRemoteArch(client *ssh.Client) (string, string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", "", err
	}
	defer session.Close()

	// Try uname first (Linux/macOS)
	out, err := session.CombinedOutput("TERM=dumb uname -s -m 2>/dev/null")
	if err == nil {
		parts := strings.Fields(strings.TrimSpace(string(out)))
		if len(parts) >= 2 {
			osName := strings.ToLower(parts[0])
			arch := normalizeArch(parts[1])
			return osName, arch, nil
		}
	}

	// Fallback: try PowerShell for Windows
	session2, err := client.NewSession()
	if err == nil {
		defer session2.Close()
		out2, err := session2.CombinedOutput(`powershell -NoProfile -Command "[Environment]::OSVersion.Platform; if($env:PROCESSOR_ARCHITECTURE){$env:PROCESSOR_ARCHITECTURE}else{'AMD64'}"`)
		if err == nil {
			output := strings.TrimSpace(string(out2))
			if strings.Contains(output, "Win32NT") || strings.Contains(output, "WinNT") {
				arch := "amd64"
				if strings.Contains(output, "ARM64") {
					arch = "arm64"
				}
				return "windows", arch, nil
			}
		}
	}

	// Final fallback: try systeminfo for Windows Server
	session3, err := client.NewSession()
	if err == nil {
		defer session3.Close()
		out3, err := session3.CombinedOutput("systeminfo 2>/dev/null | findstr /C:\"OS Name\" /C:\"Processor(s)\" || echo")
		if err == nil {
			output := strings.ToLower(string(out3))
			if strings.Contains(output, "windows") {
				arch := "amd64"
				if strings.Contains(output, "arm64") {
					arch = "arm64"
				}
				return "windows", arch, nil
			}
		}
	}

	return "", "", fmt.Errorf("cannot detect OS/arch: uname failed, no PowerShell or systeminfo available")
}

func normalizeArch(arch string) string {
	switch arch {
	case "x86_64":
		return "amd64"
	case "aarch64":
		return "arm64"
	default:
		return arch
	}
}

var assetSuffixes = map[string]string{
	"linux/amd64":   "linux_amd64",
	"linux/arm64":   "linux_arm64",
	"darwin/amd64":  "darwin_amd64",
	"darwin/arm64":  "darwin_arm64",
	"windows/amd64": "windows_amd64",
	"windows/arm64": "windows_arm64",
}

func downloadRelease(osName, arch string, version ...string) ([]byte, error) {
	var tagName, filename, url string

	if len(version) > 0 && version[0] != "" {
		tagName = "v" + version[0]
	} else {
		tagName, _ = FetchLatestVersion()
		if tagName == "" {
			tagName = "latest"
		}
	}

	suffix := assetSuffixes[osName+"/"+arch]
	if suffix == "" {
		return nil, fmt.Errorf("unsupported platform: %s/%s", osName, arch)
	}

	if strings.HasPrefix(tagName, "v") {
		filename = fmt.Sprintf("homebutler_%s_%s.tar.gz", strings.TrimPrefix(tagName, "v"), suffix)
		url = fmt.Sprintf("https://github.com/Higangssh/homebutler/releases/download/%s/%s", tagName, filename)
	} else {
		filename = fmt.Sprintf("homebutler_%s_%s.tar.gz", tagName, suffix)
		url = fmt.Sprintf("https://github.com/Higangssh/homebutler/releases/%s/download/%s", tagName, filename)
	}

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download failed: HTTP %d for %s", resp.StatusCode, url)
	}

	tarData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if err := verifyChecksum(tarData, filename, version...); err != nil {
		return nil, fmt.Errorf("checksum verification failed: %w", err)
	}

	return extractBinaryFromTarGz(tarData)
}

// verifyChecksum downloads checksums.txt and verifies the SHA256 hash.
func verifyChecksum(data []byte, filename string, version ...string) error {
	var checksumsURL string
	if len(version) > 0 && version[0] != "" {
		checksumsURL = fmt.Sprintf("https://github.com/Higangssh/homebutler/releases/download/v%s/checksums.txt", version[0])
	} else {
		checksumsURL = releaseURL + "/checksums.txt"
	}

	resp, err := http.Get(checksumsURL)
	if err != nil {
		return fmt.Errorf("cannot fetch checksums: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		// No checksums available — skip verification for older releases
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read checksums: %w", err)
	}

	// Find expected hash for our file
	var expectedHash string
	for _, line := range strings.Split(string(body), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == filename {
			expectedHash = parts[0]
			break
		}
	}

	if expectedHash == "" {
		return fmt.Errorf("no checksum found for %s", filename)
	}

	// Compute actual hash
	h := sha256.Sum256(data)
	actualHash := hex.EncodeToString(h[:])

	if actualHash != expectedHash {
		return fmt.Errorf("hash mismatch for %s\n  expected: %s\n  got:      %s", filename, expectedHash, actualHash)
	}

	return nil
}

func runSession(client *ssh.Client, cmd string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	return session.Run(cmd)
}

// scpUpload writes data to a remote file using SCP protocol.
func scpUpload(client *ssh.Client, data []byte, remotePath string, mode os.FileMode) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	go func() {
		w, _ := session.StdinPipe()
		defer w.Close()
		fmt.Fprintf(w, "C%04o %d %s\n", mode, len(data), filepath.Base(remotePath))
		w.Write(data)
		fmt.Fprint(w, "\x00")
	}()

	return session.Run(fmt.Sprintf("scp -t %s", util.ShellQuote(remotePath)))
}
