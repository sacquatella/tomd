VERSION=`git describe --tags`
BUILD=`date +%FT%T%z`
HASH=`git log -1 --format=%H`
AUTHOR=`git log -1 --format=%ce`
LDFLAGS=-ldflags "-w -s -X cmd.Version=${VERSION}  -X cmd.Build=${HASH}"

dependencies:
	go mod tidy
	ollama pull llava:7b

build-linux-amd:
	go mod tidy
	GOOS=linux GOARCH=amd64 go build -a -v ${LDFLAGS} -o bin/linux/tomd

build-linux-arm:
	go mod tidy
	GOOS=linux GOARCH=arm64 go build -a -v ${LDFLAGS} -o bin/linux-arm/tomd

build-osx-arm:
	go mod tidy
	GOOS=darwin GOARCH=arm64 go build -a -v ${LDFLAGS} -o bin/osx-silicon/tomd

build-windows:
	go mod tidy
	GOOS=windows GOARCH=amd64 go build -a -v ${LDFLAGS} -o bin/windows/tomd.exe

clean:
	go clean

test:
	go test -v ./...