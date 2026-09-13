package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Page is a one-based authored slide number, not a transient overflow page.
type DeckTab struct {
	SlideTab bool   `json:"slideTab,omitempty"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url,omitempty"`
	Page     int    `json:"page,omitempty"`
}

var deckTabsRE = regexp.MustCompile(`(?m)^<!--\s*keynope-tabs version=1 base64:([A-Za-z0-9+/=]+)\s*-->\s*`)
var slideTabRE = regexp.MustCompile(`<!--\s*keynope-tab=([a-zA-Z0-9_-]{1,80})\s*-->`)

// Tab slides retain their slot in Slides. Their separate display order lives
// in Tabs, so turning the toggle off restores the original slide position.
func syncSlideTabs(deck *Deck) {
	positions := map[string]int{}
	for i := range deck.Slides {
		id := deck.Slides[i].TabID
		if id == "" {
			continue
		}
		if _, exists := positions[id]; exists {
			id = newStableID("tab")
			deck.Slides[i].TabID = id
		}
		positions[id] = i + 1
	}
	var tabs []DeckTab
	for _, tab := range deck.Tabs {
		if tab.SlideTab {
			page, ok := positions[tab.ID]
			if !ok {
				continue
			}
			tab.Page, tab.URL = page, ""
			delete(positions, tab.ID)
		}
		tabs = append(tabs, tab)
	}
	for i, slide := range deck.Slides {
		if _, missing := positions[slide.TabID]; !missing {
			continue
		}
		name := fmt.Sprintf("Tab %d", i+1)
		for _, element := range slide.Elements {
			if text := strings.TrimSpace(element.Text); text != "" {
				name = strings.SplitN(text, "\n", 2)[0]
				for len(name) > 80 {
					name = string([]rune(name)[:len([]rune(name))-1])
				}
				break
			}
		}
		tabs = append(tabs, DeckTab{ID: slide.TabID, Name: name, Page: i + 1, SlideTab: true})
	}
	deck.Tabs = tabs
}

func nextPresentationSlide(slides []Slide, current, delta int) int {
	for i := current + delta; i >= 0 && i < len(slides); i += delta {
		if slides[i].TabID == "" {
			return i
		}
	}
	return current
}

// Keep existing manually configured page tabs pointing to the same slide
// after insertion, deletion or reordering. Managed tabs follow TabID instead.
func remapTabPages(deck *Deck, mapping func(int) int) {
	tabs := deck.Tabs[:0]
	for _, tab := range deck.Tabs {
		if tab.Page > 0 && !tab.SlideTab {
			tab.Page = mapping(tab.Page-1) + 1
			if tab.Page <= 0 {
				continue
			}
		}
		tabs = append(tabs, tab)
	}
	deck.Tabs = tabs
}

func validateDeckTabs(tabs []DeckTab) error {
	if len(tabs) > 20 {
		return fmt.Errorf("use at most 20 participant tabs")
	}
	ids := map[string]bool{}
	for _, tab := range tabs {
		if strings.TrimSpace(tab.Name) == "" || len(tab.Name) > 80 || tab.ID == "" || len(tab.ID) > 80 || ids[tab.ID] {
			return fmt.Errorf("each tab needs a unique ID and a name (up to 80 characters)")
		}
		ids[tab.ID] = true
		if tab.URL != "" {
			u, err := url.Parse(tab.URL)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || tab.Page != 0 || len(tab.URL) > 4096 {
				return fmt.Errorf("choose an HTTP(S) URL or a page, not both")
			}
		} else if tab.Page < 1 {
			return fmt.Errorf("choose a page number")
		}
	}
	return nil
}

func decodeDeckTabs(text string) ([]DeckTab, string, error) {
	m := deckTabsRE.FindStringSubmatch(text)
	if m == nil {
		return nil, text, nil
	}
	data, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		return nil, text, err
	}
	var tabs []DeckTab
	if err = json.Unmarshal(data, &tabs); err != nil {
		return nil, text, err
	}
	if err = validateDeckTabs(tabs); err != nil {
		return nil, text, err
	}
	return tabs, deckTabsRE.ReplaceAllString(text, ""), nil
}

func deckHasActivities(deck Deck) bool {
	for _, slide := range deck.Slides {
		if slide.Engagement != nil {
			return true
		}
	}
	return false
}
