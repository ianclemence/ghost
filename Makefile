# Ghost Pi Makefile

.PHONY: all build run install clean

BINARY_NAME=ghost-pi
SRC_DIR=picoclaw

all: build

build:
	@echo "Building Ghost Pi..."
	cd $(SRC_DIR) && go build -o ../$(BINARY_NAME) ./cmd/picoclaw

run: build
	@echo "Running Ghost Pi..."
	./$(BINARY_NAME) gateway

install: build
	@echo "Installing Ghost Pi..."
	sudo cp $(BINARY_NAME) /usr/local/bin/
	sudo cp ghost.service /etc/systemd/system/
	sudo systemctl daemon-reload
	sudo systemctl enable ghost.service
	sudo systemctl start ghost.service
	@echo "Ghost Pi installed and started!"

clean:
	rm -f $(BINARY_NAME)
