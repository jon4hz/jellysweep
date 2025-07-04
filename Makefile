.PHONY: run build templ css js clean

# Run the application with debug logging
run: build
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

# Clean generated files
clean:
	rm -rf web/templates/**/*_templ.go
	rm -rf static/static/dist/
