package bench

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func NewEnvironment(repoRoot string, suitePath string, benchRoot string, resultsDir string, toolOptions map[string]string) (Environment, error) {
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return Environment{}, fmt.Errorf("resolve repo root: %w", err)
	}
	suitePath, err = filepath.Abs(suitePath)
	if err != nil {
		return Environment{}, fmt.Errorf("resolve suite path: %w", err)
	}
	benchRoot, err = filepath.Abs(benchRoot)
	if err != nil {
		return Environment{}, fmt.Errorf("resolve benchmark root: %w", err)
	}
	resultsDir, err = filepath.Abs(resultsDir)
	if err != nil {
		return Environment{}, fmt.Errorf("resolve results dir: %w", err)
	}

	env := Environment{
		RepoRoot:      repoRoot,
		SuitePath:     suitePath,
		BenchRoot:     benchRoot,
		CacheRoot:     filepath.Join(benchRoot, "cache"),
		ReposRoot:     filepath.Join(benchRoot, "repos"),
		BinDir:        filepath.Join(benchRoot, "bin"),
		ResultsDir:    resultsDir,
		SemsearchHome: filepath.Join(benchRoot, "cache", "semsearch-home"),
		WarmupRoot:    filepath.Join(benchRoot, "warmup"),
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		CPUInfo:       detectCPUInfo(),
		NumCPU:        runtime.NumCPU(),
		ToolOptions:   cloneMap(toolOptions),
	}

	for _, dir := range []string{
		env.BenchRoot,
		env.CacheRoot,
		env.ReposRoot,
		env.BinDir,
		env.ResultsDir,
		env.SemsearchHome,
		env.WarmupRoot,
		filepath.Join(env.CacheRoot, "gocache"),
		filepath.Join(env.CacheRoot, "gomodcache"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Environment{}, fmt.Errorf("create benchmark dir %s: %w", dir, err)
		}
	}

	return env, nil
}

func detectCPUInfo() string {
	switch runtime.GOOS {
	case "darwin":
		return runBestEffortCommand("sysctl", "-n", "machdep.cpu.brand_string")
	case "linux":
		if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(strings.ToLower(line), "model name") {
					if parts := strings.SplitN(line, ":", 2); len(parts) == 2 {
						return strings.TrimSpace(parts[1])
					}
				}
			}
		}
	case "windows":
		cpuName := runBestEffortCommand(
			"powershell.exe",
			"-NoProfile",
			"-Command",
			"(Get-CimInstance Win32_Processor | Select-Object -First 1 -ExpandProperty Name)",
		)
		systemType := runBestEffortCommand(
			"powershell.exe",
			"-NoProfile",
			"-Command",
			"(Get-CimInstance Win32_ComputerSystem | Select-Object -ExpandProperty SystemType)",
		)
		switch {
		case cpuName != "" && systemType != "":
			return fmt.Sprintf("%s (%s)", cpuName, systemType)
		case cpuName != "":
			return cpuName
		case systemType != "":
			return systemType
		}
		if arch := strings.TrimSpace(os.Getenv("PROCESSOR_ARCHITEW6432")); arch != "" {
			return arch
		}
		if arch := strings.TrimSpace(os.Getenv("PROCESSOR_ARCHITECTURE")); arch != "" {
			return arch
		}
	}

	return strings.TrimSpace(runBestEffortCommand("uname", "-m"))
}

func runBestEffortCommand(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(stdout.String())
}

func cloneMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
