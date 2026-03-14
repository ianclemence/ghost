BINARY   = ghost-bridge
PI_HOST  ?= pi@raspberrypi.local
PI_DIR   ?= /home/pi/ghost-bridge

.PHONY: build pi pi4 deploy clean

build:
	go mod tidy
	go build -o $(BINARY) .
	@echo "✓ Built $(BINARY)"

pi:
	GOOS=linux GOARCH=arm64 go build -o $(BINARY)-arm64 .
	@echo "✓ Built $(BINARY)-arm64 for Raspberry Pi 5"

pi4:
	GOOS=linux GOARCH=arm GOARM=7 go build -o $(BINARY)-armv7 .
	@echo "✓ Built $(BINARY)-armv7 for Raspberry Pi 4"

deploy: pi
	ssh $(PI_HOST) "mkdir -p $(PI_DIR)"
	scp $(BINARY)-arm64 $(PI_HOST):$(PI_DIR)/$(BINARY)
	scp ghost-bridge.service $(PI_HOST):/tmp/ghost-bridge.service
	ssh $(PI_HOST) "sudo cp /tmp/ghost-bridge.service /etc/systemd/system/ && \
	               sudo systemctl daemon-reload && \
	               sudo systemctl enable ghost-bridge && \
	               sudo systemctl restart ghost-bridge && \
	               echo '✓ ghost-bridge deployed and running'"

clean:
	rm -f $(BINARY) $(BINARY)-arm64 $(BINARY)-armv7
