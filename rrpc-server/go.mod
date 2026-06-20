module github.com/netgarden/maf/rrpc-server

go 1.26.1

replace github.com/netgarden/rrpc => ../../netgarden/rrpc/rrpc

require (
	github.com/netgarden/maf v0.0.0-20260417193235-7c38ceefb8c3
	github.com/netgarden/maf/security v0.0.0-20260516150744-e7763ffd1638
	github.com/netgarden/rrpc v0.0.0-20260410205907-234c96e624c8
)

require (
	github.com/elliotchance/orderedmap v1.6.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/netgarden/maf => ..

replace github.com/netgarden/maf/security => ../security
