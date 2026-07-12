package services

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/netgarden/maf/auth/dto"
	"github.com/netgarden/maf/auth/entities"
	"github.com/netgarden/maf/datatables"
	"github.com/netgarden/maf/locks"
	"github.com/netgarden/maf/security/passwords"
	uuid "github.com/satori/go.uuid"
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

func (s *UsersService) GetUserByEmail(email string) (*entities.User, error) {
	return s.getUser(s.db, "email", email)
}

// CreateExternalUser JIT-provisions a User for a first-time external-login
// (OIDC today) identity that didn't match any existing account — see
// AuthService.CompleteExternalLogin. Active=true, Admin=false; Password is
// left at its zero value, which the SHA512 encoder never produces from
// Encode, so password login correctly fails closed for these users until
// (if ever) an admin sets one.
func (s *UsersService) CreateExternalUser(email, firstName, lastName string) (*entities.User, error) {
	username, err := s.uniqueUsernameFor(email)
	if err != nil {
		return nil, err
	}

	user := &entities.User{
		Username:  username,
		Email:     email,
		FirstName: firstName,
		LastName:  lastName,
		Active:    true,
	}
	if err := s.db.Save(user).Error; err != nil {
		return nil, err
	}
	return user, nil
}

// SetAdmin sets User.Admin directly — used to sync the flag from an
// external provider's admin-claim mapping on every login (see
// AuthService.CompleteExternalLogin), independent of UpdateUser's full-row
// admin-UI update path.
func (s *UsersService) SetAdmin(id string, admin bool) error {
	return s.db.Model(&entities.User{}).Where("id = ?", id).Update("admin", admin).Error
}

// uniqueUsernameFor picks a username for CreateExternalUser: the local part
// of email if free, else the full email, else a random fallback. Usernames
// have no DB-level uniqueness constraint in this codebase (see CreateUser's
// own check-then-create above), so this only needs to avoid the common
// case, not guarantee atomicity under a concurrent-signup race.
func (s *UsersService) uniqueUsernameFor(email string) (string, error) {
	var candidates []string
	if email != "" {
		if at := strings.IndexByte(email, '@'); at > 0 {
			candidates = append(candidates, email[:at])
		}
		candidates = append(candidates, email)
	}
	for _, candidate := range candidates {
		existing, err := s.GetUserByUsername(candidate)
		if err != nil {
			return "", err
		}
		if existing == nil {
			return candidate, nil
		}
	}
	return "user-" + uuid.NewV4().String(), nil
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
		err := s.mailer.SendTemplate(NewUserCredentialsTemplateID, []string{user.Email}, nil, nil, NewUserCredentialsData{
			Username:          user.Username,
			TemporaryPassword: data.Password,
			LoginURL:          s.loginURL,
		})
		if err != nil {
			slog.Error("auth: failed to send new-user-credentials email", slog.Any("error", err), slog.String("username", user.Username))
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

// userColumns declares User's queryable fields for ListUsersPage: which
// are sortable, and — if filterable — the single operator applied
// whenever a request's Filters has a value for it (see datatables.Column).
// admin/active are sortable only, not filterable — a boolean column has
// no clean "not applied" value to distinguish from a real false/true
// (rrpc has no optional-field support to fall back on either — see
// citadel's HostGroups plan notes on the same constraint).
var userColumns = []datatables.Column{
	{Name: "username", DBColumn: "username", Sortable: true, FilterOperator: datatables.Contains},
	{Name: "email", DBColumn: "email", Sortable: true, FilterOperator: datatables.Contains},
	{Name: "firstName", DBColumn: "first_name", Sortable: true, FilterOperator: datatables.Contains},
	{Name: "lastName", DBColumn: "last_name", Sortable: true, FilterOperator: datatables.Contains},
	{Name: "admin", DBColumn: "admin", Sortable: true},
	{Name: "active", DBColumn: "active", Sortable: true},
}

// ListUsersPage returns a filtered, sorted, paginated page of users per q.
// When q.SortBy is unset, users are ordered by username by default —
// datatables.Apply itself only adds an ORDER BY when a sort is actually
// requested, so the default lives here on the base query instead.
func (s *UsersService) ListUsersPage(q datatables.Query) (*datatables.Result[*entities.User], error) {
	db := s.db.Model(&entities.User{})
	if q.SortBy == "" {
		db = db.Order("username")
	}
	return datatables.Apply[*entities.User](db, userColumns, q)
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
