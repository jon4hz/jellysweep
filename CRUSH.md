# Jellysweep Development Guide

## Build/Test Commands
- `make build` - Build all assets (templ, CSS, JS)
- `make run` - Build and run with debug logging
- `go test ./...` - Run all tests
- `go test -run TestName ./package` - Run specific test
- `go test -v ./engine` - Run engine tests with verbose output
- `make templ` - Generate Go templates from .templ files
- `npm run build` - Build CSS and JS assets

## Code Style Guidelines
- Use `gofmt` for formatting (already enforced)
- Import order: stdlib, external, internal (github.com/jon4hz/jellysweep/...)
- Use testify/assert and testify/require for tests
- Error handling: return errors, use `//nolint:errcheck` when intentionally ignoring
- Struct tags: use both `yaml` and `mapstructure` for config structs
- Comments: document exported types and functions
- Naming: use camelCase for unexported, PascalCase for exported
- Use context.Context for cancellation and timeouts
- Prefer table-driven tests with `tests := []struct{}`
- Use `require` for test setup, `assert` for assertions
- Mock external dependencies in tests (see mocks_test.go)
