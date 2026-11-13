package main

import (
	"os"
	"path/filepath"
	"runtime"
)

type Target struct {
	ID, Label, Scope, Path, Key, EnvStyle, Toggle string
	Remote                                        bool
}

func (t Target) String() string { return t.Label + " · " + t.Scope }

func envPath(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func targets(home, project string) []Target {
	return targetsForOS(home, project, runtime.GOOS)
}

func targetsForOS(home, project, platform string) []Target {
	desktop := filepath.Join(home, ".config", "Claude")
	vscode := filepath.Join(envPath("XDG_CONFIG_HOME", filepath.Join(home, ".config")), "Code", "User")
	switch platform {
	case "darwin":
		base := filepath.Join(home, "Library", "Application Support")
		desktop, vscode = filepath.Join(base, "Claude"), filepath.Join(base, "Code", "User")
	case "windows":
		base := envPath("APPDATA", filepath.Join(home, "AppData", "Roaming"))
		desktop, vscode = filepath.Join(base, "Claude"), filepath.Join(base, "Code", "User")
	}
	list := []Target{
		{"codex", "Codex", "User", filepath.Join(envPath("CODEX_HOME", filepath.Join(home, ".codex")), "config.toml"), "mcp_servers", "codex", "codex", true},
		{"claude", "Claude Code", "User", filepath.Join(envPath("CLAUDE_CONFIG_DIR", home), ".claude.json"), "mcpServers", "braces", "claude", true},
		{"desktop", "Claude Desktop", "User", filepath.Join(desktop, "claude_desktop_config.json"), "mcpServers", "", "", false},
		{"gemini", "Gemini CLI", "User", filepath.Join(envPath("GEMINI_CLI_HOME", home), ".gemini", "settings.json"), "mcpServers", "gemini", "", true},
		{"cursor", "Cursor", "User", filepath.Join(home, ".cursor", "mcp.json"), "mcpServers", "editor", "", true},
		{"vscode", "VS Code", "User", filepath.Join(vscode, "mcp.json"), "servers", "editor", "", true},
	}
	if project != "" {
		list = append(list,
			Target{"codex", "Codex", "Workspace", filepath.Join(project, ".codex", "config.toml"), "mcp_servers", "codex", "codex", true},
			Target{"claude", "Claude Code", "Workspace", filepath.Join(project, ".mcp.json"), "mcpServers", "braces", "", true},
			Target{"gemini", "Gemini CLI", "Workspace", filepath.Join(project, ".gemini", "settings.json"), "mcpServers", "gemini", "", true},
			Target{"cursor", "Cursor", "Workspace", filepath.Join(project, ".cursor", "mcp.json"), "mcpServers", "editor", "", true},
			Target{"vscode", "VS Code", "Workspace", filepath.Join(project, ".vscode", "mcp.json"), "servers", "editor", "", true},
		)
	}
	return list
}
