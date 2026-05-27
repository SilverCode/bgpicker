.PHONY: build frontend run dev clean

# Build the frontend, then compile the Go binary (with embedded assets)
build: frontend
	go build -o bgpicker .

# Build just the Vue frontend
frontend:
	cd frontend && npm run build-only

# Run the compiled binary
run: build
	./bgpicker

# Development: start Go API and Vite dev server in parallel
dev:
	@echo "Starting Go backend on :8080 and Vite on :5173"
	@trap 'kill 0' EXIT; \
	  go run . & \
	  cd frontend && npm run dev

clean:
	rm -f bgpicker
	rm -rf frontend/dist
