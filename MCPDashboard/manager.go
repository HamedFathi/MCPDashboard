package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

var (
	serverName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)
	envName    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	headerName = regexp.MustCompile(`^[!#$%&'*+.^_|~0-9A-Za-z-]+$`)
)

type Definition struct {
	Name, Transport, URL, Command, Bearer string
	Args, Env                             []string
	Headers                               map[string]string
	ResetAuth                             bool
}
type Registration struct {
	Target                       Target
	Name, Transport, Auth, State string
	Parked                       bool
	entry                        object
}
type Manager struct{ Home, Project, StateDir string }

type RecoveryNeeded struct {
	Target Target
	Issue  *ConfigError
}

func (e *RecoveryNeeded) Error() string { return e.Target.String() + "\n" + e.Issue.Error() }
func (e *RecoveryNeeded) Unwrap() error { return e.Issue }

func targetTransport(t Target, e object) string {
	switch {
	case stringValue(e, "command") != "":
		return "stdio"
	case stringValue(e, "httpUrl") != "":
		return "http"
	case stringValue(e, "url") != "":
		kind := stringValue(e, "type")
		if kind == "sse" || t.ID == "gemini" {
			return "sse"
		}
		if kind == "ws" {
			return "ws"
		}
		return "http"
	default:
		return "unknown"
	}
}
func authLabel(e object, kind string) string {
	for _, key := range []string{"bearer_token_env_var", "env_http_headers", "headers", "http_headers", "http_headers_helper", "headersHelper"} {
		if e[key] != nil {
			return "Headers / token"
		}
	}
	if kind == "stdio" {
		if e["env"] != nil || e["env_vars"] != nil {
			return "Environment"
		}
		return "—"
	}
	return "OAuth / public"
}
func requireURL(raw string, secret bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
		return errors.New("enter an HTTP(S) URL without embedded credentials or a fragment")
	}
	host := u.Hostname()
	local := strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
	if secret && u.Scheme != "https" && !local {
		return errors.New("auth headers require HTTPS or loopback HTTP")
	}
	return nil
}
func (d Definition) validate() error {
	if !serverName.MatchString(d.Name) {
		return errors.New("use a server name starting with a letter, digit, or underscore")
	}
	switch d.Transport {
	case "stdio":
		if strings.TrimSpace(d.Command) == "" {
			return errors.New("enter an executable such as npx, uvx, or an absolute path")
		}
		if d.Bearer != "" || len(d.Headers) != 0 {
			return errors.New("HTTP headers require a remote transport")
		}
	case "http", "sse":
		if len(d.Env) != 0 {
			return errors.New("environment forwarding requires stdio")
		}
		if err := requireURL(d.URL, d.Bearer != "" || len(d.Headers) != 0); err != nil {
			return err
		}
	default:
		return errors.New("choose Streamable HTTP, SSE, or stdio")
	}
	if d.Bearer != "" && !envName.MatchString(d.Bearer) {
		return errors.New("enter the Bearer variable name without its value")
	}
	seen := map[string]bool{}
	for header, variable := range d.Headers {
		key := strings.ToLower(header)
		if !headerName.MatchString(header) || !envName.MatchString(variable) {
			return errors.New("use HEADER=VARIABLE and enter only the environment variable name")
		}
		if seen[key] || (d.Bearer != "" && key == "authorization") {
			return errors.New("configure each header once")
		}
		seen[key] = true
	}
	for _, variable := range d.Env {
		if !envName.MatchString(variable) {
			return errors.New("enter environment variable names without their values")
		}
	}
	return nil
}
func makeEntry(t Target, d Definition) (object, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}
	e := object{}
	if d.Transport == "stdio" {
		e["command"] = d.Command
		if len(d.Args) > 0 {
			e["args"] = d.Args
		}
	} else {
		if !t.Remote {
			return nil, fmt.Errorf("%s local configuration accepts stdio; use hosted connectors for remote servers", t.Label)
		}
		if d.Transport == "sse" && t.ID == "codex" {
			return nil, errors.New("Codex requires Streamable HTTP for remote servers")
		}
		key := "url"
		if t.ID == "gemini" && d.Transport == "http" {
			key = "httpUrl"
		}
		e[key] = d.URL
	}
	if t.ID == "claude" || t.ID == "vscode" || (t.ID == "cursor" && d.Transport == "sse") {
		e["type"] = d.Transport
	}
	if err := applyAuth(t, e, d); err != nil {
		return nil, err
	}
	return e, nil
}
func variableRef(style, name string) string {
	if style == "editor" {
		return "${env:" + name + "}"
	}
	return "${" + name + "}"
}
func patchHeaders(entry object, key string, replacements object, affected map[string]bool) error {
	headers, err := child(entry, key, len(replacements) > 0)
	if err != nil {
		return err
	}
	for name := range headers {
		if affected[strings.ToLower(name)] {
			delete(headers, name)
		}
	}
	for name, value := range replacements {
		headers[name] = value
	}
	return nil
}
func applyAuth(t Target, e object, d Definition) error {
	if len(d.Env) > 0 {
		switch t.EnvStyle {
		case "":
			return fmt.Errorf("%s does not document environment references for local entries; use a launcher that reads inherited variables", t.Label)
		case "codex":
			names := map[string]bool{}
			for _, name := range d.Env {
				names[name] = true
			}
			current, ok := e["env_vars"].([]any)
			if e["env_vars"] != nil && !ok {
				return errors.New("Codex env_vars must be an array")
			}
			var result []any
			for _, value := range current {
				switch x := value.(type) {
				case string:
					if !names[x] {
						result = append(result, value)
					}
				case map[string]any:
					if !names[stringValue(x, "name")] {
						result = append(result, value)
					}
				default:
					return errors.New("unexpected Codex env_vars entry")
				}
			}
			for _, name := range d.Env {
				if names[name] {
					result = append(result, name)
					delete(names, name)
				}
			}
			e["env_vars"] = result
			overrides, err := child(e, "env", false)
			if err != nil {
				return err
			}
			for _, name := range d.Env {
				delete(overrides, name)
			}
		default:
			vars, err := child(e, "env", true)
			if err != nil {
				return err
			}
			for _, name := range d.Env {
				vars[name] = variableRef(t.EnvStyle, name)
			}
		}
	}
	if d.Bearer == "" && len(d.Headers) == 0 {
		return nil
	}
	if t.ID == "gemini" {
		return errors.New("configure Gemini CLI HTTP credentials in Gemini CLI")
	}
	if t.EnvStyle == "" {
		return errors.New("environment-backed headers are unavailable for this destination")
	}
	affected := map[string]bool{}
	headers := object{}
	for key, value := range d.Headers {
		affected[strings.ToLower(key)] = true
		if t.EnvStyle == "codex" {
			headers[key] = value
		} else {
			headers[key] = variableRef(t.EnvStyle, value)
		}
	}
	if d.Bearer != "" {
		affected["authorization"] = true
	}
	if t.EnvStyle == "codex" {
		if err := patchHeaders(e, "http_headers", nil, affected); err != nil {
			return err
		}
		if err := patchHeaders(e, "env_http_headers", headers, affected); err != nil {
			return err
		}
		if d.Bearer != "" {
			e["bearer_token_env_var"] = d.Bearer
		} else if affected["authorization"] {
			delete(e, "bearer_token_env_var")
		}
		return nil
	}
	if d.Bearer != "" {
		headers["Authorization"] = "Bearer " + variableRef(t.EnvStyle, d.Bearer)
	}
	return patchHeaders(e, "headers", headers, affected)
}

