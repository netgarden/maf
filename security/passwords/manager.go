package passwords

import (
	"errors"
)

func NewManager() *Manager {

	m := &Manager{}
	m.encoders = make([]Encoder, 0)
	m.AddDefaultEncoder(NewSHA512Encoder())

	return m
}

type Manager struct {
	defaultEncoder Encoder
	encoders       []Encoder
}

func (m *Manager) Encode(password string) string {
	return m.defaultEncoder.Encode(password)
}

func (m *Manager) Verify(encodedPassword, password string) (bool, error) {

	var foundEncoder Encoder
	for _, encoder := range m.encoders {
		if encoder.CanValidate(encodedPassword) {
			foundEncoder = encoder
			break
		}
	}

	if foundEncoder == nil {
		return false, errors.New("password encoder not found")
	}

	return foundEncoder.Validate(encodedPassword, password), nil
}

func (m *Manager) AddDefaultEncoder(encoder Encoder) {
	m.defaultEncoder = encoder
	m.AddEncoder(encoder)
}

func (m *Manager) AddEncoder(encoder Encoder) {
	m.encoders = append(m.encoders, encoder)
}
