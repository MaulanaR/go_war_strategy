.PHONY: run test fmt vet

run:
	go run .

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...
