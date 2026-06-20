package passwords

type Encoder interface {
	Encode(password string) string
	CanValidate(encodedPassword string) bool
	Validate(encodedPassword string, password string) bool
}
