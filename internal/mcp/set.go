package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
)

const toolNameLimit = 64

type ManagedTool struct {
	Qualified   string
	Server      string
	Tool        string
	Description string
	Schema      map[string]any
}

type handler struct {
	client *Client
	tool   string
}

type Set struct {
	tools    []ManagedTool
	handlers map[string]handler
	status   []Status
	clients  []*Client
}

func Connect(ctx context.Context, servers map[string]ServerConfig) *Set {
	type outcome struct {
		name   string
		client *Client
		tools  []Tool
		err    error
	}
	results := make(chan outcome, len(servers))
	for name, cfg := range servers {
		go func(name string, cfg ServerConfig) {
			client, err := Start(ctx, name, cfg)
			if err != nil {
				results <- outcome{name: name, err: err}
				return
			}
			tools, err := client.Tools(ctx)
			if err != nil {
				client.Close()
				results <- outcome{name: name, err: err}
				return
			}
			results <- outcome{name: name, client: client, tools: tools}
		}(name, cfg)
	}

	set := &Set{handlers: map[string]handler{}}
	statuses := map[string]Status{}
	for range servers {
		result := <-results
		if result.err != nil {
			statuses[result.name] = Status{Name: result.name, Error: result.err.Error()}
			continue
		}
		set.clients = append(set.clients, result.client)
		statuses[result.name] = Status{Name: result.name, Tools: len(result.tools)}
		for _, tool := range result.tools {
			qualified := toolName(result.name, tool.Name)
			if _, exists := set.handlers[qualified]; exists {
				status := statuses[result.name]
				status.Error = "tool name collision: " + qualified
				statuses[result.name] = status
				continue
			}
			set.handlers[qualified] = handler{client: result.client, tool: tool.Name}
			set.tools = append(set.tools, ManagedTool{
				Qualified:   qualified,
				Server:      result.name,
				Tool:        tool.Name,
				Description: tool.Description,
				Schema:      tool.Schema,
			})
		}
	}
	sort.Slice(set.tools, func(i, j int) bool { return set.tools[i].Qualified < set.tools[j].Qualified })
	for _, status := range statuses {
		set.status = append(set.status, status)
	}
	sort.Slice(set.status, func(i, j int) bool { return set.status[i].Name < set.status[j].Name })
	return set
}

func (s *Set) Tools() []ManagedTool {
	if s == nil {
		return nil
	}
	return s.tools
}

func (s *Set) Call(ctx context.Context, qualified string, args json.RawMessage) (string, bool, error) {
	if s == nil {
		return "", true, fmt.Errorf("%s is not connected", qualified)
	}
	entry, ok := s.handlers[qualified]
	if !ok {
		return "", true, fmt.Errorf("%s is not connected", qualified)
	}
	return entry.client.Call(ctx, entry.tool, args)
}

func (s *Set) Status() []Status {
	if s == nil || s.status == nil {
		return []Status{}
	}
	return s.status
}

func (s *Set) Close() {
	if s == nil {
		return
	}
	for _, client := range s.clients {
		client.Close()
	}
}

func toolName(server, tool string) string {
	name := "mcp__" + sanitize(server) + "__" + sanitize(tool)
	if len(name) <= toolNameLimit {
		return name
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(name))
	suffix := fmt.Sprintf("_%08x", hasher.Sum32())
	return name[:toolNameLimit-len(suffix)] + suffix
}

func sanitize(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "server"
	}
	return b.String()
}
