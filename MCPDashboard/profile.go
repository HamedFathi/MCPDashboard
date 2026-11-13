package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const profileSchema = "mcp-manager-profile/v1"

type Profile struct {
	Schema       string               `json:"schema"`
	Destinations []ProfileDestination `json:"destinations"`
}
type ProfileDestination struct {
	Client   string   `json:"client"`
	Scope    string   `json:"scope"`
	Format   string   `json:"format"`
	Active   string   `json:"configuration"`
	Paused   string   `json:"paused_configuration,omitempty"`
	Disabled []string `json:"disabled_in_workspace,omitempty"`
}
type ProfileReport struct{ Added, Paused, Kept, Settings int }

func profileFormat(t Target) string {
	if t.ID == "codex" {
		return "toml"
	}
	return "json"
}
func portableKey(client, scope string) string { return client + "\x00" + scope }
func portableExtras(t Target) []string {
	switch t.ID {
	case "vscode":
		return []string{"inputs"}
	case "gemini":
		return []string{"mcp"}
	default:
		return nil
	}
}
func portableTarget(t Target, d ProfileDestination) (object, object, error) {
	if d.Format != profileFormat(t) {
		return nil, nil, errors.New("unexpected configuration format")
	}
	isTOML := d.Format == "toml"
	active, err := parseConfig([]byte(d.Active), isTOML)
	if err != nil {
		return nil, nil, err
	}
	paused := object{}
	if d.Paused != "" {
		paused, err = parseConfig([]byte(d.Paused), isTOML)
		if err != nil {
			return nil, nil, err
		}
	}
	for key := range active {
		if key != t.Key && !includes(portableExtras(t), key) {
			return nil, nil, fmt.Errorf("unexpected configuration field %q", key)
		}
	}
	for key := range paused {
		if key != t.Key {
			return nil, nil, errors.New("unexpected paused configuration field")
		}
	}
	servers, err := child(active, t.Key, false)
	if err != nil {
		return nil, nil, err
	}
	parked, err := child(paused, t.Key, false)
	if err != nil {
		return nil, nil, err
	}
	for name, value := range servers {
		if name == "" {
			return nil, nil, errors.New("empty server name")
		}
		if _, ok := value.(map[string]any); !ok {
			return nil, nil, errors.New("server definitions must be objects")
		}
		if _, ok := parked[name]; ok {
			return nil, nil, errors.New("a server cannot be both active and paused")
		}
	}
	for name, value := range parked {
		if name == "" {
			return nil, nil, errors.New("empty paused server name")
		}
		if _, ok := value.(map[string]any); !ok {
			return nil, nil, errors.New("paused server definitions must be objects")
		}
	}
	if len(d.Disabled) > 0 && t.Toggle != "claude" {
		return nil, nil, errors.New("unexpected workspace disabled list")
	}
	for _, name := range d.Disabled {
		if _, ok := servers[name]; !ok {
			return nil, nil, errors.New("workspace disabled entry has no matching server")
		}
	}
	return active, paused, nil
}

func (m Manager) exportProfile() (Profile, error) {
	profile := Profile{Schema: profileSchema, Destinations: []ProfileDestination{}}
	err := m.locked(func() error {
		for _, t := range targets(m.Home, m.Project) {
			c, err := targetConfig(t, nil)
			if err != nil {
				return fmt.Errorf("export %s: %w", t.String(), err)
			}
			p, err := loadConfig(m.parkedFile(t), t.ID == "codex", nil)
			if err != nil {
				return fmt.Errorf("export paused %s: %w", t.String(), err)
			}
			servers, err := child(c.values, t.Key, false)
			if err != nil {
				return fmt.Errorf("export %s: %w", t.String(), err)
			}
			active, paused := object{}, object{}
			if len(servers) > 0 {
				active[t.Key] = servers
			}
			for _, key := range portableExtras(t) {
				if value, ok := c.values[key]; ok {
					active[key] = value
				}
			}
			parked := object{}
			for name, value := range p.values {
				if _, exists := servers[name]; !exists {
					parked[name] = value
				}
			}
			if len(parked) > 0 {
				paused[t.Key] = parked
			}
			if len(active) == 0 && len(paused) == 0 {
				continue
			}
			item := ProfileDestination{Client: t.ID, Scope: t.Scope, Format: profileFormat(t)}
			if t.Toggle == "claude" && m.Project != "" {
				project, err := claudeProject(c, m.Project, false)
				if err != nil {
					return fmt.Errorf("export %s workspace states: %w", t.String(), err)
				}
				for _, name := range stringsValue(project["disabledMcpServers"]) {
					if _, exists := servers[name]; exists {
						item.Disabled = append(item.Disabled, name)
					}
				}
			}
			encoded, err := (&config{toml: c.toml, values: active}).encode()
			if err != nil {
				return fmt.Errorf("encode %s: %w", t.String(), err)
			}
			item.Active = string(encoded)
			if len(paused) > 0 {
				data, err := (&config{toml: c.toml, values: paused}).encode()
				if err != nil {
					return fmt.Errorf("encode paused %s: %w", t.String(), err)
				}
				item.Paused = string(data)
			}
			if _, _, err := portableTarget(t, item); err != nil {
				return fmt.Errorf("export %s: %w", t.String(), err)
			}
			profile.Destinations = append(profile.Destinations, item)
		}
		return nil
	})
	return profile, err
}

type plannedDestination struct {
	target         Target
	active, paused object
	disabled       []string
}

