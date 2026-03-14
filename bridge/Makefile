BINARY   = ghost-bridge
ENV_FILE ?= ../.env
ifneq ("$(wildcard $(ENV_FILE))","")
include $(ENV_FILE)
endif
PI_USER  ?= pi
PI_HOST  ?= $(PI_USER)@raspberrypi.local
PI_DIR   ?= /home/$(PI_USER)/ghost/bridge
export PI_USER PI_HOST PI_DIR

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
	scp $(BINARY)-arm64 $(PI_HOST):/tmp/$(BINARY)-arm64
	scp ghost-bridge.service $(PI_HOST):/tmp/ghost-bridge.service
	ssh $(PI_HOST) "sudo mv /tmp/$(BINARY)-arm64 $(PI_DIR)/$(BINARY) && \
	               sudo chmod +x $(PI_DIR)/$(BINARY) && \
	               sudo sed -i 's/User=pi/User=$(PI_USER)/g' /tmp/ghost-bridge.service && \
	               sudo sed -i 's|/home/pi|/home/$(PI_USER)|g' /tmp/ghost-bridge.service && \
	               sudo cp /tmp/ghost-bridge.service /etc/systemd/system/ && \
	               sudo systemctl daemon-reload && \
	               sudo systemctl enable ghost-bridge && \
	               sudo systemctl restart ghost-bridge && \
	               echo '✓ ghost-bridge deployed and running for user $(PI_USER)'"

clean:
	rm -f $(BINARY) $(BINARY)-arm64 $(BINARY)-armv7
