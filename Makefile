APPDIR ?= /Applications

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
KEYNOPE_APP := $(BIN_DIR)/Keynope.app
KEYNOPE_APP_EXECUTABLE := $(KEYNOPE_APP)/Contents/MacOS/Keynope
KEYNOPE_APP_ENGINE := $(KEYNOPE_APP)/Contents/Helpers/keynope-engine
KEYNOPE_APP_INFO := app/Info.plist
KEYNOPE_APP_ENTITLEMENTS := app/Keynope.entitlements
KEYNOPE_APP_ENGINE_ENTITLEMENTS := app/KeynopeEngine.entitlements
KEYNOPE_APP_PRIVACY := app/PrivacyInfo.xcprivacy
KEYNOPE_APP_WELCOME := app/Welcome.md
KEYNOPE_APP_ICON := assets/KeynopeAssets.xcassets
KEYNOPE_APP_ICON_FILES := $(shell find $(KEYNOPE_APP_ICON) -type f)
KEYNOPE_APP_ICON_FALLBACK := assets/KeynopeApp.icns
KEYNOPE_APP_ICON_INFO := $(BIN_DIR)/KeynopeAppIconInfo.plist
KEYNOPE_EMOJI_ASSETS := assets/emoji/keynope-emoji-glyphs.bin.gz assets/emoji/keynope-emoji-fonts.zip assets/emoji/emoji-test.txt assets/emoji/OFL.txt assets/emoji/NOTICE.txt assets/emoji/NOTO-REGION-FLAGS-LICENSE.txt assets/emoji/UNICODE-LICENSE.txt
KEYNOPE_VERSION := $(shell awk '/^\#\# [0-9]/{print $$2; exit}' CHANGELOG.md)
PRESENTER_ICON := assets/KeynopeMenuTemplate.png
PRESENTER_SRC := presenter/KeynopePresenter.swift presenter/ScreenShare.swift presenter/EmbeddedIcon.swift presenter/SlideExport.swift
PRESENTER_FRAMEWORKS := -framework Cocoa -framework WebKit -framework AVFoundation -framework ScreenCaptureKit -framework PDFKit
GO_SRC := $(filter-out %_test.go,$(wildcard *.go))
GO_SRC += web/activity-games.js
GO_SRC += web/activity-design.js
GO_SRC += web/truetype.js assets/keynope-c64.ttf.base64
GO_SRC += web/participant-transfer.js
GO_SRC += web/workspace.js web/workspace.css
GO_SRC += web/text-layout.js
GO_SRC += web/scene.js
GO_SRC += assets/fonts/GO-FONT-LICENSE.txt

.DEFAULT_GOAL := all

character_assets_data.go: characters_svg/items.json eyes.json tools/generate_character_assets.go
	$(GO) run ./tools/generate_character_assets.go

.PHONY: all build app web-editor test install clean

all: build

build: app

web-editor:
	./tools/build_web_editor.sh

app: $(KEYNOPE_APP)/Contents/_CodeSignature/CodeResources

$(KEYNOPE_APP)/Contents/_CodeSignature/CodeResources: $(PRESENTER_SRC) $(KEYNOPE_APP_INFO) $(KEYNOPE_APP_ENTITLEMENTS) $(KEYNOPE_APP_ENGINE_ENTITLEMENTS) $(KEYNOPE_APP_PRIVACY) $(KEYNOPE_APP_WELCOME) $(KEYNOPE_APP_ICON_FILES) $(KEYNOPE_APP_ICON_FALLBACK) $(PRESENTER_ICON) $(KEYNOPE_EMOJI_ASSETS) $(GO_SRC) sandbox_bridge_darwin.m go.mod go.sum CHANGELOG.md LICENSE.txt
	rm -rf $(KEYNOPE_APP)
	@mkdir -p $(KEYNOPE_APP)/Contents/MacOS $(KEYNOPE_APP)/Contents/Helpers $(KEYNOPE_APP)/Contents/Resources
	cp $(KEYNOPE_APP_INFO) $(KEYNOPE_APP)/Contents/Info.plist
	$(PLUTIL) -replace CFBundleShortVersionString -string "$(KEYNOPE_VERSION)" $(KEYNOPE_APP)/Contents/Info.plist
	$(ACTOOL) $(KEYNOPE_APP_ICON) --compile $(KEYNOPE_APP)/Contents/Resources --platform macosx --minimum-deployment-target $(MACOSX_DEPLOYMENT_TARGET) --app-icon KeynopeApp --output-partial-info-plist $(KEYNOPE_APP_ICON_INFO)
	cp $(KEYNOPE_APP_ICON_FALLBACK) $(KEYNOPE_APP)/Contents/Resources/KeynopeApp.icns
	cp $(PRESENTER_ICON) $(KEYNOPE_APP)/Contents/Resources/KeynopeMenuTemplate.png
	cp $(KEYNOPE_APP_PRIVACY) $(KEYNOPE_APP)/Contents/Resources/PrivacyInfo.xcprivacy
	cp $(KEYNOPE_APP_WELCOME) $(KEYNOPE_APP)/Contents/Resources/Welcome.md
	@mkdir -p $(KEYNOPE_APP)/Contents/Resources/EmojiLicenses
	cp assets/emoji/OFL.txt assets/emoji/NOTICE.txt assets/emoji/*-LICENSE.txt $(KEYNOPE_APP)/Contents/Resources/EmojiLicenses/
	cp assets/fonts/GO-FONT-LICENSE.txt $(KEYNOPE_APP)/Contents/Resources/EmojiLicenses/
	cp LICENSE.txt $(KEYNOPE_APP)/Contents/Resources/EmojiLicenses/Keynope-LICENSE.txt
	$(GO) build -o $(KEYNOPE_APP_ENGINE) .
	$(SWIFTC) $(SWIFTFLAGS) -target $(SWIFT_TARGET) -O $(PRESENTER_FRAMEWORKS) $(PRESENTER_SRC) -o $(KEYNOPE_APP_EXECUTABLE)
	$(CODESIGN) --force --options runtime --identifier sh.keynope.app.engine --entitlements $(KEYNOPE_APP_ENGINE_ENTITLEMENTS) --sign "$(CODESIGN_IDENTITY)" $(KEYNOPE_APP_ENGINE)
	$(CODESIGN) --force --options runtime --entitlements $(KEYNOPE_APP_ENTITLEMENTS) --sign "$(CODESIGN_IDENTITY)" $(KEYNOPE_APP)

test:
	$(GO) test ./...

install: build
	@mkdir -p $(APPDIR)
	ditto $(KEYNOPE_APP) $(APPDIR)/Keynope.app

clean:
	rm -rf $(BIN_DIR) build
