set dotenv-load

default: run

run:
    go run .

# Static binary, no libc dependency
build:
    CGO_ENABLED=0 go build -o bin/music-translator .

test:
    go test ./...

lint:
    go vet ./...

tidy:
    go mod tidy
