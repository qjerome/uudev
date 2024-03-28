ifndef INSTALL_DIR
	INSTALL_DIR="/bin/"
endif

TARGET="target"

clean:
	rm -rf $(TARGET)

build: clean
	mkdir -p $(TARGET)
	go build -o $(TARGET)/uudev main.go

install: build
	cp $(TARGET)/uudev $(INSTALL_DIR)
