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
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
	Page int    `json:"page,omitempty"`
}

var deckTabsRE = regexp.MustCompile(`(?m)^<!--\s*keynope-tabs version=1 base64:([A-Za-z0-9+/=]+)\s*-->\s*`)

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
