package services

import (
	"errors"

	"github.com/netgarden/maf/auth/entities"
	"github.com/netgarden/maf/database"
	uuid "github.com/satori/go.uuid"
	"gorm.io/gorm"
)

func NewSessionsService(db *gorm.DB) *SessionsService {
	return &SessionsService{
		db: db,
	}
}

type SessionsService struct {
	db *gorm.DB
}

func (s *SessionsService) CreateSession(userID string, clientIP string, userAgent string) (*entities.Session, error) {

	userUUID, err := uuid.FromString(userID)
	if err != nil {
		return nil, err
	}

	session := &entities.Session{
		UserID:   userUUID,
		ClientIP: clientIP,
		Agent:    userAgent,
	}

	err = s.db.Save(session).Error
	if err != nil {
		return nil, err
	}

	return session, nil
}

func (s *SessionsService) DeleteSession(id string) error {
	return s.db.Delete(&entities.Session{}, id).Error
}

// DeleteSessionsByUserID removes every session belonging to userID —
// used by ConfirmPasswordReset to force re-login everywhere once a
// password has been reset, the same way a password change should
// invalidate any session an attacker may already hold.
func (s *SessionsService) DeleteSessionsByUserID(userID string) error {
	return s.db.Where("user_id = ?", userID).Delete(&entities.Session{}).Error
}

func (s *SessionsService) GetSession(id string) (*entities.Session, error) {
	return s.getSession(s.db, id)
}

func (s *SessionsService) getSession(db *gorm.DB, id string) (*entities.Session, error) {

	session := &entities.Session{}
	err := db.First(session, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return session, nil
}

func (s *SessionsService) UpdateSession(id string) error {

	sessionUUID, err := uuid.FromString(id)
	if err != nil {
		return err
	}

	return s.db.Model(
		&entities.Session{
			EntityBase: database.EntityBase{
				ID: sessionUUID,
			},
		},
	).Updates(
		map[string]interface{}{},
	).Error
}
