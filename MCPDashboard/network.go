package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const registryURL = "https://registry.modelcontextprotocol.io/v0.1/servers"

type CatalogServer struct {
	Name, Title, Description, Version string
	Repository                        struct{ URL string }
	Remotes                           []struct{ Type, URL string }
	Packages                          []struct {
		RegistryType, Identifier, Version string
		Transport                         struct{ Type string }
	}
}

func searchRegistry(ctx context.Context, query, cursor string) ([]CatalogServer, string, error) {
	u, _ := url.Parse(registryURL)
	q := u.Query()
	q.Set("limit", "30")
	q.Set("version", "latest")
	if query != "" {
		q.Set("search", query)
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("registry returned HTTP %d", resp.StatusCode)
	}
	var data struct {
		Servers  []struct{ Server CatalogServer }
		Metadata struct{ NextCursor string }
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&data); err != nil {
		return nil, "", err
	}
	list := make([]CatalogServer, 0, len(data.Servers))
	for _, item := range data.Servers {
		list = append(list, item.Server)
	}
	return list, data.Metadata.NextCursor, nil
}

var unsafeCatalogName = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

func (c CatalogServer) Draft() Definition {
	name := unsafeCatalogName.ReplaceAllString(c.Name[strings.LastIndex(c.Name, "/")+1:], "-")
	if !serverName.MatchString(name) {
		name = "mcp-" + name
	}
	d := Definition{Name: name, Transport: "http"}
	for _, kind := range []string{"streamable-http", "sse"} {
		for _, r := range c.Remotes {
			if r.Type != kind || strings.ContainsAny(r.URL, "{}") || requireURL(r.URL, false) != nil {
				continue
			}
			d.URL = r.URL
			if kind == "sse" {
				d.Transport = "sse"
			}
			return d
		}
	}
	for _, p := range c.Packages {
		if p.Transport.Type != "stdio" || p.Identifier == "" || strings.HasPrefix(p.Identifier, "-") {
			continue
		}
		id := p.Identifier
		switch p.RegistryType {
		case "npm":
			if p.Version != "" {
				id += "@" + p.Version
			}
			d.Transport, d.Command, d.Args = "stdio", "npx", []string{"-y", "--", id}
			return d
		case "pypi":
			if p.Version != "" {
				id += "==" + p.Version
			}
			d.Transport, d.Command, d.Args = "stdio", "uvx", []string{id}
			return d
		}
	}
	return d
}

var envRef = regexp.MustCompile(`^(Bearer )?\${(?:env:)?([A-Za-z_][A-Za-z0-9_]*)}$`)

func probeHeaders(r Registration) (http.Header, error) {
	h := http.Header{}
	for _, key := range []string{"headers", "http_headers", "env_http_headers"} {
		headers, err := child(r.entry, key, false)
		if err != nil {
			return nil, err
		}
		for name, value := range headers {
			s, ok := value.(string)
			if !ok || !headerName.MatchString(name) {
				return nil, errors.New("unsupported configured header")
			}
			if key == "env_http_headers" {
				s = "${" + s + "}"
			}
			if parts := envRef.FindStringSubmatch(s); parts != nil {
				value, ok := os.LookupEnv(parts[2])
				if !ok || value == "" {
					return nil, fmt.Errorf("environment variable %s is unavailable to the manager", parts[2])
				}
				s = parts[1] + value
			} else if strings.Contains(s, "${") {
				return nil, errors.New("this header uses a client-specific variable; test in the client")
			}
			if strings.ContainsAny(s, "\r\n") {
				return nil, errors.New("a configured header contains a line break")
			}
			h.Set(name, s)
		}
	}
	if name := stringValue(r.entry, "bearer_token_env_var"); name != "" {
		value, ok := os.LookupEnv(name)
		if !ok || value == "" {
			return nil, fmt.Errorf("environment variable %s is unavailable to the manager", name)
		}
		if strings.ContainsAny(value, "\r\n") {
			return nil, errors.New("bearer token contains a line break")
		}
		h.Set("Authorization", "Bearer "+value)
	}
	if r.entry["http_headers_helper"] != nil || r.entry["headersHelper"] != nil {
		return nil, errors.New("this entry uses a client header helper; test in the client")
	}
	return h, nil
}
func probe(ctx context.Context, r Registration) (string, error) {
	if r.Transport != "http" {
		return "", errors.New("the probe supports Streamable HTTP only")
	}
	raw := stringValue(r.entry, "httpUrl")
	if raw == "" {
		raw = stringValue(r.entry, "url")
	}
	headers, err := probeHeaders(r)
	if err != nil {
		return "", err
	}
	if err := requireURL(raw, len(headers) > 0); err != nil {
		return "", err
	}
	payload := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":%q,"version":%q}}}`, productName, productVersion))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, raw, bytes.NewReader(payload))
	if err != nil {
		return "", errors.New("invalid server URL")
	}
	req.Header = headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	client := &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("could not reach the MCP endpoint")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return "", fmt.Errorf("HTTP %d: authenticate or check permissions in the target client", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("endpoint returned HTTP %d", resp.StatusCode)
	}
	var reply struct {
		JSONRPC string
		ID      json.RawMessage
		Result  struct{ ProtocolVersion string }
	}
	body := io.LimitReader(resp.Body, 1<<20)
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		var event strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" && event.Len() > 0 {
				if json.Unmarshal([]byte(event.String()), &reply) == nil && string(reply.ID) == "1" {
					break
				}
				event.Reset()
			} else if strings.HasPrefix(line, "data:") {
				event.WriteString(strings.TrimPrefix(line, "data:"))
				event.WriteByte('\n')
			}
		}
	} else if err := json.NewDecoder(body).Decode(&reply); err != nil {
		return "", errors.New("endpoint did not return an MCP initialization response")
	}
	if reply.JSONRPC != "2.0" || string(reply.ID) != "1" || reply.Result.ProtocolVersion == "" {
		return "", errors.New("endpoint did not complete the MCP initialize request")
	}
	return "MCP initialize succeeded. Protocol " + strconv.Quote(reply.Result.ProtocolVersion) + ".", nil
}
