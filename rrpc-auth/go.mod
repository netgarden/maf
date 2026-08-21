module github.com/netgarden/maf/rrpc-auth

go 1.26.1

replace github.com/netgarden/rrpc => ../../rrpc

require (
	github.com/netgarden/maf v0.0.0-20250327102624-f25d54ddf786
	github.com/netgarden/maf/auth v0.0.0-00010101000000-000000000000
	github.com/netgarden/maf/datatables v0.0.0-00010101000000-000000000000
	github.com/netgarden/rrpc v0.0.0-20260410205907-234c96e624c8
)

require (
	github.com/coder/websocket v1.8.15 // indirect
	github.com/coreos/go-oidc/v3 v3.20.0 // indirect
	github.com/elliotchance/orderedmap v1.8.0 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.10.0 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/netgarden/maf/database v0.0.0-20250418155353-ce0720d881d7 // indirect
	github.com/netgarden/maf/jobs v0.0.0-00010101000000-000000000000 // indirect
	github.com/netgarden/maf/locks v0.0.0-00010101000000-000000000000 // indirect
	github.com/netgarden/maf/mailer v0.0.0-00010101000000-000000000000 // indirect
	github.com/netgarden/maf/security v0.0.0-00010101000000-000000000000 // indirect
	github.com/netgarden/orderedlist v0.0.0-00010101000000-000000000000 // indirect
	github.com/satori/go.uuid v1.2.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	gorm.io/driver/postgres v1.6.0 // indirect
	gorm.io/gorm v1.31.0 // indirect
)

replace github.com/netgarden/maf => ..

replace github.com/netgarden/maf/auth => ../auth

replace github.com/netgarden/maf/database => ../database

replace github.com/netgarden/maf/datatables => ../datatables

replace github.com/netgarden/maf/jobs => ../jobs

replace github.com/netgarden/maf/locks => ../locks

replace github.com/netgarden/maf/mailer => ../mailer

replace github.com/netgarden/maf/security => ../security

replace github.com/netgarden/orderedlist => ../../orderedlist
