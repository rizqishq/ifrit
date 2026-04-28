APP_NAME := ifrit
VERSION := 0.1.0

.PHONY: build run clean test

build:
	go build -o $(APP_NAME) .

run: build
	./$(APP_NAME)

clean:
	rm -f $(APP_NAME)

test:
	go test ./...
