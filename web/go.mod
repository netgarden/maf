module github.com/netgarden/maf/web

go 1.24.1

require (
	github.com/flosch/pongo2/v6 v6.0.0
	github.com/labstack/echo/v4 v4.13.3
	github.com/netgarden/maf v0.0.0-20250327102624-f25d54ddf786
	github.com/netgarden/maf/config v0.0.0-00010101000000-000000000000
	github.com/netgarden/maf/mergefs v0.0.0-20250327100833-ed0d8fb30bd1
)

require (
	github.com/elliotchance/orderedmap v1.8.0 // indirect
	github.com/labstack/gommon v0.4.2 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	golang.org/x/crypto v0.31.0 // indirect
	golang.org/x/net v0.33.0 // indirect
	golang.org/x/sys v0.28.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	golang.org/x/time v0.8.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/netgarden/maf => ..

replace github.com/netgarden/maf/mergefs => ../mergefs

replace github.com/netgarden/maf/config => ../config