func (m Manager) planProfile(p Profile) ([]plannedDestination, error) {
	if p.Schema != profileSchema {
		return nil, errors.New("unsupported profile version")
	}
	if len(p.Destinations) == 0 {
		return nil, errors.New("the profile contains no destinations")
	}
	// A placeholder workspace lets us distinguish an unknown target from a missing project selection.
	catalog := targets(m.Home, filepath.Join(m.Home, ".mcp-profile-placeholder"))
	labels := map[string]string{}
	for _, t := range catalog {
		labels[portableKey(t.ID, t.Scope)] = t.String()
	}
	available := map[string]Target{}
	for _, t := range targets(m.Home, m.Project) {
		available[portableKey(t.ID, t.Scope)] = t
	}
	seen := map[string]bool{}
	var plan []plannedDestination
	for _, d := range p.Destinations {
		key := portableKey(d.Client, d.Scope)
		label, known := labels[key]
		if !known {
			return nil, errors.New("profile includes an unsupported client or scope")
		}
		t, ok := available[key]
		if !ok || (len(d.Disabled) > 0 && m.Project == "") {
			return nil, fmt.Errorf("choose a workspace before importing %s", label)
		}
		if seen[key] {
			return nil, fmt.Errorf("profile lists %s more than once", label)
		}
		seen[key] = true
		active, paused, err := portableTarget(t, d)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", label, err)
		}
		plan = append(plan, plannedDestination{t, active, paused, d.Disabled})
	}
	return plan, nil
}
func profileSummary(plan []plannedDestination) string {
	var lines []string
	for _, item := range plan {
		active, _ := child(item.active, item.target.Key, false)
		paused, _ := child(item.paused, item.target.Key, false)
		var names []string
		for name := range active {
			names = append(names, name)
		}
		for name := range paused {
			names = append(names, name+" (paused)")
		}
		sort.Strings(names)
		if len(names) == 0 {
			names = append(names, "MCP options only")
		}
		lines = append(lines, item.target.String()+"\n"+strings.Join(names, ", "))
	}
	return strings.Join(lines, "\n\n")
}
func mergeProfileInputs(current, incoming any) ([]any, bool) {
	old, ok := current.([]any)
	newItems, valid := incoming.([]any)
	if !ok || !valid {
		return nil, false
	}
	seen := map[string]bool{}
	for _, v := range old {
		if entry, ok := v.(map[string]any); ok {
			seen[stringValue(entry, "id")] = true
		}
	}
	var added bool
	for _, v := range newItems {
		entry, ok := v.(map[string]any)
		if !ok {
			continue
		}
		id := stringValue(entry, "id")
		if id == "" || seen[id] {
			continue
		}
		old = append(old, v)
		seen[id] = true
		added = true
	}
	return old, added
}
func (m Manager) importProfile(plan []plannedDestination) (ProfileReport, error) {
	var report ProfileReport
	err := m.locked(func() error {
		pending, changes, err := m.prepareProfileImport(plan)
		if err != nil {
			return err
		}
		if _, err := m.commit(changes); err != nil {
			return err
		}
		report = pending
		return nil
	})
	return report, err
}

func (m Manager) prepareProfileImport(plan []plannedDestination) (ProfileReport, []*config, error) {
	var report ProfileReport
	var changes []*config
	seenPaths := map[string]bool{}
	for _, item := range plan {
		t := item.target
		c, err := targetConfig(t, nil)
		if err != nil {
			return ProfileReport{}, nil, fmt.Errorf("import %s: %w", t.String(), err)
		}
		if seenPaths[c.file] {
			return ProfileReport{}, nil, errors.New("two profile destinations resolve to the same file")
		}
		seenPaths[c.file] = true
		p, err := loadConfig(m.parkedFile(t), t.ID == "codex", nil)
		if err != nil {
			return ProfileReport{}, nil, fmt.Errorf("import paused %s: %w", t.String(), err)
		}
		active, _ := child(item.active, t.Key, false)
		parked, _ := child(item.paused, t.Key, false)
		servers, err := child(c.values, t.Key, len(active) > 0)
		if err != nil {
			return ProfileReport{}, nil, err
		}
		hadEntries := len(servers)+len(p.values) > 0
		var addedDisabled []string
		for name, e := range active {
			_, exists := servers[name]
			_, isPaused := p.values[name]
			if exists || isPaused {
				report.Kept++
				continue
			}
			servers[name] = e
			report.Added++
			if includes(item.disabled, name) {
				addedDisabled = append(addedDisabled, name)
			}
		}
		for name, e := range parked {
			_, exists := servers[name]
			_, isPaused := p.values[name]
			if exists || isPaused {
				report.Kept++
				continue
			}
			p.values[name] = e
			report.Paused++
		}
		if len(addedDisabled) > 0 {
			project, err := claudeProject(c, m.Project, true)
			if err != nil {
				return ProfileReport{}, nil, err
			}
			current := stringsValue(project["disabledMcpServers"])
			for _, name := range addedDisabled {
				if !includes(current, name) {
					current = append(current, name)
				}
			}
			project["disabledMcpServers"] = current
		}
		for _, key := range portableExtras(t) {
			incoming, exists := item.active[key]
			if !exists {
				continue
			}
			current, already := c.values[key]
			if key == "mcp" && hadEntries {
				continue
			}
			if !already {
				c.values[key] = incoming
				report.Settings++
				continue
			}
			if key == "inputs" {
				if merged, added := mergeProfileInputs(current, incoming); added {
					c.values[key] = merged
					report.Settings++
				}
			}
		}
		changes = append(changes, p, c)
	}
	return report, changes, nil
}