func (m Manager) parkedFile(t Target) string {
	identity := sha256.Sum256([]byte(t.ID + "\x00" + t.Path))
	return filepath.Join(m.StateDir, "paused", fmt.Sprintf("%x.json", identity[:16]))
}
func claudeProject(c *config, project string, create bool) (object, error) {
	if project == "" {
		return nil, errors.New("choose the Claude Code launch folder first")
	}
	projects, err := child(c.values, "projects", create)
	if err != nil {
		return nil, err
	}
	key := filepath.ToSlash(project)
	if _, exists := projects[project]; exists {
		key = project
	}
	return child(projects, key, create)
}
func includes(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func activeState(c *config, t Target, project, name string, e object) string {
	if t.Toggle == "codex" && e["enabled"] == false {
		return "Disabled"
	}
	if t.Toggle == "claude" && project != "" {
		p, err := claudeProject(c, project, false)
		if err == nil && includes(stringsValue(p["disabledMcpServers"]), name) {
			return "Disabled in workspace"
		}
	}
	if t.ID == "gemini" {
		if policy, err := child(c.values, "mcp", false); err == nil {
			allowed := stringsValue(policy["allowed"])
			if includes(stringsValue(policy["excluded"]), name) || (policy["allowed"] != nil && !includes(allowed, name)) {
				return "Filtered by config"
			}
		}
	}
	return "Registered"
}

func targetConfig(t Target, approved *ConfigError) (*config, error) {
	c, err := loadConfig(t.Path, t.ID == "codex", approved)
	if err != nil {
		var issue *ConfigError
		if errors.As(err, &issue) {
			return nil, &RecoveryNeeded{t, issue}
		}
		return nil, err
	}
	if value, exists := c.values[t.Key]; exists && value == nil {
		// A null server collection represents an unset destination.
		delete(c.values, t.Key)
	} else if _, err := child(c.values, t.Key, false); err != nil {
		issue := &ConfigError{c.file, err.Error(), sha256.Sum256(c.raw)}
		if approved == nil || *approved != *issue {
			return nil, &RecoveryNeeded{t, issue}
		}
		delete(c.values, t.Key)
		c.recovery = true
	}
	return c, nil
}
func (m Manager) List(list []Target) ([]Registration, []string) {
	var rows []Registration
	var warnings []string
	for _, t := range list {
		c, err := targetConfig(t, nil)
		if err != nil {
			warnings = append(warnings, strings.ReplaceAll(err.Error(), "\n", " · "))
			continue
		}
		servers, _ := child(c.values, t.Key, false)
		parked, err := loadConfig(m.parkedFile(t), t.ID == "codex", nil)
		if err != nil {
			warnings = append(warnings, t.String()+": "+err.Error())
			parked = &config{values: object{}}
		}
		for name, value := range servers {
			e, ok := value.(map[string]any)
			if !ok {
				warnings = append(warnings, t.String()+": malformed server entry")
				continue
			}
			k := targetTransport(t, e)
			rows = append(rows, Registration{t, name, k, authLabel(e, k), activeState(c, t, m.Project, name, e), false, e})
		}
		for name, value := range parked.values {
			if _, active := servers[name]; active {
				continue
			}
			e, ok := value.(map[string]any)
			if !ok {
				continue
			}
			k := targetTransport(t, e)
			rows = append(rows, Registration{t, name, k, authLabel(e, k), "Paused", true, e})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Name != rows[j].Name {
			return rows[i].Name < rows[j].Name
		}
		return rows[i].Target.String() < rows[j].Target.String()
	})
	return rows, warnings
}

func (m Manager) Add(list []Target, d Definition, approvals map[string]*ConfigError) ([]string, error) {
	if len(list) == 0 {
		return nil, errors.New("select at least one destination")
	}
	var backups []string
	err := m.locked(func() error {
		var changes []*config
		seen := map[string]bool{}
		for _, t := range list {
			e, err := makeEntry(t, d)
			if err != nil {
				return fmt.Errorf("%s: %w", t.String(), err)
			}
			c, err := targetConfig(t, approvals[t.Path])
			if err != nil {
				return err
			}
			if seen[c.file] {
				return errors.New("two selected destinations resolve to the same file")
			}
			seen[c.file] = true
			p, err := loadConfig(m.parkedFile(t), t.ID == "codex", nil)
			if err != nil {
				return fmt.Errorf("paused registration data: %v", err)
			}
			servers, err := child(c.values, t.Key, true)
			if err != nil {
				return err
			}
			_, existing := servers[d.Name]
			_, paused := p.values[d.Name]
			if existing || paused {
				return fmt.Errorf("%s already has %s; select that entry to manage it", t.String(), d.Name)
			}
			servers[d.Name] = e
			changes = append(changes, c)
		}
		var err error
		backups, err = m.commit(changes)
		return err
	})
	return backups, err
}

var connectionFields = []string{"command", "args", "url", "httpUrl"}
var authFields = []string{"env", "env_vars", "envFile", "headers", "http_headers", "env_http_headers", "bearer_token_env_var", "headersHelper", "http_headers_helper", "auth", "authProviderType", "oauth", "oauth_client_id", "oauth_resource", "oauth_scopes"}

func changeConnection(t Target, name string, e object, d Definition) error {
	d.Name, d.Transport = name, targetTransport(t, e)
	replacement, err := makeEntry(t, d)
	if err != nil {
		return err
	}
	changed := false
	for _, field := range connectionFields {
		old, newValue := e[field], replacement[field]
		if field == "args" {
			old, newValue = stringsValue(old), stringsValue(newValue)
		}
		if !reflect.DeepEqual(old, newValue) {
			changed = true
		}
	}
	if !changed {
		return nil
	}
	for _, field := range authFields {
		if _, exists := e[field]; exists && !d.ResetAuth {
			return errors.New("confirm clearing existing authentication before changing the connection")
		}
		delete(e, field)
	}
	for _, field := range connectionFields {
		delete(e, field)
		if value, exists := replacement[field]; exists {
			e[field] = value
		}
	}
	return nil
}

func (m Manager) Act(row Registration, action string, d Definition) error {
	return m.locked(func() error {
		t := row.Target
		c, err := targetConfig(t, nil)
		if err != nil {
			return err
		}
		p, err := loadConfig(m.parkedFile(t), t.ID == "codex", nil)
		if err != nil {
			return err
		}
		servers, err := child(c.values, t.Key, action == "enable" && row.Parked)
		if err != nil {
			return err
		}
		entry, active := servers[row.Name]
		if !active {
			entry = p.values[row.Name]
		}
		e, ok := entry.(map[string]any)
		if !ok || active == row.Parked || !reflect.DeepEqual(row.entry, e) {
			return errors.New("this entry changed; refresh and select it again")
		}
		order := []*config{p, c}
		switch action {
		case "remove":
			delete(servers, row.Name)
			delete(p.values, row.Name)
		case "connection":
			if err := changeConnection(t, row.Name, e, d); err != nil {
				return err
			}
		case "auth":
			d.Name, d.Transport, d.Command = row.Name, targetTransport(t, e), stringValue(e, "command")
			d.URL = stringValue(e, "httpUrl")
			if d.URL == "" {
				d.URL = stringValue(e, "url")
			}
			if d.Bearer == "" && len(d.Headers) == 0 && len(d.Env) == 0 {
				return errors.New("enter at least one environment reference")
			}
			if err := d.validate(); err != nil {
				return err
			}
			if err := applyAuth(t, e, d); err != nil {
				return err
			}
		case "enable", "disable":
			enable := action == "enable"
			switch {
			case !active && enable:
				servers[row.Name] = e
				delete(p.values, row.Name)
				order = []*config{c, p}
			case !active:
				return nil
			case t.Toggle == "codex":
				e["enabled"] = enable
			case t.Toggle == "claude":
				project, err := claudeProject(c, m.Project, !enable)
				if err != nil {
					return err
				}
				current := stringsValue(project["disabledMcpServers"])
				if enable {
					if !includes(current, row.Name) {
						return nil
					}
					updated := make([]string, 0, len(current))
					for _, n := range current {
						if n != row.Name {
							updated = append(updated, n)
						}
					}
					project["disabledMcpServers"] = updated
				} else if !includes(current, row.Name) {
					project["disabledMcpServers"] = append(current, row.Name)
				}
			case !enable:
				p.values[row.Name] = e
				delete(servers, row.Name)
			default:
				return nil
			}
		default:
			return errors.New("unsupported operation")
		}
		_, err = m.commit(order)
		return err
	})
}

func (m Manager) locked(f func() error) error {
	if err := os.MkdirAll(m.StateDir, 0700); err != nil {
		return err
	}
	lock := flock.New(filepath.Join(m.StateDir, "write.lock"))
	ok, err := lock.TryLock()
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("another operation is writing; retry shortly")
	}
	defer lock.Close()
	return f()
}

