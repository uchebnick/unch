package cli

import (
	"fmt"
	"io"
	"runtime/debug"
	"strings"
)

var buildVersion string

func printVersion(w io.Writer) error {
	_, err := fmt.Fprintln(w, versionString())
	return err
}

func versionString() string {
	if version := strings.TrimSpace(buildVersion); version != "" {
		return version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}

	if version := strings.TrimSpace(info.Main.Version); version != "" && version != "(devel)" {
		return version
	}

	var revision string
	var dirty bool
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = strings.TrimSpace(setting.Value)
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}

	if revision == "" {
		return "dev"
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if dirty {
		return "devel+" + revision + "-dirty"
	}
	return "devel+" + revision
}
