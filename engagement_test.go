package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestEngagementMetadataRoundTrip(t *testing.T) {
	definition := EngagementDefinition{
		Kind:   "sort",
		Prompt: "Where should these ideas go?",
		Zones:  []string{"Now", "Next", "Later"},
		Cards:  []string{"Ship it", "Test it", "Discuss it"},
		Named:  true,
	}
	metadata, err := encodeEngagementMetadata(&definition)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeEngagementMetadata(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ID == "" || decoded.Code != "" {
		t.Fatalf("decoded engagement has no valid identity: %#v", decoded)
	}
	decoded.ID, decoded.Code = "", ""
	if !reflect.DeepEqual(decoded, &definition) {
		t.Fatalf("decoded engagement = %#v, want %#v", decoded, definition)
	}
}

func TestPulseDefaultsToDescriptiveAnonymousScale(t *testing.T) {
	definition, err := normalizeEngagement(EngagementDefinition{Kind: "pulse"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Not at all", "Hardly", "Meh", "Somewhat", "Quite", "Very"}
	if !reflect.DeepEqual(definition.Options, want) || definition.Named {
		t.Fatalf("pulse defaults = options %#v, named %v", definition.Options, definition.Named)
	}
	legacy, err := normalizeEngagement(EngagementDefinition{Kind: "pulse", Options: []string{"0", "1", "2", "3", "4", "5"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(legacy.Options, want) {
		t.Fatalf("legacy pulse defaults = %#v", legacy.Options)
	}
}

func TestOnboardingCodeSurvivesMetadataRoundTrip(t *testing.T) {
	definition, err := normalizeEngagement(EngagementDefinition{Kind: "onboarding", Prompt: "Join us"})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := encodeEngagementMetadata(&definition)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeEngagementMetadata(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Code != definition.Code || decoded.ID != definition.ID || !decoded.Named {
		t.Fatalf("onboarding identity changed across metadata: %#v -> %#v", definition, decoded)
	}
}

func TestExpandedEngagementKindsNormalize(t *testing.T) {
	tests := []EngagementDefinition{
		{Kind: "dual"},
		{Kind: "quiz", Questions: []EngagementQuestion{{Prompt: "Two plus two?", Options: []string{"3", "4"}, Correct: 1}}},
		{Kind: "truefalse", Prompt: "The sky is blue"},
		{Kind: "onboarding"},
		{Kind: "match", Zones: []string{"A", "B"}, Cards: []string{"One"}},
		{Kind: "questions"},
		{Kind: "wall"},
		{Kind: "draw"},
		{Kind: "introduction"},
		{Kind: "pair"},
		{Kind: "expertise"},
		{Kind: "cards"},
		{Kind: "impostor"},
	}
	for _, input := range tests {
		normalized, err := normalizeEngagement(input)
		if err != nil {
			t.Fatalf("normalize %s: %v", input.Kind, err)
		}
		if normalized.ID == "" || (normalized.Named && input.Kind != "onboarding" && input.Kind != "cards" && input.Kind != "pair" && input.Kind != "draw" && input.Kind != "introduction") {
			t.Fatalf("normalized %s = %#v", input.Kind, normalized)
		}
	}
	dual, _ := normalizeEngagement(EngagementDefinition{Kind: "dual"})
	if !reflect.DeepEqual(dual.Options, []string{"What worked?", "What could improve?"}) {
		t.Fatalf("dual defaults = %#v", dual.Options)
	}
	truth, _ := normalizeEngagement(EngagementDefinition{Kind: "truefalse"})
	if !reflect.DeepEqual(truth.Options, []string{"Fact", "Fiction"}) {
		t.Fatalf("fact or fiction defaults = %#v", truth.Options)
	}
	falseAnswer, err := normalizeEngagement(EngagementDefinition{Kind: "truefalse", Correct: 1})
	if err != nil || falseAnswer.Correct != 1 || len(falseAnswer.Questions) != 1 || falseAnswer.Questions[0].Correct != 1 {
		t.Fatalf("fact or fiction answer = %#v, %v", falseAnswer, err)
	}
	multipleTruth, err := normalizeEngagement(EngagementDefinition{Kind: "truefalse", Questions: []EngagementQuestion{
		{Prompt: "One", Options: []string{"Fact", "Fiction"}, Correct: 0},
		{Prompt: "Two", Options: []string{"Fact", "Fiction"}, Correct: 1},
	}})
	if err != nil || len(multipleTruth.Questions) != 2 {
		t.Fatalf("multi-question fact or fiction = %#v, %v", multipleTruth, err)
	}
	tooManyTruths := make([]EngagementQuestion, 6)
	for index := range tooManyTruths {
		tooManyTruths[index] = EngagementQuestion{Prompt: "Statement", Options: []string{"Fact", "Fiction"}, Correct: index % 2}
	}
	if _, err := normalizeEngagement(EngagementDefinition{Kind: "truefalse", Questions: tooManyTruths}); err == nil {
		t.Fatal("fact or fiction accepted more than five questions")
	}
	onboarding, err := normalizeEngagement(EngagementDefinition{Kind: "onboarding"})
	if err != nil || onboarding.Code == "" || !activityCodeRE.MatchString(onboarding.Code) || !onboarding.Named {
		t.Fatalf("onboarding session = %#v, %v", onboarding, err)
	}
	reopened, err := normalizeEngagement(onboarding)
	if err != nil || reopened.Code != onboarding.Code {
		t.Fatalf("onboarding code did not persist: %#v -> %#v (%v)", onboarding, reopened, err)
	}
	wall, _ := normalizeEngagement(EngagementDefinition{Kind: "wall"})
	if !reflect.DeepEqual(wall.Options, []string{"What went well", "What could be better", "What is your key takeaway"}) {
		t.Fatalf("feedback wall defaults = %#v", wall.Options)
	}
	pair, _ := normalizeEngagement(EngagementDefinition{Kind: "pair"})
	cards, _ := normalizeEngagement(EngagementDefinition{Kind: "cards"})
	impostor, _ := normalizeEngagement(EngagementDefinition{Kind: "impostor"})
	if pair.GroupSize != 2 || cards.GroupCount != 4 || cards.GroupSize != 0 {
		t.Fatalf("group defaults = pair size %d cards count %d size %d", pair.GroupSize, cards.GroupCount, cards.GroupSize)
	}
	if !cards.Named {
		t.Fatal("playing cards must always reveal participant names")
	}
	if cards.JoinSeconds != 120 || cards.TimerSeconds != 0 {
		t.Fatalf("playing card timer defaults = %#v", cards)
	}
	if impostor.ImpostorCount != 1 || impostor.JoinSeconds != 120 || impostor.TimerSeconds != 0 || impostor.Named {
		t.Fatalf("impostor defaults = %#v", impostor)
	}
	if !pair.Named || pair.JoinSeconds != 120 || pair.DiscussionSeconds != 300 || pair.TimerSeconds != 0 {
		t.Fatalf("pair share defaults = %#v", pair)
	}
	draw, _ := normalizeEngagement(EngagementDefinition{Kind: "draw"})
	if !draw.Named {
		t.Fatal("quick draw must always reveal participant names")
	}
	introduction, _ := normalizeEngagement(EngagementDefinition{Kind: "introduction"})
	if !introduction.Named {
		t.Fatal("introduction must always reveal participant names")
	}
}

func TestExpandedActivitiesArePresentInWebRuntime(t *testing.T) {
	script := exportHTMLSuffix()
	for _, required := range []string{
		"Dual response", "Fact or Fiction", "Mix & Match", "Feedback Wall",
		"Onboarding", "Draw yourself", "Introduction", "Pair Share", "Expertise Map", "Playing Cards", "Impostor",
		"drawing:Array.from({length:20}", "for(let y=0;y<20;y++)", "definition.kind === 'introduction'", "'Aces','Kings','Queens','Jacks'", "'3s','2s'",
		"function distributePlayingCards(runtime)", "Math.floor(identities.length / groupCount) < 2", "dealPlayingCards(keynopeEngagementRuntime)",
		"function engagementVoteSummary(definition,items)", "runtime.entryResponses.push({id:event.id,identity:event.identity,displayName,idea})",
		"function toggleEngagementTimer(runtime)", "Resume timer", "Pause timer",
		"function revealImpostorRoles(runtime)", "roleAssignments", "Reveal roles",
		"RSA-OAEP", "sendImpostorRole(runtime,identity", "ciphertext:engagementBase64URL(ciphertext)",
		"keynopeDrawingEmpty", "keynopeDecodeDrawingCell", "keynopeIntroductionAssets", "keynopeIntroductionAvatarElement",
		"keynopeIntroductionDarkerColor", "keynopeIntroductionDarkerSkinTone", "keynopeIntroductionBrighterSkinTone", "keynopeIntroductionInteriorRows", "keynopeIntroductionEyeInteriorRows", "keynopeIntroductionGlyphBackgroundRows",
		"keynopeIntroductionAuthoredColor", "keynopeIntroductionAuthoredLayer", "asset.colorRoles", "asset.backgroundColorRoles",
		"category.id==='eyes'&&role==='#ffaaff'", "return keynopeIntroductionDarkerSkinTone(avatar.colors.head)",
		"colors.eyes=keynopeIntroductionDarkerSkinTone(colors.head)", "colors.nose=keynopeIntroductionDarkerSkinTone(colors.head)",
		"!['hair','facial_hair','nose','eyes','mouth','glasses'].includes(category.id)",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("expanded activity runtime is missing %q", required)
		}
	}
}

func TestIntroductionAssetsAreEmbeddedAndPlacedInPortrait(t *testing.T) {
	if introductionAssets.Width != 24 || introductionAssets.Height != 18 || len(introductionAssets.Palette) != 16 {
		t.Fatalf("introduction asset canvas = %dx%d with %d colours", introductionAssets.Width, introductionAssets.Height, len(introductionAssets.Palette))
	}
	if len(introductionAssets.Categories) != 8 {
		t.Fatalf("introduction categories = %d, want 8", len(introductionAssets.Categories))
	}
	beardBottom := 0
	matchedEditedFace := false
	facialHairUsesBlocks := false
	facialHairOnlyUsesBlocks := true
	layers := map[string]int{}
	defaultColors := map[string]string{}
	var authoredEye introductionAsset
	eyebrowRows := map[string]int{}
	beardRows := map[string]int{}
	glassColumns := map[string]int{}
	for _, category := range introductionAssets.Categories {
		layers[category.ID] = category.Layer
		defaultColors[category.ID] = category.DefaultColor
		if len(category.Assets) != 16 {
			t.Fatalf("%s assets = %d, want 16", category.ID, len(category.Assets))
		}
		for _, asset := range category.Assets {
			if category.ID == "glasses" {
				glassColumns[asset.ID] = asset.X
			}
			if category.ID == "facial_hair" {
				beardRows[asset.ID] = asset.Y
			}
			if category.ID == "eyebrows" {
				eyebrowRows[asset.ID] = asset.Y
			}
			if category.ID == "eyes" && asset.ID == "01" {
				authoredEye = asset
			}
			if category.ID == "head" && asset.ID == "01" && asset.X == 2 && asset.Y == 2 && len(asset.Rows) == 13 {
				matchedEditedFace = true
			}
			if asset.X < 0 || asset.Y < 0 || asset.Y+len(asset.Rows) > introductionAssets.Height {
				t.Fatalf("%s/%s is outside portrait at %d,%d", category.ID, asset.ID, asset.X, asset.Y)
			}
			hasInk := false
			for _, row := range asset.Rows {
				hasInk = hasInk || strings.TrimSpace(row) != ""
				if category.ID == "head" {
					glyphs := []rune(row)
					left, right := -1, -1
					for index, glyph := range glyphs {
						if glyph != ' ' {
							if left < 0 {
								left = index
							}
							right = index
						}
					}
					for index := left + 1; index < right; index++ {
						if glyphs[index] == ' ' {
							t.Fatalf("head/%s has an unfilled interior on row %q", asset.ID, row)
						}
					}
				}
				if category.ID == "facial_hair" {
					for _, glyph := range row {
						if glyph == ' ' {
							continue
						}
						if glyph >= '\u2580' && glyph <= '\u259f' {
							facialHairUsesBlocks = true
						} else {
							facialHairOnlyUsesBlocks = false
						}
					}
				}
				if asset.X+len([]rune(row)) > introductionAssets.Width {
					t.Fatalf("%s/%s row is outside portrait", category.ID, asset.ID)
				}
			}
			if category.ID == "nose" && !hasInk {
				t.Fatalf("nose/%s disappeared during compact resampling", asset.ID)
			}
			if category.ID == "facial_hair" {
				beardBottom = max(beardBottom, asset.Y+len(asset.Rows))
			}
		}
	}
	if !matchedEditedFace || beardBottom != introductionAssets.Height || !facialHairUsesBlocks || !facialHairOnlyUsesBlocks {
		t.Fatalf("compiled character layout was not applied: editedFace=%v beardBottom=%d blockHair=%v onlyBlocks=%v", matchedEditedFace, beardBottom, facialHairUsesBlocks, facialHairOnlyUsesBlocks)
	}
	if layers["facial_hair"] >= layers["nose"] {
		t.Fatalf("nose must render after facial hair: facial layer %d, nose layer %d", layers["facial_hair"], layers["nose"])
	}
	for id, layer := range layers {
		if id != "hair" && layer >= layers["hair"] {
			t.Fatalf("hair must render last: %s layer %d, hair layer %d", id, layer, layers["hair"])
		}
	}
	wantDefaultColors := map[string]string{"head": "#d9977b", "hair": "#6b422c", "facial_hair": "#6b422c", "nose": "#8f5f45", "eyebrows": "#6b422c", "eyes": "#8f5f45", "mouth": "#a64032", "glasses": "#454b55"}
	if !reflect.DeepEqual(defaultColors, wantDefaultColors) {
		t.Fatalf("Introduction default colours = %#v, want %#v", defaultColors, wantDefaultColors)
	}
	wantEyeRows := []string{"▗▆▖    ▗▆▖", "▝▆▘    ▝▆▘"}
	if authoredEye.Width != 10 || authoredEye.Height != 2 || authoredEye.X != 7 || authoredEye.Y != 7 || !reflect.DeepEqual(authoredEye.Rows, wantEyeRows) {
		t.Fatalf("authored eye 01 was not compiled faithfully: %#v", authoredEye)
	}
	if authoredEye.ColorRoles["0,0"] != "face-dark" || authoredEye.BackgroundColorRoles["1,1"] != "#000000" {
		t.Fatalf("authored eye 01 colour roles were not compiled faithfully: fg=%#v bg=%#v", authoredEye.ColorRoles, authoredEye.BackgroundColorRoles)
	}
	wantEyebrowRows := map[string]int{"01": 6, "02": 5, "03": 6, "04": 5, "05": 5, "06": 5, "07": 6, "08": 6, "09": 5, "10": 5, "11": 5, "12": 5, "13": 5, "14": 5, "15": 6, "16": 6}
	if !reflect.DeepEqual(eyebrowRows, wantEyebrowRows) {
		t.Fatalf("Introduction eyebrow rows = %#v, want %#v", eyebrowRows, wantEyebrowRows)
	}
	wantMovedBeardRows := map[string]int{"04": 10, "06": 11, "07": 9, "08": 8, "09": 14, "10": 12, "13": 11, "15": 10, "16": 11}
	for id, want := range wantMovedBeardRows {
		if beardRows[id] != want {
			t.Fatalf("Introduction beard %s row = %d, want %d", id, beardRows[id], want)
		}
	}
	if glassColumns["09"] != 3 {
		t.Fatalf("Introduction glasses 09 column = %d, want 3", glassColumns["09"])
	}
	for _, category := range introductionAssets.Categories {
		if category.ID != "facial_hair" {
			continue
		}
		for _, asset := range category.Assets {
			if asset.ID == "05" || asset.ID == "12" || asset.ID == "14" {
				if asset.Y != 10 || asset.Height != 7 || len(asset.Rows) != 7 || strings.TrimSpace(asset.Rows[5]) == "" || strings.TrimSpace(asset.Rows[6]) == "" {
					t.Fatalf("Introduction goatee %s must remain below the mouth at rows 15–16: %#v", asset.ID, asset)
				}
			}
			if asset.ID == "09" && (asset.Width != 14 || asset.Height != 4 || asset.X != 5 || !reflect.DeepEqual(asset.Rows, []string{"▜████████████▛", "██████████████", " ▝▀████████▛▛", "   ▝▀▀▜█▀▀▘"})) {
				t.Fatalf("Introduction beard 09 was not regenerated at its centered 14-column size: %#v", asset)
			}
		}
	}
}

func TestActivityCodesAreCasePreservingAlphanumeric(t *testing.T) {
	for iteration := 0; iteration < 1000; iteration++ {
		code, err := randomActivityString(8)
		if err != nil {
			t.Fatal(err)
		}
		if !activityCodeRE.MatchString(code) || strings.ContainsAny(code, "-_") {
			t.Fatalf("generated non-alphanumeric activity code %q", code)
		}
	}
	legacy := EngagementDefinition{Kind: "storm", Code: "-old_Cod"}
	if err := ensureActivityIdentity(&legacy); err != nil {
		t.Fatal(err)
	}
	if !activityCodeRE.MatchString(legacy.Code) || legacy.Code == "-old_Cod" {
		t.Fatalf("legacy activity code was not migrated: %q", legacy.Code)
	}
}

func TestWebEditorCanHostActivityRoom(t *testing.T) {
	script := exportHTMLSuffix()
	if !strings.Contains(script, "keynopeEngagementControllerSurface || !window.KEYNOPE_PRESENTER") {
		t.Fatal("non-presenter surfaces, including the web editor, do not host activity rooms")
	}
	if strings.Contains(script, "keynopeHostedEngagementControllerSurface = keynopeEngagementControllerSurface || (!window.KEYNOPE_PRESENTER && !window.KEYNOPE_WEB_EDITOR)") {
		t.Fatal("web editor is explicitly excluded from hosting activity rooms")
	}
	for _, required := range []string{"keynopeEngagementSessions = new Map()", "renderActivityMarker()", "loadEngagementQRCode(runtime)", "canvasLinkAtPointer(e)"} {
		if !strings.Contains(script, required) {
			t.Fatalf("presentation runtime is missing %q", required)
		}
	}
	if strings.Contains(script, "autoOpenEngagementForCurrentPage") {
		t.Fatal("activities still auto-open instead of waiting for their slide marker")
	}
}

func TestDeckSerializesEngagementWithoutRuntimeAnswers(t *testing.T) {
	previousWidth, previousHeight := authoredTerminalWidth, authoredTerminalHeight
	authoredTerminalWidth, authoredTerminalHeight = 80, 25
	t.Cleanup(func() { authoredTerminalWidth, authoredTerminalHeight = previousWidth, previousHeight })

	deck := Deck{Slides: []Slide{{
		Elements:   []Element{{Kind: "heading", Level: 1, Text: "Confidence"}},
		Engagement: &EngagementDefinition{Kind: "pulse", Code: "Ab12Cd34", Prompt: "How ready are we?", Options: []string{"0", "1", "2", "3", "4", "5"}},
	}}}
	data, err := serializeDeck("Untitled.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("keynope-engagement version=1")) {
		t.Fatalf("serialized deck has no engagement metadata:\n%s", data)
	}
	parsed, err := parseDeckData("Untitled.md", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Slides) != 1 || parsed.Slides[0].Engagement == nil {
		t.Fatalf("parsed engagement missing: %#v", parsed.Slides)
	}
	if got := parsed.Slides[0].Engagement; got.Kind != "pulse" || got.Prompt != "How ready are we?" || len(got.Options) != 6 {
		t.Fatalf("parsed engagement = %#v", got)
	}
	if parsed.Slides[0].Engagement.Code != "" {
		t.Fatalf("session code leaked into Markdown: %#v", parsed.Slides[0].Engagement)
	}
}

func TestNativeEditorEngagementUndo(t *testing.T) {
	session := newNativeEditorSession("Untitled.md", Deck{Slides: []Slide{{}}}, true)
	definition := EngagementDefinition{Kind: "storm", Prompt: "What are we missing?"}
	if err := session.apply(nativeEditorAction{Action: "set-engagement", EngagementData: &definition}); err != nil {
		t.Fatal(err)
	}
	if got := session.state().Slides[0].Engagement; got == nil || got.Kind != "storm" {
		t.Fatalf("engagement was not applied: %#v", got)
	}
	if err := session.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if got := session.state().Slides[0].Engagement; got != nil {
		t.Fatalf("undo retained engagement: %#v", got)
	}
}

func TestNativeEditorAttachesActivityToCurrentSlide(t *testing.T) {
	session := newNativeEditorSession("Untitled.md", Deck{Slides: []Slide{{Background: "aurora", BackgroundSet: true, BG: "48;2;20;30;40", BGSet: true, Elements: []Element{{Kind: "text", Text: "Before"}}}, {Elements: []Element{{Kind: "text", Text: "After"}}}}}, true)
	definition := EngagementDefinition{Kind: "pulse", Prompt: "How are we?", Options: []string{"Good", "Great"}, TimerSeconds: 90}
	if err := session.apply(nativeEditorAction{Action: "set-engagement", EngagementData: &definition}); err != nil {
		t.Fatal(err)
	}
	state := session.state()
	if state.Current != 0 || len(state.Slides) != 2 {
		t.Fatalf("activity attachment state = current %d, %d slides", state.Current, len(state.Slides))
	}
	activity := state.Slides[0]
	if activity.Engagement == nil || activity.Engagement.Code != "" || activity.Engagement.ID == "" || activity.Engagement.TimerSeconds != 90 {
		t.Fatalf("attached activity = %#v", activity.Engagement)
	}
	if activity.Background != "aurora" || !activity.BackgroundSet || activity.BG != "48;2;20;30;40" || !activity.BGSet {
		t.Fatalf("activity did not retain the chosen slide appearance: %#v", activity)
	}
	if got := state.Slides[1].Elements[0].Text; got != "After" {
		t.Fatalf("following slide moved incorrectly: %q", got)
	}
}

func TestActivityQRCodeUsesBlocksAndOneModuleQuietZone(t *testing.T) {
	qrRows := strings.Split(activityQRCodeText("https://keynope.sh/join/Ab12Cd34"), "\n")
	if rendered := strings.Join(qrRows, "\n"); strings.ContainsRune(rendered, '?') || !strings.ContainsRune(rendered, '█') {
		t.Fatalf("activity QR was passed through the text fallback renderer: %q", rendered)
	}
	if len(qrRows) < 25 || maxLineDisplayWidth(qrRows) < 50 {
		t.Fatalf("activity QR was not enlarged: %d rows by %d columns", len(qrRows), maxLineDisplayWidth(qrRows))
	}
	if strings.TrimSpace(qrRows[0]) != "" || strings.TrimSpace(qrRows[len(qrRows)-1]) != "" ||
		strings.TrimSpace(qrRows[1]) == "" || strings.TrimSpace(qrRows[len(qrRows)-2]) == "" {
		t.Fatalf("activity QR quiet zone is not exactly one module: %#v", qrRows)
	}
}

func TestActivityQRCodeEndpoint(t *testing.T) {
	session := newNativeEditorSession("Untitled.md", Deck{Slides: []Slide{{}}}, true)
	request := httptest.NewRequest(http.MethodGet, "/api/editor/activity-qr?value=https%3A%2F%2Fkeynope.sh%2Fjoin%2FAb12Cd34", nil)
	response := httptest.NewRecorder()
	session.handleActivityQR(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "█") {
		t.Fatalf("QR endpoint status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestActivityMasterRequiredElementsCannotBeDeleted(t *testing.T) {
	masters := defaultMasterDeck()
	masters.Layouts = append(masters.Layouts, defaultActivityMaster())
	session := newNativeEditorSession("Untitled.md", Deck{Slides: []Slide{{}}, Masters: masters}, true)
	if err := session.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
		t.Fatal(err)
	}
	state := session.state()
	activityIndex := -1
	for index, layout := range state.Masters.Layouts {
		if layout.ID == activityLayoutID {
			activityIndex = index + 1
		}
	}
	if activityIndex < 1 {
		t.Fatal("activity master missing")
	}
	if err := session.apply(nativeEditorAction{Action: "select-slide", Slide: activityIndex}); err != nil {
		t.Fatal(err)
	}
	if err := session.apply(nativeEditorAction{Action: "delete-element", Element: 0}); err != nil {
		t.Fatal(err)
	}
	state = session.state()
	if got := len(state.Masters.Layouts[activityIndex-1].Slide.Elements); got != 3 {
		t.Fatalf("activity master has %d elements after deleting protected title", got)
	}
}

func TestNormalizeEngagementRejectsIncompleteSort(t *testing.T) {
	_, err := normalizeEngagement(EngagementDefinition{Kind: "sort", Zones: []string{"Only"}, Cards: []string{"Card"}})
	if err == nil {
		t.Fatal("expected incomplete sort to be rejected")
	}
}

func TestResolvedLayoutSlideCarriesEngagement(t *testing.T) {
	deck := Deck{
		Slides:  []Slide{{LayoutID: "blank", Engagement: &EngagementDefinition{Kind: "storm", Prompt: "Ideas?"}}},
		Masters: defaultMasterDeck(),
	}
	resolved := deck.ResolveSlide(0, false)
	if resolved.Engagement == nil || resolved.Engagement.Prompt != "Ideas?" {
		t.Fatalf("resolved slide lost engagement: %#v", resolved.Engagement)
	}
	resolved.Engagement.Prompt = "Changed"
	if deck.Slides[0].Engagement.Prompt != "Ideas?" {
		t.Fatal("resolved slide shares engagement storage with authored slide")
	}
}

func TestPresenterEngagementStateIsTransientAndSynchronized(t *testing.T) {
	companion := &presenterCompanion{}
	runtime := EngagementRuntimeState{
		AppearanceMode: "modern",
		Definition:     EngagementDefinition{Kind: "pulse", Prompt: "Ready?", Options: []string{"No", "Yes"}},
		Slide:          2,
		Phase:          3,
		Counts:         []int{1, 4},
	}
	payload, err := json.Marshal(runtime)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/engagement", bytes.NewReader(payload))
	response := httptest.NewRecorder()
	companion.handleEngagement(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if companion.state.Engagement == nil || companion.state.Engagement.Counts[1] != 4 || companion.state.Version != 1 {
		t.Fatalf("presenter state = %#v", companion.state)
	}
	if companion.state.Engagement.AppearanceMode != "modern" || cloneEngagementRuntime(companion.state.Engagement).AppearanceMode != "modern" {
		t.Fatal("activity appearance lost in native presenter handoff")
	}

	clearRequest := httptest.NewRequest(http.MethodPost, "/engagement", bytes.NewBufferString("null"))
	clearResponse := httptest.NewRecorder()
	companion.handleEngagement(clearResponse, clearRequest)
	if clearResponse.Code != http.StatusNoContent || companion.state.Engagement != nil {
		t.Fatalf("engagement was not cleared: status=%d state=%#v", clearResponse.Code, companion.state.Engagement)
	}
}
