package main

import _ "embed"

// Shared by the native presenter, exported decks and browser participant UI.
//
//go:embed web/activity-games.js
var activityGamesJS string

//go:embed web/activity-design.js
var activityDesignJS string
