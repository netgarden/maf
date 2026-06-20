package services

import (
	"errors"

	"gorm.io/gorm"
	"github.com/netgarden/maf/auth/dto"
	"github.com/netgarden/maf/auth/entities"
)

func NewUsersService(db *gorm.DB) *UsersService {
	return &UsersService{
		db: db,
	}
}

type UsersService struct {
	db *gorm.DB
}

func (s *UsersService) GetUser(id string) (*entities.User, error) {
	return s.getUser(s.db, "id", id)
}

func (s *UsersService) GetUserByUsername(username string) (*entities.User, error) {
	return s.getUser(s.db, "username", username)
}

func (s *UsersService) CreateUser(data *dto.UserCreateDTO) (*entities.User, error) {

	user := &entities.User{}
	user.Username = data.Username
	user.Password = data.Password
	user.Email = data.Email
	user.FirstName = data.FirstName
	user.LastName = data.LastName
	user.Admin = data.Admin
	user.Active = true

	err := s.db.Save(user).Error
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (s *UsersService) UpdatePassword(id, passwordHash string) error {
	return s.db.Model(&entities.User{}).Where("id = ?", id).Update("password", passwordHash).Error
}

func (s *UsersService) ListUsers() ([]*entities.User, error) {
	var users []*entities.User
	if err := s.db.Order("username").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

func (s *UsersService) UpdateUser(id string, data *dto.UserUpdateDTO) (*entities.User, error) {
	user := &entities.User{}
	if err := s.db.First(user, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	user.Username = data.Username
	user.Email = data.Email
	user.FirstName = data.FirstName
	user.LastName = data.LastName
	user.Admin = data.Admin
	user.Active = data.Active
	if err := s.db.Save(user).Error; err != nil {
		return nil, err
	}
	return user, nil
}

func (s *UsersService) DeleteUser(id string) (bool, error) {
	result := s.db.Where("id = ?", id).Delete(&entities.User{})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (*UsersService) getUser(db *gorm.DB, field string, value string) (*entities.User, error) {

	user := &entities.User{}
	err := db.First(user, field+" = ?", value).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return user, nil
}
