package version

import (
	"runtime/debug"
	"testing"
)

func TestString(t *testing.T) {
	vcs := func(rev, modified string) *debug.BuildInfo {
		return &debug.BuildInfo{
			Main:     debug.Module{Version: "v0.0.0-20261001141148-02bdd31a4075"},
			Settings: []debug.BuildSetting{{Key: "vcs", Value: "git"}, {Key: "vcs.revision", Value: rev}, {Key: "vcs.modified", Value: modified}},
		}
	}
	tests := []struct {
		name string
		tag  string
		info *debug.BuildInfo
		want string
	}{
		{"release build", "v0.1.0", vcs("5efe46a4e1cf395dc65ad70f65613092f8cb4ccc", "false"), "v0.1.0"},
		{"pre-release build", "v0.2.0-rc.1", nil, "v0.2.0-rc.1"},
		{"clean local build", "", vcs("5efe46a4e1cf395dc65ad70f65613092f8cb4ccc", "false"), "dev (5efe46a)"},
		{"dirty local build", "", vcs("5efe46a4e1cf395dc65ad70f65613092f8cb4ccc", "true"), "dev (5efe46a, dirty)"},
		{"go install @version", "", &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}}, "v0.1.0"},
		{"no VCS stamp", "", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, "dev"},
		{"no build info", "", nil, "dev"},
	}
	for _, tt := range tests {
		if got := String(tt.tag, tt.info); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
}
