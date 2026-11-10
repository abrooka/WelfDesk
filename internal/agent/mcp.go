package agent

import (
	"context"
	"encoding/json"

	"github.com/weftdesk/weft/internal/domain"
	"github.com/weftdesk/weft/internal/mcp"
	"github.com/weftdesk/weft/internal/provider"
)

func (a *Agent) StartMCP(ctx context.Context) {
	cfg := a.Config()
	servers := make(map[string]mcp.ServerConfig, len(cfg.MCPServers))
	for name, server := range cfg.MCPServers {
		servers[name] = mcp.ServerConfig{Command: server.Command, Args: server.Args, Env: server.Env}
	}
	set := mcp.Connect(ctx, servers)
	a.mcpMu.Lock()
	old := a.mcpSet
	a.mcpSet = set
	a.mcpMu.Unlock()
	if old != nil {
		old.Close()
	}
}

func (a *Agent) mcpClient() *mcp.Set {
	a.mcpMu.RLock()
	defer a.mcpMu.RUnlock()
	return a.mcpSet
}

func (a *Agent) CloseMCP() {
	a.mcpMu.Lock()
	set := a.mcpSet
	a.mcpSet = nil
	a.mcpMu.Unlock()
	if set != nil {
		set.Close()
	}
}

func (a *Agent) MCPStatus() []mcp.Status {
	return a.mcpClient().Status()
}

func (a *Agent) MCPToolNames() []string {
	set := a.mcpClient()
	tools := set.Tools()
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Qualified)
	}
	return names
}

func mcpToolDefs(set *mcp.Set) []provider.Tool {
	tools := set.Tools()
	defs := make([]provider.Tool, 0, len(tools))
	for _, tool := range tools {
		description := tool.Description
		if description == "" {
			description = "MCP tool " + tool.Tool + " from " + tool.Server + "."
		} else {
			description = "MCP tool from " + tool.Server + ": " + description
		}
		defs = append(defs, provider.Tool{Name: tool.Qualified, Description: description, Schema: tool.Schema})
	}
	return defs
}

func (a *Agent) callMCP(ctx context.Context, set *mcp.Set, call provider.ToolCall, result *domain.ToolResultData) {
	out, isError, err := set.Call(ctx, call.Name, json.RawMessage(call.Arguments))
	result.Output = out
	result.IsError = isError
	if err != nil {
		result.IsError = true
		if result.Output == "" {
			result.Output = err.Error()
		} else {
			result.Output += "\n" + err.Error()
		}
	}
}
