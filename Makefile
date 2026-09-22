.PHONY: all build run dev test lint clean help

# Default target
all: build

# Help
help:
	@echo "Available commands:"
	@echo "  make build       - Build both backend and frontend"
	@echo "  make run         - Run the backend server"
	@echo "  make dev         - Run both backend and frontend in dev mode"
	@echo "  make dev-backend - Run only backend in dev mode"
	@echo "  make dev-frontend- Run only frontend in dev mode"
	@echo "  make test        - Run all tests"
	@echo "  make lint        - Run linters"
	@echo "  make clean       - Clean build artifacts"
	@echo "  make db-up       - Start PostgreSQL with Docker"
	@echo "  make db-down     - Stop PostgreSQL"
	@echo "  make db-reset    - Reset database"
	@echo "  make deps        - Install all dependencies"
	@echo "  make deps-backend- Install backend dependencies"
	@echo "  make deps-frontend- Install frontend dependencies"

# Build
build: build-backend build-frontend

build-backend:
	@echo "Building backend..."
	cd backend && go build -o bin/server ./cmd/server

build-frontend:
	@echo "Building frontend..."
	cd frontend && npm run build

# Run
run: build-backend
	@echo "Starting server..."
	cd backend && ./bin/server

# Development
dev:
	@echo "Starting development environment..."
	@make -j2 dev-backend dev-frontend

dev-backend:
	@echo "Starting backend..."
	cd backend && go run ./cmd/server

dev-frontend:
	@echo "Starting frontend..."
	cd frontend && npm run dev

# Test
test: test-backend test-frontend

test-backend:
	@echo "Running backend tests..."
	cd backend && go test ./...

test-frontend:
	@echo "Running frontend tests..."
	cd frontend && npm test

# Lint
lint: lint-backend lint-frontend

lint-backend:
	@echo "Linting backend..."
	cd backend && go vet ./...
	cd backend && gofmt -l .

lint-frontend:
	@echo "Linting frontend..."
	cd frontend && npm run lint

# Clean
clean:
	@echo "Cleaning..."
	rm -rf backend/bin
	rm -rf frontend/dist
	rm -rf frontend/node_modules

# Database
db-up:
	@echo "Starting PostgreSQL..."
	docker compose up -d postgres

db-down:
	@echo "Stopping PostgreSQL..."
	docker compose down

db-reset:
	@echo "Resetting database..."
	docker compose down -v
	docker compose up -d postgres
	@sleep 3
	cd backend && go run ./cmd/server & sleep 2 && kill %1

# Dependencies
deps: deps-backend deps-frontend

deps-backend:
	@echo "Installing backend dependencies..."
	cd backend && go mod tidy

deps-frontend:
	@echo "Installing frontend dependencies..."
	cd frontend && npm install

# Quick start
start: deps db-up
	@echo "Waiting for database..."
	@sleep 3
	@make dev

# Production build
production: deps
	@echo "Building for production..."
	cd backend && CGO_ENABLED=0 go build -o bin/server ./cmd/server
	cd frontend && npm run build

# Docker
docker-up:
	@echo "Starting all services..."
	docker compose up --build -d

docker-down:
	@echo "Stopping all services..."
	docker compose down
