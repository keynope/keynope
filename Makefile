PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
LIBEXECDIR ?= $(PREFIX)/libexec/keynope

GO ?= go
SWIFTC ?= swiftc
CODESIGN ?= codesign
ACTOOL ?= xcrun actool
PLUTIL ?= plutil
SWIFTFLAGS ?= -warnings-as-errors -strict-concurrency=complete
MACOSX_DEPLOYMENT_TARGET ?= 14.0
SWIFT_ARCH ?= $(shell uname -m)
SWIFT_TARGET ?= $(SWIFT_ARCH)-apple-macosx$(MACOSX_DEPLOYMENT_TARGET)
CODESIGN_IDENTITY ?= -

BIN_DIR := bin
KEYNOPE := $(BIN_DIR)/keynope
PRESENTER_APP := $(BIN_DIR)/KeynopePresenter.app
PRESENTER := $(PRESENTER_APP)/Contents/MacOS/KeynopePresenter
KEYNOPE_APP := $(BIN_DIR)/Keynope.app
KEYNOPE_APP_EXECUTABLE := $(KEYNOPE_APP)/Contents/MacOS/Keynope
KEYNOPE_APP_ENGINE := $(KEYNOPE_APP)/Contents/Helpers/keynope-engine
KEYNOPE_APP_INFO := app/Info.plist
KEYNOPE_APP_ENTITLEMENTS := app/Keynope.entitlements
KEYNOPE_APP_ENGINE_ENTITLEMENTS := app/KeynopeEngine.entitlements
KEYNOPE_APP_PRIVACY := app/PrivacyInfo.xcprivacy
KEYNOPE_APP_WELCOME := app/Welcome.md
KEYNOPE_APP_ICON := assets/KeynopeApp.icon
KEYNOPE_APP_ICON_FILES := $(shell find $(KEYNOPE_APP_ICON) -type f)
KEYNOPE_APP_ICON_INFO := $(BIN_DIR)/KeynopeAppIconInfo.plist
KEYNOPE_EMOJI_ASSETS := assets/emoji/keynope-emoji-glyphs.bin.gz assets/emoji/emoji-test.txt assets/emoji/OFL.txt assets/emoji/NOTICE.txt assets/emoji/NOTO-REGION-FLAGS-LICENSE.txt assets/emoji/UNICODE-LICENSE.txt
KEYNOPE_VERSION := $(shell awk '/^\#\# [0-9]/{print $$2; exit}' CHANGELOG.md)
PRESENTER_INFO := presenter/Info.plist
PRESENTER_ICON := assets/KeynopeMenuTemplate.png
PRESENTER_SIGNATURE := $(PRESENTER_APP)/Contents/_CodeSignature/CodeResources
PRESENTER_SRC := presenter/KeynopePresenter.swift presenter/ScreenShare.swift presenter/EmbeddedIcon.swift
PRESENTER_FRAMEWORKS := -framework Cocoa -framework WebKit -framework AVFoundation -framework ScreenCaptureKit
GO_SRC := $(filter-out %_test.go,$(wildcard *.go))

.PHONY: all build app keynope presenter test install clean

all: build

build: keynope presenter

keynope: $(KEYNOPE)

presenter: $(PRESENTER_SIGNATURE)

app: $(KEYNOPE_APP)/Contents/_CodeSignature/CodeResources

$(KEYNOPE_APP)/Contents/_CodeSignature/CodeResources: $(PRESENTER_SRC) $(KEYNOPE_APP_INFO) $(KEYNOPE_APP_ENTITLEMENTS) $(KEYNOPE_APP_ENGINE_ENTITLEMENTS) $(KEYNOPE_APP_PRIVACY) $(KEYNOPE_APP_WELCOME) $(KEYNOPE_APP_ICON_FILES) $(PRESENTER_ICON) $(KEYNOPE_EMOJI_ASSETS) $(GO_SRC) sandbox_bridge_darwin.m go.mod go.sum CHANGELOG.md LICENSE.txt
	rm -rf $(KEYNOPE_APP)
	@mkdir -p $(KEYNOPE_APP)/Contents/MacOS $(KEYNOPE_APP)/Contents/Helpers $(KEYNOPE_APP)/Contents/Resources
	cp $(KEYNOPE_APP_INFO) $(KEYNOPE_APP)/Contents/Info.plist
	$(PLUTIL) -replace CFBundleShortVersionString -string "$(KEYNOPE_VERSION)" $(KEYNOPE_APP)/Contents/Info.plist
	$(ACTOOL) $(KEYNOPE_APP_ICON) --compile $(KEYNOPE_APP)/Contents/Resources --platform macosx --minimum-deployment-target $(MACOSX_DEPLOYMENT_TARGET) --app-icon KeynopeApp --output-partial-info-plist $(KEYNOPE_APP_ICON_INFO)
	cp $(PRESENTER_ICON) $(KEYNOPE_APP)/Contents/Resources/KeynopeMenuTemplate.png
	cp $(KEYNOPE_APP_PRIVACY) $(KEYNOPE_APP)/Contents/Resources/PrivacyInfo.xcprivacy
	cp $(KEYNOPE_APP_WELCOME) $(KEYNOPE_APP)/Contents/Resources/Welcome.md
	@mkdir -p $(KEYNOPE_APP)/Contents/Resources/EmojiLicenses
	cp assets/emoji/OFL.txt assets/emoji/NOTICE.txt assets/emoji/*-LICENSE.txt $(KEYNOPE_APP)/Contents/Resources/EmojiLicenses/
	cp LICENSE.txt $(KEYNOPE_APP)/Contents/Resources/EmojiLicenses/Keynope-LICENSE.txt
	$(GO) build -o $(KEYNOPE_APP_ENGINE) .
	$(SWIFTC) $(SWIFTFLAGS) -target $(SWIFT_TARGET) -O $(PRESENTER_FRAMEWORKS) $(PRESENTER_SRC) -o $(KEYNOPE_APP_EXECUTABLE)
	$(CODESIGN) --force --options runtime --identifier sh.keynope.app.engine --entitlements $(KEYNOPE_APP_ENGINE_ENTITLEMENTS) --sign "$(CODESIGN_IDENTITY)" $(KEYNOPE_APP_ENGINE)
	$(CODESIGN) --force --options runtime --entitlements $(KEYNOPE_APP_ENTITLEMENTS) --sign "$(CODESIGN_IDENTITY)" $(KEYNOPE_APP)

$(KEYNOPE): $(GO_SRC) $(KEYNOPE_EMOJI_ASSETS) go.mod go.sum LICENSE.txt
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
	@mkdir -p $(LIBEXECDIR)/licenses
	cp assets/emoji/*-LICENSE.txt $(LIBEXECDIR)/licenses/
	rm -rf $(LIBEXECDIR)/KeynopePresenter.app
	cp -R $(PRESENTER_APP) $(LIBEXECDIR)/KeynopePresenter.app
	ln -sfn $(LIBEXECDIR)/keynope $(BINDIR)/keynope

clean:
	rm -rf $(BIN_DIR) build
