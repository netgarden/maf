# security

`github.com/netgarden/maf/security` — password hashing/verification and
symmetric encryption for data at rest, as two independent sub-packages
(`passwords`, `encryption`) wired into one maf module.

## Usage

As a maf module:

```go
modules = append(modules, security.NewModule())

// after Initialize():
securityModule := manager.GetModule("security").(*security.Module)
passwordsManager := securityModule.GetPasswordsManager()
encryptionManager := securityModule.GetEncryptionManager()
```

Or construct either sub-package's manager directly —
`passwords.NewManager()` / `encryption.NewManager(secret)` — if you don't
need it wired through maf.

## Passwords (`security/passwords`)

`Manager.Encode(password) string` hashes a password for storage;
`Manager.Verify(encodedPassword, password) (bool, error)` checks a
plaintext password against a previously-encoded one. Hashing is pluggable
via the `Encoder` interface (`Encode`, `CanValidate`, `Validate`):
`Manager` holds a `defaultEncoder` (what `Encode` uses for new passwords)
and a list of every `Encoder` it knows how to `Verify` against, selected by
asking each in turn whether `CanValidate` recognizes the encoded string's
format. `NewManager()` wires up `SHA512Encoder` (prefix `$6$`, salted
SHA-512) as both.

This is the same "one default for new writes, several recognized for
reads" shape as `encryption.Manager` below — see there for the fuller
explanation of why, and for how to add a second `Encoder` if password
hashing ever needs to move to something else without breaking existing
hashes.

## Encryption at rest (`security/encryption`)

`Manager.Encrypt(plaintext []byte) ([]byte, error)` / `Manager.Decrypt(ciphertext []byte) ([]byte, error)`
for encrypting values before writing them to a database (e.g.
`maf/mailer`'s queued email bodies — see its README's "Encryption at
rest"). Both work in raw bytes, not strings — a caller that needs a
text-safe representation for a `TEXT` column (`mailer` does) base64-encodes
the result itself; `encryption` doesn't do that internally, since a caller
storing into a `BYTEA` column, or hashing/comparing raw ciphertext,
shouldn't pay for a text encoding it doesn't need.

`NewManager(secret string)` derives an AES-256 key from `secret` via
SHA-256 (see `NewAESGCMCryptor`), so this configures the same simple way
`security.secret` already does (a plain string, not a properly-sized/encoded
key an operator has to get exactly right). `encryption` itself doesn't
know or care where `secret` comes from — `security.Module` is the one
that cares that reusing the exact same secret raw for two unrelated
cryptographic purposes (this, and JWT signing in `maf/auth`) is bad
practice: it namespaces `security.secret` with a fixed, permanent suffix
before ever passing it here, so only one config value is needed but the
value `encryption` actually keys on is never the bare secret. That suffix
is deliberately *not* a hash — hashing here would be a second,
un-versioned cryptographic commitment outside the `Cryptor.Prefix()`
crypto-agility mechanism below, unable to change later without breaking
every already-encrypted value. The real key derivation algorithm stays
entirely inside whichever `Cryptor` owns it, already covered by that
mechanism.

### Crypto-agility via the `Cryptor` interface

Encryption algorithms don't stay best-practice forever, so `Manager`
doesn't hardcode AES-256-GCM — it dispatches to whichever registered
`Cryptor` produced a given piece of ciphertext:

```go
type Cryptor interface {
    Prefix() uint64                          // e.g. AESGCMPrefix = 1
    Encrypt(plaintext []byte) ([]byte, error)
    Decrypt(ciphertext []byte) ([]byte, error)
}
```

Every `Cryptor`'s output is prefixed with its own stable identifier — a
fixed-width 8-byte big-endian `uint64`, not a string, so it's cheap to
serialize/compare and doesn't need any escaping (`AESGCMCryptor`'s is the
constant `AESGCMPrefix = 1`). `Manager.Encrypt` always uses the current
*default* `Cryptor` and prepends its prefix; `Manager.Decrypt` reads the
leading 8 bytes, looks up whichever registered `Cryptor`'s `Prefix()`
matches, and hands it the rest — so it doesn't need to know in advance
which algorithm produced a given value.

This is what makes it possible to move to a more secure algorithm later
without losing the ability to read data already encrypted under the old
one: implement the new `Cryptor`, make it the default, and keep the old
one registered.

```go
func NewManager(secret string) *Manager {
    m := &Manager{}
    m.AddDefaultCryptor(NewAESGCMCryptor(secret)) // today

    // In the future, replacing the default while keeping old data readable:
    // m.AddCryptor(NewAESGCMCryptor(secret))            // demoted, still readable
    // m.AddDefaultCryptor(NewSomethingMoreSecure(secret)) // new default

    return m
}
```

`AddDefaultCryptor` both sets the default and registers it (so it's also
recognized on `Decrypt`); `AddCryptor` registers without changing the
default — that's the one to call for an algorithm you're phasing out but
still need to read.

Given a Cryptor with a genuinely unrecognized prefix (or malformed input
with no matching prefix at all), `Decrypt` returns an error rather than
guessing — see `TestDecrypt_UnknownPrefixReturnsError`. See
`TestManager_DecryptsUnderPreviousDefaultAfterCryptorChange` for the exact
scenario this whole design exists for: encrypt under one default, switch
the default, confirm the old ciphertext still decrypts and new ciphertext
uses the new default.

## Configuration

| Key | Type | Default | Notes |
|---|---|---|---|
| `security.secret` | string | — (required) | Used elsewhere (`maf/auth`) for JWT signing, and — with a fixed suffix appended — to derive the `encryption.Manager`'s default `Cryptor`'s key. The only secret this module requires. |

## Testing

`passwords` has no tests yet. `encryption` is pure logic with no database
or network dependency — round-trip, nonce randomness, tamper detection,
wrong-key failure, malformed input, and the `Cryptor`-swap scenario above
are all covered in `encryption/manager_test.go`:

```bash
cd encryption && go test ./...
```
