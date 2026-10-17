package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func appearanceComment(payload string) string {
	return "<!-- keynope-appearance base64:" + base64.StdEncoding.EncodeToString([]byte(payload)) + " -->"
}

func TestAppearanceAmbiguousFieldsAndProfiles(t *testing.T) {
	for _, payload := range []string{
		`{"version":1,"version":1,"mode":"retro"}`,
		`{"version":1,"VERSION":1,"mode":"retro"}`,
		`{"version":1,"mode":"retro","mo\u0064e":"modern"}`,
		`{"version":1,"mode":"retro","profiles":{"modern":{"textScale":0.5},"modern":{"textScale":1}}}`,
		`{"version":1,"mode":"retro","profiles":{"modern":{"textScale":0.5,"TEXTSCALE":1}}}`,
		`{"version":1,"mode":"retro","profiles":{"modern":{"textScale":3}}}`,
		`{"version":1,"mode":"retro","profiles":{"modern":{"textScale":"0.5"}}}`,
		`{"version":1,"mode":"retro","profiles":null}`,
	} {
		if _, _, err := decodeDeckAppearance(appearanceComment(payload)); err == nil {
			t.Errorf("accepted ambiguous/invalid appearance %s", payload)
		}
	}
	before := DeckAppearance{Version: 3, Extra: map[string]json.RawMessage{"keep": json.RawMessage(`true`)}}
	receiver := *before.Clone()
	if err := json.Unmarshal([]byte(`{"version":1,"version":2,"mode":"retro"}`), &receiver); err == nil || !reflect.DeepEqual(receiver, before) {
		t.Fatal("failed appearance decode replaced existing receiver", err)
	}
}

func TestAppearanceEnvelopeOwnershipAndRecovery(t *testing.T) {
	valid := appearanceComment(`{"version":1,"mode":"modern"}`)
	for _, extra := range []string{"<!-- keynope-appearance broken -->", "<!-- keynope-appearance=broken -->", valid} {
		input := valid + "\n" + extra + "\n# Kept\n"
		if _, remaining, err := decodeDeckAppearance(input); err == nil || remaining != input {
			t.Error("accepted or consumed ambiguous appearance envelope", err)
		}
		a, remaining, diagnostics := recoverDeckAppearance(input)
		if a != nil || len(diagnostics) != 1 || strings.Contains(remaining, "keynope-appearance") || !strings.Contains(remaining, "# Kept") {
			t.Fatal("optional recovery lost content or retained ambiguous metadata", remaining, diagnostics)
		}
	}
	unknown := "<!-- keynope-appearance-extension future=true -->\n"
	if a, remaining, diagnostics := recoverDeckAppearance(unknown + "# Kept"); a != nil || remaining != unknown+"# Kept" || len(diagnostics) != 0 {
		t.Fatal("unowned appearance namespace was consumed", remaining, diagnostics)
	}
	fenced := "```\n" + valid + "\n<!-- keynope-appearance broken -->\n```\n"
	if a, remaining, diagnostics := recoverDeckAppearance(fenced); a != nil || remaining != fenced || len(diagnostics) != 0 {
		t.Fatal("literal code treated as appearance", diagnostics)
	}
	// Future nested objects stay opaque, even when their internal keys repeat.
	payload := `{"version":1,"mode":"retro","profiles":{"modern":{"textScale":0.75,"future":{"x":1,"x":2}},"future":{"x":1,"x":2}},"future":{"x":1,"x":2}}`
	a, _, err := decodeDeckAppearance(appearanceComment(payload))
	if err != nil || (Deck{Appearance: a}).appearanceTextScale("modern") != .75 {
		t.Fatal("known scale or opaque nested data lost", err)
	}
	encoded, err := encodeDeckAppearance(a)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := decodeDeckAppearance(encoded)
	if err != nil || !reflect.DeepEqual(a, b) {
		t.Fatal("future appearance data did not round trip", err)
	}
	scale := .9
	changed, err := (Deck{Appearance: a}).withAppearanceTextScale("modern", &scale)
	if err != nil || !reflect.DeepEqual(a, b) || (Deck{Appearance: changed}).appearanceTextScale("modern") != scale || !strings.Contains(string(changed.Extra["profiles"]), `"x":1,"x":2`) {
		t.Fatal("profile mutation lost unknown data or mutated source", err)
	}
	alias, _, err := decodeDeckAppearance(appearanceComment(`{"VERSION":1,"MODE":"retro","PROFILES":{"MODERN":{"TEXTSCALE":0.6}}}`))
	if err != nil || (Deck{Appearance: alias}).appearanceTextScale("modern") != .6 {
		t.Fatal("single known aliases are not interpreted consistently", err)
	}
}

