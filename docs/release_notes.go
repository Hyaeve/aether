package docs

import "embed"

//go:embed releases/v*.md
var releaseNotes embed.FS

func ReleaseNotes(version string) string {
	if len(version) > 32 {
		return ""
	}
	for _, c := range version {
		if c != '.' && (c < '0' || c > '9') {
			return ""
		}
	}
	data, _ := releaseNotes.ReadFile("releases/v" + version + ".md")
	return string(data)
}
