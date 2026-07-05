RRPC_GEN_DIR := ../../rrpc/gen
RRPC_GEN     := RRPC_DEVEL=true go run ./cmd/rrpc-gen/

.PHONY: generate

generate:
	cd $(RRPC_GEN_DIR) && $(RRPC_GEN) \
		--source $(CURDIR)/rpc/def/module.rrpc \
		--target $(CURDIR)/rpc \
		--type go
	gofmt -w $(CURDIR)/rpc
