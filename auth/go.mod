module github.com/netgarden/maf/auth

go 1.24.1

require (
	github.com/netgarden/maf v0.0.0-20250327102624-f25d54ddf786
	github.com/netgarden/maf/database v0.0.0-20250418155353-ce0720d881d7
	github.com/netgarden/maf/security v0.0.0-00010101000000-000000000000
	github.com/satori/go.uuid v1.2.0
	gorm.io/gorm v1.31.0
)

require (
	github.com/elliotchance/orderedmap v1.8.0 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jackc/pgx/v5 v5.5.5 // indirect
	github.com/jackc/puddle/v2 v2.2.1 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/stretchr/testify v1.10.0 // indirect
	golang.org/x/crypto v0.31.0 // indirect
	golang.org/x/sync v0.10.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	gorm.io/driver/postgres v1.5.11 // indirect
)

replace github.com/netgarden/maf => ..

replace github.com/netgarden/maf/database => ../database

replace github.com/netgarden/maf/security => ../security
