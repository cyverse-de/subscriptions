package db

import (
	"context"

	"github.com/cyverse-de/subscriptions/internal/qmsapi/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UpsertUsage either inserts a new usage record into the database or updates an existing one. A new update record
// is also recorded at the same time.
func UpsertUsage(ctx context.Context, db *gorm.DB, usage *model.Usage) error {
	return db.WithContext(ctx).Debug().Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{
				Name: "subscription_id",
			},
			{
				Name: "resource_type_id",
			},
		},
		UpdateAll: true,
	}).Create(&usage).Error
}
