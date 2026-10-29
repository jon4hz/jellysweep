.PHONY: run build templ css js clean test test-race test-cover

# Run the application with debug logging
run: build serve

serve:
	go run . serve --log-level=debug

# Build all assets and templates
build: templ css js

# Generate Go templates from templ files
templ:
	go tool templ generate -v

# Build CSS assets
css:
	npm run build-css

# Build JavaScript assets
js:
	npm run build-js

# Run all tests
test:
	go test ./...

# Run all tests with the race detector
test-race:
	go test -race ./...

# Run all tests with race detector and coverage report
# The printed total matches CI: see scripts/coverage-ratchet.sh for excluded packages.
test-cover:
	go test -race -coverpkg=./... -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out > coverage.func
	./scripts/coverage-ratchet.sh coverage.out coverage.floor
	rm -f coverage.func

# Clean generated files
clean:
	rm -rf web/templates/**/*_templ.go
	rm -rf internal/static/static/dist/