func TestSpeakerNotesAdmissionAndScope(t *testing.T) {
	comment := func(text string) string {
		return "<!-- notes=base64:" + base64.StdEncoding.EncodeToString([]byte(text)) + " -->"
	}
	for _, notes := range []string{comment("One") + "\n" + comment("Two"), "<!-- notes=base64:%%%% -->", "<!-- notes=base64:YQ= -->", "<!-- notes -->", comment(string([]byte{0xff})), comment(strings.Repeat("x", (1<<20)+1))} {
		if _, err := parseEditorDeckData("notes.md", []byte("<!-- keynope width=245 height=56 -->\n# Kept\n"+notes)); err == nil {
			t.Errorf("invalid/duplicate notes accepted: %.70s", notes)
		}
	}
	for _, notes := range []string{"", "Notes\nwith Unicode 🦀", strings.Repeat("n", 1<<20)} {
		input := comment(notes) + "\n# One\n---\n" + comment(notes) + "\n# Two"
		d, err := parseDeckData("notes.md", []byte(input))
		if err != nil || len(d.Slides) != 2 || d.Slides[0].Notes != notes || d.Slides[1].Notes != notes {
			t.Fatal("valid notes/distinct owners failed", len(notes), err)
		}
	}
	literal := "<!-- notes=base64:%%%% -->\n" + comment("One") + "\n" + comment("Two")
	d, err := parseDeckData("code.md", []byte("```\n"+literal+"\n```"))
	if err != nil || len(d.Slides) != 1 || d.Slides[0].Elements[0].Text != literal || d.Slides[0].Notes != "" {
		t.Fatal("literal notes parsed as metadata", err)
	}
}

func TestSpeakerNotesMutationAndSaveBoundaries(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, master := range []bool{false, true} {
		t.Run(fmt.Sprint(master), func(t *testing.T) {
			s := structuredBudgetSession(t, master)
			before, version, mutation := cloneDeck(s.deck), s.version, s.mutationRevision
			for _, text := range []string{strings.Repeat("x", (1<<20)+1), string([]byte{0xff})} {
				if err := s.apply(nativeEditorAction{Action: "update-slide-notes", Slide: 0, Notes: text}); err == nil || !reflect.DeepEqual(before, s.deck) || s.version != version || s.mutationRevision != mutation || len(s.undo) != 0 {
					t.Fatal("invalid notes mutated session/history", err)
				}
				bad := cloneDeck(before)
				if master {
					bad.Masters.Base.Slide.Notes = text
				} else {
					bad.Slides[0].Notes = text
				}
				if _, err := serializeDeck("notes.md", bad); err == nil {
					t.Fatal("saved unloadable notes")
				}
			}
			request, _ := json.Marshal(nativeEditorAction{Action: "update-slide-notes", Slide: 0, Notes: strings.Repeat("x", (1<<20)+1)})
			response := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/api/editor/action", bytes.NewReader(request))
			r.Header.Set("Accept", "application/problem+json")
			s.handleAction(response, r)
			var problem editorProblem
			if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil || response.Code != 400 || problem.Code != "invalid-value" || problem.Field != "notes" || s.version != version || !reflect.DeepEqual(before, s.deck) {
				t.Fatal("notes wire admission lost atomic/readable failure", response.Code, problem, err)
			}
			if err := s.apply(nativeEditorAction{Action: "update-slide-notes", Slide: 0, Notes: "Valid 🦀 notes"}); err != nil {
				t.Fatal(err)
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
				t.Fatal("notes Undo lost source", err)
			}
		})
	}
}

func TestSpeakerNotesMasterArchiveAndRejectedRuntime(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, layout := range []bool{false, true} {
		masters := MasterDeck{Version: masterDeckVersion, Base: MasterLayout{ID: "base", Name: "Base"}}
		owner := &masters.Base.Slide
		if layout {
			masters.Layouts = []MasterLayout{{ID: "named", Name: "Named"}}
			owner = &masters.Layouts[0].Slide
		}
		owner.Notes = strings.Repeat("n", (1<<20)+1)
		payload, err := json.Marshal(masters)
		if err != nil {
			t.Fatal(err)
		}
		input := "<!-- keynope width=100 height=80 -->\n<!-- keynope-masters version=1 base64:" + base64.StdEncoding.EncodeToString(payload) + " -->\n# Kept"
		authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged = 132, 41, true
		if _, err := parseEditorDeckData("notes.md", []byte(input)); err == nil || authoredTerminalWidth != 132 || authoredTerminalHeight != 41 || !parsedDeckElementOrderChanged {
			t.Fatal("invalid archived notes published candidate runtime", err)
		}
	}
}
