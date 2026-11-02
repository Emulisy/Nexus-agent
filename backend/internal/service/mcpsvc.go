// MCP services manage server configuration, discovered tools, and direct calls.
// Synchronization mirrors remote definitions into the local tool catalog. When
// a registry is supplied through SetRegistry, matching handlers forward runtime
// calls using the server's latest URL and authentication headers.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Emulisy/SupaAgent/internal/mcp"
	"github.com/Emulisy/SupaAgent/internal/model"
	"github.com/Emulisy/SupaAgent/internal/pkg/errcode"
	"github.com/Emulisy/SupaAgent/internal/tool"
)

// MCPService shares server persistence and optionally registers remote-call handlers in a tool registry.
type MCPService struct {
	svcs     *Services
	registry tool.Registry
}

// NewMCPService creates management services without attaching a runtime registry; SetRegistry supplies one.
func NewMCPService(svcs *Services) *MCPService {
	return &MCPService{svcs: svcs}
}

// SetRegistry enables handler registration during subsequent tool synchronization.
func (s *MCPService) SetRegistry(reg tool.Registry) { s.registry = reg }

// Create requires a name/URL, defaults an empty transport to streamable_http, and saves the definition.
func (s *MCPService) Create(ctx context.Context, srv *model.MCPServer) error {
	if srv.Name == "" || srv.URL == "" {
		return errcode.ErrInvalidParam.WithMsg("name and url are required")
	}
	if srv.Transport == "" {
		srv.Transport = "streamable_http"
	}
	if err := s.svcs.Deps.Repos.MCP.Create(ctx, srv); err != nil {
		return errcode.ErrInternal.WithCause(err)
	}
	return nil
}

// Update verifies the server exists before saving the supplied configuration.
// It does not rediscover tool schemas; synchronization is a separate operation.
// Installed forwarding handlers reload connection settings when invoked.
func (s *MCPService) Update(ctx context.Context, srv *model.MCPServer) error {
	if srv.ID <= 0 {
		return errcode.ErrInvalidParam.WithMsg("id is required")
	}
	if _, err := s.get(ctx, srv.ID); err != nil {
		return err
	}
	return s.svcs.Deps.Repos.MCP.Update(ctx, srv)
}

// Delete removes catalog tools for the server before deleting its registration; the writes are separate.
func (s *MCPService) Delete(ctx context.Context, id int64) error {
	if _, err := s.get(ctx, id); err != nil {
		return err
	}
	if err := s.svcs.Deps.Repos.MCP.DeleteToolsByServer(ctx, id); err != nil {
		return errcode.ErrInternal.WithCause(err)
	}
	return s.svcs.Deps.Repos.MCP.Delete(ctx, id)
}

// List returns persisted server definitions and wraps storage failures as internal errors.
func (s *MCPService) List(ctx context.Context) ([]model.MCPServer, error) {
	list, err := s.svcs.Deps.Repos.MCP.List(ctx)
	if err != nil {
		return nil, errcode.ErrInternal.WithCause(err)
	}
	return list, nil
}

// get translates missing or failed server lookups into the MCP connection error used by callers.
func (s *MCPService) get(ctx context.Context, id int64) (*model.MCPServer, error) {
	srv, err := s.svcs.Deps.Repos.MCP.Get(ctx, id)
	if err != nil {
		return nil, errcode.ErrMCPConnFail.WithMsg("MCP server not found: %d", id)
	}
	return srv, nil
}

// Get exposes the normalized server lookup for callers outside this service.
func (s *MCPService) Get(ctx context.Context, id int64) (*model.MCPServer, error) {
	return s.get(ctx, id)
}

