// Package version names the running build for --version.
package version

import "runtime/debug"

// String is the release tag set at build time (-ldflags "-X main.tag=v1.2.3"), else
// "dev (<revision>[, dirty])" from the VCS stamp of a local build, else the module version
// of a `go install …@version` build, else "dev".
func String(tag string, info *debug.BuildInfo) string {
	if tag != "" {
		return tag
	}
	if info == nil {
		return "dev"
	}
	var revision string
	dirty := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if revision != "" {
		if len(revision) > 7 {
			revision = revision[:7]
		}
		if dirty {
			return "dev (" + revision + ", dirty)"
		}
		return "dev (" + revision + ")"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	return "dev"
}

// Current is String for this binary.
func Current(tag string) string {
	info, _ := debug.ReadBuildInfo()
	return String(tag, info)
}
