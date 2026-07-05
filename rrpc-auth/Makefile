RRPC_GEN_DIR := ../../rrpc/gen

.PHONY: generate

generate:
	cd $(RRPC_GEN_DIR) && RRPC_DEVEL=true go run ./cmd/rrpc-gen/ \
		--source $(CURDIR)/rpc/def/module.rrpc \
		--target $(CURDIR)/rpc \
		--type go
	gofmt -w $(CURDIR)/rpc