// SyncTools discovers remote definitions, replaces catalog entries, and updates the cache.
// Discovery failure preserves existing tools and marks the server as errored.
// Replacement writes are not one transaction, so a later failure can leave a
// partial catalog. Runtime handlers are installed only when a registry is attached.
func (s *MCPService) SyncTools(ctx context.Context, serverID int64) ([]model.Tool, error) {
	srv, err := s.get(ctx, serverID)
	if err != nil {
		return nil, err
	}

	client := mcp.NewClient(srv.URL, parseHeaders(srv.Headers))

	defs, err := client.ListTools(ctx)
	if err != nil {
		s.markStatus(ctx, srv, "error")
		return nil, errcode.ErrMCPConnFail.WithCause(err)
	}

	// Remove old tools so entries withdrawn by the remote server do not remain available.
	repo := s.svcs.Deps.Repos
	if err := repo.MCP.DeleteToolsByServer(ctx, srv.ID); err != nil {
		return nil, errcode.ErrInternal.WithCause(err)
	}
	tools := make([]model.Tool, 0, len(defs))
	for _, d := range defs {
		params, _ := json.Marshal(schemaOrEmpty(d.InputSchema))
		t := model.Tool{
			Code:        mcpToolCode(srv.ID, d.Name),
			Name:        d.Name,
			Type:        model.ToolTypeMCP,
			Description: d.Description,
			Parameters:  params,
			MCPServerID: &srv.ID,
			TimeoutMs:   60000,
			Status:      1,
		}
		if err := repo.Tool.Create(ctx, &t); err != nil {
			return nil, errcode.ErrInternal.WithCause(err)
		}
		tools = append(tools, t)

		s.registerHandler(srv.ID, d.Name)
	}

	now := time.Now()
	srv.Status = "connected"
	srv.LastHealthAt = &now
	if cache, err := json.Marshal(defs); err == nil {
		srv.ToolsCache = cache
	}
	if err := repo.MCP.Update(ctx, srv); err != nil {
		return nil, errcode.ErrInternal.WithCause(err)
	}
	return tools, nil
}

// CallTool directly invokes a named remote tool using current stored headers and URL.
// This management path does not depend on the optional runtime registry and
// translates call failures to MCP tool errors.
func (s *MCPService) CallTool(ctx context.Context, serverID int64, name string, args map[string]any) (string, error) {
	srv, err := s.get(ctx, serverID)
	if err != nil {
		return "", err
	}
	client := mcp.NewClient(srv.URL, parseHeaders(srv.Headers))
	out, err := client.CallTool(ctx, name, args)
	if err != nil {
		return "", errcode.ErrMCPToolFail.WithCause(err)
	}
	return out, nil
}

// HealthCheck pings the remote client and best-effort persists connected/error status and check time.
func (s *MCPService) HealthCheck(ctx context.Context, serverID int64) error {
	srv, err := s.get(ctx, serverID)
	if err != nil {
		return err
	}
	client := mcp.NewClient(srv.URL, parseHeaders(srv.Headers))
	if err := client.Ping(ctx); err != nil {
		s.markStatus(ctx, srv, "error")
		return errcode.ErrMCPConnFail.WithCause(err)
	}
	s.markStatus(ctx, srv, "connected")
	return nil
}

// registerHandler installs a namespaced forwarding closure when a registry is attached.
// The closure captures only server ID and remote tool name, loading current
// connection settings for each invocation instead of retaining old credentials.
func (s *MCPService) registerHandler(serverID int64, remoteName string) {
	if s.registry == nil {
		return
	}
	code := mcpToolCode(serverID, remoteName)
	repo := s.svcs.Deps.Repos.MCP
	s.registry.Register(code, func(ctx context.Context, args map[string]any, _ string) (string, error) {
		srv, err := repo.Get(ctx, serverID)
		if err != nil {
			return "", fmt.Errorf("mcp server %d unavailable: %w", serverID, err)
		}
		client := mcp.NewClient(srv.URL, parseHeaders(srv.Headers))
		return client.CallTool(ctx, remoteName, args)
	})
}

// markStatus records availability and LastHealthAt without replacing the operation's primary result on failure.
func (s *MCPService) markStatus(ctx context.Context, srv *model.MCPServer, status string) {
	now := time.Now()
	srv.Status = status
	srv.LastHealthAt = &now
	_ = s.svcs.Deps.Repos.MCP.Update(ctx, srv)
}

// mcpToolCode prefixes the server ID so identically named tools on different servers have distinct codes.
func mcpToolCode(serverID int64, remoteName string) string {
	return fmt.Sprintf("mcp_%d_%s", serverID, remoteName)
}

// parseHeaders decodes stored JSON headers into a usable map, tolerating absent or malformed data.
func parseHeaders(raw []byte) map[string]string {
	out := map[string]string{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

// schemaOrEmpty preserves a supplied schema and defaults an omitted schema to an empty object definition.
func schemaOrEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return m
}
