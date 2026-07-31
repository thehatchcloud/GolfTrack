.DEFAULT_GOAL := help

.PHONY: help pb-test pb-bench pb-loadtest pb-css pb-dev clean

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

pb-test: ## Vet and test the PocketBase app (pocketbase/, Go)
	cd pocketbase && go vet ./... && go test ./...

pb-bench: ## Benchmark the PocketBase hot paths (not part of pb-test)
	cd pocketbase && go test -run '^$$' -bench . -benchmem

pb-loadtest: ## Load-test PocketBase at 10/50/100 concurrent players (slow; not part of pb-test)
	cd pocketbase && GOLFTRACK_LOADTEST=1 go test -run TestLoad -v -timeout 30m

pb-css: ## Compile Tailwind CSS for the PocketBase frontend (embedded in the binary)
	sh ./bin/build-pb-css.sh

pb-dev: ## Start the PocketBase app with the frontend at http://127.0.0.1:8090
	cd pocketbase && go run . serve

clean: ## Remove generated artifacts (pocketbase/.local/)
	rm -rf pocketbase/.local/
