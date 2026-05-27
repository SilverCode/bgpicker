.PHONY: build frontend run dev lambda deploy clean

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

# Build the Lambda deployment zip (requires Linux or cross-compile)
lambda: frontend
	GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap .
	zip -j bgpicker-lambda.zip bootstrap
	rm bootstrap

# Deploy to Lambda (requires aws CLI and prior `make lambda`)
# Usage: FUNCTION_NAME=bgpicker make deploy
deploy: lambda
	aws lambda update-function-code \
	  --function-name $${FUNCTION_NAME:-bgpicker} \
	  --zip-file fileb://bgpicker-lambda.zip \
	  --architectures arm64

clean:
	rm -f bgpicker bootstrap bgpicker-lambda.zip
	rm -rf frontend/dist
