package entities

import (
	"time"

	"github.com/netgarden/maf/database"
)

type User struct {
	database.EntityBase
	Username  string
	Password  string
	Email     string
	FirstName string
	LastName  string
	Admin     bool
	Active    bool
	LastLogin *time.Time
}

func (User) TableName() string {
	return "auth_users"
}
