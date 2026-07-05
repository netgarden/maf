package rrpc_auth

import (
	"github.com/netgarden/maf/auth/entities"
	"github.com/netgarden/maf/auth/services"
)

type Session = entities.Session
type User = entities.User

type AuthService = services.AuthService
type SessionsService = services.SessionsService
type UsersService = services.UsersService

type TemplateMailer = services.TemplateMailer
type NewUserCredentialsData = services.NewUserCredentialsData
type PasswordResetData = services.PasswordResetData

const NewUserCredentialsTemplateID = services.NewUserCredentialsTemplateID
const PasswordResetTemplateID = services.PasswordResetTemplateID
