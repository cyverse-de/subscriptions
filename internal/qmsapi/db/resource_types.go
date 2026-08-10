package db

import (
	"context"
	"fmt"

	"github.com/cyverse-de/subscriptions/internal/qmsapi/model"
	"github.com/pkg/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GetResourceTypeByNameGORM looks up the resource type with the given name.
func GetResourceTypeByNameGORM(ctx context.Context, db *gorm.DB, name string) (*model.ResourceType, error) {
	wrapMsg := fmt.Sprintf("unable to look up resource type '%s'", name)
	var err error

	var resourceType model.ResourceType
	err = db.WithContext(ctx).Where(&model.ResourceType{Name: name}).First(&resourceType).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	} else if err != nil {
		return nil, errors.Wrap(err, wrapMsg)
	}

	return &resourceType, nil
}

// GetResourceTypeByIDGORM looks up the resource type with the given identifier.
func GetResourceTypeByIDGORM(ctx context.Context, db *gorm.DB, id string) (*model.ResourceType, error) {
	wrapMsg := fmt.Sprintf("unable to look up resource type '%s'", id)
	var err error

	var resourceType model.ResourceType
	err = db.WithContext(ctx).Where(&model.ResourceType{ID: &id}).First(&resourceType).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	} else if err != nil {
		return nil, errors.Wrap(err, wrapMsg)
	}

	return &resourceType, nil
}

// ListResourceTypesGORM lists all of the resource types defined in the database.
func ListResourceTypesGORM(ctx context.Context, db *gorm.DB) (*model.ResourceTypeList, error) {
	wrapMsg := "unable to list resource types"
	var err error

	var resourceTypes []*model.ResourceType
	err = db.WithContext(ctx).Find(&resourceTypes).Error
	if err != nil {
		return nil, errors.Wrap(err, wrapMsg)
	}

	return &model.ResourceTypeList{ResourceTypes: resourceTypes}, nil
}

// UpdateResourceTypeGORM updates an existing resource type.
func UpdateResourceTypeGORM(ctx context.Context, db *gorm.DB, resourceType model.ResourceType) error {
	wrapMsg := "unable to update resource type"
	var err error

	// Make sure that the incoming resource type has an identifier associated with it.
	if resourceType.ID == nil || *resourceType.ID == "" {
		return fmt.Errorf("%s: no resource type ID specified", wrapMsg)
	}

	// Save the resource type.
	err = db.WithContext(ctx).Save(&resourceType).Error
	if err != nil {
		return errors.Wrap(err, wrapMsg)
	}

	return nil
}

// SaveResourceTypeGORM saves a new resource type.
func SaveResourceTypeGORM(ctx context.Context, db *gorm.DB, resourceType model.ResourceType) (*model.ResourceType, error) {
	wrapMsg := "unable to save resource type"
	var err error

	// A non-nil resource type ID would break our duplicate check.
	resourceType.ID = nil

	// Save the resource type.
	err = db.
		Select("ID", "Name", "Unit", "Consumable").
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&resourceType).
		Error
	if err != nil {
		return nil, errors.Wrap(err, wrapMsg)
	}

	// If the ID wasn't populated in the resource type then there must have been a conflict.
	if resourceType.ID == nil || *resourceType.ID == "" {
		return nil, ErrResourceTypeConflict
	}

	// Return the resource type with the ID.
	return &resourceType, nil
}
