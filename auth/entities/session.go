package entities

import (
	uuid "github.com/satori/go.uuid"
	"github.com/netgarden/maf/database"
)

type Session struct {
	database.EntityBase
	UserID   uuid.UUID
	User     *User
	Agent    string
	ClientIP string
}

func (Session) TableName() string {
	return "auth_sessions"
}
