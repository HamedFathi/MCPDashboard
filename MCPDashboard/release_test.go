package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestClientPathsAcrossPlatforms(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("APPDATA", filepath.Join(home, "Roaming"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex-custom"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude-custom"))
	t.Setenv("GEMINI_CLI_HOME", filepath.Join(home, "gemini-custom"))
	for _, test := range []struct {
		platform, desktop, vscode string
	}{
		{"darwin", "Library/Application Support/Claude/claude_desktop_config.json", "Library/Application Support/Code/User/mcp.json"},
		{"windows", "Roaming/Claude/claude_desktop_config.json", "Roaming/Code/User/mcp.json"},
		{"linux", ".config/Claude/claude_desktop_config.json", "xdg/Code/User/mcp.json"},
	} {
		t.Run(test.platform, func(t *testing.T) {
			got := map[string]string{}
			for _, target := range targetsForOS(home, workspace, test.platform) {
				got[target.ID+"/"+target.Scope] = target.Path
			}
			for key, want := range map[string]string{
				"desktop/User":     filepath.Join(home, filepath.FromSlash(test.desktop)),
				"vscode/User":      filepath.Join(home, filepath.FromSlash(test.vscode)),
				"claude/User":      filepath.Join(home, "claude-custom", ".claude.json"),
				"codex/User":       filepath.Join(home, "codex-custom", "config.toml"),
				"gemini/User":      filepath.Join(home, "gemini-custom", ".gemini", "settings.json"),
				"vscode/Workspace": filepath.Join(workspace, ".vscode", "mcp.json"),
			} {
				if got[key] != want {
					t.Errorf("%s = %q; want %q", key, got[key], want)
				}
			}
		})
	}
}

func TestReleaseProbeMetadataAndNoRedirect(t *testing.T) {
	t.Setenv("RELEASE_PROBE_TOKEN", "local-test-token")
	var redirected atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Store(true)
	}))
	defer other.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Params struct {
				ClientInfo struct{ Name, Version string }
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil ||
			body.Params.ClientInfo.Name != "MCPDashboard" || body.Params.ClientInfo.Version != "1.0.0" ||
			r.Header.Get("Authorization") != "Bearer local-test-token" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer s.Close()
	r := Registration{Transport: "http", entry: object{"url": s.URL, "bearer_token_env_var": "RELEASE_PROBE_TOKEN"}}
	_, err := probe(t.Context(), r)
	if err == nil || !strings.Contains(err.Error(), "HTTP 307") || redirected.Load() {
		t.Fatalf("probe metadata or redirect guard failed: %v", err)
	}
}

func TestMicrosoftLearnLive(t *testing.T) {
	if os.Getenv("MCPD_LIVE_SMOKE") != "1" {
		t.Skip("set MCPD_LIVE_SMOKE=1 to test the public Microsoft Learn endpoint")
	}
	r := Registration{Transport: "http", entry: object{"url": "https://learn.microsoft.com/api/mcp"}}
	result, err := probe(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(result)
}
