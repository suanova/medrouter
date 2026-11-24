package model

import "time"

// WorkspacePhase mirrors the coarse state of the underlying CubeStack resource.
// The resource remains the source of truth; this column is a projection that
// the list endpoint can serve without hitting the API server.
const (
	WorkspaceCreating  = "creating"
	WorkspaceRunning   = "running"
	WorkspaceStopped   = "stopped"
	WorkspaceFailed    = "failed"
	WorkspaceSucceeded = "succeeded"
)

// Workspace is one launched environment derived from a favorite. A favorite
// owns at most one workspace, which the unique index enforces.
type Workspace struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	OwnerID    int64     `gorm:"not null;uniqueIndex:idx_workspaces_owner_source,priority:1" json:"-"`
	SourceKind string    `gorm:"size:16;not null;uniqueIndex:idx_workspaces_owner_source,priority:2" json:"source_kind"`
	SourceName string    `gorm:"size:128;not null;uniqueIndex:idx_workspaces_owner_source,priority:3" json:"source_name"`
	Kind       string    `gorm:"size:16;not null" json:"kind"`
	Name       string    `gorm:"size:128;not null" json:"name"`
	Namespace  string    `gorm:"size:128;not null" json:"namespace"`
	Phase      string    `gorm:"size:16;not null" json:"phase"`
	Reason     string    `gorm:"size:255" json:"reason,omitempty"`
	Endpoint   string    `gorm:"size:512" json:"endpoint,omitempty"`
	Matched    string    `gorm:"size:1024" json:"matched"` // JSON: resources the backend chose
	Spec       string    `gorm:"size:8192" json:"-"`       // JSON: the CR spec that was requested
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (Workspace) TableName() string { return "workspaces" }
