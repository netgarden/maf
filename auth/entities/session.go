package entities

import (
	"github.com/netgarden/maf/database"
	uuid "github.com/satori/go.uuid"
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
