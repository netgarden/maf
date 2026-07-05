package dto

type UserCreateDTO struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	Email     string `json:"email"`
	FirstName string `json:"firstname"`
	LastName  string `json:"lastname"`
	Admin     bool   `json:"admin"`

	// SendCredentialsEmail requests a best-effort notification email with
	// the new user's username/password — a no-op if the consuming
	// application never wired a TemplateMailer into UsersService (see
	// services.NewUserCredentialsTemplateID).
	SendCredentialsEmail bool `json:"sendCredentialsEmail"`
}

type UserUpdateDTO struct {
	Username  string
	Email     string
	FirstName string
	LastName  string
	Admin     bool
	Active    bool
}
