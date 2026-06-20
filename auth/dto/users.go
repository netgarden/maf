package dto

type UserCreateDTO struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	Email     string `json:"email"`
	FirstName string `json:"firstname"`
	LastName  string `json:"lastname"`
	Admin     bool   `json:"admin"`
}

type UserUpdateDTO struct {
	Username  string
	Email     string
	FirstName string
	LastName  string
	Admin     bool
	Active    bool
}
