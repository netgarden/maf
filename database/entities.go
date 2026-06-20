package database

import (
	"github.com/satori/go.uuid"
	"gorm.io/gorm"
	"time"
)

// EntityBase contains common columns for all tables.
type EntityBase struct {
	ID        uuid.UUID  `gorm:"type:uuid;primary_key;" json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
	//DeletedAt *time.Time
}

// BeforeCreate will set a UUID rather than numeric ID.
func (base *EntityBase) BeforeCreate(tx *gorm.DB) error {
	base.ID = uuid.NewV4()
	return nil
}

func (base *EntityBase) BeforeUpdate(tx *gorm.DB) error {
	now := time.Now()
	base.UpdatedAt = &now
	return nil
}
