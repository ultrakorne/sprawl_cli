package tui

import (
	"context"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// Client is the subset of *client.Client the TUI depends on. Declaring it as an
// interface keeps the model decoupled from the concrete HTTP client so tests can
// inject a fake (or a real *client.Client wired to an httptest server, matching
// internal/client/testhelper_test.go). *client.Client satisfies it directly.
type Client interface {
	ListTasks(ctx context.Context) ([]*client.Task, error)
	GetTask(ctx context.Context, id string, full bool) (*client.Task, error)
	SearchTasks(ctx context.Context, query string) ([]*client.Task, error)
	SetChecklistItemCompleted(ctx context.Context, itemID string, completed bool) (*client.ChecklistItem, error)
	CreateTask(ctx context.Context, attrs map[string]any) (*client.Task, error)
	UpdateTask(ctx context.Context, id string, attrs map[string]any) (*client.Task, error)
	SetTaskDueDate(ctx context.Context, id string, due *string) (*client.Task, error)
	DeleteTask(ctx context.Context, id string) error
	CreateChecklistItem(ctx context.Context, taskID string, attrs map[string]any) (*client.ChecklistItem, error)
	UpdateChecklistItem(ctx context.Context, itemID string, attrs map[string]any) (*client.ChecklistItem, error)
	SetNotes(ctx context.Context, itemID, notes string) (*string, error)
	DeleteChecklistItem(ctx context.Context, itemID string) error
}

// compile-time assertion that the concrete client satisfies the interface.
var _ Client = (*client.Client)(nil)
