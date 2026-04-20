BINARY      := teapi
INSTALL_DIR := $(HOME)/.local/bin
BUILD_DIR   := bin

.PHONY: all build install uninstall clean

all: build

build:
	@mkdir -p $(BUILD_DIR)
	go build -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY) .
	@echo "Built: $(BUILD_DIR)/$(BINARY)"

install: build
	@mkdir -p $(INSTALL_DIR)
	cp $(BUILD_DIR)/$(BINARY) $(INSTALL_DIR)/$(BINARY)
	@echo ""
	@echo "  teapi installed to $(INSTALL_DIR)/$(BINARY)"
	@echo ""
	@if echo "$$PATH" | grep -q "$(INSTALL_DIR)"; then \
		echo "  $(INSTALL_DIR) is already in your PATH."; \
	else \
		echo "  NOTE: $(INSTALL_DIR) is not in your PATH."; \
		echo "  Add this to your shell rc file and reload:"; \
		echo "    export PATH=\"\$$HOME/.local/bin:\$$PATH\""; \
	fi
	@echo ""
	@echo "  Config file (created on first launch):"
	@echo "    Linux:  \$$HOME/.config/delbysoft/teapi.toml"
	@echo "    macOS:  \$$HOME/Library/Application Support/delbysoft/teapi.toml"
	@echo ""
	@echo "  Data file:"
	@echo "    Linux:  \$$HOME/.config/delbysoft/teapi.json"
	@echo "    macOS:  \$$HOME/Library/Application Support/delbysoft/teapi.json"
	@echo ""
	@echo "  Run: teapi"

uninstall:
	@rm -f $(INSTALL_DIR)/$(BINARY)
	@echo "Removed $(INSTALL_DIR)/$(BINARY)"
	@echo ""
	@echo "Config and data files have been left in place."
	@echo "To fully remove, delete:"
	@echo "  Linux:  \$$HOME/.config/delbysoft/"
	@echo "  macOS:  \$$HOME/Library/Application Support/delbysoft/"

clean:
	rm -rf $(BUILD_DIR)
