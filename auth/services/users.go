package services

import (
	"errors"
	"log/slog"

	"github.com/netgarden/maf/auth/dto"
	"github.com/netgarden/maf/auth/entities"
	"github.com/netgarden/maf/locks"
	"github.com/netgarden/maf/security/passwords"
	"gorm.io/gorm"
)

// adminBootstrapLockNamespace serializes EnsureAdminExists across replicas
// starting concurrently against the same database. Distinct from
// locks.SystemLockNamespace (0) and jobs.JobsLockNamespace (1) so this
// doesn't serialize against unrelated lock users sharing the same locks
// table.
const adminBootstrapLockNamespace locks.LockNamespace = 2

func NewUsersService(db *gorm.DB, passwordsManager *passwords.Manager, locksService *locks.Service) *UsersService {
	return &UsersService{
		db:               db,
		passwordsManager: passwordsManager,
		locksService:     locksService,
	}
}

type UsersService struct {
	db               *gorm.DB
	passwordsManager *passwords.Manager
	locksService     *locks.Service

	mailer   TemplateMailer
	loginURL string
}

// SetMailer wires optional new-user-credentials email delivery. Called by
// rrpc-auth's Module.Initialize() only when a "mailer" module is also
// registered by the consuming application; leave unset (the default) to
// make dto.UserCreateDTO.SendCredentialsEmail a no-op.
func (s *UsersService) SetMailer(mailer TemplateMailer, loginURL string) {
	s.mailer = mailer
	s.loginURL = loginURL
}

func (s *UsersService) GetUser(id string) (*entities.User, error) {
	return s.getUser(s.db, "id", id)
}

func (s *UsersService) GetUserByUsername(username string) (*entities.User, error) {
	return s.getUser(s.db, "username", username)
}

// CreateUser creates a new user with a hashed password. Returns (nil, nil)
// when the username is already taken.
func (s *UsersService) CreateUser(data *dto.UserCreateDTO) (*entities.User, error) {

	existing, err := s.GetUserByUsername(data.Username)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, nil
	}

	user := &entities.User{}
	user.Username = data.Username
	user.Password = s.passwordsManager.Encode(data.Password)
	user.Email = data.Email
	user.FirstName = data.FirstName
	user.LastName = data.LastName
	user.Admin = data.Admin
	user.Active = true

	if err := s.db.Save(user).Error; err != nil {
		return nil, err
	}

	// Best-effort: a failed notification email doesn't undo a successfully
	// created user, it's just logged — the admin who created the account
	// can always retry via the mail queue's own admin UI.
	if data.SendCredentialsEmail && s.mailer != nil {
		err := s.mailer.EnqueueTemplate(NewUserCredentialsTemplateID, []string{user.Email}, nil, nil, NewUserCredentialsData{
			Username:          user.Username,
			TemporaryPassword: data.Password,
			LoginURL:          s.loginURL,
		})
		if err != nil {
			slog.Error("auth: failed to enqueue new-user-credentials email", slog.Any("error", err), slog.String("username", user.Username))
		}
	}

	return user, nil
}

// EnsureAdminExists creates a default admin user (username "admin") with
// the given password if no user with the admin flag set exists yet — it's
// a no-op if any admin user already exists, whatever their username.
// Meant to be called once at application startup so a fresh installation
// has a working admin account out of the box.
//
// Safe to call concurrently from multiple replicas starting at the same
// time against the same database: the check-then-create runs inside
// locksService.RunExclusive, which serializes every caller across every
// replica via a Postgres advisory lock held for the transaction's
// duration — so only one replica ever gets past the "does an admin exist"
// check before actually creating one.
func (s *UsersService) EnsureAdminExists(defaultPassword string) error {
	return s.locksService.RunExclusive(adminBootstrapLockNamespace, func(tx *gorm.DB) error {

		var count int64
		if err := tx.Model(&entities.User{}).Where("admin = ?", true).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil
		}

		user := &entities.User{
			Username: "admin",
			Password: s.passwordsManager.Encode(defaultPassword),
			Admin:    true,
			Active:   true,
		}
		return tx.Create(user).Error
	})
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
