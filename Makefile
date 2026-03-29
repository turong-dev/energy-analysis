BIN := bin

.PHONY: all build harvest serve clean

all: build

build: harvest serve

harvest:
	go build -o $(BIN)/harvest ./cmd/harvest

serve:
	go build -o $(BIN)/serve ./cmd/serve

clean:
	rm -rf $(BIN)
