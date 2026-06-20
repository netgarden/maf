package dto

type CredentialsLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}