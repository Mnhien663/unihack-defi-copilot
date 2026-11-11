package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"unihack-backend/internal/ai"
)

var (
	ErrNilTool               = errors.New("tool is nil")
	ErrInvalidToolName       = errors.New("tool name is invalid")
	ErrInvalidToolSchema     = errors.New("tool input schema is invalid")
	ErrToolAlreadyRegistered = errors.New("tool is already registered")
	ErrToolNotFound          = errors.New("tool not found")
)

// Registry stores tools by their unique names.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]ai.Tool
}

func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]ai.Tool),
	}
}

// Register adds one tool to the registry.
func (r *Registry) Register(tool ai.Tool) error {
	if tool == nil {
		return ErrNilTool
	}

	definition := tool.Definition()
	name := strings.TrimSpace(definition.Name)

	if name == "" || name != definition.Name {
		return fmt.Errorf(
			"%w: name must be non-empty and contain no surrounding spaces",
			ErrInvalidToolName,
		)
	}

	if !json.Valid(definition.InputSchema) {
		return fmt.Errorf(
			"%w for tool %q",
			ErrInvalidToolSchema,
			name,
		)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[name]; exists {
		return fmt.Errorf(
			"%w: %s",
			ErrToolAlreadyRegistered,
			name,
		)
	}

	r.tools[name] = tool

	return nil
}

// Definitions returns tool definitions in deterministic name order.
func (r *Registry) Definitions() []ai.ToolDefinition {
	r.mu.RLock()

	definitions := make(
		[]ai.ToolDefinition,
		0,
		len(r.tools),
	)

	for _, tool := range r.tools {
		definitions = append(
			definitions,
			tool.Definition(),
		)
	}

	r.mu.RUnlock()

	sort.Slice(
		definitions,
		func(i int, j int) bool {
			return definitions[i].Name < definitions[j].Name
		},
	)

	return definitions
}

// Execute finds and runs a tool by name.
func (r *Registry) Execute(
	ctx context.Context,
	name string,
	arguments json.RawMessage,
) (ai.ToolResult, error) {
	name = strings.TrimSpace(name)

	r.mu.RLock()
	tool, exists := r.tools[name]
	r.mu.RUnlock()

	if !exists {
		return ai.ToolResult{}, fmt.Errorf(
			"%w: %s",
			ErrToolNotFound,
			name,
		)
	}

	return tool.Execute(ctx, arguments)
}

var _ ai.ToolRegistry = (*Registry)(nil)
