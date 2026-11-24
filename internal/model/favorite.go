package model

import "time"

// Favorite is one starred catalog entry. Kind is "model" or "dataset" and Name
// is the catalog identifier the user sees in the page URL. Favorites are the
// launch surface of the studio, so they are hard-deleted (no soft-delete
// tombstone) and unique per user.
type Favorite struct {
	ID        int64     `gorm:"primaryKey" json:"-"`
	UserID    int64     `gorm:"not null;uniqueIndex:idx_favorites_user_kind_name,priority:1" json:"-"`
	Kind      string    `gorm:"size:16;not null;uniqueIndex:idx_favorites_user_kind_name,priority:2" json:"kind"`
	Name      string    `gorm:"size:128;not null;uniqueIndex:idx_favorites_user_kind_name,priority:3" json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func (Favorite) TableName() string { return "favorites" }
