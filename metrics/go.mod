module github.com/netgarden/maf/metrics

go 1.27.1

require (
	github.com/netgarden/maf v0.0.0-20250327102624-f25d54ddf786
	github.com/netgarden/maf/security v0.0.0-00010101000000-000000000000
	github.com/netgarden/rrpc v0.0.0-00010101000000-000000000000
	github.com/prometheus/client_golang v1.24.1
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coder/websocket v1.8.15 // indirect
	github.com/elliotchance/orderedmap v1.6.0 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.70.1 // indirect
	github.com/prometheus/procfs v0.21.1 // indirect
	golang.org/x/sys v0.47.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/netgarden/maf => ..

replace github.com/netgarden/maf/security => ../security

replace github.com/netgarden/rrpc => ../../rrpc
