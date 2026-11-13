package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func findOAuthClient(name string) (string, error) {
	executable, err := exec.LookPath(name)
	if err == nil || runtime.GOOS != "darwin" {
		return executable, err
	}
	// Finder-launched applications may not inherit the interactive shell's PATH.
	locations := []string{"/opt/homebrew/bin", "/usr/local/bin"}
	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		locations = append(locations, filepath.Join(home, ".local", "bin"))
	}
	for _, directory := range locations {
		if executable, lookErr := exec.LookPath(filepath.Join(directory, name)); lookErr == nil {
			return executable, nil
		}
	}
	return "", err
}

func oauthTerminal(t Target, action, name, project string) error {
	if (t.ID != "codex" && t.ID != "claude") || (action != "login" && action != "logout") || !serverName.MatchString(name) {
		return errors.New("use the client's MCP panel for this connection")
	}
	if project == "" {
		project, _ = os.UserHomeDir()
	}
	client, err := findOAuthClient(t.ID)
	if err != nil {
		return fmt.Errorf("%s CLI was not found on PATH", t.Label)
	}
	if strings.EqualFold(filepath.Ext(client), ".cmd") || strings.EqualFold(filepath.Ext(client), ".bat") {
		return errors.New("use the copyable command in your terminal for this Windows script launcher")
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"--oauth-terminal", client, action, name}
	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command(self, args...)
		cmd.Dir = project
		configureConsole(cmd)
		return cmd.Start()
	case "darwin":
		command := []string{"cd", shellQuote(project), "&&", "env"}
		for _, key := range []string{"CODEX_HOME", "CLAUDE_CONFIG_DIR"} {
			if val := os.Getenv(key); val != "" {
				command = append(command, shellQuote(key+"="+val))
			}
		}
		for _, item := range append([]string{self}, args...) {
			command = append(command, shellQuote(item))
		}
		script := "on run argv\n tell application \"Terminal\" to do script (item 1 of argv)\n tell application \"Terminal\" to activate\nend run"
		return exec.Command("/usr/bin/osascript", "-e", script, "--", strings.Join(command, " ")).Start()
	default:
		for _, t := range [][2]string{{"x-terminal-emulator", "-e"}, {"gnome-terminal", "--"}, {"konsole", "-e"}, {"kgx", "--"}, {"xfce4-terminal", "-x"}, {"xterm", "-e"}} {
			if executable, err := exec.LookPath(t[0]); err == nil {
				cmd := exec.Command(executable, append([]string{t[1], self}, args...)...)
				cmd.Dir = project
				if err := cmd.Start(); err == nil {
					return nil
				}
			}
		}
		return errors.New("no supported terminal could be opened")
	}
}
func runOAuthHelper(args []string) int {
	if len(args) != 3 || (args[1] != "login" && args[1] != "logout") || !serverName.MatchString(args[2]) {
		return 2
	}
	cmd := exec.Command(args[0], "mcp", args[1], args[2])
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "MCP command failed:", err)
	}
	fmt.Print("\nPress Enter to close.")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return 1
	}
	return 0
}
