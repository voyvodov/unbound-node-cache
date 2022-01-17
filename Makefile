


export CGO_ENABLED=0

build:
	@go build -o build/nodelocaldns -ldflags "-s -w"  ./cmd


	docker build -t nodelocaldns build