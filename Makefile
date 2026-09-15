# Simple Makefile for a Go project

# Build the application
all: build test

build:
	@echo "Building..."
	@templ generate
	@go build -o main cmd/api/main.go

# Run the application
run:
	@templ generate
	@go run cmd/api/main.go

# Test the application
test:
	@echo "Testing..."
	@go test ./... -v

# Clean the binary
clean:
	@echo "Cleaning..."
	@rm -f main

# Wipe the local database and rebuild it from migrations - see cmd/dbreset.
# Interactive (asks for confirmation); pass ARGS=-yes to skip that.
db-reset:
	@go run ./cmd/dbreset $(ARGS)

# Populate the local database with example scheduling data - see cmd/dbseed.
db-seed:
	@go run ./cmd/dbseed

# Delete every Song and Plan in the signed-in Planning Center org - see
# cmd/pcoreset. Interactive (asks for confirmation); pass ARGS=-yes to skip
# that. Only clears Planning Center itself - pair with db-reset for a full
# reseed cycle.
pco-reset:
	@go run ./cmd/pcoreset $(ARGS)

# Run the admin app with live reload - see cmd/admin. Requires ADMIN_TOKEN
# in the environment/.env; binds to 127.0.0.1 only. Same air-based flow as
# watch, just its own config (.air.admin.toml) so the two don't fight over
# the same built binary/build-errors log.
admin:
	@if command -v air > /dev/null; then \
            air -c .air.admin.toml; \
            echo "Watching...";\
        else \
            read -p "Go's 'air' is not installed on your machine. Do you want to install it? [Y/n] " choice; \
            if [ "$$choice" != "n" ] && [ "$$choice" != "N" ]; then \
                go install github.com/air-verse/air@latest; \
                air -c .air.admin.toml; \
                echo "Watching...";\
            else \
                echo "You chose not to install air. Exiting..."; \
                exit 1; \
            fi; \
        fi

# Live Reload
watch:
	@if command -v air > /dev/null; then \
            air; \
            echo "Watching...";\
        else \
            read -p "Go's 'air' is not installed on your machine. Do you want to install it? [Y/n] " choice; \
            if [ "$$choice" != "n" ] && [ "$$choice" != "N" ]; then \
                go install github.com/air-verse/air@latest; \
                air; \
                echo "Watching...";\
            else \
                echo "You chose not to install air. Exiting..."; \
                exit 1; \
            fi; \
        fi

.PHONY: all build run test clean watch db-reset db-seed pco-reset admin
