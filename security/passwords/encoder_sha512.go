package passwords

import (
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"math/rand"
	"strings"
)

func NewSHA512Encoder() *SHA512Encoder {
	return &SHA512Encoder{}
}

type SHA512Encoder struct{}

func (e *SHA512Encoder) Encode(password string) string {

	salt := e.generateSalt(e.getSaltLength())
	encoded := e.doEncode(salt, password)

	return fmt.Sprintf("%s%s$%s", e.getPrefix(), salt, encoded)
}

func (e *SHA512Encoder) CanValidate(encodedPassword string) bool {
	return strings.HasPrefix(encodedPassword, e.getPrefix())
}

func (e *SHA512Encoder) Validate(encodedPassword string, password string) bool {

	prefix := e.getPrefix()
	if !strings.HasPrefix(encodedPassword, prefix) {
		return false
	}

	encodedPasswordWithoutPrefix := encodedPassword[len(prefix):]

	parts := strings.SplitN(encodedPasswordWithoutPrefix, "$", 2)

	origHash := parts[1]
	checkHash := e.doEncode(parts[0], password)

	return origHash == checkHash
}

func (e *SHA512Encoder) getPrefix() string {
	return "$6$"
}

func (e *SHA512Encoder) getSaltLength() int {
	return 8
}

func (e *SHA512Encoder) doEncode(salt, password string) string {

	hasher := sha512.New()
	hasher.Write([]byte(password))
	hasher.Write([]byte(salt))

	hash := hasher.Sum(nil)

	return base64.StdEncoding.EncodeToString(hash)
}

func (e *SHA512Encoder) generateSalt(n int) string {

	saltCharset := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	b := make([]byte, n)
	for i := range b {
		b[i] = saltCharset[rand.Intn(len(saltCharset))]
	}

	return string(b)
}
