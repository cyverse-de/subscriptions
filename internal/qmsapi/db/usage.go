package db

import (
	"context"

	"github.com/cyverse-de/subscriptions/internal/qmsapi/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UpsertUsageGORM either inserts a new usage record into the database or updates an existing one. A new update record
// is also recorded at the same time.
func UpsertUsageGORM(ctx context.Context, db *gorm.DB, usage *model.Usage) error {
	return db.WithContext(ctx).Clauses(clause.OnConflict{
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
