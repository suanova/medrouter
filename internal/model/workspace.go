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
	// A fine-tune moves through merging (adapter folded into the base) before
	// its ModelVersion is registered.
	WorkspaceMerging = "merging"
	WorkspaceReady   = "ready"
)

// Workspace is one launched environment derived from a favorite. Each user may
// hold at most one workspace per kind (notebook, inference, finetune); the
// unique index on (owner_id, kind) is created in InitDB because it cannot be
// added to an existing database that still holds duplicate rows.
type Workspace struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	OwnerID      int64     `gorm:"not null" json:"-"`
	Kind         string    `gorm:"size:16;not null" json:"kind"`
	SourceKind   string    `gorm:"size:16;not null" json:"source_kind"`
	SourceName   string    `gorm:"size:128;not null" json:"source_name"`
	Name         string    `gorm:"size:128;not null" json:"name"`
	Namespace    string    `gorm:"size:128;not null" json:"namespace"`
	Phase        string    `gorm:"size:16;not null" json:"phase"`
	Reason       string    `gorm:"size:255" json:"reason,omitempty"`
	Endpoint     string    `gorm:"size:512" json:"endpoint,omitempty"`
	Matched      string    `gorm:"size:1024" json:"matched"`
	ModelVersion string    `gorm:"size:128" json:"model_version,omitempty"` // JSON: resources the backend chose
	Spec         string    `gorm:"size:8192" json:"-"`                      // JSON: the CR spec that was requested
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (Workspace) TableName() string { return "workspaces" }
