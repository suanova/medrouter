package service

import (
	"errors"
	"net/http"
	"strings"

	"gorm.io/gorm"

	"github.com/suanova/medrouter/internal/common"
	"github.com/suanova/medrouter/internal/model"
)

// Favorite kinds. They match the two catalog detail routes.
const (
	FavoriteModel   = "model"
	FavoriteDataset = "dataset"

	// favoriteLimit keeps a single user from turning the studio into a wall of
	// cards; it is also the bound the launch UI assumes.
	favoriteLimit   = 100
	favoriteNameMax = 128
)

// FavoriteService stores which models and datasets a user wants one-click
// access to. It deliberately does not verify the catalog entry exists: the
// catalogs can be temporarily unavailable, and a stale favorite is harmless
// (the studio renders it as unknown rather than failing the write).
type FavoriteService struct{}

func NewFavoriteService() *FavoriteService { return &FavoriteService{} }

// FavoriteDTO is one starred entry. It carries no catalog metadata; the page
// joins that locally from the catalogs it already loads.
type FavoriteDTO struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

// List returns the caller's favorites, newest first.
func (s *FavoriteService) List(userID int64) ([]FavoriteDTO, *common.APIError) {
	var rows []model.Favorite
	if err := model.DB.Where("user_id = ?", userID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, common.NewAPIErrorStatus(http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load favorites")
	}
	out := make([]FavoriteDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, FavoriteDTO{Kind: row.Kind, Name: row.Name, CreatedAt: row.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")})
	}
	return out, nil
}

// Add is idempotent: starring the same entry twice returns the existing row.
func (s *FavoriteService) Add(userID int64, kind, name string) (*FavoriteDTO, *common.APIError) {
	if apiErr := validateFavorite(kind, name); apiErr != nil {
		return nil, apiErr
	}
	name = strings.TrimSpace(name)

	var count int64
	if err := model.DB.Model(&model.Favorite{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		return nil, common.NewAPIErrorStatus(http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to save the favorite")
	}
	if count >= favoriteLimit {
		return nil, common.NewAPIErrorStatus(http.StatusConflict, "FAVORITE_LIMIT_REACHED",
			"You have reached the maximum number of favorites")
	}

	favorite := model.Favorite{UserID: userID, Kind: kind, Name: name}
	if err := model.DB.Where(model.Favorite{UserID: userID, Kind: kind, Name: name}).
		FirstOrCreate(&favorite).Error; err != nil {
		return nil, common.NewAPIErrorStatus(http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to save the favorite")
	}
	return &FavoriteDTO{
		Kind:      favorite.Kind,
		Name:      favorite.Name,
		CreatedAt: favorite.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}, nil
}

// Remove deletes a favorite. Removing one that does not exist is a success:
// the caller's intent is "this is not starred", and it now is not.
func (s *FavoriteService) Remove(userID int64, kind, name string) *common.APIError {
	if apiErr := validateFavorite(kind, name); apiErr != nil {
		return apiErr
	}
	err := model.DB.Where("user_id = ? AND kind = ? AND name = ?", userID, kind, strings.TrimSpace(name)).
		Delete(&model.Favorite{}).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return common.NewAPIErrorStatus(http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to remove the favorite")
	}
	return nil
}

func validateFavorite(kind, name string) *common.APIError {
	if kind != FavoriteModel && kind != FavoriteDataset {
		return common.NewAPIErrorStatus(http.StatusBadRequest, "INVALID_KIND", `kind must be "model" or "dataset"`)
	}
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return common.NewAPIErrorStatus(http.StatusBadRequest, "INVALID_NAME", "name is required")
	}
	if len(trimmed) > favoriteNameMax {
		return common.NewAPIErrorStatus(http.StatusBadRequest, "INVALID_NAME", "name is too long")
	}
	if strings.ContainsAny(trimmed, "/\\\x00") {
		return common.NewAPIErrorStatus(http.StatusBadRequest, "INVALID_NAME", "name contains invalid characters")
	}
	return nil
}
