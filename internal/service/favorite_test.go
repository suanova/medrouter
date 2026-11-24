package service

import (
	"path/filepath"
	"testing"

	"github.com/suanova/medrouter/internal/config"
	"github.com/suanova/medrouter/internal/model"
)

func favoriteTestDB(t *testing.T) {
	t.Helper()
	cfg := &config.Config{
		SQLitePath:     filepath.Join(t.TempDir(), "favorites.db"),
		DBMaxOpenConns: 1,
	}
	if err := model.InitDB(cfg); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() {
		_ = model.Close()
		model.DB = nil
	})
}

func TestFavoriteAddListRemove(t *testing.T) {
	favoriteTestDB(t)
	svc := NewFavoriteService()

	if _, apiErr := svc.Add(1, FavoriteModel, "biomedbert"); apiErr != nil {
		t.Fatalf("Add: %v", apiErr)
	}
	if _, apiErr := svc.Add(1, FavoriteDataset, "pubmedqa"); apiErr != nil {
		t.Fatalf("Add dataset: %v", apiErr)
	}

	list, apiErr := svc.List(1)
	if apiErr != nil {
		t.Fatalf("List: %v", apiErr)
	}
	if len(list) != 2 {
		t.Fatalf("list = %+v", list)
	}
	// Newest first.
	if list[0].Name != "pubmedqa" || list[1].Name != "biomedbert" {
		t.Fatalf("order = %+v", list)
	}

	if apiErr := svc.Remove(1, FavoriteModel, "biomedbert"); apiErr != nil {
		t.Fatalf("Remove: %v", apiErr)
	}
	list, _ = svc.List(1)
	if len(list) != 1 || list[0].Name != "pubmedqa" {
		t.Fatalf("after remove = %+v", list)
	}

	// Removing again is a success: the caller's intent is "not starred".
	if apiErr := svc.Remove(1, FavoriteModel, "biomedbert"); apiErr != nil {
		t.Fatalf("idempotent Remove: %v", apiErr)
	}
}

func TestFavoriteAddIsIdempotent(t *testing.T) {
	favoriteTestDB(t)
	svc := NewFavoriteService()

	first, apiErr := svc.Add(1, FavoriteModel, "esm2")
	if apiErr != nil {
		t.Fatalf("Add: %v", apiErr)
	}
	second, apiErr := svc.Add(1, FavoriteModel, "esm2")
	if apiErr != nil {
		t.Fatalf("second Add: %v", apiErr)
	}
	if first.CreatedAt != second.CreatedAt {
		t.Fatalf("starring twice created a new row: %+v vs %+v", first, second)
	}
	list, _ := svc.List(1)
	if len(list) != 1 {
		t.Fatalf("list = %+v", list)
	}
}

func TestFavoriteIsPerUser(t *testing.T) {
	favoriteTestDB(t)
	svc := NewFavoriteService()

	if _, apiErr := svc.Add(1, FavoriteModel, "esm2"); apiErr != nil {
		t.Fatalf("Add: %v", apiErr)
	}
	other, _ := svc.List(2)
	if len(other) != 0 {
		t.Fatalf("user 2 sees %+v", other)
	}
}

func TestFavoriteRejectsInvalidInput(t *testing.T) {
	favoriteTestDB(t)
	svc := NewFavoriteService()

	cases := []struct{ kind, name string }{
		{"workflow", "esm2"},
		{FavoriteModel, "   "},
		{FavoriteModel, "a/b"},
		{FavoriteModel, "back\\slash"},
	}
	for _, tc := range cases {
		if _, apiErr := svc.Add(1, tc.kind, tc.name); apiErr == nil {
			t.Fatalf("Add(%q, %q) should fail", tc.kind, tc.name)
		}
	}
}

func TestFavoriteTrimsName(t *testing.T) {
	favoriteTestDB(t)
	svc := NewFavoriteService()

	added, apiErr := svc.Add(1, FavoriteModel, "  esm2  ")
	if apiErr != nil {
		t.Fatalf("Add: %v", apiErr)
	}
	if added.Name != "esm2" {
		t.Fatalf("name = %q", added.Name)
	}
	if apiErr := svc.Remove(1, FavoriteModel, "esm2"); apiErr != nil {
		t.Fatalf("Remove: %v", apiErr)
	}
	list, _ := svc.List(1)
	if len(list) != 0 {
		t.Fatalf("list = %+v", list)
	}
}
