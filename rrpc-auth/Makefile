RRPC_GEN_DIR := ../../netgarden/rrpc/rrpc-gen

.PHONY: generate

generate:
	cd $(RRPC_GEN_DIR) && RRPC_DEVEL=true go run ./cmd/rrpc-gen/ \
		--source $(CURDIR)/def/module.rrpc \
		--target $(CURDIR)/rpc \
		--type go