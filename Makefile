GO ?= godep go
GOPATH := $(CURDIR)/Godeps/_workspace:$(GOPATH)

all: build

build:
	$(GO) build -o paxos_server paxos/server

clean:
	rm paxos_server
