package team

import (
	_ "embed"
	"runtime/debug"
)

//go:embed AGPL-3.0-only.txt
var LicenseText string

type About struct {
	Version   string `json:"version"`
	License   string `json:"license"`
	Copyright string `json:"copyright"`
	Revision  string `json:"revision"`
	Modified  bool   `json:"modified"`
	Source    string `json:"source"`
}

func BuildAbout() About {
	a := About{Version: "2.0.0-rc.1", License: "AGPL-3.0-only", Copyright: "2026 GeneJie199", Source: "https://github.com/GeneJie199/ai-dev-cycle-manager"}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				a.Revision = setting.Value
			case "vcs.modified":
				a.Modified = setting.Value == "true"
			}
		}
	}
	if a.Revision != "" {
		a.Source += "/tree/" + a.Revision
	}
	return a
}
