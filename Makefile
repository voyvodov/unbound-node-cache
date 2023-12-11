


export CGO_ENABLED=0

.PHONY: build
build:
	@go build -o build/nodelocaldns -ldflags "-s -w"  ./cmd


	docker build -t nodelocaldns build

.PHONY: update-all
update-all:
	@go get -u ./...