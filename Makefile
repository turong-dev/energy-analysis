BIN := bin

.PHONY: all build harvest serve demo clean

all: build

build: harvest serve demo

harvest:
	go build -o $(BIN)/harvest ./cmd/harvest

serve:
	go build -o $(BIN)/serve ./cmd/serve

demo:
	go build -o $(BIN)/demo ./cmd/demo

clean:
	rm -rf $(BIN)