func (m Manager) commit(configs []*config) ([]string, error) {
	type update struct {
		c    *config
		data []byte
	}
	var updates []update
	for _, c := range configs {
		if reflect.DeepEqual(c.original, c.values) {
			continue
		}
		data, err := c.encode()
		if err != nil {
			return nil, err
		}
		updates = append(updates, update{c, data})
	}
	var recoveryBackups []string
	for _, u := range updates {
		c := u.c
		if err := checkSnapshot(c); err != nil {
			return recoveryBackups, err
		}
		if c.raw == nil || strings.HasPrefix(c.file, filepath.Join(m.StateDir, "paused")+string(os.PathSeparator)) {
			continue
		}
		identity := sha256.Sum256([]byte(c.file))
		category := "backups"
		name := filepath.Base(c.file)
		if c.recovery {
			category = "recovery"
			name = time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + name
		}
		backup := filepath.Join(m.StateDir, category, fmt.Sprintf("%x", identity[:16]), name)
		if err := atomicWrite(backup, c.raw, 0600); err != nil {
			return recoveryBackups, fmt.Errorf("back up %s: %w", c.file, err)
		}
		if c.recovery {
			recoveryBackups = append(recoveryBackups, backup)
		}
	}
	for i, u := range updates {
		err := checkSnapshot(u.c)
		if err == nil {
			err = atomicWrite(u.c.file, u.data, u.c.mode)
		}
		if err == nil {
			continue
		}
		failures := []error{err}
		for j := i - 1; j >= 0; j-- {
			old := updates[j]
			current, _, readErr := snapshot(old.c.file)
			if readErr != nil || !bytes.Equal(current, old.data) {
				failures = append(failures, fmt.Errorf("could not restore %s", old.c.file))
				continue
			}
			if old.c.raw == nil {
				readErr = os.Remove(old.c.file)
			} else {
				readErr = atomicWrite(old.c.file, old.c.raw, old.c.mode)
			}
			if readErr != nil {
				failures = append(failures, readErr)
			}
		}
		return recoveryBackups, errors.Join(failures...)
	}
	return recoveryBackups, nil
}
func checkSnapshot(c *config) error {
	raw, mode, err := snapshot(c.file)
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, c.raw) || mode != c.mode || (raw == nil) != (c.raw == nil) {
		return fmt.Errorf("%s changed during the operation; retry", c.file)
	}
	return nil
}
