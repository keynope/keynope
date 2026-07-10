PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
LIBEXECDIR ?= $(PREFIX)/libexec/keynope

GO ?= go
SWIFTC ?= swiftc
CODESIGN ?= codesign
SWIFTFLAGS ?= -warnings-as-errors -strict-concurrency=complete
MACOSX_DEPLOYMENT_TARGET ?= 14.0
SWIFT_ARCH ?= $(shell uname -m)
SWIFT_TARGET ?= $(SWIFT_ARCH)-apple-macosx$(MACOSX_DEPLOYMENT_TARGET)
CODESIGN_IDENTITY ?= -

BIN_DIR := bin
KEYNOPE := $(BIN_DIR)/keynope
PRESENTER_APP := $(BIN_DIR)/KeynopePresenter.app
PRESENTER := $(PRESENTER_APP)/Contents/MacOS/KeynopePresenter
PRESENTER_INFO := presenter/Info.plist
PRESENTER_ICON := assets/KeynopeMenuTemplate.png
PRESENTER_SIGNATURE := $(PRESENTER_APP)/Contents/_CodeSignature/CodeResources
PRESENTER_SRC := presenter/KeynopePresenter.swift presenter/ScreenShare.swift presenter/EmbeddedIcon.swift
PRESENTER_FRAMEWORKS := -framework Cocoa -framework WebKit -framework AVFoundation -framework ScreenCaptureKit
GO_SRC := $(filter-out %_test.go,$(wildcard *.go))

.PHONY: all build keynope presenter test install clean

all: build

build: keynope presenter

keynope: $(KEYNOPE)

presenter: $(PRESENTER_SIGNATURE)

$(KEYNOPE): $(GO_SRC) go.mod go.sum
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(KEYNOPE) .

$(PRESENTER_SIGNATURE): $(PRESENTER_SRC) $(PRESENTER_INFO) $(PRESENTER_ICON)
	rm -f $(BIN_DIR)/KeynopePresenter
	rm -rf $(PRESENTER_APP)
	@mkdir -p $(PRESENTER_APP)/Contents/MacOS $(PRESENTER_APP)/Contents/Resources
	cp $(PRESENTER_INFO) $(PRESENTER_APP)/Contents/Info.plist
	cp $(PRESENTER_ICON) $(PRESENTER_APP)/Contents/Resources/KeynopeMenuTemplate.png
	$(SWIFTC) $(SWIFTFLAGS) -target $(SWIFT_TARGET) -O $(PRESENTER_FRAMEWORKS) $(PRESENTER_SRC) -o $(PRESENTER)
	$(CODESIGN) --force --sign "$(CODESIGN_IDENTITY)" $(PRESENTER_APP)

test:
	$(GO) test ./...

install: build
	@mkdir -p $(BINDIR) $(LIBEXECDIR)
	cp $(KEYNOPE) $(LIBEXECDIR)/keynope
	rm -rf $(LIBEXECDIR)/KeynopePresenter.app
	cp -R $(PRESENTER_APP) $(LIBEXECDIR)/KeynopePresenter.app
	ln -sfn $(LIBEXECDIR)/keynope $(BINDIR)/keynope

clean:
	rm -rf $(BIN_DIR) build
