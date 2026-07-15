package main

import (
	"bytes"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/image/webp"
)

type Element struct {
	Kind            string `json:"kind,omitempty"`
	Level           int    `json:"level,omitempty"`
	Text            string `json:"text,omitempty"`
	Path            string `json:"path,omitempty"`
	Query           string `json:"query,omitempty"`
	Placeholder     bool   `json:"placeholder,omitempty"`
	ID              string `json:"id,omitempty"`
	SlotID          string `json:"slotId,omitempty"`
	PlaceholderRole string `json:"placeholderRole,omitempty"`
	MasterSlotID    string `json:"masterSlotId,omitempty"`
	Inherited       bool   `json:"-"`
}

type Slide struct {
	Elements      []Element `json:"elements,omitempty"`
	Effect        string    `json:"effect,omitempty"`
	Background    string    `json:"background,omitempty"`
	FG            string    `json:"fg,omitempty"`
	BG            string    `json:"bg,omitempty"`
	HeaderFG      string    `json:"headerFg,omitempty"`
	Notes         string    `json:"notes,omitempty"`
	LayoutID      string    `json:"layoutId,omitempty"`
	EffectSet     bool      `json:"effectSet,omitempty"`
	BackgroundSet bool      `json:"backgroundSet,omitempty"`
	FGSet         bool      `json:"fgSet,omitempty"`
	BGSet         bool      `json:"bgSet,omitempty"`
	HeaderFGSet   bool      `json:"headerFgSet,omitempty"`
	PageNumber    string    `json:"pageNumber,omitempty"`
}

type Line struct {
	Text    string
	Role    string
	Query   string
	Row     int
	Col     int
	Element int
}

type KeyEvent struct {
	Action string `json:"action"`
	Text   string `json:"text,omitempty"`
	X      int    `json:"x,omitempty"`
	Y      int    `json:"y,omitempty"`
	Button int    `json:"button,omitempty"`
}

type EditState struct {
	Selected       int
	LastSelected   int
	MultiSelected  map[int]bool
	Cursor         map[int]int
	Clipboard      *Element
	Undo           []SlideSnapshot
	Redo           []SlideSnapshot
	DeckUndo       []DeckSnapshot
	DeckRedo       []DeckSnapshot
	ShowNotes      bool
	ShowSlides     bool
	NotesCursor    int
	SlideNavIndex  int
	SlideNavScroll int
	TimerMode      string
	TimerInput     string
	TimerDeadline  time.Time
}

type SlideSnapshot struct {
	Before Slide
	After  Slide
}

type DeckSnapshot struct {
	Before        []Slide
	After         []Slide
	BeforeMasters MasterDeck
	AfterMasters  MasterDeck
	HasMasters    bool
	BeforeIndex   int
	AfterIndex    int
}

type ViewState struct {
	Chrome         bool
	SlideIndex     int
	SlideCount     int
	Page           int
	PageCount      int
	Slides         []Slide
	ShowNotes      bool
	ShowSlides     bool
	SlideNavIndex  int
	SlideNavScroll int
	TimerMode      string
	TimerInput     string
	TimerDeadline  time.Time
}

type exportDeck struct {
	Cols   int          `json:"cols"`
	Rows   int          `json:"rows"`
	Pages  []exportPage `json:"pages"`
	Source string       `json:"source"`
}

type exportPage struct {
	Slide                int                  `json:"slide"`
	Page                 int                  `json:"page"`
	PageCount            int                  `json:"pageCount"`
	SlideCount           int                  `json:"slideCount"`
	Effect               string               `json:"effect"`
	Background           string               `json:"background"`
	BackgroundLines      []exportLine         `json:"backgroundLines,omitempty"`
	Transparency         []exportLine         `json:"transparency,omitempty"`
	ContentFrames        []exportContentFrame `json:"contentFrames,omitempty"`
	FG                   string               `json:"fg"`
	BG                   string               `json:"bg"`
	HeaderFG             string               `json:"headerFg"`
	Lines                []exportLine         `json:"lines"`
	HideChromePageNumber bool                 `json:"hideChromePageNumber,omitempty"`
}

type exportFrame struct {
	Lines []exportLine `json:"lines"`
}

type exportContentFrame struct {
	Full   bool         `json:"full,omitempty"`
	Lines  []exportLine `json:"lines,omitempty"`
	Clear  []string     `json:"clear,omitempty"`
	Update []exportLine `json:"update,omitempty"`
}

type exportLine struct {
	Row     int          `json:"row"`
	Col     int          `json:"col"`
	Element int          `json:"element"`
	Role    string       `json:"role"`
	Link    string       `json:"link,omitempty"`
	Parts   []exportPart `json:"parts"`
}

type exportPart struct {
	Col        int    `json:"col"`
	Text       string `json:"text"`
	Color      string `json:"color,omitempty"`
	Background string `json:"background,omitempty"`
}

type appArgs struct {
	ExportOnly bool
	Classic    bool
	AppMode    bool
	DeckPath   string
	Startup    bool
}

type presenterState struct {
	Slide       int    `json:"slide"`
	Page        int    `json:"page"`
	Presenting  bool   `json:"presenting"`
	Version     int64  `json:"version"`
	DeckVersion int64  `json:"deckVersion"`
	DeckSlide   int    `json:"deckSlide"`
	TimerMode   string `json:"timerMode,omitempty"`
	TimerInput  string `json:"timerInput,omitempty"`
	TimerEndMS  int64  `json:"timerEndMs,omitempty"`
}

type presenterTerminalFrame struct {
	Version int64        `json:"version"`
	Cols    int          `json:"cols"`
	Rows    int          `json:"rows"`
	Lines   []exportLine `json:"lines"`
	ANSI    string       `json:"ansi,omitempty"`
}

type presenterCompanion struct {
	server  *http.Server
	cmd     *exec.Cmd
	url     string
	html    string
	pages   map[int][]exportPage
	frame   presenterTerminalFrame
	mu      sync.RWMutex
	state   presenterState
	target  string
	helper  bool
	seq     int64
	fullSeq int64
	frames  map[chan presenterTerminalFrame]<-chan struct{}
	token   string
}

var activePresenter *presenterCompanion
var presenterModeActive bool
var nativeAppModeActive bool
var nativeInputMu sync.Mutex
var nativeInputBuffer bytes.Buffer

func inputRead(buffer []byte) (int, error) {
	if nativeAppModeActive {
		nativeInputMu.Lock()
		defer nativeInputMu.Unlock()
		return nativeInputBuffer.Read(buffer)
	}
	return os.Stdin.Read(buffer)
}

func startNativeInputReader() {
	go func() {
		buffer := make([]byte, 64*1024)
		for {
			n, err := os.Stdin.Read(buffer)
			if n > 0 {
				nativeInputMu.Lock()
				_, _ = nativeInputBuffer.Write(buffer[:n])
				nativeInputMu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
}

var queuedEditEvent *KeyEvent
var queuedMainMouseEvent *KeyEvent
var remoteKeyEvents = make(chan KeyEvent, 64)
var queuedTimerText string
var activeUINotice uiNotice
var uiNoticeMu sync.Mutex
var exportImageAnimationPosition *time.Duration
var exportImageAnimationMu sync.Mutex
var authoredTerminalWidth int
var authoredTerminalHeight int

func init() {
	c64QuadFont['·'] = []string{"", " ▗▖", " ▝▘", ""}
	c64FullFont['·'] = []string{
		"        ",
		"        ",
		"  ████  ",
		"  ████  ",
		"  ████  ",
		"  ████  ",
		"        ",
		"        ",
	}
}

func main() {
	args, ok := parseArgs(os.Args[1:])
	if !ok {
		fmt.Fprintln(os.Stderr, "usage: keynope [--export] [--classic] [--app] [deck.md]")
		os.Exit(2)
	}
	if args.Startup {
		path, err := startupDeckPath()
		if errors.Is(err, errStartupCancelled) {
			return
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		args.DeckPath = path
	}

	deck, err := parseDeck(args.DeckPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(deck.Slides) == 0 {
		fmt.Fprintln(os.Stderr, "deck has no slides")
		os.Exit(1)
	}
	resolvedSlides := deck.ResolvedSlides()
	cleanUnusedImageCache(resolvedSlides)
	width, height := terminalSize()
	presenterMode := !args.ExportOnly && !args.Classic
	presenterModeActive = presenterMode
	nativeAppModeActive = args.AppMode
	if args.AppMode {
		startNativeInputReader()
	}
	if args.ExportOnly {
		width, height = exportRenderSize(resolvedSlides)
	} else if presenterMode {
		width, height = authoredRenderSize(width, height)
	}
	prewarmImageCache(resolvedSlides, width, height)
	if args.ExportOnly {
		if err := exportHTML(args.DeckPath, resolvedSlides, width, height); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	var presenter *presenterCompanion
	var nativeEditor *nativeEditorSession
	if args.AppMode {
		nativeEditor = newNativeEditorSession(args.DeckPath, deck)
		activeNativeEditor = nativeEditor
		defer func() { activeNativeEditor = nil }()
	}
	if presenterMode {
		presenter, err = startPresenterCompanion(args.DeckPath, resolvedSlides, width, height, !args.AppMode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "presenter companion unavailable: %v\n", err)
		} else {
			activePresenter = presenter
			defer func() { activePresenter = nil }()
			defer presenter.Close()
		}
	}
	if nativeEditor != nil {
		if presenter == nil {
			fmt.Fprintln(os.Stderr, "native editor server unavailable")
			os.Exit(1)
		}
		nativeEditor.mu.Lock()
		nativeEditor.companion = presenter
		nativeEditor.mu.Unlock()
		presenter.Update(0, 0, false, nil)
		select {}
	}

	restore := func() {}
	if !args.AppMode {
		restore, err = rawTerminal()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	defer restore()
	if !args.AppMode {
		termPrint("\033[?25l\033[?1049h\033[?1000h\033[?1002h\033[?1006h\033[?2004h")
		defer termPrint("\033[0m\033[?2004l\033[?1006l\033[?1002l\033[?1000l\033[?25h\033[?1049l")
	}

	current := 0
	page := 0
	chromeVisible := true
	editStates := map[int]*EditState{}
	lastSearchQuery := ""
	for {
		resolvedSlides = deck.ResolvedSlides()
		displaySlides := resolvedSlides
		if chromeVisible {
			displaySlides = deck.ResolvedSlidesForEditing()
		}
		width, height := terminalSize()
		if presenterMode {
			width, height = authoredRenderSize(width, height)
		}
		if current < 0 {
			current = 0
		}
		if current >= len(deck.Slides) {
			current = len(deck.Slides) - 1
		}
		page = clampPage(displaySlides[current], width, height, page)
		deckState := editState(editStates, -1)
		if deckState.SlideNavIndex < 0 || deckState.SlideNavIndex >= len(deck.Slides) {
			deckState.SlideNavIndex = current
		}
		view := ViewState{
			Chrome:         chromeVisible,
			SlideIndex:     current,
			SlideCount:     len(deck.Slides),
			Page:           page,
			PageCount:      slidePageCount(displaySlides[current], width, height),
			Slides:         displaySlides,
			ShowNotes:      deckState.ShowNotes,
			ShowSlides:     deckState.ShowSlides,
			SlideNavIndex:  deckState.SlideNavIndex,
			SlideNavScroll: deckState.SlideNavScroll,
			TimerMode:      deckState.TimerMode,
			TimerInput:     deckState.TimerInput,
			TimerDeadline:  deckState.TimerDeadline,
		}
		if presenter != nil {
			presenter.Update(current, page, !chromeVisible, deckState)
		}

		if displaySlides[current].Effect != "" {
			action := playEffect(displaySlides[current], width, height, page, view)
			if action == "quit" {
				if chromeVisible {
					return
				}
				action = "controls"
			}
			var result string
			current, page, chromeVisible, result = handleAction(action, &deck, current, page, width, height, args.DeckPath, editStates, &lastSearchQuery, chromeVisible)
			if result == "quit" {
				return
			}
			continue
		}

		if slideHasAnimatedImage(displaySlides[current]) {
			action := playAnimatedSlide(displaySlides[current], width, height, page, view)
			if action == "quit" {
				if chromeVisible {
					return
				}
				action = "controls"
			}
			var result string
			current, page, chromeVisible, result = handleAction(action, &deck, current, page, width, height, args.DeckPath, editStates, &lastSearchQuery, chromeVisible)
			if result == "quit" {
				return
			}
		} else {
			action := playStaticSlide(displaySlides[current], width, height, page, view)
			if action == "quit" {
				if chromeVisible {
					return
				}
				action = "controls"
			}
			var result string
			current, page, chromeVisible, result = handleAction(action, &deck, current, page, width, height, args.DeckPath, editStates, &lastSearchQuery, chromeVisible)
			if result == "quit" {
				return
			}
		}
	}
}

func parseArgs(raw []string) (appArgs, bool) {
	var args appArgs
	if len(raw) == 0 {
		args.Startup = true
		return args, true
	}
	for _, value := range raw {
		switch value {
		case "--export":
			args.ExportOnly = true
		case "--classic":
			args.Classic = true
		case "--app":
			args.AppMode = true
		default:
			if strings.HasPrefix(value, "-") || args.DeckPath != "" {
				return appArgs{}, false
			}
			args.DeckPath = value
		}
	}
	if args.DeckPath == "" || (args.ExportOnly && args.Classic) || (args.AppMode && (args.ExportOnly || args.Classic)) {
		return appArgs{}, false
	}
	return args, true
}

var errStartupCancelled = errors.New("startup cancelled")

func startupDeckPath() (string, error) {
	restore, err := rawTerminal()
	if err != nil {
		return "", err
	}
	termPrint("\033[?1049h\033[?25l")
	defer func() {
		termPrint("\033[0m\033[?25h\033[?1049l")
		restore()
	}()
	for {
		choice, err := startupMenu("Keynope", "Start with a new deck or open an existing Markdown deck.", []startupMenuItem{
			{Title: "New", Detail: "Create a new .md deck in the current directory."},
			{Title: "Open", Detail: "Open an existing .md or .markdown deck by path."},
			{Title: "Cancel", Detail: "Exit without opening a deck."},
		})
		if err != nil {
			return "", err
		}
		switch choice {
		case 0:
			name, err := startupTextInput("New deck", "Deck name", "deck")
			if err != nil {
				return "", err
			}
			path, err := promptNewDeckPath(name)
			if errors.Is(err, errStartupCancelled) {
				continue
			}
			return path, err
		case 1:
			termPrint("\033[0m\033[?25h\033[?1049l")
			restore()
			path, err := openDeckWithSystemDialog()
			if rawRestore, rawErr := rawTerminal(); rawErr == nil {
				restore = rawRestore
				termPrint("\033[?1049h\033[?25l")
			} else if err == nil {
				return "", rawErr
			}
			if err != nil {
				return "", err
			}
			if err := validateOpenDeckPath(path); err != nil {
				startupMessage("Open deck", err.Error())
				continue
			}
			return path, nil
		default:
			return "", errStartupCancelled
		}
	}
}

func openDeckWithSystemDialog() (string, error) {
	if runtime.GOOS != "darwin" {
		return startupTextInput("Open deck", "Path to .md deck", "")
	}
	script := `POSIX path of (choose file with prompt "Open Keynope deck" of type {"net.daringfireball.markdown", "public.markdown", "md", "markdown"})`
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return "", errStartupCancelled
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", errStartupCancelled
	}
	return path, nil
}

func promptNewDeckPath(name string) (string, error) {
	path := deckPathFromName(name)
	if path == "" {
		return "", fmt.Errorf("deck name cannot be empty")
	}
	if _, err := os.Stat(path); err == nil {
		overwrite, err := confirmOverwriteDeck(path)
		if err != nil {
			return "", err
		}
		if !overwrite {
			return "", errStartupCancelled
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return createStarterDeck(path)
}

func validateOpenDeckPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("path cannot be empty")
	}
	if !isMarkdownDeckPath(path) {
		return fmt.Errorf("deck must be a .md or .markdown file")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	return nil
}

func isMarkdownDeckPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md" || ext == ".markdown"
}

func deckPathFromName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if filepath.Ext(name) == "" {
		name += ".md"
	}
	return filepath.Clean(name)
}

func confirmOverwriteDeck(path string) (bool, error) {
	choice, err := startupMenu("Overwrite deck?", path+" already exists.", []startupMenuItem{
		{Title: "Cancel", Detail: "Keep the existing deck."},
		{Title: "Overwrite", Detail: "Replace it with a new starter deck."},
	})
	if err != nil {
		return false, errStartupCancelled
	}
	return choice == 1, nil
}

type startupMenuItem struct {
	Title  string
	Detail string
}

func startupMenu(title, subtitle string, items []startupMenuItem) (int, error) {
	if len(items) == 0 {
		return 0, errStartupCancelled
	}
	selected := 0
	for {
		drawStartupMenu(title, subtitle, items, selected)
		event := readStartupKeyEvent()
		switch event.Action {
		case "up":
			selected = (selected + len(items) - 1) % len(items)
		case "down", "tab":
			selected = (selected + 1) % len(items)
		case "enter":
			return selected, nil
		case "escape", "quit":
			return 0, errStartupCancelled
		}
	}
}

func drawStartupMenu(title, subtitle string, items []startupMenuItem, selected int) {
	width, height := terminalSize()
	boxW := min(max(48, width/2), max(30, width-4))
	boxH := min(height-2, 7+len(items)*2)
	x := max(0, (width-boxW)/2)
	y := max(0, (height-boxH)/2)
	clearStartupScreen()
	drawStartupBox(x, y, boxW, boxH)
	termPrintf("\033[1;37m\033[%d;%dH%s", y+2, x+3, crop(title, max(0, boxW-4)))
	if subtitle != "" {
		termPrintf("\033[0;90m\033[%d;%dH%s", y+3, x+3, crop(subtitle, max(0, boxW-4)))
	}
	row := y + 5
	for i, item := range items {
		prefix := "  "
		mode := "\033[0;37m"
		if i == selected {
			prefix = "> "
			mode = "\033[7;37m"
		}
		line := prefix + item.Title
		termPrintf("%s\033[%d;%dH%s\033[0m", mode, row, x+3, padRight(crop(line, max(0, boxW-6)), max(0, boxW-6)))
		if item.Detail != "" && row+1 < y+boxH {
			termPrintf("\033[0;90m\033[%d;%dH%s", row+1, x+5, crop(item.Detail, max(0, boxW-8)))
		}
		row += 2
	}
	hint := "up/down select  enter confirm  esc cancel"
	termPrintf("\033[0;90m\033[%d;%dH%s", y+boxH-1, x+3, crop(hint, max(0, boxW-4)))
}

func startupTextInput(title, label, initial string) (string, error) {
	input := initial
	cursor := len([]rune(input))
	for {
		drawStartupTextInput(title, label, input, cursor)
		event := readStartupKeyEvent()
		runes := []rune(input)
		switch event.Action {
		case "enter":
			return strings.TrimSpace(input), nil
		case "escape", "quit":
			return "", errStartupCancelled
		case "left":
			cursor = max(0, cursor-1)
		case "right":
			cursor = min(len(runes), cursor+1)
		case "backspace":
			if cursor > 0 {
				input = string(append(runes[:cursor-1], runes[cursor:]...))
				cursor--
			}
		case "text":
			text := []rune(event.Text)
			next := append([]rune{}, runes[:cursor]...)
			next = append(next, text...)
			next = append(next, runes[cursor:]...)
			input = string(next)
			cursor += len(text)
		}
	}
}

func drawStartupTextInput(title, label, input string, cursor int) {
	width, height := terminalSize()
	boxW := min(max(54, width/2), max(32, width-4))
	boxH := 9
	x := max(0, (width-boxW)/2)
	y := max(0, (height-boxH)/2)
	clearStartupScreen()
	drawStartupBox(x, y, boxW, boxH)
	termPrintf("\033[1;37m\033[%d;%dH%s", y+2, x+3, crop(title, max(0, boxW-4)))
	termPrintf("\033[0;90m\033[%d;%dH%s", y+4, x+3, crop(label, max(0, boxW-4)))
	fieldW := max(1, boxW-6)
	display := crop(input, fieldW)
	termPrintf("\033[0;37;40m\033[%d;%dH%s", y+5, x+3, padRight(display, fieldW))
	cursorCol := x + 3 + min(cursor, fieldW-1)
	termPrintf("\033[0;37m\033[%d;%dH_", y+6, cursorCol)
	hint := "type path/name  enter confirm  esc cancel"
	termPrintf("\033[0;90m\033[%d;%dH%s", y+boxH-1, x+3, crop(hint, max(0, boxW-4)))
}

func startupMessage(title, message string) {
	for {
		drawStartupMessage(title, message)
		event := readStartupKeyEvent()
		if event.Action == "enter" || event.Action == "escape" || event.Action == "quit" {
			return
		}
	}
}

func drawStartupMessage(title, message string) {
	width, height := terminalSize()
	boxW := min(max(54, width/2), max(32, width-4))
	boxH := 8
	x := max(0, (width-boxW)/2)
	y := max(0, (height-boxH)/2)
	clearStartupScreen()
	drawStartupBox(x, y, boxW, boxH)
	termPrintf("\033[1;37m\033[%d;%dH%s", y+2, x+3, crop(title, max(0, boxW-4)))
	termPrintf("\033[0;37m\033[%d;%dH%s", y+4, x+3, crop(message, max(0, boxW-4)))
	termPrintf("\033[0;90m\033[%d;%dH%s", y+boxH-1, x+3, crop("enter/esc return", max(0, boxW-4)))
}

func clearStartupScreen() {
	termPrint("\033[0m\033[2J\033[H")
}

func drawStartupBox(x, y, w, h int) {
	if w < 4 || h < 3 {
		return
	}
	top := "+" + strings.Repeat("-", w-2) + "+"
	mid := "|" + strings.Repeat(" ", w-2) + "|"
	termPrintf("\033[0;37m\033[%d;%dH%s", y+1, x+1, top)
	for row := 1; row < h-1; row++ {
		termPrintf("\033[0;37m\033[%d;%dH%s", y+row+1, x+1, mid)
	}
	termPrintf("\033[0;37m\033[%d;%dH%s", y+h, x+1, top)
}

func readStartupKeyEvent() KeyEvent {
	var buf [64]byte
	for {
		n, _ := inputRead(buf[:])
		if n == 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		b := buf[:n]
		switch {
		case bytes.Equal(b, []byte{3}):
			return KeyEvent{Action: "quit"}
		case bytes.Equal(b, []byte{27}):
			return KeyEvent{Action: "escape"}
		case bytes.Contains(b, []byte{27, '[', 'A'}):
			return KeyEvent{Action: "up"}
		case bytes.Contains(b, []byte{27, '[', 'B'}):
			return KeyEvent{Action: "down"}
		case bytes.Contains(b, []byte{27, '[', 'D'}):
			return KeyEvent{Action: "left"}
		case bytes.Contains(b, []byte{27, '[', 'C'}):
			return KeyEvent{Action: "right"}
		case bytes.Contains(b, []byte{'\r'}) || bytes.Contains(b, []byte{'\n'}):
			return KeyEvent{Action: "enter"}
		case bytes.Contains(b, []byte{9}):
			return KeyEvent{Action: "tab"}
		case bytes.Contains(b, []byte{127}) || bytes.Contains(b, []byte{8}):
			return KeyEvent{Action: "backspace"}
		default:
			text := string(b)
			if !utf8.ValidString(text) {
				return KeyEvent{}
			}
			var out []rune
			for _, r := range text {
				if r >= 32 && r != 127 {
					out = append(out, r)
				}
			}
			if len(out) > 0 {
				return KeyEvent{Action: "text", Text: string(out)}
			}
			return KeyEvent{}
		}
	}
}

func createStarterDeck(path string) (string, error) {
	width, height := terminalSize()
	if width <= 0 || height <= 0 {
		width, height = 80, 25
	}
	content := starterDeckMarkdown(width, height)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func starterDeckMarkdown(width, height int) string {
	charsPerLine := max(24, min(72, (width-8)/4))
	reservedRows := 18
	lineCount := max(2, min(6, (height-reservedRows)/4))
	lines := loremLines(charsPerLine, lineCount)
	var out strings.Builder
	fmt.Fprintf(&out, "<!-- keynope width=%d height=%d -->\n\n", width, height)
	if metadata, err := encodeMasterDeckMetadata(defaultMasterDeck()); err == nil {
		out.WriteString(metadata)
		out.WriteString("\n\n")
	}
	out.WriteString("<!-- layout=title-subtitle -->\n\n")
	out.WriteString("<!-- master-slot=title-subtitle-title -->\n")
	out.WriteString("# Title\n\n")
	out.WriteString("<!-- master-slot=title-subtitle-subtitle -->\n")
	out.WriteString("## Subtitle\n\n")
	for _, line := range lines {
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}

func loremLines(charsPerLine, lineCount int) []string {
	words := strings.Fields("Lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt ut labore et dolore magna aliqua Ut enim ad minim veniam quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat")
	var lines []string
	var line strings.Builder
	for len(lines) < lineCount {
		for _, word := range words {
			if line.Len() == 0 {
				line.WriteString(word)
				continue
			}
			if line.Len()+1+len(word) > charsPerLine {
				lines = append(lines, line.String())
				line.Reset()
				if len(lines) >= lineCount {
					return lines
				}
			}
			if line.Len() > 0 {
				line.WriteByte(' ')
			}
			line.WriteString(word)
		}
		if line.Len() > 0 {
			lines = append(lines, line.String())
			line.Reset()
		}
	}
	return lines[:lineCount]
}

func inferExportSize(slides []Slide) (int, int) {
	width, height := 160, 45
	for _, slide := range slides {
		for _, element := range slide.Elements {
			placement := parseImagePlacement(element.Query)
			if placement.left != nil {
				width = max(width, *placement.left+80)
			}
			if placement.right != nil {
				width = max(width, 160+*placement.right)
			}
			if placement.top != nil {
				height = max(height, *placement.top+20)
			}
			if placement.bottom != nil {
				height = max(height, 45+*placement.bottom)
			}
		}
	}
	return width, height
}

func parseJumpSlideResult(result string, slideCount int) (int, bool) {
	if !strings.HasPrefix(result, "jump-slide:") {
		return 0, false
	}
	target, err := strconv.Atoi(strings.TrimPrefix(result, "jump-slide:"))
	if err != nil || target < 0 || target >= slideCount {
		return 0, false
	}
	return target, true
}

func handleAction(action string, deck *Deck, current, page, width, height int, deckPath string, editStates map[int]*EditState, lastSearchQuery *string, chromeVisible bool) (int, int, bool, string) {
	slides := &deck.Slides
	resolvedSlides := deck.ResolvedSlides()
	deckState := editState(editStates, -1)
	runEditMode := func(event *KeyEvent) string {
		if event != nil {
			queuedEditEvent = event
		}
		state := editState(editStates, current)
		state.Clipboard = deckState.Clipboard
		state.ShowNotes = deckState.ShowNotes
		state.TimerMode = deckState.TimerMode
		state.TimerInput = deckState.TimerInput
		state.TimerDeadline = deckState.TimerDeadline
		if deckState.ShowNotes && event != nil && (event.Action == "tab" || event.Action == "enter") {
			state.NotesCursor = len([]rune((*slides)[current].Notes))
		}
		result := playEditMode(deck, current, width, height, page, deckPath, state, editModeOptions{})
		deckState.Clipboard = state.Clipboard
		deckState.ShowNotes = state.ShowNotes
		deckState.TimerMode = state.TimerMode
		deckState.TimerInput = state.TimerInput
		deckState.TimerDeadline = state.TimerDeadline
		return result
	}
	if deckState.TimerMode != "" && action == "controls" {
		deckState.TimerMode = ""
		deckState.TimerInput = ""
		deckState.TimerDeadline = time.Time{}
		return current, page, chromeVisible, ""
	}
	if deckState.ShowSlides {
		switch action {
		case "slide-list", "controls":
			deckState.ShowSlides = false
			return current, page, chromeVisible, ""
		case "up":
			deckState.SlideNavIndex = max(0, deckState.SlideNavIndex-1)
			return current, page, chromeVisible, ""
		case "down":
			deckState.SlideNavIndex = min(len(*slides)-1, deckState.SlideNavIndex+1)
			return current, page, chromeVisible, ""
		case "enter":
			target := max(0, min(len(*slides)-1, deckState.SlideNavIndex))
			return target, 0, chromeVisible, ""
		case "mouse-click":
			event := queuedMainMouseEvent
			queuedMainMouseEvent = nil
			if event != nil {
				if target, ok := slideNavigatorIndexAtPoint(event.X, event.Y, deckState.SlideNavScroll, len(*slides), width, height); ok {
					deckState.SlideNavIndex = target
				}
			}
			return current, page, chromeVisible, ""
		}
	}
	if deckState.TimerMode != "" {
		switch action {
		case "controls":
			deckState.TimerMode = ""
			deckState.TimerInput = ""
			deckState.TimerDeadline = time.Time{}
			return current, page, chromeVisible, ""
		case "timer":
			if deckState.TimerMode == "config" {
				if len(deckState.TimerInput) < 4 {
					deckState.TimerInput += "0"
				}
				return current, page, chromeVisible, ""
			}
			deckState.TimerMode = "config"
			deckState.TimerInput = ""
			deckState.TimerDeadline = time.Time{}
			return current, page, chromeVisible, ""
		}
		if deckState.TimerMode == "config" {
			switch action {
			case "slide-list":
				if len(deckState.TimerInput) < 4 {
					deckState.TimerInput += "1"
				}
				return current, page, chromeVisible, ""
			case "speaker-notes":
				if len(deckState.TimerInput) < 4 {
					deckState.TimerInput += "2"
				}
				return current, page, chromeVisible, ""
			case "backspace":
				if deckState.TimerInput != "" {
					deckState.TimerInput = deckState.TimerInput[:len(deckState.TimerInput)-1]
				}
				return current, page, chromeVisible, ""
			case "enter":
				if duration := timerInputDuration(deckState.TimerInput); duration > 0 {
					deckState.TimerMode = "running"
					deckState.TimerDeadline = time.Now().Add(duration)
				}
				return current, page, chromeVisible, ""
			case "text":
				if len(deckState.TimerInput) < 4 {
					for _, r := range queuedTimerText {
						if r >= '0' && r <= '9' && len(deckState.TimerInput) < 4 {
							deckState.TimerInput += string(r)
						}
					}
					queuedTimerText = ""
				}
				return current, page, chromeVisible, ""
			}
		}
	}
	if deckState.ShowNotes {
		switch action {
		case "controls":
			deckState.ShowNotes = false
			return current, page, chromeVisible, ""
		case "speaker-notes":
			deckState.ShowNotes = false
			return current, page, chromeVisible, ""
		}
		if chromeVisible {
			switch action {
			case "tab", "enter":
				event := KeyEvent{Action: action}
				result := runEditMode(&event)
				if result == "quit" {
					return current, page, chromeVisible, "quit"
				}
				if target, ok := parseJumpSlideResult(result, len(*slides)); ok {
					return target, 0, chromeVisible, ""
				}
				return current, page, chromeVisible, ""
			case "mouse-click":
				event := queuedMainMouseEvent
				if event != nil {
					if _, ok := notesCursorAtPoint((*slides)[current].Notes, event.X, event.Y, width, height); ok {
						result := runEditMode(event)
						queuedMainMouseEvent = nil
						if result == "quit" {
							return current, page, chromeVisible, "quit"
						}
						return current, page, chromeVisible, ""
					}
				}
			}
		}
	}
	switch action {
	case "controls":
		return current, page, true, ""
	case "present":
		return current, page, false, ""
	case "shortcuts":
		event := playShortcutHelp("Main shortcuts", mainShortcutHelp(), resolvedSlides[current], page, width, height, readKeyEvent)
		if shortcutHelpDismissed(event) {
			return current, page, chromeVisible, ""
		}
		return handleAction(event.Action, deck, current, page, width, height, deckPath, editStates, lastSearchQuery, chromeVisible)
	case "mouse-click":
		event := queuedMainMouseEvent
		queuedMainMouseEvent = nil
		if event == nil {
			return current, page, chromeVisible, ""
		}
		if chromeVisible {
			result := runEditMode(event)
			if result == "quit" {
				return current, page, chromeVisible, "quit"
			}
			if target, ok := parseJumpSlideResult(result, len(*slides)); ok {
				return target, 0, chromeVisible, ""
			}
			if result == "next-slide" {
				return min(current+1, len(*slides)-1), 0, chromeVisible, ""
			}
			if result == "delete-slide" {
				return min(current, len(*slides)-1), 0, chromeVisible, ""
			}
			return current, page, chromeVisible, ""
		}
		lines := displayLines(resolvedSlides[current], width, height, page)
		if target, ok := linkAtPoint(resolvedSlides[current], lines, event.X, event.Y, len(*slides)); ok {
			if target.URL != "" {
				_ = openElementLink(target)
				return current, page, chromeVisible, ""
			}
			if target.Slide >= 0 && target.Slide < len(*slides) {
				return target.Slide, 0, chromeVisible, ""
			}
		}
		return current, page, chromeVisible, ""
	case "undo":
		if chromeVisible {
			state := editState(editStates, current)
			working := deck.ResolveSlide(current, true)
			if undoSlideSnapshot(state, &working) {
				deck.StoreResolvedSlide(current, working)
				if persistDeck(deckPath, *deck) {
					setUINotice("Undone")
				}
				page = clampPage(deck.ResolveSlide(current, false), width, height, page)
				return current, page, chromeVisible, ""
			}
			deckState := editState(editStates, -1)
			if nextCurrent, ok := undoDeckSnapshot(deckState, deck); ok {
				if persistDeck(deckPath, *deck) {
					setUINotice("Undone")
					if activePresenter != nil {
						activePresenter.RefreshAllAsync(deckPath, deck.ResolvedSlides(), width, height)
					}
				}
				return nextCurrent, 0, chromeVisible, ""
			}
		}
		return current, page, chromeVisible, ""
	case "redo":
		if chromeVisible {
			state := editState(editStates, current)
			working := deck.ResolveSlide(current, true)
			if redoSlideSnapshot(state, &working) {
				deck.StoreResolvedSlide(current, working)
				if persistDeck(deckPath, *deck) {
					setUINotice("Redone")
				}
				page = clampPage(deck.ResolveSlide(current, false), width, height, page)
				return current, page, chromeVisible, ""
			}
			deckState := editState(editStates, -1)
			if nextCurrent, ok := redoDeckSnapshot(deckState, deck); ok {
				if persistDeck(deckPath, *deck) {
					setUINotice("Redone")
					if activePresenter != nil {
						activePresenter.RefreshAllAsync(deckPath, deck.ResolvedSlides(), width, height)
					}
				}
				return nextCurrent, 0, chromeVisible, ""
			}
		}
		return current, page, chromeVisible, ""
	case "edit":
		if chromeVisible {
			result := runEditMode(nil)
			if result == "quit" {
				return current, page, chromeVisible, "quit"
			}
			if target, ok := parseJumpSlideResult(result, len(*slides)); ok {
				return target, 0, chromeVisible, ""
			}
			if result == "next-slide" {
				return min(current+1, len(*slides)-1), 0, chromeVisible, ""
			}
			if result == "delete-slide" {
				return min(current, len(*slides)-1), 0, chromeVisible, ""
			}
		}
		return current, page, chromeVisible, ""
	case "insert-image":
		if chromeVisible {
			event := KeyEvent{Action: "insert-image"}
			result := runEditMode(&event)
			if result == "quit" {
				return current, page, chromeVisible, "quit"
			}
			if target, ok := parseJumpSlideResult(result, len(*slides)); ok {
				return target, 0, chromeVisible, ""
			}
			if result == "next-slide" {
				return min(current+1, len(*slides)-1), 0, chromeVisible, ""
			}
			if result == "delete-slide" {
				return min(current, len(*slides)-1), 0, chromeVisible, ""
			}
		}
		return current, page, chromeVisible, ""
	case "insert-text":
		if chromeVisible {
			event := KeyEvent{Action: "insert-text"}
			result := runEditMode(&event)
			if result == "quit" {
				return current, page, chromeVisible, "quit"
			}
			if target, ok := parseJumpSlideResult(result, len(*slides)); ok {
				return target, 0, chromeVisible, ""
			}
			if result == "next-slide" {
				return min(current+1, len(*slides)-1), 0, chromeVisible, ""
			}
			if result == "delete-slide" {
				return min(current, len(*slides)-1), 0, chromeVisible, ""
			}
		}
		return current, page, chromeVisible, ""
	case "shape-picker":
		if chromeVisible {
			event := KeyEvent{Action: "shape-picker"}
			result := runEditMode(&event)
			if result == "quit" {
				return current, page, chromeVisible, "quit"
			}
			if target, ok := parseJumpSlideResult(result, len(*slides)); ok {
				return target, 0, chromeVisible, ""
			}
			if result == "next-slide" {
				return min(current+1, len(*slides)-1), 0, chromeVisible, ""
			}
			if result == "delete-slide" {
				return min(current, len(*slides)-1), 0, chromeVisible, ""
			}
		}
		return current, page, chromeVisible, ""
	case "tab":
		if chromeVisible {
			event := KeyEvent{Action: "tab"}
			result := runEditMode(&event)
			if result == "quit" {
				return current, page, chromeVisible, "quit"
			}
			if target, ok := parseJumpSlideResult(result, len(*slides)); ok {
				return target, 0, chromeVisible, ""
			}
			if result == "next-slide" {
				return min(current+1, len(*slides)-1), 0, chromeVisible, ""
			}
			if result == "delete-slide" {
				return min(current, len(*slides)-1), 0, chromeVisible, ""
			}
		}
		return current, page, chromeVisible, ""
	case "shift-tab":
		if chromeVisible {
			event := KeyEvent{Action: "shift-tab"}
			result := runEditMode(&event)
			if result == "quit" {
				return current, page, chromeVisible, "quit"
			}
			if target, ok := parseJumpSlideResult(result, len(*slides)); ok {
				return target, 0, chromeVisible, ""
			}
			if result == "next-slide" {
				return min(current+1, len(*slides)-1), 0, chromeVisible, ""
			}
			if result == "delete-slide" {
				return min(current, len(*slides)-1), 0, chromeVisible, ""
			}
		}
		return current, page, chromeVisible, ""
	case "slide-list":
		deckState.ShowSlides = !deckState.ShowSlides
		deckState.ShowNotes = false
		deckState.SlideNavIndex = current
		deckState.SlideNavScroll = 0
		return current, page, chromeVisible, ""
	case "speaker-notes":
		deckState.ShowNotes = true
		deckState.ShowSlides = false
		return current, page, chromeVisible, ""
	case "timer":
		deckState.TimerMode = "config"
		deckState.TimerInput = ""
		deckState.TimerDeadline = time.Time{}
		deckState.ShowSlides = false
		return current, page, chromeVisible, ""
	case "text", "backspace", "enter":
		return current, page, chromeVisible, ""
	case "copy", "cut", "paste":
		if chromeVisible {
			event := KeyEvent{Action: action}
			result := runEditMode(&event)
			if result == "quit" {
				return current, page, chromeVisible, "quit"
			}
			if target, ok := parseJumpSlideResult(result, len(*slides)); ok {
				return target, 0, chromeVisible, ""
			}
			if result == "next-slide" {
				return min(current+1, len(*slides)-1), 0, chromeVisible, ""
			}
			if result == "delete-slide" {
				return min(current, len(*slides)-1), 0, chromeVisible, ""
			}
		}
		return current, page, chromeVisible, ""
	case "insert-slide":
		if chromeVisible {
			deck.EnsureDefaultMasters()
			layoutID, ok := playLayoutPicker(deck, (*slides)[current].LayoutID, width, height)
			if !ok {
				return current, page, chromeVisible, ""
			}
			before := cloneSlides(*slides)
			insertAt := current + 1
			*slides = append(*slides, Slide{})
			copy((*slides)[insertAt+1:], (*slides)[insertAt:])
			(*slides)[insertAt] = deck.NewSlideFromLayout(layoutID)
			commitDeckSnapshot(editState(editStates, -1), before, *slides, current, insertAt)
			if persistDeck(deckPath, *deck) {
				setUINotice("Slide added")
			}
			return insertAt, 0, chromeVisible, ""
		}
		return current, page, chromeVisible, ""
	case "layout-picker":
		if chromeVisible {
			deck.EnsureDefaultMasters()
			layoutID, ok := playLayoutPicker(deck, (*slides)[current].LayoutID, width, height)
			if !ok {
				return current, page, chromeVisible, ""
			}
			before := cloneSlides(*slides)
			if deck.RebindSlideLayout(current, layoutID) {
				commitDeckSnapshot(editState(editStates, -1), before, *slides, current, current)
				if persistDeck(deckPath, *deck) {
					setUINotice("Layout changed")
				}
			}
		}
		return current, 0, chromeVisible, ""
	case "master-view":
		if chromeVisible {
			result := playMasterView(deck, deckPath, width, height, editState(editStates, -1))
			if result.Quit {
				return current, page, chromeVisible, "quit"
			}
		}
		return current, page, chromeVisible, ""
	case "page-number":
		if chromeVisible {
			before := cloneSlides(*slides)
			(*slides)[current].PageNumber = nextPageNumberOverride((*slides)[current].PageNumber)
			commitDeckSnapshot(editState(editStates, -1), before, *slides, current, current)
			if persistDeck(deckPath, *deck) {
				setUINotice("Page number: " + pageNumberModeLabel((*slides)[current].PageNumber, true))
			}
		}
		return current, page, chromeVisible, ""
	case "visual-properties":
		if chromeVisible {
			source := cloneSlide((*slides)[current])
			resolve := func(candidate Slide) Slide {
				previewDeck := cloneDeck(*deck)
				previewDeck.Slides[current] = candidate
				return previewDeck.ResolveSlide(current, true)
			}
			if updated, ok := playVisualProperties(source, source.LayoutID != "", resolve, width, height); ok {
				before := cloneSlides(*slides)
				(*slides)[current] = updated
				commitDeckSnapshot(editState(editStates, -1), before, *slides, current, current)
				if persistDeck(deckPath, *deck) {
					setUINotice("Visual properties updated")
				}
			}
		}
		return current, page, chromeVisible, ""
	case "clone-slide":
		if chromeVisible {
			before := cloneSlides(*slides)
			insertAt := current + 1
			clone := cloneSlide((*slides)[current])
			*slides = append(*slides, Slide{})
			copy((*slides)[insertAt+1:], (*slides)[insertAt:])
			(*slides)[insertAt] = clone
			commitDeckSnapshot(editState(editStates, -1), before, *slides, current, insertAt)
			if persistDeck(deckPath, *deck) {
				setUINotice("Slide cloned")
			}
			return insertAt, 0, chromeVisible, ""
		}
		return current, page, chromeVisible, ""
	case "delete-slide":
		if chromeVisible {
			before := cloneSlides(*slides)
			nextCurrent := current
			if len(*slides) <= 1 {
				*slides = []Slide{placeholderSlide()}
				nextCurrent = 0
			} else {
				*slides = append((*slides)[:current], (*slides)[current+1:]...)
				nextCurrent = min(current, len(*slides)-1)
			}
			commitDeckSnapshot(editState(editStates, -1), before, *slides, current, nextCurrent)
			if persistDeck(deckPath, *deck) {
				setUINotice("Slide deleted")
			}
			return nextCurrent, 0, chromeVisible, ""
		}
		return current, page, chromeVisible, ""
	case "effect-picker":
		if chromeVisible {
			if effect, ok := playEffectPicker(resolvedSlides[current], width, height); ok {
				before := cloneSlides(*slides)
				if effect == "none" {
					(*slides)[current].Effect = ""
				} else {
					(*slides)[current].Effect = effect
				}
				(*slides)[current].EffectSet = true
				commitDeckSnapshot(editState(editStates, -1), before, *slides, current, current)
				if persistDeck(deckPath, *deck) {
					setUINotice("Effect updated")
				}
			}
		}
		return current, page, chromeVisible, ""
	case "background-picker":
		if chromeVisible {
			if background, ok := playBackgroundPicker(resolvedSlides[current], width, height); ok {
				before := cloneSlides(*slides)
				if background == "none" {
					(*slides)[current].Background = ""
				} else {
					(*slides)[current].Background = background
				}
				(*slides)[current].BackgroundSet = true
				commitDeckSnapshot(editState(editStates, -1), before, *slides, current, current)
				if persistDeck(deckPath, *deck) {
					setUINotice("Background updated")
				}
			}
		}
		return current, page, chromeVisible, ""
	case "search":
		current, page = playSearchMode(resolvedSlides, current, page, width, height, lastSearchQuery)
		return current, page, chromeVisible, ""
	case "jump":
		current, page = playJumpMode(resolvedSlides, current, page, width, height)
		return current, page, chromeVisible, ""
	case "export":
		exportHTMLWithNotice(deckPath, resolvedSlides, width, height)
		return current, page, chromeVisible, ""
	case "prev":
		if page > 0 {
			return current, page - 1, chromeVisible, ""
		}
		return current - 1, 0, chromeVisible, ""
	default:
		if hasNextPage(resolvedSlides[current], width, height, page) {
			return current, page + 1, chromeVisible, ""
		}
		return current + 1, 0, chromeVisible, ""
	}
}

func parseDeck(path string) (Deck, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Deck{}, err
	}
	text := string(data)
	authoredTerminalWidth = 0
	authoredTerminalHeight = 0
	if match := keynopeMetaRE.FindStringSubmatch(text); match != nil {
		authoredTerminalWidth, _ = strconv.Atoi(match[1])
		authoredTerminalHeight, _ = strconv.Atoi(match[2])
		text = keynopeMetaRE.ReplaceAllString(text, "")
	}
	masters, remaining, err := decodeMasterDeckMetadata(text)
	if err != nil {
		return Deck{}, err
	}
	text = remaining
	base := filepath.Dir(path)
	resolveMasterDeckImagePaths(&masters, base)
	var slides []Slide
	for _, part := range splitSlides(text) {
		slide := parseSlide(part, base)
		if len(slide.Elements) > 0 || slide.EffectSet || slide.BackgroundSet || slide.FGSet || slide.BGSet || slide.HeaderFGSet || slide.Effect != "" || slide.Background != "" || slide.FG != "" || slide.BG != "" || slide.HeaderFG != "" || slide.Notes != "" || slide.LayoutID != "" || slide.PageNumber != "" {
			slides = append(slides, slide)
		}
	}
	return Deck{Slides: slides, Masters: masters}, nil
}

func saveDeck(path string, deck Deck) error {
	if authoredTerminalWidth <= 0 || authoredTerminalHeight <= 0 {
		authoredTerminalWidth, authoredTerminalHeight = terminalSize()
	}
	var out strings.Builder
	if authoredTerminalWidth > 0 && authoredTerminalHeight > 0 {
		fmt.Fprintf(&out, "<!-- keynope width=%d height=%d -->\n\n", authoredTerminalWidth, authoredTerminalHeight)
	}
	if deckHasMasterData(deck.Masters) {
		metadata, err := encodeMasterDeckMetadataForPath(deck.Masters, path)
		if err != nil {
			return err
		}
		out.WriteString(metadata)
		out.WriteString("\n\n")
	}
	for slideIndex, slide := range deck.Slides {
		if slideIndex > 0 {
			out.WriteString("\n---\n")
		}
		if slide.LayoutID != "" {
			fmt.Fprintf(&out, "<!-- layout=%s -->\n", slide.LayoutID)
		}
		if slide.PageNumber != "" {
			fmt.Fprintf(&out, "<!-- page-number=%s -->\n", slide.PageNumber)
		}
		if slide.EffectSet || slide.Effect != "" {
			value := slide.Effect
			if value == "" {
				value = "none"
			}
			fmt.Fprintf(&out, "<!-- effect=%s -->\n", value)
		}
		if slide.BackgroundSet || slide.Background != "" {
			value := slide.Background
			if value == "" {
				value = "none"
			}
			fmt.Fprintf(&out, "<!-- background=%s -->\n", value)
		}
		if slide.Notes != "" {
			fmt.Fprintf(&out, "<!-- notes=base64:%s -->\n", base64.StdEncoding.EncodeToString([]byte(slide.Notes)))
		}
		style := slideStyleComment(slide)
		if style != "" {
			out.WriteString(style)
			out.WriteByte('\n')
		}
		if (slide.LayoutID != "" || slide.PageNumber != "" || slide.EffectSet || slide.Effect != "" || slide.BackgroundSet || slide.Background != "" || slide.Notes != "" || style != "") && len(slide.Elements) > 0 {
			out.WriteByte('\n')
		}
		for elementIndex, element := range slide.Elements {
			if element.Placeholder && element.MasterSlotID == "" {
				continue
			}
			if elementIndex > 0 {
				out.WriteByte('\n')
			}
			if element.MasterSlotID != "" {
				if element.Placeholder {
					fmt.Fprintf(&out, "<!-- master-slot=%s placeholder=true -->\n", element.MasterSlotID)
				} else {
					fmt.Fprintf(&out, "<!-- master-slot=%s -->\n", element.MasterSlotID)
				}
			}
			if (element.Kind != "image" || element.Placeholder) && element.Query != "" {
				fmt.Fprintf(&out, "<!-- %s -->\n", placementCommentText(element.Query))
			}
			if element.Placeholder && element.MasterSlotID != "" {
				out.WriteString("Placeholder")
				out.WriteByte('\n')
				continue
			}
			switch element.Kind {
			case "heading":
				if element.Level == 1 {
					fmt.Fprintf(&out, "# %s\n", element.Text)
				} else {
					fmt.Fprintf(&out, "## %s\n", element.Text)
				}
			case "bullet":
				fmt.Fprintf(&out, "- %s\n", element.Text)
			case "code":
				out.WriteString("```\n")
				out.WriteString(element.Text)
				if !strings.HasSuffix(element.Text, "\n") {
					out.WriteByte('\n')
				}
				out.WriteString("```\n")
			case "image":
				src := element.Path
				if rel, err := filepath.Rel(filepath.Dir(path), element.Path); err == nil && !strings.HasPrefix(rel, "..") {
					src = rel
				}
				if element.Query != "" {
					src += "?" + element.Query
				}
				fmt.Fprintf(&out, "![image](%s)\n", src)
			case "shape":
				shape := shapeName(element)
				fmt.Fprintf(&out, "[shape:%s]\n", shape)
			case "text-image":
				out.WriteString(element.Text)
				out.WriteByte('\n')
			default:
				out.WriteString(element.Text)
				out.WriteByte('\n')
			}
		}
	}
	if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
		return err
	}
	if activePresenter != nil {
		width, height := terminalAuthoredSize()
		activePresenter.RefreshActiveSlideAsync(deck.ResolvedSlides(), width, height)
	}
	return nil
}

func setUINotice(message string) {
	setUINoticeLevel(message, noticeSuccess)
}

type noticeLevel int

const (
	noticeSuccess noticeLevel = iota
	noticeError
)

type uiNotice struct {
	Text      string
	Level     noticeLevel
	ExpiresAt time.Time
}

func setUINoticeLevel(message string, level noticeLevel) {
	uiNoticeMu.Lock()
	defer uiNoticeMu.Unlock()
	message = strings.TrimSpace(message)
	if message == "" {
		activeUINotice = uiNotice{}
		return
	}
	duration := 2500 * time.Millisecond
	if level == noticeError {
		duration = 6 * time.Second
	}
	activeUINotice = uiNotice{Text: message, Level: level, ExpiresAt: time.Now().Add(duration)}
}

func setUIError(message string) {
	setUINoticeLevel(message, noticeError)
}

func currentUINotice() string {
	uiNoticeMu.Lock()
	defer uiNoticeMu.Unlock()
	if activeUINotice.Text == "" || time.Now().After(activeUINotice.ExpiresAt) {
		activeUINotice = uiNotice{}
		return ""
	}
	if activeUINotice.Level == noticeError {
		return "ERROR: " + activeUINotice.Text
	}
	return activeUINotice.Text
}

func persistDeck(path string, deck Deck) bool {
	if err := saveDeck(path, deck); err != nil {
		setUIError("Save failed: " + err.Error())
		return false
	}
	return true
}

func exportHTML(deckPath string, slides []Slide, cols, rows int) error {
	outPath := strings.TrimSuffix(deckPath, filepath.Ext(deckPath)) + ".html"
	preserved := readPreservedExportHead(outPath)
	html, err := exportHTMLDocument(deckPath, slides, cols, rows, preserved, false)
	if err != nil {
		return err
	}
	return os.WriteFile(outPath, []byte(html), 0o644)
}

func exportHTMLWithNotice(deckPath string, slides []Slide, cols, rows int) bool {
	if err := exportHTML(deckPath, slides, cols, rows); err != nil {
		setUIError("Export failed: " + err.Error())
		return false
	}
	outPath := strings.TrimSuffix(deckPath, filepath.Ext(deckPath)) + ".html"
	setUINotice("Exported " + filepath.Base(outPath))
	return true
}

func exportHTMLDocument(deckPath string, slides []Slide, cols, rows int, preserved preservedExportHead, presenter bool) (string, error) {
	cols = max(20, cols)
	rows = max(10, rows)
	deck := exportDeck{
		Cols:   cols,
		Rows:   rows,
		Source: filepath.Base(deckPath),
	}
	for slideIndex, slide := range slides {
		deck.Pages = append(deck.Pages, exportSlidePages(slide, slideIndex, len(slides), cols, rows)...)
	}
	payload, err := json.Marshal(deck)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	out.WriteString(exportHTMLPrefix(preserved, presenter))
	out.WriteString("\n<script id=\"keynope-data\" type=\"application/json\">")
	out.WriteString(string(payload))
	out.WriteString("</script>\n")
	out.WriteString(exportHTMLSuffix())
	return out.String(), nil
}

func exportSlidePages(slide Slide, slideIndex, slideCount, cols, rows int) []exportPage {
	pageCount := slidePageCount(slide, cols, rows)
	pages := make([]exportPage, 0, pageCount)
	for page := 0; page < pageCount; page++ {
		lines := displayLines(slide, cols, rows, page)
		contentFrames := exportContentFrames(slide, page, cols, rows, slideCount)
		pages = append(pages, exportPage{
			Slide:                slideIndex,
			Page:                 page,
			PageCount:            pageCount,
			SlideCount:           slideCount,
			Effect:               slide.Effect,
			Background:           slide.Background,
			BackgroundLines:      staticBackgroundExportLines(slide.Background, cols, rows, ansiCSSColour(slideFG(slide)), slideBG(slide)),
			Transparency:         transparentShapeExportLines(lines, cols, rows, slide),
			ContentFrames:        contentFrames,
			FG:                   ansiCSSColour(slideFG(slide)),
			BG:                   ansiCSSColour(slideBG(slide)),
			HeaderFG:             ansiCSSColour(slideHeaderFG(slide)),
			Lines:                exportLines(lines, slide, cols, rows, slideCount),
			HideChromePageNumber: slide.PageNumber != "",
		})
	}
	return pages
}

func startPresenterCompanion(deckPath string, slides []Slide, cols, rows int, launchHelper bool) (*presenterCompanion, error) {
	html, err := exportHTMLDocument(deckPath, slides, cols, rows, readPreservedExportHead(strings.TrimSuffix(deckPath, filepath.Ext(deckPath))+".html"), true)
	if err != nil {
		return nil, err
	}
	tokenBytes := make([]byte, 32)
	if _, err := cryptorand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("create companion token: %w", err)
	}
	companion := &presenterCompanion{
		html: html, pages: map[int][]exportPage{}, frames: map[chan presenterTerminalFrame]<-chan struct{}{},
		token: hex.EncodeToString(tokenBytes),
	}
	for slideIndex, slide := range slides {
		companion.pages[slideIndex] = exportSlidePages(slide, slideIndex, len(slides), cols, rows)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		companion.mu.RLock()
		html := companion.html
		companion.mu.RUnlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, html)
	})
	mux.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
		companion.mu.RLock()
		state := companion.state
		companion.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(state)
	})
	mux.HandleFunc("/slide", func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.Query().Get("index")
		index, err := strconv.Atoi(raw)
		if err != nil {
			http.Error(w, "invalid slide index", http.StatusBadRequest)
			return
		}
		companion.mu.RLock()
		pages := append([]exportPage(nil), companion.pages[index]...)
		companion.mu.RUnlock()
		if pages == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(pages)
	})
	mux.HandleFunc("/terminal-frame", func(w http.ResponseWriter, r *http.Request) {
		companion.mu.RLock()
		frame := companion.frame
		companion.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(frame)
	})
	mux.HandleFunc("/terminal-events", companion.handleTerminalEvents)
	mux.HandleFunc("/key", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var event KeyEvent
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			http.Error(w, "invalid key event", http.StatusBadRequest)
			return
		}
		if event.Action == "" {
			http.Error(w, "missing action", http.StatusBadRequest)
			return
		}
		select {
		case remoteKeyEvents <- event:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "key queue full", http.StatusTooManyRequests)
		}
	})
	mux.HandleFunc("/presenter-status", companion.handlePresenterStatus)
	if activeNativeEditor != nil {
		mux.HandleFunc("/api/editor/state", activeNativeEditor.handleState)
		mux.HandleFunc("/api/editor/action", activeNativeEditor.handleAction)
		mux.HandleFunc("/api/editor/preview", activeNativeEditor.handlePreview)
		mux.HandleFunc("/api/editor/fit-text", activeNativeEditor.handleFitText)
		mux.HandleFunc("/api/editor/workspace", activeNativeEditor.handleWorkspace)
		mux.HandleFunc("/api/editor/upload", activeNativeEditor.handleUpload)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	companion.url = "http://" + listener.Addr().String() + "/?token=" + url.QueryEscape(companion.token)
	companion.server = &http.Server{Handler: companion.authorized(mux)}
	go func() {
		if err := companion.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "presenter companion stopped: %v\n", err)
		}
	}()
	if !launchHelper {
		companion.mu.Lock()
		companion.helper = true
		companion.target = "none"
		companion.mu.Unlock()
		fmt.Println("KEYNOPE_URL=" + companion.url)
		return companion, nil
	}
	cmd, err := launchPresenterSurface(companion.url)
	if err != nil {
		_ = companion.server.Close()
		return nil, err
	}
	companion.cmd = cmd
	companion.mu.Lock()
	companion.helper = true
	companion.target = "none"
	companion.mu.Unlock()
	go func() {
		_ = cmd.Wait()
		companion.mu.Lock()
		companion.helper = false
		companion.target = "none"
		companion.mu.Unlock()
	}()
	return companion, nil
}

func (p *presenterCompanion) authorized(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorized := false
		if token := r.URL.Query().Get("token"); token != "" && token == p.token {
			authorized = true
			http.SetCookie(w, &http.Cookie{
				Name: "keynope_session", Value: p.token, Path: "/", HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
		}
		if cookie, err := r.Cookie("keynope_session"); err == nil && cookie.Value == p.token {
			authorized = true
		}
		if !authorized {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			origin := r.Header.Get("Origin")
			if origin != "" {
				parsed, err := url.Parse(origin)
				if err != nil || parsed.Host != r.Host {
					http.Error(w, "invalid origin", http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (p *presenterCompanion) Status() (available bool, target string, live bool) {
	if p == nil {
		return false, "none", false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.helper, p.target, p.state.Presenting
}

func (p *presenterCompanion) handlePresenterStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var status struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&status); err != nil {
		http.Error(w, "invalid presenter status", http.StatusBadRequest)
		return
	}
	if status.Mode != "none" && status.Mode != "main" && status.Mode != "external" {
		http.Error(w, "invalid presenter mode", http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	p.target = status.Mode
	p.helper = true
	if nativeAppModeActive {
		presenting := status.Mode != "none"
		if p.state.Presenting != presenting {
			p.state.Presenting = presenting
			p.state.Version++
		}
	}
	p.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (p *presenterCompanion) handleTerminalEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unavailable", http.StatusInternalServerError)
		return
	}
	updates := make(chan presenterTerminalFrame, 64)
	p.mu.Lock()
	p.frames[updates] = r.Context().Done()
	initial := p.frame
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.frames, updates)
		p.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	writeFrame := func(frame presenterTerminalFrame) bool {
		payload, err := json.Marshal(frame)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", frame.Version, payload); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !writeFrame(initial) {
		return
	}
	for {
		select {
		case frame := <-updates:
			if !writeFrame(frame) {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (p *presenterCompanion) Refresh(deckPath string, slides []Slide, cols, rows int) error {
	if p == nil {
		return nil
	}
	html, err := exportHTMLDocument(deckPath, slides, cols, rows, readPreservedExportHead(strings.TrimSuffix(deckPath, filepath.Ext(deckPath))+".html"), true)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.html = html
	p.pages = map[int][]exportPage{}
	for slideIndex, slide := range slides {
		p.pages[slideIndex] = exportSlidePages(slide, slideIndex, len(slides), cols, rows)
	}
	p.seq++
	p.state.DeckVersion++
	p.state.DeckSlide = -1
	p.state.Version++
	p.mu.Unlock()
	return nil
}

func (p *presenterCompanion) RefreshAllAsync(deckPath string, slides []Slide, cols, rows int) {
	if p == nil {
		return
	}
	slides = cloneSlides(slides)
	p.mu.Lock()
	p.fullSeq++
	seq := p.fullSeq
	p.mu.Unlock()
	go func() {
		html, err := exportHTMLDocument(deckPath, slides, cols, rows, readPreservedExportHead(strings.TrimSuffix(deckPath, filepath.Ext(deckPath))+".html"), true)
		if err != nil {
			setUIError("Presenter refresh failed: " + err.Error())
			return
		}
		pages := make(map[int][]exportPage, len(slides))
		for slideIndex, slide := range slides {
			pages[slideIndex] = exportSlidePages(slide, slideIndex, len(slides), cols, rows)
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if seq != p.fullSeq {
			return
		}
		p.html = html
		p.pages = pages
		p.state.DeckVersion++
		p.state.DeckSlide = -1
		p.state.Version++
	}()
}

func (p *presenterCompanion) RefreshActiveSlideAsync(slides []Slide, cols, rows int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	slideIndex := p.state.Slide
	if slideIndex < 0 || slideIndex >= len(slides) {
		p.mu.Unlock()
		return
	}
	p.seq++
	seq := p.seq
	p.mu.Unlock()
	slide := cloneSlide(slides[slideIndex])
	slideCount := len(slides)
	go func() {
		time.Sleep(80 * time.Millisecond)
		p.mu.RLock()
		stale := seq != p.seq
		p.mu.RUnlock()
		if stale {
			return
		}
		pages := exportSlidePages(slide, slideIndex, slideCount, cols, rows)
		p.mu.Lock()
		defer p.mu.Unlock()
		if seq != p.seq {
			return
		}
		if p.pages == nil {
			p.pages = map[int][]exportPage{}
		}
		p.pages[slideIndex] = pages
		p.state.DeckVersion++
		p.state.DeckSlide = slideIndex
		p.state.Version++
	}()
}

func (p *presenterCompanion) Update(slide, page int, presenting bool, deckState *EditState) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	timerMode, timerInput, timerEndMS := p.state.TimerMode, p.state.TimerInput, p.state.TimerEndMS
	if deckState != nil {
		timerMode = deckState.TimerMode
		timerInput = deckState.TimerInput
		if !deckState.TimerDeadline.IsZero() {
			timerEndMS = deckState.TimerDeadline.UnixMilli()
		}
	}
	if p.state.Slide == slide && p.state.Page == page && p.state.Presenting == presenting && p.state.TimerMode == timerMode && p.state.TimerInput == timerInput && p.state.TimerEndMS == timerEndMS {
		return
	}
	p.state.Slide = slide
	p.state.Page = page
	p.state.Presenting = presenting
	p.state.TimerMode = timerMode
	p.state.TimerInput = timerInput
	p.state.TimerEndMS = timerEndMS
	p.state.Version++
}

func (p *presenterCompanion) StartTimer(duration time.Duration) {
	if p == nil || duration <= 0 {
		return
	}
	p.mu.Lock()
	p.state.TimerMode = "running"
	p.state.TimerInput = ""
	p.state.TimerEndMS = time.Now().Add(duration).UnixMilli()
	p.state.Version++
	p.mu.Unlock()
}

func (p *presenterCompanion) StopTimer() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.state.TimerMode = ""
	p.state.TimerInput = ""
	p.state.TimerEndMS = 0
	p.state.Version++
	p.mu.Unlock()
}

func (p *presenterCompanion) PublishTerminalFrame(frame string, cols, rows int) {
	if p == nil || cols <= 0 || rows <= 0 || frame == "" {
		return
	}
	p.mu.Lock()
	p.frame.Version++
	p.frame.Cols = cols
	p.frame.Rows = rows
	p.frame.Lines = nil
	p.frame.ANSI = frame
	update := p.frame
	subscribers := make(map[chan presenterTerminalFrame]<-chan struct{}, len(p.frames))
	for subscriber, done := range p.frames {
		subscribers[subscriber] = done
	}
	p.mu.Unlock()
	for subscriber, done := range subscribers {
		select {
		case subscriber <- update:
		case <-done:
		}
	}
}

func (p *presenterCompanion) Close() {
	if p == nil || p.server == nil {
		return
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	_ = p.server.Close()
}

func launchPresenterSurface(url string) (*exec.Cmd, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("second-screen broadcast is only available on macOS with the KeynopePresenter helper")
	}
	helper, ok := findMacPresenterHelper()
	if !ok {
		return nil, fmt.Errorf("KeynopePresenter.app not found; run `make build` from the distribution repo to enable second-screen broadcast")
	}
	cmd := exec.Command("/usr/bin/open", macPresenterLaunchArgs(helper.bundle, url, os.Getpid())...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func macPresenterLaunchArgs(bundle, url string, parentPID int) []string {
	return []string{
		"-g",
		"-n",
		"-W",
		bundle,
		"--args",
		url,
		"--parent-pid",
		strconv.Itoa(parentPID),
	}
}

type macPresenterHelper struct {
	bundle     string
	executable string
}

func findMacPresenterHelper() (macPresenterHelper, bool) {
	executable, _ := os.Executable()
	workingDirectory, _ := os.Getwd()
	return findMacPresenterHelperFrom(executable, workingDirectory)
}

func findMacPresenterHelperFrom(executable, workingDirectory string) (macPresenterHelper, bool) {
	candidates := []macPresenterHelper{}
	if executable != "" {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		base := filepath.Dir(executable)
		candidates = append(candidates, macPresenterHelperCandidates(base)...)
	}
	if workingDirectory != "" {
		candidates = append(candidates, macPresenterHelperCandidates(filepath.Join(workingDirectory, "bin"))...)
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate.executable); err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0 {
			return candidate, true
		}
	}
	return macPresenterHelper{}, false
}

func macPresenterHelperCandidates(base string) []macPresenterHelper {
	bundle := filepath.Join(base, "KeynopePresenter.app")
	return []macPresenterHelper{{
		bundle:     bundle,
		executable: filepath.Join(bundle, "Contents", "MacOS", "KeynopePresenter"),
	}}
}

type preservedExportHead struct {
	Title   string
	Metas   []string
	Scripts []string
}

const exportEffectFrameCount = 90

func exportContentFrames(slide Slide, page, width, height, slideCount int) []exportContentFrame {
	if !slideHasAnimatedImage(slide) {
		return nil
	}
	exportImageAnimationMu.Lock()
	defer exportImageAnimationMu.Unlock()
	frames := make([]exportContentFrame, 0, exportEffectFrameCount)
	seen := map[string]int{}
	previousLines := []exportLine(nil)
	previous := exportImageAnimationPosition
	defer func() { exportImageAnimationPosition = previous }()
	for frame := 0; frame < exportEffectFrameCount; frame++ {
		position := time.Duration(frame) * 70 * time.Millisecond
		exportImageAnimationPosition = &position
		lines := displayLines(slide, width, height, page)
		exported := exportLines(lines, slide, width, height, slideCount)
		signature := exportFrameSignature(exported)
		if previousIndex, ok := seen[signature]; ok {
			if previousIndex == 0 && len(frames) > 1 {
				break
			}
			continue
		}
		seen[signature] = len(frames)
		if len(frames) == 0 {
			frames = append(frames, exportContentFrame{Full: true, Lines: exported})
		} else {
			clear, update := exportLineDelta(previousLines, exported)
			frames = append(frames, exportContentFrame{Clear: clear, Update: update})
		}
		previousLines = exported
	}
	return frames
}

func exportLineDelta(previous, current []exportLine) ([]string, []exportLine) {
	prev := exportLineMap(previous)
	next := exportLineMap(current)
	var clear []string
	var update []exportLine
	for key := range prev {
		if _, ok := next[key]; !ok {
			clear = append(clear, key)
		}
	}
	for key, line := range next {
		if exportLineValue(prev[key]) != exportLineValue(line) {
			update = append(update, line)
		}
	}
	sort.Strings(clear)
	sort.Slice(update, func(i, j int) bool {
		if update[i].Row != update[j].Row {
			return update[i].Row < update[j].Row
		}
		return lineFirstCol(update[i]) < lineFirstCol(update[j])
	})
	return clear, update
}

func exportLineMap(lines []exportLine) map[string]exportLine {
	out := map[string]exportLine{}
	for _, line := range lines {
		out[exportLineKey(line)] = line
	}
	return out
}

func exportLineKey(line exportLine) string {
	return fmt.Sprintf("%d:%d:%d:%s", line.Row, lineFirstCol(line), line.Element, line.Role)
}

func lineFirstCol(line exportLine) int {
	if len(line.Parts) == 0 {
		return line.Col
	}
	return line.Parts[0].Col
}

func exportLineValue(line exportLine) string {
	data, err := json.Marshal(line)
	if err != nil {
		return ""
	}
	return string(data)
}

func exportFrameSignature(lines []exportLine) string {
	data, err := json.Marshal(lines)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func drawEffectFrame(name string, width, height, frame int, matrix *matrixEffect, stars *starsEffect, fireworks *burstEffect, background string) {
	effectFrame := captureTerminalOutput(func() {
		drawEffectFrameRaw(name, width, height, frame, matrix, stars, fireworks)
	})
	termPrint(applyANSIBackground(effectFrame, background))
}

func drawEffectFrameRaw(name string, width, height, frame int, matrix *matrixEffect, stars *starsEffect, fireworks *burstEffect) {
	switch name {
	case "matrix":
		matrix.draw(frame)
	case "stars":
		stars.draw()
	case "plasma":
		drawPlasma(width, height, frame)
	case "glitch":
		drawGlitch(width, height, frame)
	case "digital-snow":
		drawDigitalSnow(name, width, height, frame)
	case "radar":
		drawRadar(width, height, frame)
	case "neural":
		drawNeural(width, height, frame)
	case "circuit":
		drawCircuit(width, height, frame)
	case "data-storm":
		drawDataStorm(width, height, frame)
	case "flame":
		drawFlame(width, height, frame)
	case "warp":
		drawWarp(width, height, frame)
	case "scanline":
		drawScanline(width, height, frame)
	default:
		fireworks.draw(frame)
	}
}

func applyANSIBackground(frame, background string) string {
	if frame == "" || background == "" {
		return frame
	}
	var out strings.Builder
	out.Grow(len(frame) + len(background)*8)
	out.WriteString("\033[")
	out.WriteString(background)
	out.WriteByte('m')
	for index := 0; index < len(frame); {
		if frame[index] != '\033' || index+1 >= len(frame) || frame[index+1] != '[' {
			out.WriteByte(frame[index])
			index++
			continue
		}
		end := index + 2
		for end < len(frame) && (frame[end] < 0x40 || frame[end] > 0x7e) {
			end++
		}
		if end >= len(frame) {
			out.WriteString(frame[index:])
			break
		}
		if frame[end] != 'm' {
			out.WriteString(frame[index : end+1])
			index = end + 1
			continue
		}
		params := frame[index+2 : end]
		out.WriteString("\033[")
		if params == "" {
			out.WriteByte('0')
		} else {
			out.WriteString(params)
		}
		out.WriteByte(';')
		out.WriteString(background)
		out.WriteByte('m')
		index = end + 1
	}
	return out.String()
}

func drawStaticBackground(name string, width, height int, background string) {
	if width <= 0 || height <= 0 || name == "" || name == "none" {
		return
	}
	backgroundFrame := captureTerminalOutput(func() {
		drawStaticBackgroundRaw(name, width, height)
	})
	termPrint(applyANSIBackground(backgroundFrame, background))
}

func drawStaticBackgroundRaw(name string, width, height int) {
	switch name {
	case "soft-plasma":
		drawBackgroundSoftPlasma(width, height)
	case "aurora":
		drawBackgroundAurora(width, height)
	case "topography":
		drawBackgroundTopography(width, height)
	case "waves":
		drawBackgroundWaves(width, height)
	case "mesh":
		drawBackgroundMesh(width, height)
	case "constellation":
		drawBackgroundConstellation(width, height)
	case "ribbons":
		drawBackgroundRibbons(width, height)
	case "diagonal-flow":
		drawBackgroundDiagonalFlow(width, height)
	case "blueprint":
		drawBackgroundBlueprint(width, height)
	}
}

func staticBackgroundExportLines(name string, width, height int, defaultColor, background string) []exportLine {
	if name == "" || name == "none" {
		return nil
	}
	var buffer strings.Builder
	previous := terminalFrame
	terminalFrame = &buffer
	drawStaticBackground(name, width, height, background)
	terminalFrame = previous
	return ansiFrameToExportLines(buffer.String(), width, height, defaultColor)
}

func backgroundGlyph(value float64) rune {
	glyphs := []rune(" .·:░▒")
	index := int(math.Round(clampFloat(value, 0, 1) * float64(len(glyphs)-1)))
	return glyphs[index]
}

func backgroundColour(palette []string, value float64) string {
	index := int(math.Round(clampFloat(value, 0, 1) * float64(len(palette)-1)))
	return palette[index]
}

func drawBackgroundSoftPlasma(width, height int) {
	palette := []string{"\033[0;34m", "\033[0;36m", "\033[0;32m", "\033[0;35m"}
	for y := 0; y < height-1; y++ {
		for x := 0; x < width; x++ {
			v := (math.Sin(float64(x)/13.0) + math.Sin(float64(y)/5.0) + math.Sin(float64(x+y)/19.0) + 3.0) / 6.0
			glyph := backgroundGlyph(v * 0.78)
			if glyph == ' ' {
				continue
			}
			termPrintf("%s\033[%d;%dH%c", backgroundColour(palette, v), y+1, x+1, glyph)
		}
	}
}

func drawBackgroundAurora(width, height int) {
	palette := []string{"\033[0;32m", "\033[0;36m", "\033[0;35m", "\033[1;34m"}
	for band := 0; band < 4; band++ {
		for x := 0; x < width; x++ {
			center := float64(height)/2.0 + math.Sin(float64(x)/float64(7+band*3)+float64(band))*float64(2+band)
			thickness := 2.0 + float64(band)
			for dy := -2; dy <= 2; dy++ {
				y := int(math.Round(center + float64(dy)*thickness/2.0))
				if y < 0 || y >= height-1 {
					continue
				}
				glyph := []rune("·░▒▓")[min(3, abs(dy))]
				termPrintf("%s\033[%d;%dH%c", palette[band%len(palette)], y+1, x+1, glyph)
			}
		}
	}
}

func drawBackgroundTopography(width, height int) {
	for y := 0; y < height-1; y++ {
		for x := 0; x < width; x++ {
			v := math.Sin(float64(x)/8.0) + math.Sin(float64(y)/3.7) + math.Sin(float64(x+y)/11.0)
			if abs(int(math.Round(v*10)))%7 == 0 {
				termPrintf("\033[0;36m\033[%d;%dH·", y+1, x+1)
			}
		}
	}
}

func drawBackgroundWaves(width, height int) {
	for y := 1; y < height-1; y += 3 {
		color := "\033[0;34m"
		if y%2 == 0 {
			color = "\033[0;36m"
		}
		for x := 0; x < width; x++ {
			yy := y + int(math.Round(math.Sin(float64(x)/9.0+float64(y))*1.2))
			if yy >= 0 && yy < height-1 {
				glyph := '~'
				if x%9 == 0 {
					glyph = '·'
				}
				termPrintf("%s\033[%d;%dH%c", color, yy+1, x+1, glyph)
			}
		}
	}
}

func drawBackgroundMesh(width, height int) {
	points := deterministicPoints(width, max(1, height-1), max(10, width*height/180), 73)
	for i, a := range points {
		for j := i + 1; j < len(points); j++ {
			b := points[j]
			if abs(a.x-b.x)+abs(a.y-b.y) < max(8, width/8) {
				drawLine(a.x, a.y, b.x, b.y, "\033[0;34m", ".")
			}
		}
		termPrintf("\033[0;36m\033[%d;%dH·", a.y+1, a.x+1)
	}
}

func drawBackgroundConstellation(width, height int) {
	points := deterministicPoints(width, max(1, height-1), max(12, width*height/240), 109)
	for i, p := range points {
		if i%5 == 0 {
			termPrintf("\033[1;37m\033[%d;%dH·", p.y+1, p.x+1)
		} else {
			termPrintf("\033[0;36m\033[%d;%dH.", p.y+1, p.x+1)
		}
		if i > 0 && i%4 == 0 {
			prev := points[i-1]
			if abs(p.x-prev.x)+abs(p.y-prev.y) < max(10, width/6) {
				drawLine(prev.x, prev.y, p.x, p.y, "\033[0;34m", ".")
			}
		}
	}
}

func drawBackgroundRibbons(width, height int) {
	palette := []string{"\033[0;35m", "\033[0;34m", "\033[0;36m"}
	for ribbon := 0; ribbon < 5; ribbon++ {
		for x := 0; x < width; x++ {
			y := int(float64(height)/6.0*float64(ribbon+1) + math.Sin(float64(x)/float64(8+ribbon*2)+float64(ribbon))*3.0)
			if y >= 0 && y < height-1 {
				termPrintf("%s\033[%d;%dH%s", palette[ribbon%len(palette)], y+1, x+1, "▒")
			}
		}
	}
}

func drawBackgroundDiagonalFlow(width, height int) {
	palette := []string{"\033[0;34m", "\033[0;36m", "\033[0;35m"}
	glyphs := []rune("·░▒")
	for y := 0; y < height-1; y++ {
		for x := 0; x < width; x++ {
			if (x+y*2)%11 < 3 {
				value := (x + y) % len(palette)
				termPrintf("%s\033[%d;%dH%c", palette[value], y+1, x+1, glyphs[(x+y)%len(glyphs)])
			}
		}
	}
}

func drawBackgroundBlueprint(width, height int) {
	for y := 0; y < height-1; y++ {
		for x := 0; x < width; x++ {
			if y%6 == 0 || x%12 == 0 {
				termPrintf("\033[0;34m\033[%d;%dH·", y+1, x+1)
			}
		}
	}
	for x := 6; x < width; x += 24 {
		for y := 5; y < height-1; y += 12 {
			drawEllipse(x, y, 10, 4, "\033[0;36m", ".")
		}
	}
}

type exportCell struct {
	r     rune
	color string
}

func ansiFrameToExportLines(frame string, width, height int, defaultColor string) []exportLine {
	grid := make([][]exportCell, height)
	for row := range grid {
		grid[row] = make([]exportCell, width)
	}
	row, col := 0, 0
	color := defaultColor
	clear := func() {
		for y := range grid {
			for x := range grid[y] {
				grid[y][x] = exportCell{}
			}
		}
	}
	for i := 0; i < len(frame); {
		if frame[i] == 0x1b && i+1 < len(frame) && frame[i+1] == '[' {
			end := i + 2
			for end < len(frame) && !isANSICommandByte(frame[end]) {
				end++
			}
			if end >= len(frame) {
				break
			}
			params := frame[i+2 : end]
			switch frame[end] {
			case 'H', 'f':
				row, col = ansiCursorPosition(params)
				row = max(0, min(height-1, row))
				col = max(0, min(width-1, col))
			case 'J':
				if params == "2" || params == "" {
					clear()
					row, col = 0, 0
				}
			case 'm':
				if css := ansiParamsCSSColour(params); css != "" {
					color = css
				}
			}
			i = end + 1
			continue
		}
		r, size := utf8.DecodeRuneInString(frame[i:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		switch r {
		case '\n':
			row++
			col = 0
		case '\r':
			col = 0
		default:
			if row >= 0 && row < height && col >= 0 && col < width && r != ' ' {
				grid[row][col] = exportCell{r: r, color: color}
			}
			col++
			if col >= width {
				col = 0
				row++
			}
		}
		i += size
	}
	var out []exportLine
	for y, cells := range grid {
		var parts []exportPart
		var run []rune
		runCol := 0
		runColor := ""
		flush := func() {
			if len(run) == 0 {
				return
			}
			parts = append(parts, exportPart{Col: runCol, Text: string(run), Color: runColor})
			run = nil
		}
		for x, cell := range cells {
			if cell.r == 0 {
				flush()
				continue
			}
			if len(run) == 0 {
				runCol = x
				runColor = cell.color
			} else if cell.color != runColor {
				flush()
				runCol = x
				runColor = cell.color
			}
			run = append(run, cell.r)
		}
		flush()
		if len(parts) > 0 {
			out = append(out, exportLine{Row: y, Role: "effect", Parts: parts})
		}
	}
	return out
}

func isANSICommandByte(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func ansiCursorPosition(params string) (int, int) {
	if params == "" {
		return 0, 0
	}
	fields := strings.Split(params, ";")
	row, col := 1, 1
	if len(fields) >= 1 && fields[0] != "" {
		if parsed, err := strconv.Atoi(fields[0]); err == nil {
			row = parsed
		}
	}
	if len(fields) >= 2 && fields[1] != "" {
		if parsed, err := strconv.Atoi(fields[1]); err == nil {
			col = parsed
		}
	}
	return row - 1, col - 1
}

func exportLines(lines []Line, slide Slide, width, height, slideCount int) []exportLine {
	var out []exportLine
	for index, line := range lines {
		if line.Row < 0 || line.Row >= height || line.Col >= width || line.Text == "" {
			continue
		}
		transparentMask := transparencyMaskLine(line, slide)
		if transparentMask && line.Role == "shape" {
			continue
		}
		exportRole := line.Role
		if transparentMask {
			if line.Role == "image" {
				exportRole = "transparent-image"
			} else {
				exportRole = "transparent-text"
			}
		}
		color := ansiCSSColour(slideFG(slide))
		if line.Role == "heading" {
			color = ansiCSSColour(slideHeaderFG(slide))
		}
		if fg := elementFG(line.Query, line.Role == "heading"); fg != "" {
			color = ansiCSSColour(fg)
		}
		var parts []exportPart
		if line.Role == "code" {
			background := ansiCSSColour("100")
			if bg := elementBG(line.Query); bg != "" {
				background = ansiCSSColour(bg)
			}
			parts = exportSolidTextParts(line.Text, line.Col, color, background, width)
		} else {
			parts = exportANSITextParts(line.Text, line.Col, color, width)
		}
		if len(parts) == 0 {
			continue
		}
		transparency := transparentShapeCellsFrom(lines, index+1, width, height, slide)
		parts = applyExportTransparencyToParts(parts, line.Row, transparency)
		if len(parts) == 0 {
			continue
		}
		link := ""
		if line.Role != "outline" {
			if target, ok := linkTargetFromQuery(line.Query, slideCount); ok {
				link = target.Value
			}
		}
		out = append(out, exportLine{Row: line.Row, Col: line.Col, Element: line.Element, Role: exportRole, Link: link, Parts: parts})
	}
	return out
}

func transparentShapeExportLines(lines []Line, width, height int, slide Slide) []exportLine {
	cells := transparentShapeCells(lines, width, height, slide)
	if len(cells) == 0 {
		return nil
	}
	rows := make([]int, 0, len(cells))
	for row := range cells {
		rows = append(rows, row)
	}
	sort.Ints(rows)
	out := make([]exportLine, 0, len(rows))
	for _, row := range rows {
		if row < 0 || row >= height || len(cells[row]) == 0 {
			continue
		}
		cols := make([]int, 0, len(cells[row]))
		for col := range cells[row] {
			cols = append(cols, col)
		}
		sort.Ints(cols)
		var parts []exportPart
		start := cols[0]
		prev := cols[0]
		bg := cells[row][start]
		flush := func(end int, runBG string) {
			parts = append(parts, exportPart{
				Col:        start,
				Text:       strings.Repeat(" ", end-start+1),
				Color:      ansiCSSColour(slideFG(slide)),
				Background: ansiCSSColour(runBG),
			})
		}
		for _, col := range cols[1:] {
			if col == prev+1 && cells[row][col] == bg {
				prev = col
				continue
			}
			flush(prev, bg)
			start = col
			prev = col
			bg = cells[row][col]
		}
		flush(prev, bg)
		if len(parts) > 0 {
			out = append(out, exportLine{Row: row, Role: "transparent", Parts: parts})
		}
	}
	return out
}

func applyExportTransparencyToParts(parts []exportPart, row int, transparency map[int]map[int]string) []exportPart {
	rowCells := transparency[row]
	if len(rowCells) == 0 {
		return parts
	}
	var out []exportPart
	appendPart := func(col int, text, color, background string) {
		if text == "" {
			return
		}
		if len(out) > 0 {
			last := &out[len(out)-1]
			if last.Col+displayWidth(last.Text) == col && last.Color == color && last.Background == background {
				last.Text += text
				return
			}
		}
		out = append(out, exportPart{Col: col, Text: text, Color: color, Background: background})
	}
	for _, part := range parts {
		color := part.Color
		if color == "" {
			color = "#f3efe0"
		}
		for offset, r := range []rune(part.Text) {
			col := part.Col + offset
			bg := part.Background
			cellBG := rowCells[col]
			cellColor := color
			if cellBG != "" {
				bg = ansiCSSColour(cellBG)
				cellColor = darkenCSSForTransparency(color)
			}
			appendPart(col, string(r), cellColor, bg)
		}
	}
	return out
}

func darkenCSSForTransparency(css string) string {
	return ansiCSSColour(darkenFGForTransparency(cssColourToFG(css, "37")))
}

func exportSolidTextParts(text string, baseCol int, color, background string, width int) []exportPart {
	if baseCol >= width {
		return nil
	}
	if baseCol < 0 {
		text = runeSliceWithPadding(text, -baseCol, displayWidth(text))
		baseCol = 0
	}
	text = crop(text, max(0, width-baseCol))
	if text == "" {
		return nil
	}
	return []exportPart{{Col: baseCol, Text: text, Color: color, Background: background}}
}

func exportANSITextParts(text string, baseCol int, defaultColor string, width int) []exportPart {
	var parts []exportPart
	normalColor := defaultColor
	color := defaultColor
	bold := false
	col := baseCol
	var run []rune
	runCol := col
	flush := func() {
		if len(run) == 0 {
			return
		}
		parts = append(parts, exportPart{Col: runCol, Text: string(run), Color: color})
		run = nil
	}
	for i := 0; i < len(text) && col < width; {
		if text[i] == 0x1b {
			flush()
			end := i + 1
			for end < len(text) && text[end] != 'm' {
				end++
			}
			if end < len(text) {
				sequence := text[i : end+1]
				if ansiSequenceBoldOff(sequence) {
					bold = false
					color = normalColor
				}
				if ansiSequenceBoldOn(sequence) {
					bold = true
					color = lightenCSSColour(normalColor)
				}
				if css := ansiSequenceCSSColour(sequence); css != "" {
					normalColor = css
					color = css
					if bold {
						color = lightenCSSColour(normalColor)
					}
				}
				i = end + 1
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		if r == ' ' {
			flush()
			col++
			i += size
			continue
		}
		if len(run) == 0 {
			runCol = col
		}
		run = append(run, r)
		col++
		i += size
	}
	flush()
	return parts
}

func ansiSequenceBoldOn(sequence string) bool {
	return ansiParamsContain(sequence, "1")
}

func ansiSequenceBoldOff(sequence string) bool {
	return ansiParamsContain(sequence, "0") || ansiParamsContain(sequence, "22")
}

func ansiParamsContain(sequence, needle string) bool {
	sequence = strings.TrimPrefix(sequence, "\033[")
	sequence = strings.TrimSuffix(sequence, "m")
	for _, field := range strings.Split(sequence, ";") {
		if field == needle {
			return true
		}
	}
	return false
}

func ansiSequenceCSSColour(sequence string) string {
	sequence = strings.TrimPrefix(sequence, "\033[")
	sequence = strings.TrimSuffix(sequence, "m")
	return ansiParamsCSSColour(sequence)
}

func ansiParamsCSSColour(params string) string {
	fields := strings.Split(params, ";")
	for i := 0; i < len(fields); i++ {
		if fields[i] == "0" {
			continue
		}
		if (fields[i] == "38" || fields[i] == "48") && i+4 < len(fields) && fields[i+1] == "2" {
			return fmt.Sprintf("rgb(%s,%s,%s)", fields[i+2], fields[i+3], fields[i+4])
		}
		if parsed, err := strconv.Atoi(fields[i]); err == nil && parsed >= 30 && parsed <= 37 {
			return ansiCSSColour(strconv.Itoa(parsed))
		}
		if parsed, err := strconv.Atoi(fields[i]); err == nil && parsed >= 90 && parsed <= 97 {
			return ansiCSSColour(strconv.Itoa(parsed))
		}
	}
	return ""
}

func ansiCSSColour(code string) string {
	parts := strings.Split(code, ";")
	if len(parts) == 5 && (parts[0] == "38" || parts[0] == "48") && parts[1] == "2" {
		return fmt.Sprintf("rgb(%s,%s,%s)", parts[2], parts[3], parts[4])
	}
	if parsed, err := strconv.Atoi(code); err == nil {
		colours := map[int]string{
			30: "#000000", 31: "#aa0000", 32: "#00aa00", 33: "#aaaa00",
			34: "#0000aa", 35: "#aa00aa", 36: "#00aaaa", 37: "#f3efe0",
			40: "#000000", 41: "#aa0000", 42: "#00aa00", 43: "#aaaa00",
			44: "#0000aa", 45: "#aa00aa", 46: "#00aaaa", 47: "#f3efe0",
			90: "#555555", 91: "#ff5555", 92: "#55ff55", 93: "#ffff55",
			94: "#5555ff", 95: "#ff55ff", 96: "#55ffff", 97: "#ffffff",
			100: "#555555", 101: "#ff5555", 102: "#55ff55", 103: "#ffff55",
			104: "#5555ff", 105: "#ff55ff", 106: "#55ffff", 107: "#ffffff",
		}
		if css, ok := colours[parsed]; ok {
			return css
		}
	}
	return "#f3efe0"
}

func readPreservedExportHead(path string) preservedExportHead {
	data, err := os.ReadFile(path)
	if err != nil {
		return preservedExportHead{}
	}
	head, ok := extractHTMLHead(string(data))
	if !ok {
		return preservedExportHead{}
	}
	return preservedExportHead{
		Title:   firstHTMLTagBlock(head, "title"),
		Metas:   customHeadMetaTags(head),
		Scripts: customHeadScriptTags(head),
	}
}

func extractHTMLHead(html string) (string, bool) {
	lower := strings.ToLower(html)
	start := strings.Index(lower, "<head")
	if start < 0 {
		return "", false
	}
	startClose := strings.Index(html[start:], ">")
	if startClose < 0 {
		return "", false
	}
	contentStart := start + startClose + 1
	end := strings.Index(lower[contentStart:], "</head>")
	if end < 0 {
		return "", false
	}
	return html[contentStart : contentStart+end], true
}

func firstHTMLTagBlock(html, tag string) string {
	re := regexp.MustCompile(`(?is)<` + regexp.QuoteMeta(tag) + `\b[^>]*>.*?</` + regexp.QuoteMeta(tag) + `\s*>`)
	return strings.TrimSpace(re.FindString(html))
}

func customHeadMetaTags(head string) []string {
	re := regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	var metas []string
	for _, tag := range re.FindAllString(head, -1) {
		if isGeneratedHeadMeta(tag) {
			continue
		}
		metas = append(metas, strings.TrimSpace(tag))
	}
	return metas
}

func isGeneratedHeadMeta(tag string) bool {
	lower := strings.ToLower(tag)
	return strings.Contains(lower, "charset=") || htmlAttrEquals(lower, "name", "viewport")
}

func htmlAttrEquals(tag, attr, value string) bool {
	quotedValue := regexp.QuoteMeta(value)
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(attr) + `\s*=\s*(?:"` + quotedValue + `"|'` + quotedValue + `'|` + quotedValue + `(?:[\s/>]|$))`)
	return re.MatchString(tag)
}

func customHeadScriptTags(head string) []string {
	re := regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script\s*>`)
	var scripts []string
	for _, tag := range re.FindAllString(head, -1) {
		lower := strings.ToLower(tag)
		if htmlAttrEquals(lower, "id", "keynope-data") {
			continue
		}
		scripts = append(scripts, strings.TrimSpace(tag))
	}
	return scripts
}

func exportHTMLPrefix(preserved preservedExportHead, presenter bool) string {
	html := `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Keynope Export</title>
<style>
html, body { margin: 0; width: 100%; height: 100%; overflow: hidden; background: #000; user-select: none; -webkit-user-select: none; }
body { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace; }
#stage { position: fixed; inset: 0; background: #000; overflow: hidden; }
.terminal-layer { position: absolute; left: 50%; top: 50%; transform: translate(-50%, -50%); width: calc(var(--cols) * 1ch); height: calc(var(--rows) * 1em); font-size: var(--cell); line-height: 1; color: #f3efe0; }
#presenter-canvas { position: absolute; left: 50%; top: 50%; transform: translate(-50%, -50%); z-index: 2; display: none; image-rendering: auto; }
#link-layer { position: absolute; left: 50%; top: 50%; transform: translate(-50%, -50%); z-index: 4; display: none; pointer-events: none; }
.canvas-link-hit { position: absolute; pointer-events: auto; cursor: pointer; background: transparent; }
.keynope-app-toolbar { position: fixed; left: 210px; right: 0; bottom: 0; height: 52px; z-index: 20; display: none; align-items: center; justify-content: flex-end; gap: 8px; padding: 0 12px; box-sizing: border-box; color: #e8e8e8; background: rgba(18, 18, 18, 0.96); border-top: 1px solid #444; font: 13px -apple-system, BlinkMacSystemFont, sans-serif; }
.keynope-app-toolbar button { color: inherit; background: #292929; border: 1px solid #555; border-radius: 6px; padding: 6px 12px; font: inherit; cursor: default; }
.keynope-app-toolbar button:active { background: #444; }
.keynope-app-toolbar button.active { border-color: #70b7ff; background: #244766; }
.keynope-timer-input { width: 72px; box-sizing: border-box; color: #eee; background: #111; border: 1px solid #70b7ff; border-radius: 5px; padding: 6px; font: 13px ui-monospace, SFMono-Regular, Menlo, monospace; }
.keynope-editor-status { margin-right: auto; color: #aeb4bb; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
html[data-keynope-app="true"] .keynope-app-toolbar { display: flex; }
html[data-keynope-app="true"] #stage { left: 210px; right: 0; }
.keynope-editor-topbar { position: fixed; left: 210px; right: 0; top: 0; height: 52px; z-index: 21; display: none; align-items: center; gap: 6px; padding: 0 10px; box-sizing: border-box; overflow-x: auto; background: #181818; border-bottom: 1px solid #444; font: 13px -apple-system, BlinkMacSystemFont, sans-serif; }
.keynope-topbar-mode { display: flex; min-width: max-content; align-items: center; gap: 6px; }
.keynope-topbar-mode[hidden] { display: none; }
.keynope-topbar-mode button { padding: 4px 7px; }
.keynope-topbar-label { max-width: 180px; overflow: hidden; color: #bfc7cf; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.keynope-topbar-link { width: 180px; min-width: 120px !important; box-sizing: border-box; color: #eee; background: #111; border: 1px solid #555; border-radius: 5px; padding: 5px 7px; font: 12px ui-monospace, SFMono-Regular, Menlo, monospace; }
.keynope-visual-panel { position: fixed; z-index: 90; display: grid; grid-template-columns: max-content 150px; gap: 7px 9px; width: 270px; padding: 10px; box-sizing: border-box; border: 1px solid #5b6168; border-radius: 8px; color: #ddd; background: #202124; box-shadow: 0 12px 32px rgba(0,0,0,.55); }
.keynope-visual-panel[hidden] { display: none; }
.keynope-visual-panel label { align-self: center; color: #aeb4bb; font-size: 11px; }
.keynope-visual-panel select, .keynope-visual-panel input { width: 100%; min-width: 0; box-sizing: border-box; }
.keynope-link-dialog { position: fixed; z-index: 100; width: 360px; padding: 12px; box-sizing: border-box; border: 1px solid #67717b; border-radius: 8px; color: #ddd; background: #202124; box-shadow: 0 14px 38px rgba(0,0,0,.6); font: 12px -apple-system, BlinkMacSystemFont, sans-serif; }
.keynope-link-dialog h3 { margin: 0 0 10px; color: #fff; font-size: 13px; }
.keynope-link-dialog label { display: block; margin: 0 0 8px; color: #aeb4bb; }
.keynope-link-dialog [hidden] { display: none; }
.keynope-link-dialog input, .keynope-link-dialog select { display: block; width: 100%; margin-top: 4px; box-sizing: border-box; padding: 6px; color: #eee; background: #111; border: 1px solid #555; border-radius: 4px; }
.keynope-link-dialog .keynope-editor-actions { margin: 10px 0 0; }
.keynope-input-blocker { position: fixed; inset: 0 0 52px 0; z-index: 55; background: transparent; }
html[data-keynope-timer-active="true"] .keynope-editor-topbar,
html[data-keynope-timer-active="true"] .keynope-editor-slides,
html[data-keynope-timer-active="true"] .keynope-speaker-notes { opacity: .42; filter: grayscale(1); }
html[data-keynope-timer-active="true"] .keynope-canvas-overlay { display: none; }
.keynope-editor-topbar button, .keynope-editor-panel button { color: #eee; background: #292929; border: 1px solid #555; border-radius: 5px; padding: 5px 9px; font: inherit; }
.keynope-editor-topbar button.keynope-icon-button { width: 30px; min-width: 30px; height: 30px; padding: 3px; font-size: 18px; line-height: 1.1; }
.keynope-editor-topbar button.keynope-rotate-button { position: relative; display: inline-grid; place-items: center; padding: 0; overflow: visible; line-height: 1; }
.keynope-rotate-icon { font-size: 27px; line-height: 27px; }
.keynope-rotate-label { position: absolute; left: 50%; bottom: -2px; transform: translateX(-50%); padding: 0 2px; border: 1px solid #59616a; border-radius: 3px; color: #d8dee5; background: #151719; font: 700 5px/8px -apple-system, BlinkMacSystemFont, sans-serif; letter-spacing: .03em; box-shadow: 0 1px 2px rgba(0,0,0,.7); }
.keynope-editor-topbar button.keynope-monochrome-icon { display: inline-grid; place-items: center; padding: 0; color: #fff; font-family: "Apple Symbols", "SF Pro", -apple-system, sans-serif; font-variant-emoji: text; line-height: 1; }
.keynope-monochrome-glyph { display: block; line-height: 1; transform: translateY(1px); }
.keynope-monochrome-icon svg { display: block; width: 20px; height: 20px; fill: currentColor; }
.keynope-editor-topbar button.keynope-history-button { position: relative; display: inline-grid; width: 30px; min-width: 30px; height: 30px; place-items: center; padding: 0; overflow: visible; line-height: 1; }
.keynope-history-icon { font-size: 27px; line-height: 27px; }
.keynope-history-label { position: absolute; left: 50%; bottom: -2px; transform: translateX(-50%); padding: 0 3px; border: 1px solid #59616a; border-radius: 3px; color: #d8dee5; background: #151719; font: 700 6px/8px -apple-system, BlinkMacSystemFont, sans-serif; letter-spacing: .06em; box-shadow: 0 1px 2px rgba(0,0,0,.7); }
.keynope-editor-topbar button.keynope-svg-button { display: inline-grid; width: 30px; height: 30px; place-items: center; padding: 4px; }
.keynope-svg-button svg { display: block; width: 19px; height: 19px; }
.keynope-canvas-shape-kind svg { display: block; width: 18px; height: 18px; }
.keynope-add-shape-menu { position: fixed; z-index: 95; display: grid; grid-template-columns: repeat(2, 54px); gap: 7px; padding: 8px; color: #eee; background: #202124; border: 1px solid #5b6168; border-radius: 8px; box-shadow: 0 12px 32px rgba(0,0,0,.55); }
.keynope-add-shape-menu[hidden] { display: none; }
.keynope-add-shape-menu button { display: grid; width: 54px; height: 48px; place-items: center; padding: 5px; color: inherit; background: #292929; border: 1px solid #555; border-radius: 6px; }
.keynope-add-shape-menu button:hover { border-color: #70b7ff; background: #244766; }
.keynope-add-shape-menu svg { width: 36px; height: 32px; }
.keynope-editor-topbar button.active { border-color: #70b7ff; background: #244766; }
.keynope-editor-topbar select { min-width: 132px; color: #eee; background: #292929; border: 1px solid #555; border-radius: 5px; padding: 5px 9px; font: inherit; }
.keynope-editor-separator { width: 1px; height: 24px; flex: 0 0 auto; margin: 0 3px; background: #444; }
.keynope-editor-panel { position: fixed; top: 0; bottom: 0; z-index: 21; display: none; overflow: auto; box-sizing: border-box; color: #ddd; background: #1d1d1d; font: 12px -apple-system, BlinkMacSystemFont, sans-serif; }
.keynope-editor-slides { left: 0; width: 210px; border-right: 1px solid #444; padding: 8px; }
.keynope-slides-header { display: flex; min-height: 30px; align-items: center; justify-content: space-between; gap: 8px; margin: 0 0 8px; }
.keynope-slides-header h3 { margin: 0; }
.keynope-slides-header button { min-width: 30px; padding: 3px 7px; font-size: 18px; line-height: 1.1; }
.keynope-speaker-notes { position: fixed; left: 210px; right: 0; top: var(--editor-notes-top, 100%); bottom: 52px; z-index: 19; display: none; flex-direction: column; gap: 5px; padding: 7px 10px; box-sizing: border-box; color: #ddd; background: #171717; border-top: 1px solid #444; font: 12px -apple-system, BlinkMacSystemFont, sans-serif; }
.keynope-slide-context { position: fixed; z-index: 60; display: none; width: 270px; max-height: calc(100vh - 20px); overflow: auto; box-sizing: border-box; padding: 10px; color: #ddd; background: #202124; border: 1px solid #5b6168; border-radius: 8px; box-shadow: 0 12px 32px rgba(0,0,0,.55); font: 12px -apple-system, BlinkMacSystemFont, sans-serif; }
.keynope-slide-context.open { display: block; }
.keynope-slide-context h3 { margin: 0 0 9px; color: #fff; font-size: 13px; }
.keynope-slide-context button { color: #eee; background: #292929; border: 1px solid #555; border-radius: 5px; padding: 5px 9px; font: inherit; }
.keynope-slide-context select { color: #eee; background: #292929; border: 1px solid #555; border-radius: 5px; padding: 5px 7px; font: inherit; }
.keynope-context-visual { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: 7px 9px; margin: 2px 0 10px; padding: 9px 0; border-top: 1px solid #41464b; border-bottom: 1px solid #41464b; }
.keynope-context-visual h4 { grid-column: 1 / -1; margin: 0 0 2px; color: #fff; font-size: 12px; }
.keynope-context-visual label { align-self: center; color: #aeb4bb; font-size: 11px; }
.keynope-context-visual select, .keynope-context-visual input { width: 100%; min-width: 0; box-sizing: border-box; }
.keynope-slide-context .keynope-editor-field input, .keynope-slide-context .keynope-editor-field select { width: 100%; box-sizing: border-box; color: #eee; background: #111; border: 1px solid #555; border-radius: 4px; padding: 5px; font: 12px ui-monospace, SFMono-Regular, Menlo, monospace; }
.keynope-speaker-notes label { color: #aeb4bb; font-weight: 600; }
.keynope-speaker-notes textarea { min-height: 0; flex: 1; resize: none; box-sizing: border-box; padding: 7px; color: #eee; background: #0e0e0e; border: 1px solid #555; border-radius: 5px; font: 13px ui-monospace, SFMono-Regular, Menlo, monospace; }
html[data-keynope-app="true"][data-keynope-notes="true"] .keynope-speaker-notes { display: flex; }
.keynope-slide-item { display: block; width: 100%; margin: 0 0 6px; text-align: left; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.keynope-slide-item.active { border-color: #70b7ff; background: #244766; }
.keynope-editor-section { margin: 0 0 14px; padding: 0 0 12px; border-bottom: 1px solid #3c3c3c; }
.keynope-editor-section h3 { margin: 0 0 7px; color: #fff; font-size: 12px; text-transform: uppercase; letter-spacing: .04em; }
.keynope-editor-actions { display: flex; flex-wrap: wrap; gap: 5px; margin: 0 0 10px; }
.keynope-editor-actions button.danger { color: #ffb0aa; border-color: #8d4741; }
button.keynope-outline-large { min-width: 34px; padding-top: 1px; padding-bottom: 1px; font-size: 26px; line-height: 1; }
button.keynope-shape-outline-button { display: inline-flex; align-items: center; justify-content: center; text-indent: 0; }
.keynope-shape-outline-symbol { display: inline-block; transform: translateX(0); }
.keynope-shape-outline-circle { transform: translateX(-4px); }
.keynope-shape-outline-diamond { transform: translateX(4px); }
.keynope-editor-help { margin: 0 0 10px; color: #949ba3; line-height: 1.45; }
.keynope-editor-subheading { margin: 10px 0 6px; color: #aeb4bb; font-weight: 600; }
.keynope-editor-field { display: block; margin: 0 0 7px; }
.keynope-editor-field span { display: block; margin-bottom: 3px; color: #aaa; }
.keynope-editor-field input, .keynope-editor-field textarea, .keynope-editor-field select { width: 100%; box-sizing: border-box; color: #eee; background: #111; border: 1px solid #555; border-radius: 4px; padding: 5px; font: 12px ui-monospace, SFMono-Regular, Menlo, monospace; }
.keynope-editor-field textarea { min-height: 70px; resize: vertical; }
.keynope-element-item { display: flex; width: 100%; justify-content: space-between; margin-bottom: 4px; }
.keynope-element-item.active { border-color: #70b7ff; background: #244766; }
.keynope-canvas-overlay { position: absolute; left: 50%; top: 50%; z-index: 12; transform: translate(-50%, -50%); pointer-events: none; }
.keynope-canvas-element { position: absolute; min-width: 22px; min-height: 22px; box-sizing: border-box; border: 1px solid transparent; background: transparent; pointer-events: auto; cursor: move; }
.keynope-canvas-element:hover { border-color: rgba(112,183,255,.65); }
.keynope-canvas-element.active { border: 2px solid #70b7ff; box-shadow: 0 0 0 1px #111; }
.keynope-resize-handle { position: absolute; z-index: 2; width: 9px; height: 9px; border: 1px solid #111; background: #70b7ff; }
.keynope-resize-handle.nw { left: -6px; top: -6px; cursor: nwse-resize; }
.keynope-resize-handle.ne { right: -6px; top: -6px; cursor: nesw-resize; }
.keynope-resize-handle.sw { left: -6px; bottom: -6px; cursor: nesw-resize; }
.keynope-resize-handle.se { right: -6px; bottom: -6px; cursor: nwse-resize; }
.keynope-colour-tool { position: relative; overflow: hidden; display: grid; width: 28px; min-width: 28px; place-items: center; padding: 4px 0; box-sizing: border-box; border-radius: 4px; color: #e9edf1; background: #343940; font: 11px -apple-system, BlinkMacSystemFont, sans-serif; }
.keynope-colour-tool::after { content: ''; position: absolute; left: 7px; right: 7px; bottom: 4px; height: 3px; border-radius: 2px; background: var(--keynope-tool-colour, #fff); pointer-events: none; }
.keynope-colour-tool input { position: absolute; inset: 0; width: 100%; height: 100%; opacity: 0; cursor: pointer; }
.keynope-inline-capture { position: absolute; left: 0; top: 0; z-index: 30; width: 1px; height: 1px; margin: 0; padding: 0; opacity: 0; border: 0; resize: none; pointer-events: none; }
html[data-keynope-app="true"] .keynope-editor-topbar, html[data-keynope-app="true"] .keynope-editor-panel { display: flex; }
html[data-keynope-app="true"] .keynope-editor-panel { display: block; }
html[data-keynope-app="true"] #presenter-canvas, html[data-keynope-app="true"] #link-layer, html[data-keynope-app="true"] .keynope-canvas-overlay { top: var(--editor-canvas-top, 50%); transform: translateX(-50%); }
html[data-keynope-presenter="true"] .terminal-layer { visibility: hidden; pointer-events: none; }
html[data-keynope-presenter="true"] #presenter-canvas { display: block; }
#effect-layer { z-index: 1; pointer-events: none; }
#content-layer { z-index: 2; }
#chrome-layer { z-index: 3; pointer-events: none; }
.line { position: absolute; left: calc(var(--x) * 1ch); top: calc(var(--y) * 1em); white-space: pre; letter-spacing: 0; pointer-events: none; }
.link-underline { position: absolute; left: calc(var(--x) * 1ch); top: calc(var(--y) * 1em); white-space: pre; pointer-events: none; }
.link-hit { position: absolute; left: calc(var(--x) * 1ch); top: calc(var(--y) * 1em); width: calc(var(--w) * 1ch); height: calc(var(--h) * 1em); cursor: pointer; background: transparent; z-index: 2; }
.page-no { position: absolute; right: 0; bottom: 0; white-space: pre; color: #fff; }
</style>
</head>
<body>
<div id="stage"><canvas id="presenter-canvas"></canvas><div id="link-layer"></div><div id="effect-layer" class="terminal-layer"></div><div id="content-layer" class="terminal-layer"></div><div id="chrome-layer" class="terminal-layer"></div></div>
`
	if presenter {
		html = strings.Replace(html, `<html lang="en">`, `<html lang="en" data-keynope-presenter="true">`, 1)
		html = strings.Replace(html, "<head>", "<head>\n<script>window.KEYNOPE_PRESENTER = true;</script>", 1)
	}
	title := preserved.Title
	if title == "" {
		title = "<title>Keynope Export</title>"
	}
	var titleAndMeta strings.Builder
	titleAndMeta.WriteString(title)
	for _, meta := range preserved.Metas {
		titleAndMeta.WriteByte('\n')
		titleAndMeta.WriteString(meta)
	}
	html = strings.Replace(html, "<title>Keynope Export</title>", titleAndMeta.String(), 1)
	if len(preserved.Scripts) > 0 {
		html = strings.Replace(html, "</head>", strings.Join(preserved.Scripts, "\n")+"\n</head>", 1)
	}
	return html
}

func exportHTMLSuffix() string {
	return `<script>
const deck = JSON.parse(document.getElementById('keynope-data').textContent);
let pageIndex = 0;
const stage = document.getElementById('stage');
const presenterCanvas = document.getElementById('presenter-canvas');
const presenterContext = presenterCanvas.getContext('2d');
const linkLayer = document.getElementById('link-layer');
const presenterTestCardBuffer = document.createElement('canvas');
const presenterTestCardBufferContext = presenterTestCardBuffer.getContext('2d');
const effectLayer = document.getElementById('effect-layer');
let contentLayer = document.getElementById('content-layer');
const chromeLayer = document.getElementById('chrome-layer');
const presenterSurface = new URLSearchParams(location.search).get('keynopeSurface') || 'external';
const presenterMainSurface = presenterSurface === 'main';
const keynopeAppSurface = presenterSurface === 'app';
// Standalone HTML and live presentation share this renderer. Presenter state
// synchronization remains separately gated by KEYNOPE_PRESENTER.
const keynopeCanvasRenderer = true;
let renderEditorCanvasOverlay = () => {};
let editorSpeakerNotesVisible = false;
let refreshEditorPresenterControls = () => {};
let editorCanvasCaret = null;
let editorExportConfirmation = null;
let keynopeEditorMasterMode = false;
if (keynopeAppSurface) {
  document.documentElement.setAttribute('data-keynope-app', 'true');
  document.documentElement.setAttribute('data-keynope-notes', 'true');
  document.title = 'Keynope';
}
let frame = 0;
let contentAnimationCache = new Map();
let canvasCell = 1;
let canvasCharWidth = 1;
let canvasFont = '';
const canvasFontFamily = 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace';
function fitCanvasFontToGrid() {
  let fontSize = canvasCell;
  canvasFont = fontSize + 'px ' + canvasFontFamily;
  presenterContext.font = canvasFont;
  const measuredWidth = Math.max(1, presenterContext.measureText('M').width);
  const safeCellWidth = Math.max(1, canvasCharWidth * 0.98);
  if (measuredWidth > safeCellWidth) {
    fontSize *= safeCellWidth / measuredWidth;
    canvasFont = fontSize + 'px ' + canvasFontFamily;
    presenterContext.font = canvasFont;
  }
}
function sizeCanvasToAspect(availableWidth, availableHeight, targetAspect) {
  let cssWidth = Math.max(1, availableWidth);
  let cssHeight = cssWidth / targetAspect;
  if (cssHeight > availableHeight) {
    cssHeight = Math.max(1, availableHeight);
    cssWidth = cssHeight * targetAspect;
  }
  cssWidth = Math.max(1, Math.floor(cssWidth));
  cssHeight = Math.max(1, Math.floor(cssHeight));
  const ratio = Math.max(1, window.devicePixelRatio || 1);
  presenterCanvas.width = Math.max(1, Math.round(cssWidth * ratio));
  presenterCanvas.height = Math.max(1, Math.round(cssHeight * ratio));
  presenterCanvas.style.width = cssWidth + 'px';
  presenterCanvas.style.height = cssHeight + 'px';
  linkLayer.style.width = presenterCanvas.style.width;
  linkLayer.style.height = presenterCanvas.style.height;
  canvasCell = presenterCanvas.height / Math.max(1, deck.rows);
  canvasCharWidth = presenterCanvas.width / Math.max(1, deck.cols);
  fitCanvasFontToGrid();
  document.documentElement.style.setProperty('--cell', canvasCell + 'px');
  return {width: cssWidth, height: cssHeight};
}
function sizeEditorCanvas(stageRect) {
  const targetAspect = 16 / 9;
  const topChromeHeight = 52;
  const bottomChromeHeight = 52;
  const notesHeight = editorSpeakerNotesVisible ? 132 : 0;
  const notesGap = editorSpeakerNotesVisible ? 8 : 0;
  const availableHeight = Math.max(1, stageRect.height - topChromeHeight - bottomChromeHeight - notesHeight - notesGap);
  const canvasSize = sizeCanvasToAspect(stageRect.width, availableHeight, targetAspect);
  const canvasTop = topChromeHeight + Math.max(0, (availableHeight - canvasSize.height) / 2);
  document.documentElement.style.setProperty('--editor-canvas-top', canvasTop + 'px');
  document.documentElement.style.setProperty('--editor-notes-top', (canvasTop + canvasSize.height + notesGap) + 'px');
}
function resize() {
  document.documentElement.style.setProperty('--cols', deck.cols);
  document.documentElement.style.setProperty('--rows', deck.rows);
  if (keynopeAppSurface) {
    const stageRect = stage.getBoundingClientRect();
    sizeEditorCanvas(stageRect);
    return;
  }
  if (keynopeCanvasRenderer) {
    sizeCanvasToAspect(innerWidth, innerHeight, 16 / 9);
    return;
  }
  let low = 1, high = Math.max(8, innerHeight);
  for (let i = 0; i < 18; i++) {
    const mid = (low + high) / 2;
    document.documentElement.style.setProperty('--cell', mid + 'px');
    let width, height;
    if (window.KEYNOPE_PRESENTER) {
      presenterContext.font = mid + 'px ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace';
      width = deck.cols * Math.max(1, presenterContext.measureText('M').width);
      height = deck.rows * mid;
    } else {
      const rect = contentLayer.getBoundingClientRect();
      width = rect.width;
      height = rect.height;
    }
    if (width <= innerWidth && height <= innerHeight) low = mid;
    else high = mid;
  }
  // Keep the fractional fit found above. Rounding down to a whole CSS pixel
  // leaves as much as one row-height of unused space in embedded web views.
  const cell = Math.max(1, low);
  document.documentElement.style.setProperty('--cell', cell + 'px');
  canvasCell = cell;
  canvasFont = cell + 'px ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace';
  presenterContext.font = canvasFont;
  canvasCharWidth = Math.max(1, presenterContext.measureText('M').width);
  presenterCanvas.width = Math.ceil(deck.cols * canvasCharWidth);
  presenterCanvas.height = Math.ceil(deck.rows * canvasCell);
  presenterCanvas.style.width = presenterCanvas.width + 'px';
  presenterCanvas.style.height = presenterCanvas.height + 'px';
}
function esc(s) {
  return s.replace(/[&<>"]/g, ch => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[ch]));
}
function label(page) {
  let n = String(page.slide + 1);
  if (page.pageCount > 1) n += String.fromCharCode(97 + page.page);
  return n + '/' + page.slideCount;
}
function renderLines(lines) {
  let html = '';
  for (const line of lines || []) {
    if (line.role === 'transparent-text' || line.role === 'transparent-image') continue;
    for (const part of line.parts) {
      const bg = part.background ? ';background-color:' + part.background : '';
      html += '<span class="line" style="--x:' + part.col + ';--y:' + line.row + ';color:' + part.color + bg + '">' + esc(part.text) + '</span>';
    }
  }
  return html;
}
function drawCanvasLines(lines) {
  presenterContext.font = canvasFont;
  presenterContext.textBaseline = 'top';
  const textOutlineElements = textOutlineElementMap(lines);
  const textOutlineMasks = canvasTextPixelOutlineMasks(lines, textOutlineElements);
  const drawnTextOutlines = new Set();
  for (const line of lines || []) {
    if (line.role === 'transparent-text' || line.role === 'transparent-image') continue;
    if (line.role === 'outline') {
      if (textOutlineElements.has(line.element)) continue;
      drawCanvasOutlineLine(line);
      continue;
    }
    if (textOutlineElements.has(line.element) && !drawnTextOutlines.has(line.element)) {
      drawCanvasTextPixelOutlineMask(textOutlineMasks.get(line.element), textOutlineElements.get(line.element));
      drawnTextOutlines.add(line.element);
    }
    for (const part of line.parts || []) {
      const text = part.text || '';
      if (!text) continue;
      const x = part.col * canvasCharWidth;
      const y = line.row * canvasCell;
      if (part.background) {
        presenterContext.fillStyle = part.background;
        drawCanvasCellRun(part.col, line.row, [...text].length);
      }
      presenterContext.fillStyle = part.color || '#f3efe0';
      if (!drawCanvasBlockGlyphs(text, part.col, line.row, part.color || '#f3efe0')) {
        presenterContext.fillText(text, x, y);
      }
    }
  }
  drawEditorCanvasCaret(lines);
}
function drawEditorCanvasCaret(lines) {
  if (!keynopeAppSurface || !editorCanvasCaret || (performance.now() - editorCanvasCaret.started) % 900 >= 650) return;
  const matching = (lines || []).filter(line => line.element === editorCanvasCaret.element);
  if (!matching.length) return;
  let minX = deck.cols, minY = deck.rows, maxX = 0, maxY = 0;
  for (const line of matching) {
    for (const part of line.parts || []) {
      const width = [...(part.text || '')].length;
      minX = Math.min(minX, part.col || 0);
      maxX = Math.max(maxX, (part.col || 0) + width);
      minY = Math.min(minY, line.row || 0);
      maxY = Math.max(maxY, (line.row || 0) + 1);
    }
  }
  if (minX >= deck.cols || minY >= deck.rows) return;
  const text = editorCanvasCaret.text || '';
  const cursor = Math.max(0, Math.min(text.length, editorCanvasCaret.cursor || 0));
  const before = text.slice(0, cursor).split('\n');
  const rawLines = text.split('\n');
  const lineIndex = Math.max(0, before.length - 1);
  const column = [...(before[before.length - 1] || '')].length;
  const rawLineLength = Math.max(1, [...(rawLines[lineIndex] || '')].length);
  const renderedWidth = Math.max(1, maxX - minX);
  const renderedHeight = Math.max(1, maxY - minY);
  const caretCol = Math.min(deck.cols - 1, maxX, minX + column / rawLineLength * renderedWidth);
  const caretRow = Math.min(maxY - 1, minY + Math.ceil((lineIndex + 1) / Math.max(1, rawLines.length) * renderedHeight) - 1);
  const caretCells = Math.max(1, renderedWidth / rawLineLength);
  const x = Math.floor(caretCol * canvasCharWidth);
  const y = Math.max(0, Math.ceil((caretRow + 1) * canvasCell) - Math.max(2, Math.round(window.devicePixelRatio || 1)));
  presenterContext.fillStyle = '#70b7ff';
  presenterContext.fillRect(x, y, Math.max(2, Math.ceil(caretCells * canvasCharWidth)), Math.max(2, Math.round(window.devicePixelRatio || 1)));
}
function textOutlineElementMap(lines) {
  const outlines = new Map();
  const textElements = new Set();
  for (const line of lines || []) {
    if (line.role === 'outline') {
      const color = line.parts && line.parts.length ? (line.parts[0].color || '#d8d8d8') : '#d8d8d8';
      outlines.set(line.element, color);
      continue;
    }
    if (line.element == null || !['heading', 'body', 'code'].includes(line.role)) continue;
    for (const part of line.parts || []) {
      if (textContainsBlockGlyphs(part.text || '')) {
        textElements.add(line.element);
        break;
      }
    }
  }
  const out = new Map();
  for (const [element, color] of outlines.entries()) {
    if (textElements.has(element)) out.set(element, color);
  }
  return out;
}
function textContainsBlockGlyphs(text) {
  for (const ch of [...text]) {
    if (blockGlyphMask(ch) > 0) return true;
  }
  return false;
}
function canvasTextPixelOutlineMasks(lines, outlineElements) {
  const masks = new Map();
  if (!outlineElements.size) return masks;
  for (const line of lines || []) {
    if (!outlineElements.has(line.element) || !['heading', 'body', 'code'].includes(line.role)) continue;
    let mask = masks.get(line.element);
    if (!mask) {
      mask = new Set();
      masks.set(line.element, mask);
    }
    for (const part of line.parts || []) {
      const chars = [...(part.text || '')];
      for (let i = 0; i < chars.length; i++) {
        addBlockGlyphMaskPixels(mask, part.col + i, line.row, blockGlyphMask(chars[i]));
      }
    }
  }
  return masks;
}
function drawCanvasTextPixelOutlineMask(mask, color) {
  if (!mask || !mask.size) return;
  const outline = new Set();
  for (const key of mask) {
    const [xText, yText] = key.split(':');
    const x = Number(xText), y = Number(yText);
    for (let dy = -1; dy <= 1; dy++) {
      for (let dx = -1; dx <= 1; dx++) {
        if (dx === 0 && dy === 0) continue;
        const next = (x + dx) + ':' + (y + dy);
        if (!mask.has(next)) outline.add(next);
      }
    }
  }
  presenterContext.fillStyle = color || '#d8d8d8';
  for (const key of outline) {
    const [xText, yText] = key.split(':');
    drawCanvasQuarterPixel(Number(xText), Number(yText));
  }
}
function addBlockGlyphMaskPixels(out, col, row, mask) {
  if (mask <= 0) return;
  const x = col * 2, y = row * 2;
  if (mask & 1) out.add(x + ':' + y);
  if (mask & 2) out.add((x + 1) + ':' + y);
  if (mask & 4) out.add(x + ':' + (y + 1));
  if (mask & 8) out.add((x + 1) + ':' + (y + 1));
}
function drawCanvasQuarterPixel(qx, qy) {
  if (qx < 0 || qy < 0 || qx >= deck.cols * 2 || qy >= deck.rows * 2) return;
  const col = Math.floor(qx / 2), row = Math.floor(qy / 2);
  const x1 = Math.floor((col + (qx % 2) * 0.5) * canvasCharWidth);
  const y1 = Math.floor((row + (qy % 2) * 0.5) * canvasCell);
  const x2 = Math.ceil((col + (qx % 2 ? 1 : 0.5)) * canvasCharWidth);
  const y2 = Math.ceil((row + (qy % 2 ? 1 : 0.5)) * canvasCell);
  presenterContext.fillRect(x1, y1, Math.max(1, x2 - x1 + 1), Math.max(1, y2 - y1 + 1));
}
function drawCanvasBlockGlyphs(text, col, row, color) {
  const chars = [...text];
  let handled = false;
  presenterContext.fillStyle = color;
  for (let i = 0; i < chars.length; i++) {
    const mask = blockGlyphMask(chars[i]);
    if (mask < 0) continue;
    handled = true;
    drawCanvasBlockGlyph(col + i, row, mask);
  }
  if (!handled) return false;
  let run = '', runCol = col;
  const flush = () => {
    if (!run) return;
    presenterContext.fillText(run, runCol * canvasCharWidth, row * canvasCell);
    run = '';
  };
  for (let i = 0; i < chars.length; i++) {
    if (blockGlyphMask(chars[i]) >= 0) {
      flush();
      runCol = col + i + 1;
      continue;
    }
    if (!run) runCol = col + i;
    run += chars[i];
  }
  flush();
  return true;
}
function blockGlyphMask(ch) {
  switch (ch) {
    case ' ': return 0;
    case '▘': return 1;
    case '▝': return 2;
    case '▀': return 3;
    case '▖': return 4;
    case '▌': return 5;
    case '▞': return 6;
    case '▛': return 7;
    case '▗': return 8;
    case '▚': return 9;
    case '▐': return 10;
    case '▜': return 11;
    case '▄': return 12;
    case '▙': return 13;
    case '▟': return 14;
    case '█': return 15;
    default: return -1;
  }
}
function drawCanvasBlockGlyph(col, row, mask) {
  if (mask === 0) return;
  if (mask === 15) {
    drawCanvasCellRun(col, row, 1);
    return;
  }
  const x1 = Math.floor(col * canvasCharWidth);
  const y1 = Math.floor(row * canvasCell);
  const xMid = Math.floor((col + 0.5) * canvasCharWidth);
  const yMid = Math.floor((row + 0.5) * canvasCell);
  const x2 = Math.ceil((col + 1) * canvasCharWidth);
  const y2 = Math.ceil((row + 1) * canvasCell);
  if (mask & 1) presenterContext.fillRect(x1, y1, Math.max(1, xMid - x1 + 1), Math.max(1, yMid - y1 + 1));
  if (mask & 2) presenterContext.fillRect(xMid, y1, Math.max(1, x2 - xMid + 1), Math.max(1, yMid - y1 + 1));
  if (mask & 4) presenterContext.fillRect(x1, yMid, Math.max(1, xMid - x1 + 1), Math.max(1, y2 - yMid + 1));
  if (mask & 8) presenterContext.fillRect(xMid, yMid, Math.max(1, x2 - xMid + 1), Math.max(1, y2 - yMid + 1));
}
function drawCanvasOutlineLine(line) {
  for (const part of line.parts || []) {
    const text = part.text || '';
    if (!text) continue;
    const chars = [...text];
    presenterContext.fillStyle = part.color || '#d8d8d8';
    for (let i = 0; i < chars.length; i++) {
      if (chars[i] === ' ' || chars[i] === '\u00a0') continue;
      drawCanvasCellRun(part.col + i, line.row, 1);
    }
  }
}
function drawCanvasCellRun(col, row, len) {
  const x1 = Math.floor(col * canvasCharWidth);
  const y1 = Math.floor(row * canvasCell);
  const x2 = Math.ceil((col + len) * canvasCharWidth);
  const y2 = Math.ceil((row + 1) * canvasCell);
  presenterContext.fillRect(x1, y1, Math.max(1, x2 - x1 + 1), Math.max(1, y2 - y1 + 1));
}
function transparencyCellMap(lines) {
  const cells = new Map();
  for (const line of lines || []) {
    for (const part of line.parts || []) {
      const text = part.text || '';
      for (let i = 0; i < text.length; i++) {
        cells.set(line.row + ':' + (part.col + i), part.background || '#000');
      }
    }
  }
  return cells;
}
function darkenCSSForTransparency(color) {
  const rgb = parseCSSColor(color || '#f3efe0');
  if (!rgb) return color || '#f3efe0';
  const luma = (299 * rgb[0] + 587 * rgb[1] + 114 * rgb[2]) / 255000;
  const factor = 0.8 - Math.pow(Math.max(0, Math.min(1, luma)), 3) * 0.6;
  return 'rgb(' + rgb.map(v => Math.max(0, Math.min(255, Math.round(v * factor)))).join(',') + ')';
}
function parseCSSColor(color) {
  const hex = /^#([0-9a-f]{6})$/i.exec(color || '');
  if (hex) return [parseInt(hex[1].slice(0, 2), 16), parseInt(hex[1].slice(2, 4), 16), parseInt(hex[1].slice(4, 6), 16)];
  const rgb = /^rgb\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)\s*\)$/i.exec(color || '');
  if (rgb) return [Number(rgb[1]), Number(rgb[2]), Number(rgb[3])];
  return null;
}
function pushPart(parts, col, text, color, background) {
  if (!text) return;
  const last = parts[parts.length - 1];
  if (last && last.col + last.text.length === col && last.color === color && (last.background || '') === (background || '')) {
    last.text += text;
  } else {
    const part = {col, text, color};
    if (background) part.background = background;
    parts.push(part);
  }
}
function applyBackdropTransparency(lines, transparency) {
  if (!transparency || !transparency.length) return lines || [];
  const cells = transparencyCellMap(transparency);
  if (!cells.size) return lines || [];
  const occupied = new Set();
  const out = [];
  for (const line of lines || []) {
    const parts = [];
    for (const part of line.parts || []) {
      const text = part.text || '';
      for (let i = 0; i < text.length; i++) {
        const col = part.col + i;
        const key = line.row + ':' + col;
        const bg = cells.get(key);
        if (bg) {
          occupied.add(key);
          pushPart(parts, col, text[i], darkenCSSForTransparency(part.color), bg);
        } else {
          pushPart(parts, col, text[i], part.color, part.background || '');
        }
      }
    }
    if (parts.length) out.push({...line, parts});
  }
  const fillerRows = new Map();
  for (const [key, bg] of cells.entries()) {
    if (occupied.has(key)) continue;
    const [rowText, colText] = key.split(':');
    const row = Number(rowText), col = Number(colText);
    if (!fillerRows.has(row)) fillerRows.set(row, []);
    pushPart(fillerRows.get(row), col, ' ', '#000000', bg);
  }
  for (const [row, parts] of fillerRows.entries()) {
    if (parts.length) out.push({row, role: 'transparent', parts});
  }
  return out;
}
function drawCanvasBackdropTransparency(lines, transparency) {
  drawCanvasLines(applyBackdropTransparency(lines, transparency));
}
function drawCanvasPageLabel(page) {
	if (page.hideChromePageNumber) return;
  const text = label(page);
  presenterContext.font = canvasFont;
  presenterContext.textBaseline = 'top';
  presenterContext.fillStyle = '#ffffff';
  presenterContext.fillText(text, Math.max(0, (deck.cols - [...text].length) * canvasCharWidth), Math.max(0, (deck.rows - 1) * canvasCell));
}
const timerFontGlyphs = {
  '0': ['  ████  ', ' ██  ██ ', ' ██ ███ ', ' ██████ ', ' ███ ██ ', ' ██  ██ ', '  ████  ', '        '],
  '1': ['   ██   ', '  ███   ', '   ██   ', '   ██   ', '   ██   ', '   ██   ', ' ██████ ', '        '],
  '2': ['  ████  ', ' ██  ██ ', '     ██ ', '   ███  ', '  ██    ', ' ██  ██ ', ' ██████ ', '        '],
  '3': ['  ████  ', ' ██  ██ ', '     ██ ', '   ███  ', '     ██ ', ' ██  ██ ', '  ████  ', '        '],
  '4': ['    ███ ', '   ████ ', '  ██ ██ ', ' ██  ██ ', ' ███████', '     ██ ', '     ██ ', '        '],
  '5': [' ██████ ', ' ██     ', ' █████  ', '     ██ ', '     ██ ', ' ██  ██ ', '  ████  ', '        '],
  '6': ['   ███  ', '  ██    ', ' ██     ', ' █████  ', ' ██  ██ ', ' ██  ██ ', '  ████  ', '        '],
  '7': [' ██████ ', ' ██  ██ ', '     ██ ', '    ██  ', '   ██   ', '   ██   ', '   ██   ', '        '],
  '8': ['  ████  ', ' ██  ██ ', ' ██  ██ ', '  ████  ', ' ██  ██ ', ' ██  ██ ', '  ████  ', '        '],
  '9': ['  ████  ', ' ██  ██ ', ' ██  ██ ', '  █████ ', '     ██ ', '    ██  ', '  ███   ', '        '],
  ':': ['        ', '        ', '   ██   ', '        ', '        ', '   ██   ', '        ', '        ']
};
function presenterTimerText() {
  if (!presenterTimerMode) return {text: '', done: false};
  if (presenterTimerMode === 'config') {
    let input = String(presenterTimerInput || '');
    while (input.length < 4) input = '0' + input;
    if (input.length > 4) input = input.slice(input.length - 4);
    return {text: input.slice(0, 2) + ':' + input.slice(2), done: false};
  }
  const remaining = Math.max(0, Math.ceil((presenterTimerEndMS - Date.now()) / 1000));
  const minutes = Math.min(99, Math.floor(remaining / 60));
  const seconds = remaining <= 99 * 60 ? remaining % 60 : 59;
  return {text: String(minutes).padStart(2, '0') + ':' + String(seconds).padStart(2, '0'), done: remaining <= 0};
}
function drawPresenterTimer() {
  if (!presenterTimerMode) return;
  const timer = presenterTimerText();
  if (!timer.text) return;
  if (timer.done && Math.floor(Date.now() / 400) % 2 === 0) return;
  const chars = [...timer.text];
  const cell = Math.max(3, Math.floor(Math.min(presenterCanvas.width / 68, presenterCanvas.height / 18)));
  const gap = Math.max(2, Math.floor(cell * 0.75));
  const totalW = presenterTimerTextWidth(timer.text, cell, gap);
  const glyphH = 8 * cell;
  const x0 = Math.floor((presenterCanvas.width - totalW) / 2), y0 = Math.floor((presenterCanvas.height - glyphH) / 2);
  presenterContext.fillStyle = 'rgba(0,0,0,0.72)';
  presenterContext.fillRect(x0 - cell, y0 - cell, totalW + cell * 2, glyphH + cell * 2);
  let x = x0;
  let digitIndex = 0;
  const typed = Math.max(0, Math.min(4, String(presenterTimerInput || '').length));
  const typedFrom = 4 - typed;
  for (const ch of chars) {
    const color = presenterTimerMode === 'config'
      ? (ch !== ':' && digitIndex >= typedFrom ? '#55ffff' : '#666666')
      : (timer.done ? '#ffffff' : '#ff5555');
    drawPresenterTimerGlyph(ch, x, y0, cell, color);
    x += presenterTimerGlyphWidth(ch, cell) + gap;
    if (ch !== ':') digitIndex++;
  }
}
function presenterTimerGlyphWidth(ch, cell) {
  const glyph = timerFontGlyphs[ch] || timerFontGlyphs['0'];
  return Math.max(...glyph.map(row => [...row].length)) * cell;
}
function presenterTimerTextWidth(text, cell, gap) {
  const chars = [...text];
  return chars.reduce((sum, ch, index) => sum + (index ? gap : 0) + presenterTimerGlyphWidth(ch, cell), 0);
}
function drawPresenterTimerGlyph(ch, x, y, cell, color) {
  const glyph = timerFontGlyphs[ch] || timerFontGlyphs['0'];
  presenterContext.fillStyle = color;
  for (let row = 0; row < glyph.length; row++) {
    const chars = [...glyph[row]];
    for (let col = 0; col < chars.length; col++) {
      drawPresenterTimerBlock(chars[col], x + col * cell, y + row * cell, cell);
    }
  }
}
function drawPresenterTimerBlock(ch, x, y, cell) {
  const mask = blockGlyphMask(ch);
  if (mask <= 0) return;
  const half = cell / 2;
  for (let bit = 0; bit < 4; bit++) {
    if (!(mask & (1 << bit))) continue;
    const qx = bit % 2, qy = Math.floor(bit / 2);
    const x1 = Math.floor(x + qx * half), y1 = Math.floor(y + qy * half);
    const x2 = Math.ceil(x + (qx + 1) * half), y2 = Math.ceil(y + (qy + 1) * half);
    presenterContext.fillRect(x1, y1, Math.max(1, x2 - x1), Math.max(1, y2 - y1));
  }
}
const editorExportGlyphs = {
  E:['11111','10000','10000','11110','10000','10000','11111'],
  X:['10001','10001','01010','00100','01010','10001','10001'],
  P:['11110','10001','10001','11110','10000','10000','10000'],
  O:['01110','10001','10001','10001','10001','10001','01110'],
  R:['11110','10001','10001','11110','10100','10010','10001'],
  T:['11111','00100','00100','00100','00100','00100','00100'],
  D:['11110','10001','10001','10001','10001','10001','11110']
};
function showEditorExportConfirmation() {
  if (!keynopeAppSurface) return;
  editorExportConfirmation = {started: performance.now(), duration: 1500};
  const animate = () => {
    if (!editorExportConfirmation) return;
    if (performance.now() - editorExportConfirmation.started >= editorExportConfirmation.duration) editorExportConfirmation = null;
    drawFrame();
    if (editorExportConfirmation) requestAnimationFrame(animate);
  };
  requestAnimationFrame(animate);
}
function drawEditorExportConfirmation() {
  if (!keynopeAppSurface || !editorExportConfirmation) return;
  const elapsed = performance.now() - editorExportConfirmation.started;
  const progress = Math.max(0, Math.min(1, elapsed / editorExportConfirmation.duration));
  const fade = progress < .48 ? 1 : Math.pow(1 - (progress - .48) / .52, 2);
  const text = 'EXPORTED';
  const block = Math.max(2, Math.floor(Math.min(presenterCanvas.width / 66, presenterCanvas.height / 24)));
  const glyphWidth = 5 * block, glyphGap = 2 * block;
  const textWidth = text.length * glyphWidth + (text.length - 1) * glyphGap;
  const textHeight = 7 * block;
  const padX = 3 * block, padY = 2 * block;
  const scale = 1 + progress * .035;
  const x = (presenterCanvas.width - textWidth) / 2;
  const y = (presenterCanvas.height - textHeight) / 2 - progress * 12;
  presenterContext.save();
  presenterContext.globalAlpha = fade;
  presenterContext.translate(presenterCanvas.width / 2, presenterCanvas.height / 2);
  presenterContext.scale(scale, scale);
  presenterContext.translate(-presenterCanvas.width / 2, -presenterCanvas.height / 2);
  presenterContext.fillStyle = 'rgba(3,18,9,.92)';
  presenterContext.strokeStyle = '#22cc66';
  presenterContext.lineWidth = Math.max(2, block * .45);
  presenterContext.fillRect(x - padX, y - padY, textWidth + padX * 2, textHeight + padY * 2);
  presenterContext.strokeRect(x - padX, y - padY, textWidth + padX * 2, textHeight + padY * 2);
  let cursor = x;
  presenterContext.lineWidth = Math.max(1, block * .38);
  for (const ch of text) {
    const glyph = editorExportGlyphs[ch];
    for (let row = 0; row < glyph.length; row++) for (let col = 0; col < glyph[row].length; col++) {
      if (glyph[row][col] !== '1') continue;
      const px = cursor + col * block, py = y + row * block;
      presenterContext.fillStyle = '#00aa55';
      presenterContext.strokeStyle = '#77ff99';
      presenterContext.fillRect(px, py, block, block);
      presenterContext.strokeRect(px, py, block, block);
    }
    cursor += glyphWidth + glyphGap;
  }
  presenterContext.restore();
}
function drawPresenterTestCard() {
  presenterCanvas.style.display = 'block';
  effectLayer.style.display = 'none';
  contentLayer.style.display = 'none';
  chromeLayer.style.display = 'none';
  const w = presenterCanvas.width, h = presenterCanvas.height;
  presenterContext.globalAlpha = 1;
  presenterContext.clearRect(0, 0, w, h);
  const rect = (color, x, y, width, height) => {
    presenterContext.fillStyle = color;
    presenterContext.fillRect(Math.floor(x * w), Math.floor(y * h), Math.ceil(width * w), Math.ceil(height * h));
  };

  presenterContext.fillStyle = '#bdbdbd';
  presenterContext.fillRect(0, 0, w, h);

  presenterContext.strokeStyle = '#ffffff';
  presenterContext.lineWidth = Math.max(2, Math.round(Math.min(w, h) / 260));
  for (let x = 0.0167; x < 1; x += 0.0572) {
    presenterContext.beginPath();
    presenterContext.moveTo(Math.round(x * w), 0);
    presenterContext.lineTo(Math.round(x * w), h);
    presenterContext.stroke();
  }
  for (let y = 0.023; y < 1; y += 0.0725) {
    presenterContext.beginPath();
    presenterContext.moveTo(0, Math.round(y * h));
    presenterContext.lineTo(w, Math.round(y * h));
    presenterContext.stroke();
  }

  presenterContext.fillStyle = '#ffffff';
  presenterContext.beginPath();
  presenterContext.ellipse(w * 0.5, h * 0.496, w * 0.342, h * 0.435, 0, 0, Math.PI * 2);
  presenterContext.fill();
  presenterContext.save();
  presenterContext.beginPath();
  presenterContext.ellipse(w * 0.5, h * 0.496, w * 0.342, h * 0.435, 0, 0, Math.PI * 2);
  presenterContext.clip();

  rect('#000000', 0.388, 0.093, 0.229, 0.075);
  presenterContext.fillStyle = '#000000';
  presenterContext.beginPath();
  presenterContext.moveTo(w * 0.182, h * 0.314);
  presenterContext.lineTo(w * 0.276, h * 0.168);
  presenterContext.lineTo(w * 0.329, h * 0.168);
  presenterContext.lineTo(w * 0.329, h * 0.241);
  presenterContext.lineTo(w * 0.310, h * 0.241);
  presenterContext.lineTo(w * 0.310, h * 0.314);
  presenterContext.closePath();
  presenterContext.fill();
  presenterContext.beginPath();
  presenterContext.moveTo(w * 0.818, h * 0.314);
  presenterContext.lineTo(w * 0.724, h * 0.168);
  presenterContext.lineTo(w * 0.671, h * 0.168);
  presenterContext.lineTo(w * 0.671, h * 0.241);
  presenterContext.lineTo(w * 0.690, h * 0.241);
  presenterContext.lineTo(w * 0.690, h * 0.314);
  presenterContext.closePath();
  presenterContext.fill();
  rect('#000000', 0.250, 0.241, 0.024, 0.073);
  rect('#000000', 0.352, 0.241, 0.039, 0.073);
  rect('#000000', 0.430, 0.241, 0.039, 0.073);
  rect('#000000', 0.508, 0.241, 0.039, 0.073);
  rect('#000000', 0.586, 0.241, 0.039, 0.073);
  rect('#000000', 0.665, 0.241, 0.039, 0.073);
  rect('#000000', 0.744, 0.241, 0.024, 0.073);
  rect('#000000', 0.354, 0.168, 0.006, 0.146);
  rect('#000000', 0.664, 0.168, 0.006, 0.146);

  const colourBars = [
    ['#ffff00', 0.159, 0.115], ['#00e6e6', 0.274, 0.115], ['#00f000', 0.389, 0.110],
    ['#ef00df', 0.499, 0.115], ['#f20d0d', 0.614, 0.115], ['#1111df', 0.729, 0.112],
  ];
  for (const [color, x, width] of colourBars) rect(color, x, 0.314, width, 0.146);

  rect('#000000', 0.159, 0.460, 0.682, 0.217);
  presenterContext.strokeStyle = '#ffffff';
  presenterContext.lineWidth = Math.max(2, Math.round(w * 0.003));
  for (const y of [0.460, 0.497, 0.532]) {
    presenterContext.beginPath();
    presenterContext.moveTo(w * 0.159, h * y);
    presenterContext.lineTo(w * 0.841, h * y);
    presenterContext.stroke();
  }
  for (let x = 0.186; x <= 0.814; x += 0.0572) {
    presenterContext.beginPath();
    presenterContext.moveTo(w * x, h * 0.460);
    presenterContext.lineTo(w * x, h * 0.532);
    presenterContext.stroke();
  }

  let wedgeX = w * 0.188;
  const wedgeEnd = w * 0.786;
  while (wedgeX < wedgeEnd) {
    const progress = (wedgeX - w * 0.188) / Math.max(1, wedgeEnd - w * 0.188);
    const barWidth = Math.max(1, w * (0.011 * (1 - progress) + 0.0007 * progress));
    presenterContext.fillStyle = '#ffffff';
    presenterContext.fillRect(Math.floor(wedgeX), Math.floor(h * 0.535), Math.max(1, Math.ceil(barWidth)), Math.ceil(h * 0.142));
    wedgeX += barWidth * 1.72;
  }

  rect('#000000', 0.472, 0.389, 0.056, 0.216);
  presenterContext.strokeStyle = '#ffffff';
  presenterContext.lineWidth = Math.max(2, Math.round(w * 0.003));
  presenterContext.beginPath();
  presenterContext.moveTo(w * 0.472, h * 0.389);
  presenterContext.lineTo(w * 0.472, h * 0.605);
  presenterContext.lineTo(w * 0.528, h * 0.605);
  presenterContext.lineTo(w * 0.528, h * 0.389);
  presenterContext.stroke();

  const greys = ['#111111', '#303030', '#515151', '#747474', '#969696', '#b7b7b7', '#d4d4d4'];
  const greyStart = 0.245, greyWidth = 0.510 / greys.length;
  for (let i = 0; i < greys.length; i++) rect(greys[i], greyStart + i * greyWidth, 0.677, greyWidth, 0.075);
  rect('#ffff00', 0.245, 0.823, 0.510, 0.120);
  rect('#ff1010', 0.472, 0.823, 0.056, 0.120);
  rect('#000000', 0.329, 0.752, 0.342, 0.072);
  rect('#000000', 0.354, 0.752, 0.008, 0.097);
  presenterContext.restore();

  rect('#00a05a', 0.073, 0.095, 0.057, 0.402);
  rect('#ce4967', 0.073, 0.497, 0.057, 0.400);
  rect('#1735bd', 0.130, 0.095, 0.056, 0.145);
  rect('#bdbdbd', 0.130, 0.240, 0.056, 0.510);
  rect('#cd3c00', 0.130, 0.750, 0.056, 0.147);
  rect('#1735bd', 0.814, 0.095, 0.057, 0.145);
  rect('#bdbdbd', 0.814, 0.240, 0.057, 0.510);
  rect('#cd3c00', 0.814, 0.750, 0.057, 0.147);
  rect('#82c83c', 0.871, 0.095, 0.057, 0.405);
  rect('#554de1', 0.871, 0.500, 0.057, 0.397);

  const checkerWidth = 1 / 18;
  for (let i = 0; i < 18; i++) {
    rect(i % 2 === 0 ? '#ffffff' : '#000000', i * checkerWidth, 0, checkerWidth, 0.024);
    rect(i % 2 === 0 ? '#000000' : '#ffffff', i * checkerWidth, 0.968, checkerWidth, 0.032);
  }
  const sideHeight = 1 / 14;
  for (let i = 0; i < 14; i++) {
    rect(i % 2 === 0 ? '#ffffff' : '#000000', 0, i * sideHeight, 0.017, sideHeight);
    rect(i % 2 === 0 ? '#000000' : '#ffffff', 0.983, i * sideHeight, 0.017, sideHeight);
  }
  drawPresenterTestCardSyncDistortion(w, h);
  drawPresenterPhosphor(w, h, 1.3);
  drawPresenterTestCardHumBar(w, h);
}
function drawPresenterTestCardSyncDistortion(w, h) {
  if (w <= 0 || h <= 0) return;
  if (presenterTestCardBuffer.width !== w || presenterTestCardBuffer.height !== h) {
    presenterTestCardBuffer.width = w;
    presenterTestCardBuffer.height = h;
  }
  presenterTestCardBufferContext.globalAlpha = 1;
  presenterTestCardBufferContext.globalCompositeOperation = 'source-over';
  presenterTestCardBufferContext.clearRect(0, 0, w, h);
  presenterTestCardBufferContext.drawImage(presenterCanvas, 0, 0);
  presenterContext.drawImage(presenterTestCardBuffer, 0, 0);

  const stripHeight = Math.max(2, Math.round(h / 180));
  const sideInset = Math.max(1, Math.round(w * 0.03));
  const signalWidth = Math.max(1, w - sideInset * 2);
  const tearCenter = ((frame * 0.013) % 1) * h;
  const tearGate = Math.pow(Math.max(0, Math.sin(frame * 0.105)), 6);
  for (let y = 0; y < h; y += stripHeight) {
    const height = Math.min(stripHeight, h - y);
    const topEnvelope = Math.max(0, 1 - y / Math.max(1, h * 0.24));
    const flagging = topEnvelope * (
      Math.sin(frame * 0.047) * w * 0.0008 +
      Math.sin(frame * 0.019 + y * 0.035) * w * 0.00045
    );
    const tearDistance = Math.abs(y - tearCenter) / Math.max(1, h * 0.055);
    const tearing = Math.exp(-tearDistance * tearDistance) * tearGate * Math.sin(frame * 0.31) * w * 0.0022;
    const jitter = Math.sin(frame * 0.17 + y * 0.11) * w * 0.00018;
    const shift = Math.max(-signalWidth + 1, Math.min(signalWidth - 1, Math.round(flagging + tearing + jitter)));
    if (shift === 0) {
      continue;
    }
    if (shift > 0) {
      presenterContext.drawImage(presenterTestCardBuffer, sideInset, y, signalWidth - shift, height, sideInset + shift, y, signalWidth - shift, height);
    } else {
      presenterContext.drawImage(presenterTestCardBuffer, sideInset - shift, y, signalWidth + shift, height, sideInset, y, signalWidth + shift, height);
    }
  }
}
function drawPresenterTestCardHumBar(w, h) {
  const barHeight = Math.max(18, Math.round(h * 0.14));
  const travel = h + barHeight;
  const y = ((frame * 0.0045) % 1) * travel - barHeight;
  const gradient = presenterContext.createLinearGradient(0, y, 0, y + barHeight);
  gradient.addColorStop(0, 'rgba(0,0,0,0)');
  gradient.addColorStop(0.22, 'rgba(0,0,0,0.065)');
  gradient.addColorStop(0.5, 'rgba(0,0,0,0.16)');
  gradient.addColorStop(0.78, 'rgba(0,0,0,0.065)');
  gradient.addColorStop(1, 'rgba(0,0,0,0)');
  presenterContext.fillStyle = gradient;
  presenterContext.fillRect(0, Math.floor(y), w, barHeight);
}
function drawPresenterPhosphor(w, h, intensity = 1) {
  const local = makeRNG((frame + 1) * 2654435761 + 0x50484f53);
  const dotSize = Math.max(1, Math.round(Math.min(w, h) / 900));
  const dotCount = Math.min(1900, Math.max(260, Math.floor(w * h / 5500)));
  presenterContext.save();
  presenterContext.globalCompositeOperation = 'screen';
  for (let i = 0; i < dotCount; i++) {
    const x = local.int(Math.max(1, w - dotSize * 3));
    const y = local.int(Math.max(1, h - dotSize));
    const alpha = Math.min(0.32, (0.03 + local.next() * 0.125) * intensity);
    presenterContext.fillStyle = 'rgba(255,65,55,' + alpha + ')';
    presenterContext.fillRect(x, y, dotSize, dotSize);
    presenterContext.fillStyle = 'rgba(70,255,90,' + alpha + ')';
    presenterContext.fillRect(x + dotSize, y, dotSize, dotSize);
    presenterContext.fillStyle = 'rgba(70,120,255,' + alpha + ')';
    presenterContext.fillRect(x + dotSize * 2, y, dotSize, dotSize);
  }
  const streaks = 1 + local.int(3);
  for (let i = 0; i < streaks; i++) {
    const y = local.int(Math.max(1, h));
    presenterContext.fillStyle = 'rgba(255,255,255,' + Math.min(0.2, (0.03 + local.next() * 0.065) * intensity) + ')';
    presenterContext.fillRect(0, y, w, Math.max(1, dotSize));
  }
  presenterContext.globalCompositeOperation = 'source-over';
  const scanStep = Math.max(3, Math.round(h / 360));
  presenterContext.fillStyle = 'rgba(0,0,0,' + Math.min(0.2, 0.115 * intensity) + ')';
  for (let y = frame % scanStep; y < h; y += scanStep) {
    presenterContext.fillRect(0, y, w, 1);
  }
  presenterContext.restore();
}
function drawPresenterSnow() {
  presenterCanvas.style.display = 'block';
  effectLayer.style.display = 'none';
  contentLayer.style.display = 'none';
  chromeLayer.style.display = 'none';
  presenterContext.clearRect(0, 0, presenterCanvas.width, presenterCanvas.height);
  presenterContext.fillStyle = '#050505';
  presenterContext.fillRect(0, 0, presenterCanvas.width, presenterCanvas.height);
  const local = makeRNG(frame * 1103515245 + 12345);
  const palette = ['#101010', '#303030', '#707070', '#a8a8a8', '#f0f0f0'];
  for (let row = 0; row < deck.rows; row++) {
    for (let col = 0; col < deck.cols; col++) {
      presenterContext.fillStyle = palette[local.int(palette.length)];
      drawCanvasCellRun(col, row, 1);
    }
  }
  if (frame % 9 < 2) {
    presenterContext.fillStyle = '#ffffff';
    drawCanvasCellRun(0, local.int(Math.max(1, deck.rows)), deck.cols);
  }
}
function drawChannelStatic(alpha, seed) {
  const previousAlpha = presenterContext.globalAlpha;
  presenterContext.globalAlpha = alpha;
  const local = makeRNG(seed);
  const palette = ['#000000', '#181818', '#404040', '#8a8a8a', '#f7f7f7'];
  for (let row = 0; row < deck.rows; row++) {
    const wobble = local.int(9) - 4;
    for (let col = 0; col < deck.cols; col++) {
      if (local.int(100) < 22) continue;
      presenterContext.fillStyle = palette[local.int(palette.length)];
      drawCanvasCellRun(Math.max(0, Math.min(deck.cols - 1, col + wobble)), row, 1);
    }
  }
  presenterContext.fillStyle = '#ffffff';
  drawCanvasCellRun(0, local.int(Math.max(1, deck.rows)), deck.cols);
  presenterContext.fillStyle = '#090909';
  drawCanvasCellRun(0, local.int(Math.max(1, deck.rows)), deck.cols);
  presenterContext.globalAlpha = previousAlpha;
}
function drawChannelFlipTransition() {
  presenterCanvas.style.display = 'block';
  effectLayer.style.display = 'none';
  contentLayer.style.display = 'none';
  chromeLayer.style.display = 'none';
  const duration = Math.max(1, presenterTransitionUntil - presenterTransitionStarted);
  const progress = Math.max(0, Math.min(1, (performance.now() - presenterTransitionStarted) / duration));
  const midpoint = 0.48;
  if (progress < midpoint) {
    const from = presenterPageAt(presenterTransitionFromIndex);
    if (!from) return;
    drawPresenterPage(from, frame, presenterContentLinesFor(presenterTransitionFromIndex, from));
    drawChannelStatic(0.20 + progress / midpoint * 0.80, (frame + 1) * 2654435761 + presenterTransitionFromIndex * 9973);
  } else {
    const to = presenterPageAt(presenterTransitionToIndex);
    if (!to) return;
    drawPresenterPage(to, frame, presenterContentLinesFor(presenterTransitionToIndex, to));
    drawChannelStatic(0.95 - (progress - midpoint) / (1 - midpoint) * 0.95, (frame + 1) * 2246822519 + presenterTransitionToIndex * 7919);
  }
}
function linkGroups(lines, includeImages) {
  const groups = new Map();
  for (const line of lines || []) {
    if (!line.link || !line.parts || !line.parts.length) continue;
    if (!includeImages && (line.role === 'image' || line.role === 'transparent-image')) continue;
    const element = line.element == null ? 'row-' + line.row + '-' + line.link : line.element;
    const key = line.link + '|' + element;
    let group = groups.get(key);
    if (!group) {
      group = {link: line.link, minRow: line.row, maxRow: line.row, minCol: deck.cols, maxCol: 0, color: ''};
      groups.set(key, group);
    }
    group.minRow = Math.min(group.minRow, line.row);
    group.maxRow = Math.max(group.maxRow, line.row);
    for (const part of line.parts) {
      const len = [...part.text].length;
      group.minCol = Math.min(group.minCol, part.col);
      group.maxCol = Math.max(group.maxCol, part.col + len);
      if (!group.color && part.color) group.color = part.color;
    }
  }
  return [...groups.values()].filter(group => group.maxCol > group.minCol);
}
function renderLinkUnderlines(lines) {
  let html = '';
  for (const group of linkGroups(lines, false)) {
    const row = Math.min(deck.rows - 1, group.maxRow + 1);
    html += '<span class="link-underline" style="--x:' + group.minCol + ';--y:' + row + ';color:' + (group.color || '#f3efe0') + '">' + '▔'.repeat(group.maxCol - group.minCol) + '</span>';
  }
  return html;
}
function renderLinkHitAreas(lines) {
  let html = '';
  for (const group of linkGroups(lines, true)) {
    if (group.maxCol <= group.minCol) continue;
    html += '<span class="link-hit" data-link="' + esc(group.link) + '" style="--x:' + group.minCol + ';--y:' + group.minRow + ';--w:' + (group.maxCol - group.minCol) + ';--h:' + (group.maxRow - group.minRow + 1) + '" aria-label="link"></span>';
  }
  return html;
}
function renderCanvasLinkHitAreas(lines) {
  let html = '';
  for (const group of linkGroups(lines, true)) {
    if (group.maxCol <= group.minCol) continue;
    const left = group.minCol / Math.max(1, deck.cols) * 100;
    const top = group.minRow / Math.max(1, deck.rows) * 100;
    const width = (group.maxCol - group.minCol) / Math.max(1, deck.cols) * 100;
    const height = (group.maxRow - group.minRow + 1) / Math.max(1, deck.rows) * 100;
    html += '<span class="canvas-link-hit" data-link="' + esc(group.link) + '" style="left:' + left + '%;top:' + top + '%;width:' + width + '%;height:' + height + '%" aria-label="link"></span>';
  }
  return html;
}
function drawCanvasLinkUnderlines(lines) {
  presenterContext.save();
  presenterContext.lineWidth = Math.max(1, canvasCell * 0.055);
  for (const group of linkGroups(lines, false)) {
    const y = Math.min(presenterCanvas.height - 1, (group.maxRow + 1) * canvasCell + presenterContext.lineWidth);
    presenterContext.strokeStyle = group.color || '#f3efe0';
    presenterContext.beginPath();
    presenterContext.moveTo(group.minCol * canvasCharWidth, y);
    presenterContext.lineTo(group.maxCol * canvasCharWidth, y);
    presenterContext.stroke();
  }
  presenterContext.restore();
}
function render() {
  const page = deck.pages[pageIndex];
  stage.style.background = page.bg;
  effectLayer.style.color = page.fg;
  contentLayer.style.color = page.fg;
  chromeLayer.innerHTML = page.hideChromePageNumber ? '' : '<span class="page-no">' + esc(label(page)) + '</span>';
  drawFrame();
  if (keynopeAppSurface) requestAnimationFrame(renderEditorCanvasOverlay);
}
function lineKey(line) {
  const col = line.parts && line.parts.length ? line.parts[0].col : (line.col || 0);
  return line.row + ':' + col + ':' + (line.element ?? -1) + ':' + (line.role || '');
}
function decodedContentFrames(page) {
  const frames = page.contentFrames || [];
  if (!frames.length) return null;
  const cached = contentAnimationCache.get(pageIndex);
  if (cached) return cached;
  const map = new Map();
  const decoded = [];
  for (const frame of frames) {
    if (frame.full) {
      map.clear();
      for (const line of frame.lines || []) map.set(lineKey(line), line);
    } else {
      for (const key of frame.clear || []) map.delete(key);
      for (const line of frame.update || []) map.set(lineKey(line), line);
    }
    decoded.push({lines: Array.from(map.values())});
  }
  contentAnimationCache.set(pageIndex, decoded);
  return decoded;
}
function makeLine(row, col, text, color) {
  return {row, parts: [{col, text, color}]};
}
function makeRNG(seed) {
  let state = seed >>> 0;
  return {
    next() {
      state = (state + 0x6D2B79F5) >>> 0;
      let t = state;
      t = Math.imul(t ^ (t >>> 15), t | 1);
      t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
      return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    },
    int(n) { return Math.floor(this.next() * Math.max(1, n)); }
  };
}
const effectState = new Map();
function pageSeed(page) {
  return ((page.slide + 1) * 1000003 + (page.page + 1) * 9176 + deck.cols * 131 + deck.rows * 17) >>> 0;
}
function getEffectState(page) {
  const key = page.slide + ':' + page.page + ':' + (page.effect || '') + ':' + deck.cols + 'x' + deck.rows;
  if (effectState.has(key)) return effectState.get(key);
  const rng = makeRNG(pageSeed(page));
  const state = {rng};
  if (page.effect === 'matrix') {
    state.matrixCells = Array.from({length: deck.rows}, () => Array(deck.cols).fill(null));
    state.trails = [];
    for (let x = 0; x < deck.cols; x++) {
      const t = {x, y: 0, life: 0, rate: 1, clear: true};
      reseedMatrixTrail(t, rng);
      state.trails.push(t);
    }
  }
  if (page.effect === 'stars') {
    const pattern = '..+..   ...x...  ...*...         ';
    state.pattern = pattern;
    state.stars = [];
    const count = Math.floor((deck.cols + deck.rows) / 2);
    for (let i = 0; i < count; i++) {
      state.stars.push({x: rng.int(deck.cols), y: rng.int(deck.rows), cycle: rng.int(pattern.length)});
    }
  }
  if (page.effect === 'fireworks' || page.effect === 'explosion') {
    state.bursts = makeBursts(page, page.effect);
  }
  if (!['matrix', 'stars'].includes(page.effect)) {
    state.stateless = makeRNG(pageSeed(page));
  }
  effectState.set(key, state);
  return state;
}
function makeBursts(page, name) {
  const rng = makeRNG(pageSeed(page) + (name === 'fireworks' ? 0x4657524b : 0x4558504c));
  const colors = ['#ff5555', '#ffff55', '#ffffff', '#55ffff', '#ff55ff'];
  const bursts = [];
  for (let i = 0; i < 20; i++) {
    let x = rng.int(deck.cols);
    if (rng.int(3) === 0) {
      x = rng.int(Math.max(1, Math.floor(deck.cols / 3)));
    } else if (rng.int(3) === 0) {
      const edge = Math.floor(deck.cols * 2 / 3);
      x = edge + rng.int(Math.max(1, deck.cols - edge));
    }
    let y = rng.int(deck.rows);
    if (rng.int(2) === 0) {
      const half = Math.floor(deck.rows / 2);
      y = half + rng.int(Math.max(1, deck.rows - half));
    }
    bursts.push({
      x, y,
      start: rng.int(250),
      life: 20 + rng.int(11),
      points: 6 + rng.int(15),
      seed: rng.int(0x7fffffff),
      color: colors[rng.int(colors.length)]
    });
  }
  return bursts;
}
function drawBurstEffects(page, frame, out) {
  const state = getEffectState(page);
  const name = page.effect;
  for (const burst of state.bursts || []) {
    const age = (frame - burst.start) % 250;
    if (age < 0 || age > burst.life) continue;
    if (name === 'fireworks') drawFireworkBurst(burst, age, out);
    else drawExplosionBurst(burst, age, out);
  }
}
function pushParticle(out, x, y, color, ch) {
  x = Math.trunc(x);
  y = Math.trunc(y);
  if (x >= 0 && x < deck.cols && y >= 0 && y < deck.rows) out.push(makeLine(y, x, ch, color));
}
function drawFireworkBurst(burst, age, out) {
  if (age < 10) {
    const y = deck.rows - 1 + Math.trunc((burst.y - (deck.rows - 1)) * age / 10);
    pushParticle(out, burst.x, y, '#ffff55', '|');
    return;
  }
  const explosionAge = age - 10;
  const explosionLife = Math.max(1, burst.life - 10);
  const acceleration = 1.0 - 1.0 / explosionLife;
  for (let p = 0; p < burst.points; p++) {
    const direction = p * 2 * Math.PI / burst.points;
    let x = burst.x, y = burst.y;
    let dx = Math.sin(direction) * 3 * 8 / explosionLife;
    let dy = Math.cos(direction) * 1.5 * 8 / explosionLife;
    for (let step = 0; step <= explosionAge; step++) {
      dy = dy * acceleration + 0.03;
      dx *= acceleration;
      x += dx;
      y += dy;
    }
    const ch = explosionAge > explosionLife * 2 / 3 ? '.' : '+';
    pushParticle(out, x, y, burst.color, ch);
    if (explosionAge > 1 && explosionAge < explosionLife - 1) {
      pushParticle(out, x - dx * 2, y - dy * 2, '#f3efe0', '.');
      if (explosionAge % 3 === 0) pushParticle(out, x - dx * 4, y - dy * 4, '#f3efe0', ',');
    }
  }
}
function drawExplosionBurst(burst, age, out) {
  const spawnFrames = Math.min(age, Math.max(0, burst.life - 10));
  for (let spawn = 0; spawn <= spawnFrames; spawn++) {
    const particleAge = age - spawn;
    if (particleAge >= 10) continue;
    const rng = makeRNG((burst.seed + spawn * 7919) >>> 0);
    for (let i = 0; i < 30; i++) {
      const direction = rng.next() * 2 * Math.PI;
      const d = Math.max(1, burst.life - 10);
      const r = rng.next() * Math.sin(Math.PI * (d - Math.max(0, burst.life - 10 - spawn)) / (d * 2)) * 3.0;
      let x = burst.x + Math.sin(direction) * r * 2.0;
      let y = burst.y + Math.cos(direction) * r;
      const dx = Math.sin(direction) / 2.0;
      const dy = Math.cos(direction) / 4.0;
      x += dx * particleAge;
      y += dy * particleAge;
      let color = '#ffffff';
      if (particleAge > 2) color = '#ffff55';
      if (particleAge > 5) color = '#ff5555';
      if (particleAge > 8) color = '#aa0000';
      pushParticle(out, x, y, color, '#');
    }
  }
}
function reseedMatrixTrail(t, rng) {
  t.y += t.rate;
  t.life--;
  if (t.life > 0) return;
  t.clear = !t.clear;
  t.rate = 1 + rng.int(2);
  if (t.clear) {
    t.y = 0;
    t.life = Math.max(1, Math.floor(deck.rows / t.rate));
  } else {
    t.y = rng.int(Math.max(1, Math.floor(deck.rows / 2))) - Math.floor(deck.rows / 4);
    t.life = Math.max(1, Math.floor(rng.int(Math.max(1, deck.rows - t.y)) / t.rate));
  }
}
function drawLineCells(x0, y0, x1, y1, color, ch, out) {
  const dx = Math.abs(x1 - x0), dy = -Math.abs(y1 - y0);
  const sx = x0 < x1 ? 1 : -1, sy = y0 < y1 ? 1 : -1;
  let err = dx + dy;
  for (;;) {
    if (x0 >= 0 && x0 < deck.cols && y0 >= 0 && y0 < deck.rows) out.push(makeLine(y0, x0, ch, color));
    if (x0 === x1 && y0 === y1) break;
    const e2 = 2 * err;
    if (e2 >= dy) { err += dy; x0 += sx; }
    if (e2 <= dx) { err += dx; y0 += sy; }
  }
}
function deterministicPoints(count, seed) {
  const points = [];
  for (let i = 0; i < count; i++) {
    points.push({
      x: (seed + i * 23 + i * i * 7) % Math.max(1, deck.cols),
      y: (Math.floor(seed / 2) + i * 13 + i * i * 5) % Math.max(1, deck.rows)
    });
  }
  return points;
}
function clippedText(y, x, color, text, out) {
  if (y < 0 || y >= deck.rows || x >= deck.cols) return;
  if (x < 0) {
    text = [...text].slice(-x).join('');
    x = 0;
  }
  if (!text) return;
  out.push(makeLine(y, x, [...text].slice(0, Math.max(0, deck.cols - x)).join(''), color));
}
function clippedTextMasked(y, x, color, text, mask, out) {
  if (!mask || mask.size === 0) return clippedText(y, x, color, text, out);
  if (y < 0 || y >= deck.rows || x >= deck.cols) return;
  const chars = [...text];
  let startIndex = 0;
  if (x < 0) {
    startIndex = -x;
    x = 0;
  }
  let run = '', runX = x;
  const flush = () => {
    if (!run) return;
    out.push(makeLine(y, runX, run, color));
    run = '';
  };
  for (let index = startIndex; index < chars.length && x < deck.cols; index++, x++) {
    if (mask.has(index)) {
      flush();
      runX = x + 1;
      continue;
    }
    if (!run) runX = x;
    run += chars[index];
  }
  flush();
}
function effectLines(page, frame) {
  const name = page.effect || '';
  if (!name) return [];
  const out = [];
  const state = getEffectState(page);
  const rng = state.rng || makeRNG(pageSeed(page) + frame);
  if (name === 'matrix') {
    if (frame % 2 === 0) {
      for (const t of state.trails) {
        if (t.clear) {
          for (let dy = 0; dy < 3; dy++) {
            const y = t.y + dy;
            if (y >= 0 && y < deck.rows) state.matrixCells[y][t.x] = null;
          }
        } else {
        for (let dy = 0; dy < 3; dy++) {
          const y = t.y + dy;
          if (y >= 0 && y < deck.rows) state.matrixCells[y][t.x] = {text: String.fromCharCode(32 + rng.int(95)), color: '#00aa00'};
        }
        for (let dy = 4; dy < 6; dy++) {
          const y = t.y + dy;
          if (y >= 0 && y < deck.rows) state.matrixCells[y][t.x] = {text: String.fromCharCode(32 + rng.int(95)), color: '#55ff55'};
        }
      }
      reseedMatrixTrail(t, rng);
      }
    }
    for (let y = 0; y < deck.rows; y++) {
      for (let x = 0; x < deck.cols; x++) {
        const cell = state.matrixCells[y][x];
        if (cell) out.push(makeLine(y, x, cell.text, cell.color));
      }
    }
  } else if (name === 'stars') {
    for (const st of state.stars) {
      st.cycle = (st.cycle + 1) % state.pattern.length;
      const ch = state.pattern[st.cycle];
      if (ch === ' ' && rng.int(120) === 0) { st.x = rng.int(deck.cols); st.y = rng.int(deck.rows); }
      if (ch !== ' ') out.push(makeLine(st.y, st.x, ch, '#f3efe0'));
    }
  } else if (name === 'plasma') {
    const chars = ' .:;rsA23hHG#9&@', palette = ['#0000aa', '#0000aa', '#aa00aa', '#aa00aa', '#aa0000', '#ff5555'];
    const t = frame + 1;
    const f = (x, y, xp, yp, n) => Math.sin(Math.sqrt(Math.pow(x - deck.cols * xp, 2) + 4 * Math.pow(y - deck.rows * yp, 2)) * Math.PI / n);
    for (let y = 0; y < deck.rows - 1; y++) for (let x = 0; x < deck.cols - 1; x++) {
      const value = Math.abs(f(x + t / 3, y, 1/4, 1/3, 15) + f(x, y, 1/8, 1/5, 11) + f(x, y + t / 3, 1/2, 1/5, 13) + f(x, y, 3/4, 4/5, 13)) / 4;
      out.push(makeLine(y, x, chars[Math.min(chars.length - 1, Math.floor(value * chars.length))], palette[Math.min(palette.length - 1, Math.round(value * (palette.length - 1)))]));
    }
  } else if (name === 'radar') {
    const cx = Math.floor(deck.cols / 2), cy = Math.floor(deck.rows / 2), radius = Math.min(Math.floor(deck.cols / 2), deck.rows - 2);
    const angle = (frame % 120) / 120 * 2 * Math.PI;
    for (let r = 2; r <= radius; r += Math.max(2, Math.floor(radius / 4))) {
      for (let deg = 0; deg < 360; deg += 6) {
        const a = deg * Math.PI / 180, x = cx + Math.floor(Math.cos(a) * r), y = cy + Math.floor(Math.sin(a) * r);
        if (x >= 0 && x < deck.cols && y >= 0 && y < deck.rows) out.push(makeLine(y, x, '.', '#00aa00'));
      }
    }
    for (let r = 0; r < radius; r++) {
      const x = cx + Math.floor(Math.cos(angle) * r * 2), y = cy + Math.floor(Math.sin(angle) * r);
      if (x >= 0 && x < deck.cols && y >= 0 && y < deck.rows) out.push(makeLine(y, x, '█', '#55ff55'));
    }
    for (let i = 0; i < 18; i++) out.push(makeLine((i*i*7 + Math.floor(frame/3)) % deck.rows, (i*17 + Math.floor(frame/2)) % deck.cols, '+', '#00aa00'));
  } else if (name === 'neural') {
    const nodes = deterministicPoints(18, 41);
    for (let i = 0; i < nodes.length; i++) for (let j = i + 1; j < nodes.length; j++) {
      const a = nodes[i], b = nodes[j];
      if (Math.abs(a.x-b.x) + Math.abs(a.y-b.y) < Math.floor(deck.cols / 3)) drawLineCells(a.x, a.y, b.x, b.y, (i+j+Math.floor(frame/3))%9===0 ? '#55ffff' : '#0000aa', '.', out);
    }
    nodes.forEach((p, i) => out.push(makeLine(p.y, p.x, '●', (i+Math.floor(frame/2))%7===0 ? '#ffffff' : '#00aaaa')));
  } else if (name === 'circuit') {
    for (let y = 2; y < deck.rows; y += 4) {
      const color = (y + Math.floor(frame/2)) % 12 < 4 ? '#55ffff' : '#00aaaa';
      for (let x = 0; x < deck.cols; x++) {
        if (x % 11 === 0) out.push(makeLine(y, x, '┬', color));
        else if ((x + Math.floor(frame/2)) % 23 < 14) out.push(makeLine(y, x, '─', color));
      }
      for (let x = (y * 7 + Math.floor(frame/3)) % 11; x < deck.cols; x += 22) for (let yy = y; yy < Math.min(deck.rows, y + 4); yy++) out.push(makeLine(yy, x, '│', color));
    }
  } else if (name === 'data-storm') {
    const snippets = ['{"event":"ingest","status":"ok"}', 'vector.search(top_k=32)', 'query.plan -> tool.call', 'chunk.window overlap=128', '{"mime":"application/pdf"}', 'rerank: lexical + semantic', 'cache.hit ratio=0.87', 'trace_id=7f3a latency=42ms', 'index.flush segments=12', 'embedding.model dimensions=1536', 'worker.queue depth=0042', 'ACL filter applied', 'pipeline.stage normalize', 'ocr.page confidence=0.94', 'audio.transcript segment=18', 'image.caption generated', 'tool.result bytes=4096', 'guardrail.check pass', 'schema.validate ok', 'retry.backoff 250ms', 'stream.delta tokens=64', 'memory.write ephemeral', 'context.pack budget=8192', 'rank.fusion weight=0.62', 'metadata.extract fields=17', 'blob.read range=0..65535', 'session.state synced', 'eval.sample score=0.91', 'router.intent classify', 'response.draft revise'];
    const hash = v => { v = v | 0; v ^= v << 13; v ^= v >> 17; v ^= v << 5; return Math.abs(v); };
    const stormRune = seed => {
      const s = snippets[hash(seed) % snippets.length];
      let start = hash(Math.floor(seed / snippets.length) + 17) % s.length;
      for (let off = 0; off < s.length; off++) {
        const ch = s[(start + off) % s.length];
        if (ch !== ' ' && ch !== '\t') return ch;
      }
      return '?';
    };
    const maxPile = Math.max(1, Math.floor(deck.rows / 4));
    const pileHeight = x => 1 + ((hash(x * 17) % maxPile + Math.floor((hash(x * 31 + 7) % Math.max(1, Math.floor(maxPile / 2) + 1)) / 2)) % maxPile);
    const snippetForLane = (lane, atFrame) => snippets[(lane + Math.floor(atFrame / 240)) % snippets.length];
    const streamState = (lane, atFrame, textLen) => {
      const span = Math.max(1, deck.cols + textLen + 12);
      let pass = Math.floor(atFrame / span);
      let speed = streamSpeed(lane, pass);
      let progress = Math.floor(atFrame * speed / 2) + lane * 17;
      pass = Math.floor(progress / span);
      speed = streamSpeed(lane, pass);
      progress = Math.floor(atFrame * speed / 2) + lane * 17;
      pass = Math.floor(progress / span);
      const position = progress % span;
      const startFrame = Math.max(0, Math.floor(((pass * span - lane * 17) * 2 + speed - 1) / speed));
      const endFrame = Math.max(startFrame + 1, Math.floor((((pass + 1) * span - lane * 17) * 2 + speed - 1) / speed));
      return {pass, speed, span, x: deck.cols - position, startFrame, duration: Math.max(1, endFrame - startFrame)};
    };
    const scheduledDrop = (lane, pass, slot, chars, startFrame, duration) => {
      const textLen = chars.length;
      if (textLen <= 0) return null;
      const survivors = hash(lane * 139 + pass * 47) % 5;
      const dropBudget = Math.max(0, textLen - survivors);
      if (slot < 0 || slot >= dropBudget) return null;
      let charIndex = (hash(lane * 23 + pass * 31) + slot * 7) % textLen;
      for (let offset = 0; offset < textLen; offset++) {
        const candidate = (charIndex + offset) % textLen;
        if (chars[candidate] !== ' ' && chars[candidate] !== '\t') {
          charIndex = candidate;
          break;
        }
      }
      const spawnOffset = Math.floor((slot + 1) * Math.max(1, duration) / Math.max(1, dropBudget + 1));
      return {spawnFrame: startFrame + spawnOffset, charIndex};
    };
    const dropLanding = (spawnX, startY, speed, drift) => {
      let landX = Math.min(Math.max(0, spawnX), Math.max(0, deck.cols - 1));
      let floor = 0, travel = 1;
      for (let iteration = 0; iteration < 4; iteration++) {
        floor = Math.max(0, deck.rows - pileHeight(landX) - 1);
        const distance = Math.max(0, floor - startY);
        travel = Math.max(1, Math.floor((distance * 3 + Math.max(1, speed) - 1) / Math.max(1, speed)));
        const nextX = Math.min(Math.max(0, spawnX - Math.floor(travel * drift / 7)), Math.max(0, deck.cols - 1));
        if (nextX === landX) break;
        landX = nextX;
      }
      floor = Math.max(0, deck.rows - pileHeight(landX) - 1);
      const distance = Math.max(0, floor - startY);
      travel = Math.max(1, Math.floor((distance * 3 + Math.max(1, speed) - 1) / Math.max(1, speed)));
      return {landX, floor, travel};
    };
    const dropPosition = (spawnX, startY, landX, floor, travel, age) => {
      if (travel <= 0 || age >= travel) return {x: landX, y: floor};
      age = Math.max(0, age);
      return {
        x: spawnX + Math.trunc((landX - spawnX) * age / travel),
        y: startY + Math.trunc((floor - startY) * age / travel)
      };
    };
    const laneCount = Math.min(snippets.length, Math.max(3, Math.floor(deck.rows / 5)));
    const streamRows = Array.from({length: laneCount}, (_, i) => Math.min(Math.max(0, deck.rows - 1), 2 + i * Math.max(2, Math.floor(deck.rows / Math.max(1, laneCount + 1)))));
    const streamSpeed = (lane, spawn) => 3 + hash(lane * 101 + spawn * 29) % 7;
    const drops = Math.max(32, deck.cols);
    const streamMasks = new Map();
    for (let i = 0; i < drops; i++) {
      const lane = i % streamRows.length;
      const text = snippetForLane(lane, frame);
      const chars = [...text];
      const state = streamState(lane, frame, chars.length);
      const scheduled = scheduledDrop(lane, state.pass, Math.floor(i / streamRows.length), chars, state.startFrame, state.duration);
      if (!scheduled || frame < scheduled.spawnFrame) continue;
      if (snippetForLane(lane, scheduled.spawnFrame) !== text) continue;
      if (!streamMasks.has(lane)) streamMasks.set(lane, new Set());
      streamMasks.get(lane).add(scheduled.charIndex);
    }
    for (let x = 0; x < deck.cols; x++) {
      const pile = pileHeight(x);
      for (let y = deck.rows - pile; y < deck.rows; y++) {
        if (y >= 0) out.push(makeLine(y, x, stormRune(x * 13 + y * 7 + Math.floor(frame / 9)), y >= deck.rows - 2 ? '#00aaaa' : '#00aa00'));
      }
    }
    for (let lane = 0; lane < streamRows.length; lane++) {
      const text = snippetForLane(lane, frame);
      const state = streamState(lane, frame, [...text].length);
      const x = state.x;
      const color = lane % 3 === 1 ? '#ffffff' : lane % 3 === 2 ? '#00aa00' : '#55ff55';
      clippedTextMasked(streamRows[lane], x, color, text, streamMasks.get(lane), out);
    }
    for (let i = 0; i < drops; i++) {
      const lane = i % streamRows.length;
      const currentText = snippetForLane(lane, frame);
      const currentChars = [...currentText];
      const currentState = streamState(lane, frame, currentChars.length);
      const scheduled = scheduledDrop(lane, currentState.pass, Math.floor(i / streamRows.length), currentChars, currentState.startFrame, currentState.duration);
      if (!scheduled || frame < scheduled.spawnFrame) continue;
      const text = snippetForLane(lane, scheduled.spawnFrame);
      if (text !== currentText) continue;
      if (!text.length) continue;
      const spawnState = streamState(lane, scheduled.spawnFrame, [...text].length);
      let charIndex = scheduled.charIndex;
      const spawnX = spawnState.x + charIndex;
      const age = frame - scheduled.spawnFrame;
      const drift = 1 + hash(i * 43 + lane * 89) % 4;
      if (spawnX < 0 || spawnX >= deck.cols || age < 0) continue;
      const dropSpeed = 2 + hash(i * 97 + spawnX * 31) % 7;
      const landing = dropLanding(spawnX, streamRows[lane], dropSpeed, drift);
      if (landing.floor <= streamRows[lane]) continue;
      const landedLifetime = 36 + hash(i * 59 + landing.landX * 71) % 48;
      if (age > landing.travel + landedLifetime) continue;
      const trailCount = age >= landing.travel + 4 ? 1 : 5;
      for (let trail = 0; trail < trailCount; trail++) {
        const trailAge = Math.min(age, landing.travel) - trail * 2;
        if (trailAge < 0) continue;
        const pos = dropPosition(spawnX, streamRows[lane], landing.landX, landing.floor, landing.travel, trailAge);
        if (pos.y < streamRows[lane] || pos.y > landing.floor) continue;
        if (pos.x < 0 || pos.x >= deck.cols) continue;
        let glyph = text[(charIndex + trail) % text.length];
        if (glyph === ' ' || glyph === '\t') glyph = stormRune(i * 37 + trail * 11 + frame);
        const color = trail === 0 ? '#ffffff' : trail === 1 ? '#55ff55' : '#00aa00';
        out.push(makeLine(pos.y, pos.x, glyph, color));
      }
    }
  } else if (name === 'flame') {
    const chars = ' .:░▒▓█', local = makeRNG(pageSeed(page) + frame * 7919);
    for (let y = Math.max(0, Math.floor(deck.rows / 2)); y < deck.rows; y++) for (let x = 0; x < deck.cols; x++) {
      const v = Math.sin((x+frame)/3) + Math.sin((x*2+frame)/7) + local.next()*1.5;
      const falloff = (deck.rows-y) / Math.max(1, Math.floor(deck.rows/2));
      const idx = Math.max(0, Math.min(chars.length-1, Math.floor((v+2.0)*falloff*(chars.length-1)/3.0)));
      if (chars[idx] !== ' ') out.push(makeLine(y, x, chars[idx], idx > 4 ? '#ffff55' : idx > 2 ? '#ff5555' : '#aa0000'));
    }
  } else if (name === 'warp') {
    const cx = Math.floor(deck.cols/2), cy = Math.floor(deck.rows/2);
    for (let i = 0; i < 90; i++) {
      const angle = i * 137 * Math.PI / 180, speed = 1 + i % 5, r = (frame * speed + i * 11) % Math.max(1, deck.cols + deck.rows);
      const x = cx + Math.floor(Math.cos(angle) * r), y = cy + Math.floor(Math.sin(angle) * r / 2);
      if (x >= 0 && x < deck.cols && y >= 0 && y < deck.rows) out.push(makeLine(y, x, r > deck.cols/3 ? '+' : '.', '#ffffff'));
    }
  } else if (name === 'scanline') {
    const y = frame % Math.max(1, deck.rows);
    for (let row = 0; row < deck.rows; row++) if (row % 2 === 0 || Math.abs(row-y) < 3) out.push(makeLine(row, 0, '─'.repeat(deck.cols), row === y ? '#ffffff' : Math.abs(row-y) < 3 ? '#55ff55' : '#00aa00'));
  } else if (name === 'fireworks' || name === 'explosion') {
    drawBurstEffects(page, frame, out);
  } else {
    const local = makeRNG(pageSeed(page) + frame * 3571), chars = ' ░▒▓█!#$%&*+-=[]{}', colors = ['#aa0000', '#00aaaa', '#ffffff', '#aa00aa'];
    for (let band = 0; band < Math.max(3, Math.floor(deck.rows/5)); band++) {
      const y = local.int(deck.rows), xOffset = local.int(9) - 4, color = colors[local.int(colors.length)];
      for (let x = 0; x < deck.cols; x++) if (local.int(100) < 45) {
        const col = x + xOffset;
        if (col >= 0 && col < deck.cols) out.push(makeLine(y, col, chars[local.int(chars.length)], color));
      }
    }
    if (frame % 8 === 0) out.push(makeLine(local.int(deck.rows), 0, '▀'.repeat(deck.cols), '#ffffff'));
  }
  return out;
}
function drawFrame() {
  const page = presenterPageAt(pageIndex);
  if (!page) return;
  if (window.KEYNOPE_PRESENTER && !presenterPresenting && !presenterMainSurface && !keynopeAppSurface) {
    drawPresenterSnow();
    return;
  }
  if (keynopeCanvasRenderer && presenterTransitionUntil && performance.now() < presenterTransitionUntil) {
    drawChannelFlipTransition();
    if (presenterTimerMode === 'running') drawPresenterTestCard();
    drawPresenterTimer();
    drawEditorExportConfirmation();
    return;
  }
  presenterTransitionUntil = 0;
  const contentLines = presenterContentLinesFor(pageIndex, page);
  if (keynopeCanvasRenderer) {
    drawPresenterPage(page, frame, contentLines);
    if (presenterTimerMode === 'running') drawPresenterTestCard();
    drawPresenterTimer();
    drawEditorExportConfirmation();
    return;
  }
  const effectLinesWithBackground = (page.backgroundLines || []).concat(effectLines(page, frame));
  presenterCanvas.style.display = 'none';
  linkLayer.style.display = 'none';
  effectLayer.style.display = 'block';
  contentLayer.style.display = 'block';
  chromeLayer.style.display = 'block';
  effectLayer.innerHTML = renderLines(applyBackdropTransparency(effectLinesWithBackground, page.transparency || []));
  contentLayer.innerHTML = renderLines(contentLines) + renderLinkUnderlines(contentLines) + renderLinkHitAreas(contentLines);
}
function drawPresenterPage(page, frameValue, contentLines) {
  const effectLinesWithBackground = (page.backgroundLines || []).concat(effectLines(page, frameValue));
  presenterCanvas.style.display = 'block';
  linkLayer.style.display = 'block';
  linkLayer.innerHTML = renderCanvasLinkHitAreas(contentLines);
  effectLayer.style.display = 'none';
  contentLayer.style.display = 'none';
  chromeLayer.style.display = 'none';
  presenterContext.globalAlpha = 1;
  presenterContext.clearRect(0, 0, presenterCanvas.width, presenterCanvas.height);
  presenterContext.fillStyle = page.bg || '#000';
  presenterContext.fillRect(0, 0, presenterCanvas.width, presenterCanvas.height);
  drawCanvasBackdropTransparency(effectLinesWithBackground, page.transparency || []);
  drawCanvasLines(contentLines);
  drawCanvasLinkUnderlines(contentLines);
  drawCanvasPageLabel(page);
  drawPresenterPhosphor(presenterCanvas.width, presenterCanvas.height);
}
function drawPresenterPageFallback() {
  const page = presenterPageAt(pageIndex);
  if (!page) return;
  presenterCanvas.style.display = 'block';
  effectLayer.style.display = 'none';
  contentLayer.style.display = 'none';
  chromeLayer.style.display = 'none';
  presenterContext.globalAlpha = 1;
  presenterContext.clearRect(0, 0, presenterCanvas.width, presenterCanvas.height);
  presenterContext.fillStyle = page.bg || '#000';
  presenterContext.fillRect(0, 0, presenterCanvas.width, presenterCanvas.height);
  drawCanvasLines(page.lines || []);
  drawCanvasLinkUnderlines(page.lines || []);
  drawCanvasPageLabel(page);
  drawPresenterPhosphor(presenterCanvas.width, presenterCanvas.height);
}
function presenterPageAt(index) {
  if (!deck.pages || !deck.pages.length) return null;
  index = Math.max(0, Math.min(deck.pages.length - 1, index));
  return deck.pages[index] || null;
}
function presenterContentLinesFor(index, page) {
  if (!page) return [];
  if (index === pageIndex) {
    const contentFrames = decodedContentFrames(page);
    if (contentFrames && contentFrames.length) return contentFrames[frame % contentFrames.length].lines;
  }
  return page.lines || [];
}
function tick() {
  try {
    drawFrame();
  } catch (_err) {
    presenterTransitionUntil = 0;
    presenterContext.globalAlpha = 1;
    try {
      drawPresenterPageFallback();
    } catch (_fallbackErr) {
    }
  }
  setTimeout(() => { frame++; tick(); }, 70);
}
addEventListener('resize', () => { resize(); render(); });
function activateLink(target) {
  if (!target) return false;
  if (/^#?\d+$/.test(target)) {
    const slide = Math.max(1, parseInt(target.replace('#', ''), 10));
    const index = deck.pages.findIndex(page => page.slide === slide - 1 && page.page === 0);
    if (index >= 0) {
      const previousPageIndex = pageIndex;
      pageIndex = index;
      startPageTransition(previousPageIndex, pageIndex);
    }
    frame = 0;
    render();
    return true;
  }
  if (/^https?:\/\//i.test(target)) {
    open(target, '_blank', 'noopener');
    return true;
  }
  return false;
}
addEventListener('pointerdown', e => {
  const link = e.target.closest && e.target.closest('[data-link]');
  if (!link) return;
  const target = link.getAttribute('data-link') || '';
  if (activateLink(target)) {
    e.preventDefault();
    e.stopPropagation();
  }
}, true);
function publishPresenterPage(index) {
  if (!window.KEYNOPE_PRESENTER || !deck.pages || index < 0 || index >= deck.pages.length) return;
  const page = deck.pages[index];
  if (!page || !Number.isInteger(page.slide) || !Number.isInteger(page.page)) return;
  fetch('/api/editor/action', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({action: 'navigate-presentation', slide: page.slide, page: page.page})
  }).catch(() => {});
}
function publishPresenterTimer(seconds) {
  if (!window.KEYNOPE_PRESENTER) return;
  fetch('/api/editor/action', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify(seconds > 0 ? {action: 'start-timer', value: seconds} : {action: 'stop-timer'})
  }).catch(() => {});
}
addEventListener('keydown', e => {
  if (window.KEYNOPE_PRESENTER && presenterMainSurface) {
    if (presenterTimerMode === 'config') {
      if (/^\d$/.test(e.key) && String(presenterTimerInput || '').length < 4) {
        presenterTimerInput = String(presenterTimerInput || '') + e.key;
      } else if (e.key === 'Backspace') {
        presenterTimerInput = String(presenterTimerInput || '').slice(0, -1);
      } else if (e.key === 'Enter') {
        const raw = String(presenterTimerInput || '').padStart(4, '0').slice(-4);
        const minutes = Number(raw.slice(0, 2));
        const seconds = Number(raw.slice(2, 4));
        const total = Math.max(0, minutes * 60 + seconds);
        presenterTimerMode = total > 0 ? 'running' : '';
        presenterTimerEndMS = total > 0 ? Date.now() + total * 1000 : 0;
        publishPresenterTimer(total);
      } else if (e.key === 'Escape') {
        presenterTimerMode = '';
        presenterTimerInput = '';
        presenterTimerEndMS = 0;
      } else {
        return;
      }
      e.preventDefault();
      e.stopPropagation();
      render();
      return;
    }
    const previousPageIndex = pageIndex;
    if (['ArrowRight', ' ', 'n', 'PageDown'].includes(e.key)) pageIndex = Math.min(deck.pages.length - 1, pageIndex + 1);
    else if (['ArrowLeft', 'PageUp'].includes(e.key)) pageIndex = Math.max(0, pageIndex - 1);
    else if (e.key === 'Home') pageIndex = 0;
    else if (e.key === 'End') pageIndex = deck.pages.length - 1;
    else if (e.key === '0') {
      presenterTimerMode = 'config';
      presenterTimerInput = '';
      presenterTimerEndMS = 0;
    }
    else if (e.key === 'Escape') {
      presenterTimerMode = '';
      presenterTimerInput = '';
      presenterTimerEndMS = 0;
      publishPresenterTimer(0);
    }
    else if (e.key === 'q') {
      e.preventDefault();
      e.stopPropagation();
      if (window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.keynopePresenter) {
        window.webkit.messageHandlers.keynopePresenter.postMessage({action: 'stop'});
      }
      return;
    } else return;
    e.preventDefault();
    e.stopPropagation();
    startPageTransition(previousPageIndex, pageIndex);
    frame = 0;
    render();
    publishPresenterPage(pageIndex);
    return;
  }
  const previousPageIndex = pageIndex;
  if (['ArrowRight', ' ', 'n', 'PageDown'].includes(e.key)) pageIndex = Math.min(deck.pages.length - 1, pageIndex + 1);
  else if (['ArrowLeft', 'PageUp'].includes(e.key)) pageIndex = Math.max(0, pageIndex - 1);
  else if (e.key === 'Home') pageIndex = 0;
  else if (e.key === 'End') pageIndex = deck.pages.length - 1;
  else return;
  e.preventDefault();
  e.stopPropagation();
  frame = 0;
  startPageTransition(previousPageIndex, pageIndex);
  render();
  publishPresenterPage(pageIndex);
});
let presenterVersion = -1;
let presenterDeckVersion = -1;
let presenterPresenting = false;
let presenterTimerMode = '';
let presenterTimerInput = '';
let presenterTimerEndMS = 0;
let presenterTransitionStarted = 0;
let presenterTransitionUntil = 0;
let presenterTransitionFromIndex = 0;
let presenterTransitionToIndex = 0;
function startPageTransition(fromIndex, toIndex) {
  if (fromIndex === toIndex) return;
  presenterTransitionStarted = performance.now();
  presenterTransitionUntil = presenterTransitionStarted + 180;
  presenterTransitionFromIndex = fromIndex;
  presenterTransitionToIndex = toIndex;
}
async function refreshPresenterSlide(slideIndex) {
  if (!Number.isInteger(slideIndex) || slideIndex < 0) {
    location.reload();
    return false;
  }
  const response = await fetch('/slide?index=' + encodeURIComponent(slideIndex), {cache: 'no-store'});
  if (!response.ok) {
    return false;
  }
  const pages = await response.json();
  if (!Array.isArray(pages) || pages.length === 0) {
    return false;
  }
  const first = deck.pages.findIndex(page => page.slide === slideIndex);
  if (first < 0) {
    location.reload();
    return false;
  }
  let end = first;
  while (end < deck.pages.length && deck.pages[end].slide === slideIndex) end++;
  deck.pages.splice(first, end - first, ...pages);
  contentAnimationCache.clear();
  effectState.clear();
  pageIndex = Math.min(pageIndex, deck.pages.length - 1);
  return true;
}
async function syncPresenterState() {
  if (!window.KEYNOPE_PRESENTER) return;
  try {
    const response = await fetch('/state', {cache: 'no-store'});
    if (!response.ok) return;
    const state = await response.json();
    if (keynopeAppSurface && keynopeEditorMasterMode) return;
    const initialSync = presenterVersion < 0;
    let slideRefreshed = false;
    if (initialSync) {
      presenterDeckVersion = state.deckVersion || 0;
    } else if ((state.deckVersion || 0) !== presenterDeckVersion) {
      presenterDeckVersion = state.deckVersion || 0;
      slideRefreshed = await refreshPresenterSlide(Number.isInteger(state.deckSlide) ? state.deckSlide : -1);
      if (!slideRefreshed) return;
    }
    if (!initialSync && state.version === presenterVersion) return;
    presenterVersion = state.version;
    const wasPresenting = presenterPresenting;
    presenterPresenting = !!state.presenting;
    if (!presenterMainSurface) {
      presenterTimerMode = state.timerMode || '';
      presenterTimerInput = state.timerInput || '';
      presenterTimerEndMS = Number(state.timerEndMs || 0);
    }
    const index = deck.pages.findIndex(page => page.slide === state.slide && page.page === state.page);
    if (index >= 0 && index !== pageIndex) {
      if (presenterPresenting && wasPresenting) startPageTransition(pageIndex, index);
      pageIndex = index;
      frame = 0;
      render();
    } else if (slideRefreshed) {
      frame = 0;
      render();
    }
    refreshEditorPresenterControls();
  } catch (_err) {
  }
}
let keynopeAppFrameVersion = -1;
let keynopeTerminalCols = 0;
let keynopeTerminalRows = 0;
let keynopeTerminalChars = [];
let keynopeTerminalColors = [];
let keynopeTerminalRow = 0;
let keynopeTerminalCol = 0;
let keynopeTerminalColor = '#f3efe0';
let keynopeTerminalDrawPending = false;
const keynopeANSIColors = ['#000000', '#aa0000', '#00aa00', '#aa5500', '#0000aa', '#aa00aa', '#00aaaa', '#aaaaaa'];
const keynopeANSIBrightColors = ['#555555', '#ff5555', '#55ff55', '#ffff55', '#5555ff', '#ff55ff', '#55ffff', '#ffffff'];
function resetKeynopeTerminal(cols, rows) {
  keynopeTerminalCols = cols;
  keynopeTerminalRows = rows;
  keynopeTerminalChars = new Array(cols * rows).fill(' ');
  keynopeTerminalColors = new Array(cols * rows).fill('#f3efe0');
  keynopeTerminalRow = 0;
  keynopeTerminalCol = 0;
  keynopeTerminalColor = '#f3efe0';
}
function applyKeynopeSGR(raw) {
  const values = (raw === '' ? [0] : raw.split(';').map(value => Number(value || 0)));
  for (let i = 0; i < values.length; i++) {
    const value = values[i];
    if (value === 0 || value === 39) keynopeTerminalColor = '#f3efe0';
    else if (value >= 30 && value <= 37) keynopeTerminalColor = keynopeANSIColors[value - 30];
    else if (value >= 90 && value <= 97) keynopeTerminalColor = keynopeANSIBrightColors[value - 90];
    else if (value === 38 && values[i + 1] === 2 && i + 4 < values.length) {
      const component = index => Math.max(0, Math.min(255, values[index] || 0)).toString(16).padStart(2, '0');
      keynopeTerminalColor = '#' + component(i + 2) + component(i + 3) + component(i + 4);
      i += 4;
    } else if (value === 38 && values[i + 1] === 5 && i + 2 < values.length) {
      const index = Math.max(0, Math.min(255, values[i + 2] || 0));
      if (index < 8) keynopeTerminalColor = keynopeANSIColors[index];
      else if (index < 16) keynopeTerminalColor = keynopeANSIBrightColors[index - 8];
      i += 2;
    }
  }
}
function applyKeynopeANSI(value) {
  for (let i = 0; i < value.length;) {
    if (value.charCodeAt(i) === 27 && value[i + 1] === '[') {
      let end = i + 2;
      while (end < value.length && !/[A-Za-z]/.test(value[end])) end++;
      if (end >= value.length) break;
      const params = value.slice(i + 2, end);
      const command = value[end];
      if (command === 'H' || command === 'f') {
        const fields = params.split(';');
        keynopeTerminalRow = Math.max(0, Math.min(keynopeTerminalRows - 1, Number(fields[0] || 1) - 1));
        keynopeTerminalCol = Math.max(0, Math.min(keynopeTerminalCols - 1, Number(fields[1] || 1) - 1));
      } else if (command === 'J' && (params === '' || params === '2')) {
        keynopeTerminalChars.fill(' ');
        keynopeTerminalColors.fill('#f3efe0');
        keynopeTerminalRow = 0;
        keynopeTerminalCol = 0;
      } else if (command === 'm') {
        applyKeynopeSGR(params);
      }
      i = end + 1;
      continue;
    }
    const codePoint = value.codePointAt(i);
    const character = String.fromCodePoint(codePoint);
    i += character.length;
    if (character === '\n') {
      keynopeTerminalRow++;
      keynopeTerminalCol = 0;
      continue;
    }
    if (character === '\r') {
      keynopeTerminalCol = 0;
      continue;
    }
    if (keynopeTerminalRow >= 0 && keynopeTerminalRow < keynopeTerminalRows && keynopeTerminalCol >= 0 && keynopeTerminalCol < keynopeTerminalCols) {
      const index = keynopeTerminalRow * keynopeTerminalCols + keynopeTerminalCol;
      keynopeTerminalChars[index] = character;
      keynopeTerminalColors[index] = keynopeTerminalColor;
    }
    keynopeTerminalCol++;
    if (keynopeTerminalCol >= keynopeTerminalCols) {
      keynopeTerminalCol = 0;
      keynopeTerminalRow++;
    }
  }
}
function drawKeynopeTerminal() {
  keynopeTerminalDrawPending = false;
  presenterCanvas.style.display = 'block';
  effectLayer.style.display = 'none';
  contentLayer.style.display = 'none';
  chromeLayer.style.display = 'none';
  presenterContext.clearRect(0, 0, presenterCanvas.width, presenterCanvas.height);
  presenterContext.fillStyle = '#000000';
  presenterContext.fillRect(0, 0, presenterCanvas.width, presenterCanvas.height);
  presenterContext.font = canvasFont;
  presenterContext.textBaseline = 'top';
  for (let row = 0; row < keynopeTerminalRows; row++) {
    let col = 0;
    while (col < keynopeTerminalCols) {
      const start = row * keynopeTerminalCols + col;
      const color = keynopeTerminalColors[start];
      let text = keynopeTerminalChars[start];
      let end = col + 1;
      while (end < keynopeTerminalCols && keynopeTerminalColors[row * keynopeTerminalCols + end] === color) {
        text += keynopeTerminalChars[row * keynopeTerminalCols + end];
        end++;
      }
      if (text.trim() !== '') {
        presenterContext.fillStyle = color;
        presenterContext.fillText(text, col * canvasCharWidth, row * canvasCell);
      }
      col = end;
    }
  }
}
function renderKeynopeAppFrame(next) {
  if (!next || !Number.isInteger(next.version) || next.version <= keynopeAppFrameVersion) return;
  keynopeAppFrameVersion = next.version;
  if (next.cols > 0 && next.rows > 0 && (deck.cols !== next.cols || deck.rows !== next.rows)) {
    deck.cols = next.cols;
    deck.rows = next.rows;
    resize();
  }
  if (keynopeTerminalCols !== next.cols || keynopeTerminalRows !== next.rows) {
    resetKeynopeTerminal(next.cols, next.rows);
  }
  applyKeynopeANSI(next.ansi || '');
  if (!keynopeTerminalDrawPending) {
    keynopeTerminalDrawPending = true;
    requestAnimationFrame(drawKeynopeTerminal);
  }
}
function startKeynopeAppFrames() {
  const events = new EventSource('/terminal-events');
  events.onmessage = event => {
    try { renderKeynopeAppFrame(JSON.parse(event.data)); } catch (_err) {}
  };
}
function sendKeynopeAppInput(value) {
  let input = value;
  if (value instanceof Uint8Array) input = Array.from(value, byte => String.fromCharCode(byte)).join('');
  if (!input || typeof input !== 'string') return;
  const handler = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.keynopeInput;
  if (handler) handler.postMessage(input);
}
function keynopeTerminalSequence(e) {
  if (e.metaKey) {
    const key = e.key.toLowerCase();
    if (key === 'c') return new Uint8Array([3]);
    if (key === 'x') return new Uint8Array([24]);
    if (key === 'v') return new Uint8Array([22]);
    if (key === 'z') return new Uint8Array([e.shiftKey ? 25 : 26]);
    return null;
  }
  if (e.ctrlKey && e.key.length === 1) {
    const code = e.key.toUpperCase().charCodeAt(0);
    if (code >= 64 && code <= 95) return new Uint8Array([code - 64]);
  }
  const special = {
    ArrowUp: e.shiftKey ? '\x1b[1;2A' : '\x1b[A',
    ArrowDown: e.shiftKey ? '\x1b[1;2B' : '\x1b[B',
    ArrowRight: e.shiftKey ? '\x1b[1;2C' : '\x1b[C',
    ArrowLeft: e.shiftKey ? '\x1b[1;2D' : '\x1b[D',
    Enter: e.shiftKey ? '\x1b[13;2u' : '\r',
    Backspace: '\x7f', Escape: '\x1b', Tab: e.shiftKey ? '\x1b[Z' : '\t',
    PageUp: '\x1b[5~', PageDown: '\x1b[6~', Home: '\x1b[H', End: '\x1b[F', Delete: '\x1b[3~'
  };
  if (special[e.key]) return special[e.key];
  if (e.key.length === 1 && !e.altKey) return e.key;
  return null;
}
if (keynopeAppSurface) {
  let editorState = null;
  let editorStateVersion = -1;
  let activeInlineEditor = null;
  let suppressSelectionTopbar = false;
  let pendingCanvasSelection = null;
  let editorStatus = null;
  let editorNormalPages = null;
  let editorWorkspaceSequence = 0;
  let editorMutationPreviewSequence = 0;
  let activeCanvasVisualMenu = null;
  let activeCanvasLinkDialog = null;
  async function editorAction(action) {
    const enteringMasters = action.action === 'toggle-master-mode' && editorState && !editorState.masterMode;
    if (enteringMasters) editorNormalPages = (deck.pages || []).slice();
    const response = await fetch('/api/editor/action', {
      method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(action)
    });
    if (!response.ok) {
      const message = (await response.text()).trim() || 'Editor action failed';
      if (editorStatus) editorStatus.textContent = message;
      throw new Error(message);
    }
    editorState = await response.json();
    keynopeEditorMasterMode = !!editorState.masterMode;
    editorStateVersion = editorState.version;
    renderEditorPanels();
    await syncEditorWorkspace();
  }
  function editorButton(label, action, parent) {
    const button = document.createElement('button');
    button.type = 'button';
    button.textContent = label;
    button.addEventListener('click', () => editorAction(action).catch(() => {}));
    parent.appendChild(button);
    return button;
  }
  function editorField(parent, label, value, multiline, changed) {
    const wrapper = document.createElement('label');
    wrapper.className = 'keynope-editor-field';
    const caption = document.createElement('span');
    caption.textContent = label;
    const input = multiline ? document.createElement('textarea') : document.createElement('input');
    input.value = value == null ? '' : String(value);
    input.addEventListener('change', () => changed(input.value));
    wrapper.append(caption, input);
    parent.appendChild(wrapper);
    return input;
  }
  function editorNumber(parent, label, value, options, changed) {
    const input = editorField(parent, label, value, false, changed);
    input.type = 'number';
    if (options && options.min != null) input.min = String(options.min);
    if (options && options.max != null) input.max = String(options.max);
    if (options && options.step != null) input.step = String(options.step);
    return input;
  }
  function editorSelect(parent, label, value, options, changed) {
    const wrapper = document.createElement('label');
    wrapper.className = 'keynope-editor-field';
    const caption = document.createElement('span');
    caption.textContent = label;
    const select = document.createElement('select');
    for (const optionValue of options) {
      const option = document.createElement('option');
      option.value = optionValue === 'none' ? '' : optionValue;
      option.textContent = optionValue;
      option.selected = option.value === (value || '');
      select.appendChild(option);
    }
    select.addEventListener('change', () => changed(select.value));
    wrapper.append(caption, select);
    parent.appendChild(wrapper);
  }
  function editorColor(parent, label, value, fallback, changed) {
    const wrapper = document.createElement('label');
    wrapper.className = 'keynope-editor-field';
    const caption = document.createElement('span');
    caption.textContent = label;
    const input = document.createElement('input');
    input.type = 'color';
    const rgb = /^(?:38|48);2;(\d+);(\d+);(\d+)$/.exec(value || '');
    input.value = /^#[0-9a-f]{6}$/i.test(value || '') ? value : rgb
      ? '#' + rgb.slice(1).map(part => Math.max(0, Math.min(255, Number(part))).toString(16).padStart(2, '0')).join('')
      : fallback;
    input.addEventListener('input', () => changed(input.value));
    wrapper.append(caption, input);
    parent.appendChild(wrapper);
  }
  const topbar = document.createElement('div');
  topbar.className = 'keynope-editor-topbar';
  const mainTopbar = document.createElement('div');
  mainTopbar.className = 'keynope-topbar-mode';
  const selectionTopbar = document.createElement('div');
  selectionTopbar.className = 'keynope-topbar-mode';
  selectionTopbar.hidden = true;
  topbar.append(mainTopbar, selectionTopbar);
  function svgToolbarButton(title, drawing, action) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'keynope-svg-button';
    button.title = title;
    button.setAttribute('aria-label', title);
    button.innerHTML = '<svg viewBox="0 0 20 20" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">' + drawing + '</svg>';
    button.addEventListener('click', action);
    mainTopbar.appendChild(button);
    return button;
  }
  const addSlideButton = svgToolbarButton('Add slide', '<path d="M5 2.5h7l3 3V17.5H5z"/><path d="M12 2.5v3h3M2 11h6M5 8v6"/>', () => editorAction({action: 'add-slide'}).catch(() => {}));
  const cloneSlideButton = editorButton('⿻', {action: 'clone-slide'}, mainTopbar);
  cloneSlideButton.classList.add('keynope-icon-button');
  cloneSlideButton.title = 'Clone slide';
  cloneSlideButton.setAttribute('aria-label', 'Clone slide');
  const deleteSlideButton = editorButton('🗑️', {action: 'delete-slide'}, mainTopbar);
  deleteSlideButton.classList.add('keynope-icon-button');
  deleteSlideButton.title = 'Delete slide';
  deleteSlideButton.setAttribute('aria-label', 'Delete slide');
  const appearanceButton = svgToolbarButton('Appearance', '<path d="M4 4h12M4 10h12M4 16h12"/><circle cx="8" cy="4" r="2" fill="currentColor"/><circle cx="13" cy="10" r="2" fill="currentColor"/><circle cx="7" cy="16" r="2" fill="currentColor"/>', () => {
    const rect = appearanceButton.getBoundingClientRect();
    showSlideContextMenu(editorState ? editorState.current : -1, rect.left, rect.bottom + 5);
  });
  const slideSeparator = document.createElement('span');
  slideSeparator.className = 'keynope-editor-separator';
  mainTopbar.appendChild(slideSeparator);
  function addElementIconButton(title, kind, drawing, level, activate) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'keynope-svg-button';
    button.title = title;
    button.setAttribute('aria-label', title);
    button.innerHTML = '<svg viewBox="0 0 20 20" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">' + drawing + '</svg>';
    button.addEventListener('click', () => {
      if (activate) activate(button);
      else editorAction({action: 'add-element', kind, level:level || 0}).catch(() => {});
    });
    mainTopbar.appendChild(button);
    return button;
  }
  addElementIconButton('Add title', 'heading', '<path d="M3 4v12M11 4v12M3 10h8"/><path d="M14 7h3v9M14 16h5"/>', 1);
  addElementIconButton('Add subtitle', 'heading', '<path d="M3 4v12M11 4v12M3 10h8"/><text x="13" y="17" fill="currentColor" stroke="none" font-size="11" font-family="-apple-system, sans-serif" font-weight="700">2</text>', 2);
  addElementIconButton('Add text', 'text', '<path d="M3 4h14M10 4v12M7 16h6"/>');
  addElementIconButton('Add bullet point', 'bullet', '<circle cx="4" cy="6" r="1" fill="currentColor" stroke="none"/><circle cx="4" cy="14" r="1" fill="currentColor" stroke="none"/><path d="M8 6h9M8 14h9"/>');
  addElementIconButton('Add code', 'code', '<path d="M7 5 3 10l4 5M13 5l4 5-4 5M11 3 9 17"/>');
  const addShapeMenu = document.createElement('div');
  addShapeMenu.className = 'keynope-add-shape-menu';
  addShapeMenu.hidden = true;
  document.body.appendChild(addShapeMenu);
  function closeAddShapeMenu() {
    addShapeMenu.hidden = true;
  }
  function toggleAddShapeMenu(anchor) {
    if (!addShapeMenu.hidden) { closeAddShapeMenu(); return; }
    addShapeMenu.hidden = false;
    const rect = anchor.getBoundingClientRect();
    const menuRect = addShapeMenu.getBoundingClientRect();
    addShapeMenu.style.left = Math.max(8, Math.min(innerWidth - menuRect.width - 8, rect.left)) + 'px';
    addShapeMenu.style.top = Math.max(8, Math.min(innerHeight - menuRect.height - 8, rect.bottom + 6)) + 'px';
  }
  const addShapeButton = addElementIconButton('Add shape', 'shape', '<circle cx="7" cy="8" r="4"/><path d="m13 7 4 8H9z"/>', 0, toggleAddShapeMenu);
  for (const [shape, label, drawing] of [
    ['circle','Circle','<circle cx="20" cy="18" r="13"/>'],
    ['square','Square','<rect x="7" y="5" width="26" height="26" rx="1"/>'],
    ['triangle','Triangle','<path d="M20 4 35 31H5z"/>'],
    ['diamond','Diamond','<path d="m20 3 16 15-16 15L4 18z"/>']
  ]) {
    const button = document.createElement('button');
    button.type = 'button';
    button.title = 'Add ' + label.toLowerCase();
    button.setAttribute('aria-label', button.title);
    button.innerHTML = '<svg viewBox="0 0 40 36" aria-hidden="true" fill="currentColor" stroke="currentColor" stroke-width="1.5">' + drawing + '</svg>';
    button.addEventListener('click', () => {
      closeAddShapeMenu();
      editorAction({action: 'add-element', kind:'shape', name:shape}).catch(() => {});
    });
    addShapeMenu.appendChild(button);
  }
  document.addEventListener('pointerdown', event => {
    if (!addShapeMenu.contains(event.target) && event.target !== addShapeButton) closeAddShapeMenu();
  }, true);
  const imageInput = document.createElement('input');
  imageInput.type = 'file';
  imageInput.accept = 'image/*';
  imageInput.hidden = true;
  imageInput.addEventListener('change', async () => {
    if (!imageInput.files || !imageInput.files[0]) return;
    const body = new FormData();
    body.append('image', imageInput.files[0]);
    const response = await fetch('/api/editor/upload', {method: 'POST', body});
    if (response.ok) {
      editorState = await response.json();
      editorStateVersion = editorState.version;
      renderEditorPanels();
    }
    imageInput.value = '';
  });
  const importButton = document.createElement('button');
  importButton.innerHTML = '<svg viewBox="-13 80 1050 1040" aria-hidden="true"><path d="M-13 1120V80h1050v1040Zm292-428q-18 0-27.5-17t-9.5-35.5 8-28q-4-10-4-23 0-12 4-23.75t13.5-15.25q-4.5-9-4.5-22 0-20.5 6-38.5t12.5-18 12.5 17.75 6 38.75q0 13-4.5 22 10 3.5 13.75 15t3.75 23q0 7.5-1.25 13.75T305 611.5q8.5 10 8.5 30.5 0 17.5-8.5 33.75T279 692m130.5 32.5q-15 0-33.5-4.75t-32-14.25-13.5-24q0-13.5 6.5-22.75T353 648q-3.5-6.5-3.5-13.5 0-10.5 7.5-18.25t18.5-9.75q-1-3-1-6 0-11 5.75-18.5t13.75-7.5q7.5 0 12.5 7t5 17q0 4.5-1.5 10 13 12 13 28 0 9-5 15.5 11 9 16.75 20t5.75 22.5q0 16-7 23t-24 7m266 97.5q-81.5 0-151.25-23T378.5 742q-54-24.5-99-38.25T191 684l6-40q46 6.5 93.75 21.5T394.5 706q74.5 34 139.75 55t140.25 21q34 0 70.75-5t79.25-15.5l10 39q-44.5 11-84 16.25t-75 5.25M27 1080h970V120H27Zm147-145.5v-669h676v669Zm40-40h596v-589H214ZM404.5 750v-40q40 0 73.5-2t64-7q24.5-4 47.75-11.25A1586 1586 0 0 0 637.5 674q40-14.5 86-26.5T829 633l2 40q-56 2-98.75 13.5t-82.25 25q-24.5 8.5-49.25 16T549 740q-32 6-67.25 8t-77.25 2m84-193q-29.5 0-46-24-17.5 1.5-30-9.25T400 497q0-15.5 10.5-26.25t26-12.25Q434 447 434 437q0-28 20.5-48t48.5-20q23.5 0 43 14.5t24.5 36.5q27.5-1.5 41.75 9.75T625 464.5q17.5-1.5 30.25 9.5T668 499t-13.75 24.25T621 533.5q-11.5 0-23.5-4-13 17-39 17-16 0-28.5-8-17.5 18.5-41.5 18.5m129 130q0-17 10.5-30.5t25-20 26.5-3q2.5-16 17-24.5t28.5-8.5q17.5 0 22.5 10.5 5-16.5 24.75-30t44.25-13.5V642q-50.5 0-104.75 14.75T617.5 687"/></svg>';
  importButton.className = 'keynope-icon-button keynope-monochrome-icon';
  importButton.title = 'Import image';
  importButton.setAttribute('aria-label', 'Import image');
  importButton.addEventListener('click', () => imageInput.click());
  mainTopbar.append(importButton, imageInput);
  const editSeparator = document.createElement('span');
  editSeparator.className = 'keynope-editor-separator';
  mainTopbar.appendChild(editSeparator);
  const undoButton = editorButton('⟲', {action: 'undo'}, mainTopbar);
  undoButton.classList.add('keynope-icon-button', 'keynope-history-button');
  undoButton.innerHTML = '<span class="keynope-history-icon" aria-hidden="true">⟲</span><span class="keynope-history-label">UNDO</span>';
  undoButton.title = 'Undo';
  undoButton.setAttribute('aria-label', 'Undo');
  const redoButton = editorButton('⟳', {action: 'redo'}, mainTopbar);
  redoButton.classList.add('keynope-icon-button', 'keynope-history-button');
  redoButton.innerHTML = '<span class="keynope-history-icon" aria-hidden="true">⟳</span><span class="keynope-history-label">REDO</span>';
  redoButton.title = 'Redo';
  redoButton.setAttribute('aria-label', 'Redo');
  const exportButton = document.createElement('button');
  exportButton.type = 'button';
  exportButton.innerHTML = '<span class="keynope-monochrome-glyph" aria-hidden="true">🌐︎</span>';
  exportButton.addEventListener('click', () => editorAction({action: 'export'}).then(showEditorExportConfirmation).catch(() => {}));
  mainTopbar.appendChild(exportButton);
  exportButton.classList.add('keynope-icon-button', 'keynope-monochrome-icon');
  exportButton.title = 'Export to HTML';
  exportButton.setAttribute('aria-label', 'Export to HTML');
  document.body.appendChild(topbar);

  const slidesPanel = document.createElement('aside');
  slidesPanel.className = 'keynope-editor-panel keynope-editor-slides';
  document.body.appendChild(slidesPanel);
  const masterModeButton = document.createElement('button');
  masterModeButton.type = 'button';
  masterModeButton.textContent = 'M';
  masterModeButton.title = 'Master slides';
  masterModeButton.setAttribute('aria-label', 'Master slides');
  masterModeButton.addEventListener('click', () => editorAction({action: 'toggle-master-mode'}).catch(() => {}));
  const inspector = document.createElement('div');
  const slideContextMenu = document.createElement('div');
  slideContextMenu.className = 'keynope-slide-context';
  document.body.appendChild(slideContextMenu);
  const canvasOverlay = document.createElement('div');
  canvasOverlay.className = 'keynope-canvas-overlay';
  stage.appendChild(canvasOverlay);
  const speakerNotesPanel = document.createElement('section');
  speakerNotesPanel.className = 'keynope-speaker-notes';
  const speakerNotesLabel = document.createElement('label');
  speakerNotesLabel.textContent = 'Speaker notes';
  const speakerNotesInput = document.createElement('textarea');
  speakerNotesInput.setAttribute('aria-label', 'Speaker notes');
  speakerNotesInput.placeholder = 'Add notes for this slide…';
  speakerNotesPanel.append(speakerNotesLabel, speakerNotesInput);
  document.body.appendChild(speakerNotesPanel);
  let speakerNotesSlide = -1;
  let speakerNotesSaveTimer = null;
  let speakerNotesDirty = false;
  let speakerNotesDirtySlide = -1;
  let speakerNotesDirtyValue = '';
  let speakerNotesSavingSlide = -1;
  let notesToggleButton = null;
  function setSpeakerNotesVisible(visible, focusNotes) {
    editorSpeakerNotesVisible = !!visible;
    document.documentElement.setAttribute('data-keynope-notes', editorSpeakerNotesVisible ? 'true' : 'false');
    if (notesToggleButton) {
      notesToggleButton.classList.toggle('active', editorSpeakerNotesVisible);
      notesToggleButton.setAttribute('aria-pressed', editorSpeakerNotesVisible ? 'true' : 'false');
    }
    resize();
    requestAnimationFrame(renderEditorCanvasOverlay);
    if (editorSpeakerNotesVisible && focusNotes) requestAnimationFrame(() => speakerNotesInput.focus());
  }
  function renderSpeakerNotes(slide) {
    if (!slide) return;
    const changedSlide = speakerNotesSlide !== editorState.current;
    if (changedSlide || (document.activeElement !== speakerNotesInput && speakerNotesSavingSlide !== editorState.current && !speakerNotesDirty)) {
      speakerNotesInput.value = slide.notes || '';
      speakerNotesSlide = editorState.current;
    }
  }
  function flushSpeakerNotes() {
    if (!speakerNotesDirty || speakerNotesDirtySlide < 0) return;
    if (speakerNotesSaveTimer) {
      clearTimeout(speakerNotesSaveTimer);
      speakerNotesSaveTimer = null;
    }
    const slide = speakerNotesDirtySlide;
    const notes = speakerNotesDirtyValue;
    speakerNotesDirty = false;
    speakerNotesSavingSlide = slide;
    editorAction({action: 'update-slide-notes', slide, notes}).then(() => {
      if (speakerNotesSavingSlide === slide) speakerNotesSavingSlide = -1;
    }).catch(() => {
      speakerNotesSavingSlide = -1;
      speakerNotesDirty = true;
      speakerNotesDirtySlide = slide;
      speakerNotesDirtyValue = notes;
    });
  }
  speakerNotesInput.addEventListener('input', () => {
    speakerNotesDirty = true;
    speakerNotesDirtySlide = editorState ? editorState.current : -1;
    speakerNotesDirtyValue = speakerNotesInput.value;
    if (speakerNotesSaveTimer) clearTimeout(speakerNotesSaveTimer);
    speakerNotesSaveTimer = setTimeout(flushSpeakerNotes, 2000);
  });
  speakerNotesInput.addEventListener('keydown', async event => {
    event.stopPropagation();
    if (!(event.ctrlKey || event.metaKey) || !['c','x','v'].includes(event.key.toLowerCase())) return;
    event.preventDefault();
    const command = event.key.toLowerCase();
    const start = speakerNotesInput.selectionStart || 0;
    const end = speakerNotesInput.selectionEnd || 0;
    try {
      if (command === 'c' || command === 'x') {
        if (start === end) return;
        await navigator.clipboard.writeText(speakerNotesInput.value.slice(start, end));
        if (command === 'x') {
          speakerNotesInput.setRangeText('', start, end, 'start');
          speakerNotesInput.dispatchEvent(new Event('input', {bubbles:true}));
        }
      } else {
        const pasted = await navigator.clipboard.readText();
        speakerNotesInput.setRangeText(pasted, start, end, 'end');
        speakerNotesInput.dispatchEvent(new Event('input', {bubbles:true}));
      }
    } catch (_err) {
      document.execCommand(command === 'c' ? 'copy' : command === 'x' ? 'cut' : 'paste');
    }
  });
  speakerNotesInput.addEventListener('keyup', event => event.stopPropagation());
  speakerNotesInput.addEventListener('blur', flushSpeakerNotes);
  document.addEventListener('pointerdown', event => {
    if (!speakerNotesPanel.contains(event.target)) flushSpeakerNotes();
  }, true);

  let editorPreviewOriginal = null;
  let editorPreviewSequence = 0;
  function replaceEditorPreviewPages(slide, pages) {
    const first = deck.pages.findIndex(page => page.slide === slide);
    if (first < 0 || !Array.isArray(pages) || !pages.length) return;
    let end = first;
    while (end < deck.pages.length && deck.pages[end].slide === slide) end++;
    const currentPage = deck.pages[pageIndex] && deck.pages[pageIndex].slide === slide ? deck.pages[pageIndex].page : 0;
    deck.pages.splice(first, end - first, ...pages);
    const next = deck.pages.findIndex(page => page.slide === slide && page.page === currentPage);
    pageIndex = next >= 0 ? next : first;
  }
  function restoreEditorPreview() {
    if (!editorPreviewOriginal) return;
    replaceEditorPreviewPages(editorPreviewOriginal.slide, editorPreviewOriginal.pages);
    editorPreviewOriginal = null;
  }
  async function previewEditorText(index, element, sequence) {
    try {
      const response = await fetch('/api/editor/preview', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({element: index, elementData: element, cols: deck.cols, rows: deck.rows})
      });
      if (!response.ok) return;
      const pages = await response.json();
      if (!activeInlineEditor || activeInlineEditor.element !== index || sequence !== editorPreviewSequence) return;
      replaceEditorPreviewPages(editorState.current, pages);
      drawFrame();
    } catch (_err) {}
  }
  function editorElementSignature(element) {
    if (!element) return '';
    return [element.kind || '', element.level || 0, element.text || '', element.path || '', element.query || '', element.id || '', element.slotId || ''].join('\u001f');
  }
  function editorElementIndexMaps() {
    const raw = editorState && editorState.slides && editorState.slides[editorState.current] ? (editorState.slides[editorState.current].elements || []) : [];
    const resolved = editorState && editorState.resolved && editorState.resolved[editorState.current] ? (editorState.resolved[editorState.current].elements || []) : raw;
    const rawToResolved = new Map();
    const resolvedToRaw = new Map();
    const used = new Set();
    raw.forEach((element, rawIndex) => {
      const signature = editorElementSignature(element);
      let resolvedIndex = resolved.findIndex((candidate, index) => !used.has(index) && editorElementSignature(candidate) === signature);
      if (resolvedIndex < 0 && rawIndex < resolved.length && !used.has(rawIndex)) resolvedIndex = rawIndex;
      if (resolvedIndex < 0) return;
      used.add(resolvedIndex);
      rawToResolved.set(rawIndex, resolvedIndex);
      resolvedToRaw.set(resolvedIndex, rawIndex);
    });
    return {rawToResolved, resolvedToRaw};
  }
  function beginInlineEdit(index, hit) {
    if (!editorState || !editorState.slides || !editorState.slides[editorState.current]) return;
    const original = editorState.slides[editorState.current].elements[index];
    if (!original || !['heading','text','bullet','code'].includes(original.kind)) {
      if (editorStatus) editorStatus.textContent = 'Select a text element to edit its content';
      return;
    }
    if (!hit) hit = canvasOverlay.querySelector('[data-element="' + index + '"]');
    if (!hit) return;
    if (activeInlineEditor) activeInlineEditor.finish(true);
    if (pendingCanvasSelection) {
      clearTimeout(pendingCanvasSelection);
      pendingCanvasSelection = null;
    }
    const element = {...original};
    const originalText = element.text || '';
    const resolvedElement = editorElementIndexMaps().rawToResolved.get(index);
    const editor = document.createElement('textarea');
    editor.className = 'keynope-inline-capture';
    editor.setAttribute('aria-label', 'Edit element text');
    editor.spellcheck = false;
    editor.value = element.text || '';
    const slide = editorState.current;
    const first = deck.pages.findIndex(page => page.slide === slide);
    let end = first;
    while (end >= 0 && end < deck.pages.length && deck.pages[end].slide === slide) end++;
    editorPreviewOriginal = first >= 0 ? {slide, pages: deck.pages.slice(first, end)} : null;
    let finished = false;
    const finish = save => {
      if (finished) return;
      finished = true;
      activeInlineEditor = null;
      suppressSelectionTopbar = true;
      const text = editor.value;
      editor.remove();
      editorCanvasCaret = null;
      mainTopbar.hidden = false;
      selectionTopbar.hidden = true;
      selectionTopbar.replaceChildren();
      let completion = Promise.resolve();
      if (save && text !== originalText) {
        const originalPreview = editorPreviewOriginal;
        editorPreviewOriginal = null;
        element.text = text;
        completion = editorAction({action: 'update-element', element: index, elementData: element}).catch(() => {
          editorPreviewOriginal = originalPreview;
          restoreEditorPreview();
          drawFrame();
        });
      } else {
        restoreEditorPreview();
        drawFrame();
      }
      completion.finally(() => {
        editorAction({action: 'select-element', element: -1}).catch(() => {}).finally(() => {
          suppressSelectionTopbar = false;
          renderEditorTopbar();
        });
      });
      if (editorStatus) editorStatus.textContent = save ? 'Text saved' : 'Editing cancelled';
    };
    const updateCaret = () => {
      editorCanvasCaret = {element: resolvedElement == null ? index : resolvedElement, text: editor.value, cursor: editor.selectionStart, started: performance.now()};
      drawFrame();
    };
    activeInlineEditor = {element: index, finish, editor};
    renderEditorTopbar();
    editor.addEventListener('input', () => {
      element.text = editor.value;
      updateCaret();
      const sequence = ++editorPreviewSequence;
      previewEditorText(index, {...element}, sequence);
    });
    editor.addEventListener('select', updateCaret);
    editor.addEventListener('click', updateCaret);
    editor.addEventListener('keyup', updateCaret);
    editor.addEventListener('keydown', keyEvent => {
      if (keyEvent.key === 'Enter' && !keyEvent.shiftKey) {
        keyEvent.preventDefault();
        finish(true);
      } else if (keyEvent.key === 'Escape') {
        keyEvent.preventDefault();
        finish(false);
      }
      keyEvent.stopPropagation();
    });
    editor.addEventListener('blur', () => finish(true), {once: true});
    canvasOverlay.appendChild(editor);
    editor.focus();
    editor.setSelectionRange(editor.value.length, editor.value.length);
    updateCaret();
    if (editorStatus) editorStatus.textContent = 'Editing text · Enter save · Shift+Enter newline · Esc cancel';
  }

  function canvasTool(label, className, action) {
    const button = document.createElement('button');
    button.type = 'button';
    button.textContent = label;
    if (className) button.className = className;
    button.addEventListener('pointerdown', event => { event.preventDefault(); event.stopPropagation(); });
    button.addEventListener('click', event => {
      event.preventDefault();
      event.stopPropagation();
      action();
    });
    return button;
  }

  function canvasElementAt(index) {
    if (!editorState || !editorState.slides || !editorState.slides[editorState.current]) return null;
    const element = editorState.slides[editorState.current].elements[index];
    return element ? {...element} : null;
  }
  function updateCanvasElement(index, mutate) {
    const element = canvasElementAt(index);
    if (!element) return;
    mutate(element);
    previewCanvasMutation(index, element);
    editorAction({action: 'update-element', element: index, elementData: element}).catch(() => {});
  }
  function setCanvasTextKind(index, kind, level) {
    updateCanvasElement(index, element => {
      element.kind = kind;
      element.level = kind === 'heading' ? level : 0;
    });
  }
  function appendCanvasTextKindTools(container, index, element) {
    const choices = [
      ['H1', 'heading', 1, element.kind === 'heading' && element.level !== 2],
      ['H2', 'heading', 2, element.kind === 'heading' && element.level === 2],
      ['T', 'text', 0, element.kind === 'text' || element.kind === 'text-image'],
      ['⏺', 'bullet', 0, element.kind === 'bullet']
    ];
    for (const [label, kind, level, active] of choices) {
      const button = canvasTool(label, active ? 'active' : '', () => setCanvasTextKind(index, kind, level));
      button.title = kind === 'heading' ? 'Convert to heading ' + level : kind === 'bullet' ? 'Convert to bullet point' : 'Convert to plain text';
      button.setAttribute('aria-label', button.title);
      container.appendChild(button);
    }
  }
  async function previewCanvasMutation(index, element, refreshOverlay = true) {
    const sequence = ++editorMutationPreviewSequence;
    const slide = editorState ? editorState.current : -1;
    try {
      const response = await fetch('/api/editor/preview', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({element: index, elementData: element, cols: deck.cols, rows: deck.rows})
      });
      if (!response.ok) return;
      const pages = await response.json();
      if (sequence !== editorMutationPreviewSequence || !editorState || editorState.current !== slide) return;
      replaceEditorPreviewPages(slide, pages);
      drawFrame();
      if (refreshOverlay) requestAnimationFrame(renderEditorCanvasOverlay);
    } catch (_err) {}
  }
  async function fitCanvasTextElement(index, element, boxWidth, boxHeight) {
    const response = await fetch('/api/editor/fit-text', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({element: index, elementData: element, boxWidth, boxHeight, cols: deck.cols, rows: deck.rows})
    });
    if (!response.ok) throw new Error('Could not fit text');
    return response.json();
  }
  function canvasTextNativeSize(element) {
    if (element.kind === 'heading') return Number(element.level || 1) === 1 ? 20 : 10;
    return 0;
  }
  function canvasTextSize(element) {
    const query = new URLSearchParams(element.query || '');
    const explicit = Number.parseInt(query.get('text-size'), 10);
    if (Number.isInteger(explicit)) return Math.max(-1, Math.min(25, explicit));
    const scale = Number.parseFloat(query.get('scale') || '1');
    if (query.get('render') === 'text-image' && Number.isFinite(scale)) {
      const source = query.get('source') || '';
      if (source === 'bitmap') {
        if (scale < 1) return Math.max(-1, Math.min(-1, Math.round((scale - 1) / .1)));
        if (scale < 2) return Math.max(1, Math.min(9, Math.round((scale - 1) * 10)));
        if (scale < 4) return Math.max(11, Math.min(19, 10 + Math.round((scale - 2) / .2)));
        return Math.max(21, Math.min(25, 20 + Math.round((scale - 4) / .2)));
      }
    }
    return canvasTextNativeSize(element);
  }
  function applyCanvasTextSize(element, size) {
    size = Math.max(-1, Math.min(25, size));
    const query = new URLSearchParams(element.query || '');
    if (size === canvasTextNativeSize(element)) {
      for (const key of ['render','source','scale','text-size']) query.delete(key);
    } else {
      let scale = 1;
      if (size < 0) scale = 1 + size * .1;
      else if (size < 10) scale = 1 + size / 10;
      else if (size < 20) scale = 2 + (size - 10) * .2;
      else scale = 4 + (size - 20) * .2;
      query.set('render', 'text-image');
      query.set('source', 'bitmap');
      query.set('scale', scale.toFixed(2));
      query.set('text-size', String(size));
    }
    element.query = query.toString();
  }
  function changeCanvasTextSize(index, delta) {
    updateCanvasElement(index, element => applyCanvasTextSize(element, canvasTextSize(element) + delta));
  }
  function cycleCanvasOutline(index) {
    updateCanvasElement(index, element => {
      const query = new URLSearchParams(element.query || '');
      const outline = query.get('outline') || '';
      if (!outline) query.set('outline', '1');
      else if (outline === '1') query.set('outline', 'dark');
      else query.delete('outline');
      element.query = query.toString();
    });
  }
  function canvasOutlineTool(index, element, query) {
    const selectedText = ['heading','text','text-image','bullet','code'].includes(element.kind);
    let label = selectedText ? '『』' : 'Outline';
    let large = false;
    let shapeIcon = '';
    if (element.kind === 'shape') {
      const shape = query.get('shape') || 'circle';
      shapeIcon = shape;
      if (shape === 'circle') label = '⃣⃣⃣⃣⃣';
      else if (shape === 'square') label = '𓉘𓉝';
      else if (shape === 'triangle') { label = '△'; large = true; }
      else if (shape === 'diamond') { label = '\u00a0⃟'; large = true; }
    }
    const outline = query.get('outline') || '';
    const button = canvasTool(label, outline ? 'active' : '', () => cycleCanvasOutline(index));
    if (large) button.classList.add('keynope-outline-large');
    if (shapeIcon) {
      const symbol = document.createElement('span');
      symbol.className = 'keynope-shape-outline-symbol keynope-shape-outline-' + shapeIcon;
      symbol.textContent = label;
      button.replaceChildren(symbol);
      button.classList.add('keynope-shape-outline-button');
    }
    button.title = outline === 'dark' ? 'Remove dark outline' : outline ? 'Switch to dark outline' : 'Add outline';
    button.setAttribute('aria-label', button.title);
    return button;
  }
  function canvasDuplicateTool(index) {
    const button = canvasTool('⿻', 'keynope-icon-button', () => editorAction({action: 'duplicate-element', element: index}).catch(() => {}));
    button.title = 'Clone element';
    button.setAttribute('aria-label', 'Clone element');
    return button;
  }
  function rotateCanvasText(index) {
    updateCanvasElement(index, element => {
      const query = new URLSearchParams(element.query || '');
      const orientation = query.get('orientation') || '';
      if (!orientation) query.set('orientation', 'cw');
      else if (orientation === 'cw') query.set('orientation', 'down');
      else if (orientation === 'down') query.set('orientation', 'ccw');
      else query.delete('orientation');
      element.query = query.toString();
    });
  }
  function canvasStyleSelect(index, element) {
    const query = new URLSearchParams(element.query || '');
    const select = document.createElement('select');
    select.title = 'Text rendering style';
    select.setAttribute('aria-label', 'Text rendering style');
    for (const [value, label] of [['','Style: Default'],['blocks','Style: Blocks'],['braille','Style: Braille'],['shade','Style: Shade'],['ascii','Style: ASCII'],['dense','Style: Dense']]) {
      const option = document.createElement('option');
      option.value = value;
      option.textContent = label;
      option.selected = query.get('glyph') === value;
      select.appendChild(option);
    }
    select.addEventListener('pointerdown', event => event.stopPropagation());
    select.addEventListener('change', event => {
      event.stopPropagation();
      updateCanvasElement(index, updated => {
        const values = new URLSearchParams(updated.query || '');
        if (select.value) {
          values.set('glyph', select.value);
          if (values.get('render') !== 'text-image') {
            const size = canvasTextSize(updated);
            let scale = size < 0 ? 1 + size * .1 : size < 10 ? 1 + size / 10 : size < 20 ? 2 + (size - 10) * .2 : 4 + (size - 20) * .2;
            values.set('render', 'text-image');
            values.set('source', 'bitmap');
            values.set('scale', scale.toFixed(2));
            values.set('text-size', String(size));
          }
        } else {
          values.delete('glyph');
        }
        updated.query = values.toString();
      });
    });
    return select;
  }
  function canvasColourTool(index, element, renderedColour) {
    const query = new URLSearchParams(element.query || '');
    const colourKey = element.kind === 'heading' ? 'header' : 'fg';
    const colour = /^#[0-9a-f]{6}$/i.test(query.get(colourKey) || '') ? query.get(colourKey) : (/^#[0-9a-f]{6}$/i.test(renderedColour || '') ? renderedColour : '#ffffff');
    const label = document.createElement('span');
    label.className = 'keynope-colour-tool';
    label.title = 'Text colour';
    label.setAttribute('aria-label', 'Text colour');
    label.textContent = 'A';
    label.style.setProperty('--keynope-tool-colour', colour);
    const input = document.createElement('input');
    input.type = 'color';
    input.value = colour;
    input.setAttribute('aria-label', 'Choose text colour');
    input.addEventListener('pointerdown', event => event.stopPropagation());
    input.addEventListener('input', event => {
      event.stopPropagation();
      label.style.setProperty('--keynope-tool-colour', input.value);
    });
    input.addEventListener('change', event => {
      event.stopPropagation();
      updateCanvasElement(index, updated => {
        const values = new URLSearchParams(updated.query || '');
        values.set(colourKey, input.value);
        if (colourKey === 'header') values.delete('fg');
        updated.query = values.toString();
      });
    });
    label.appendChild(input);
    return label;
  }
  function setCanvasAlignment(index, alignment) {
    updateCanvasElement(index, element => {
      const query = new URLSearchParams(element.query || '');
      query.set('align', alignment);
      for (const key of ['left','right','left_pct','right_pct']) query.delete(key);
      element.query = query.toString();
    });
  }
  function toggleCanvasMarkdownStyle(index, marker) {
    updateCanvasElement(index, element => {
      const text = element.text || '';
      if (!text) return;
      element.text = text.startsWith(marker) && text.endsWith(marker) && text.length >= marker.length * 2
        ? text.slice(marker.length, -marker.length)
        : marker + text + marker;
    });
  }
  function toggleCanvasTransparency(index) {
    updateCanvasElement(index, element => {
      const query = new URLSearchParams(element.query || '');
      if (query.get('transparent') === '1') query.delete('transparent'); else query.set('transparent', '1');
      element.query = query.toString();
    });
  }
  function canvasTransparencyTool(index, query) {
    const enabled = query.get('transparent') === '1';
    const button = canvasTool('See through', enabled ? 'active' : '', () => toggleCanvasTransparency(index));
    button.title = enabled ? 'Disable see through' : 'Enable see through';
    button.setAttribute('aria-label', button.title);
    return button;
  }
  function appendCanvasShapeKindTools(container, index, query) {
    const current = query.get('shape') || 'circle';
    for (const [shape, drawing] of [
      ['circle','<circle cx="10" cy="10" r="7"/>'],
      ['square','<rect x="3" y="3" width="14" height="14" rx="1"/>'],
      ['triangle','<path d="M10 2 18 17H2z"/>'],
      ['diamond','<path d="m10 2 8 8-8 8-8-8z"/>']
    ]) {
      const button = canvasTool('', current === shape ? 'active keynope-icon-button keynope-canvas-shape-kind' : 'keynope-icon-button keynope-canvas-shape-kind', () => {
        updateCanvasElement(index, element => {
          const values = new URLSearchParams(element.query || '');
          values.set('shape', shape);
          element.query = values.toString();
        });
      });
      button.innerHTML = '<svg viewBox="0 0 20 20" aria-hidden="true" fill="currentColor" stroke="currentColor" stroke-width="1.5">' + drawing + '</svg>';
      button.title = 'Change to ' + shape;
      button.setAttribute('aria-label', button.title);
      container.appendChild(button);
    }
  }
  function canvasDeleteTool(index) {
    const button = canvasTool('🗑️', 'danger keynope-icon-button', () => editorAction({action: 'delete-element', element: index}).catch(() => {}));
    button.title = 'Delete element';
    button.setAttribute('aria-label', 'Delete element');
    return button;
  }
  function visualQueryControl(panel, index, labelText, key, value, options) {
    const label = document.createElement('label');
    label.textContent = labelText;
    let input;
    if (options.values) {
      input = document.createElement('select');
      for (const [optionValue, optionLabel] of options.values) {
        const option = document.createElement('option');
        option.value = optionValue;
        option.textContent = optionLabel;
        option.selected = optionValue === value;
        input.appendChild(option);
      }
    } else {
      input = document.createElement('input');
      input.type = 'range';
      input.min = String(options.min);
      input.max = String(options.max);
      input.step = String(options.step);
      input.value = value || String(options.fallback);
      input.title = labelText + ': ' + input.value;
      input.addEventListener('input', () => { input.title = labelText + ': ' + input.value; });
    }
    input.setAttribute('aria-label', labelText);
    input.addEventListener('pointerdown', event => event.stopPropagation());
    input.addEventListener('change', () => {
      updateCanvasElement(index, element => {
        const query = new URLSearchParams(element.query || '');
        if (input.value === '' || input.value === String(options.fallback)) query.delete(key); else query.set(key, input.value);
        element.query = query.toString();
      });
    });
    panel.append(label, input);
  }
  function canvasVisualMenu(index, element) {
    const button = canvasTool('Adjust Image', '', () => {
      if (activeCanvasVisualMenu && activeCanvasVisualMenu.button === button) {
        closeCanvasVisualMenu();
        return;
      }
      closeCanvasVisualMenu();
      document.body.appendChild(panel);
      panel.hidden = false;
      const anchor = button.getBoundingClientRect();
      const panelRect = panel.getBoundingClientRect();
      panel.style.left = Math.max(8, Math.min(innerWidth - panelRect.width - 8, anchor.left)) + 'px';
      panel.style.top = Math.max(8, Math.min(innerHeight - panelRect.height - 8, anchor.bottom + 7)) + 'px';
      const dismiss = event => {
        if (!panel.contains(event.target) && event.target !== button) closeCanvasVisualMenu();
      };
      activeCanvasVisualMenu = {index, button, panel, dismiss};
      document.addEventListener('pointerdown', dismiss, true);
    });
    button.title = 'Adjust image';
    button.setAttribute('aria-label', 'Adjust image');
    const panel = document.createElement('div');
    panel.className = 'keynope-visual-panel';
    panel.hidden = true;
    panel.addEventListener('pointerdown', event => event.stopPropagation());
    appendCanvasVisualControls(panel, index, element);
    return button;
  }
  function appendCanvasVisualControls(panel, index, element) {
    const query = new URLSearchParams(element.query || '');
    if (element.kind === 'image') {
      visualQueryControl(panel, index, 'Glyph', 'glyph', query.get('glyph') || 'blocks', {values: [['blocks','Blocks'],['braille','Braille'],['shade','Shade'],['ascii','ASCII'],['dense','Dense']]});
      visualQueryControl(panel, index, 'Sampling', 'shape', query.get('shape') || 'subject', {values: [['subject','Subject'],['contrast','Contrast'],['saturation','Saturation'],['luma','Luma'],['alpha','Alpha']]});
      visualQueryControl(panel, index, 'Brightness', 'brightness', query.get('brightness'), {min:.2,max:2,step:.1,fallback:1});
      visualQueryControl(panel, index, 'Contrast', 'contrast', query.get('contrast'), {min:.2,max:2,step:.1,fallback:1});
      visualQueryControl(panel, index, 'Saturation', 'saturation', query.get('saturation'), {min:0,max:2,step:.1,fallback:1});
      visualQueryControl(panel, index, 'Sharpness', 'sharpness', query.get('sharpness'), {min:.2,max:2,step:.1,fallback:1});
      visualQueryControl(panel, index, 'Alpha', 'alpha', query.get('alpha'), {min:0,max:255,step:16,fallback:96});
    }
  }
  function closeCanvasVisualMenu() {
    if (!activeCanvasVisualMenu) return;
    document.removeEventListener('pointerdown', activeCanvasVisualMenu.dismiss, true);
    activeCanvasVisualMenu.panel.remove();
    activeCanvasVisualMenu = null;
  }
  function canvasShapeSelectionBounds(element, pageNumber) {
    const query = new URLSearchParams(element.query || '');
    const width = Math.max(1, Number.parseInt(query.get('width') || '12', 10));
    const height = Math.max(1, Number.parseInt(query.get('height') || '6', 10));
    let left = 0;
    if (query.has('left_pct')) left = Math.round(Number(query.get('left_pct')) * Math.max(0, deck.cols - 1));
    else if (query.has('left')) left = Number.parseInt(query.get('left'), 10) || 0;
    else if (query.get('align') === 'center') left = Math.floor((deck.cols - width) / 2);
    else if (query.get('align') === 'right') left = deck.cols - width;
    let top = Number.parseInt(query.get('top') || '0', 10) - Math.max(0, pageNumber || 0) * deck.rows;
    if (query.has('bottom')) top = deck.rows - height - (Number.parseInt(query.get('bottom'), 10) || 0);
    if (top >= deck.rows || top + height <= 0) return null;
    left = Math.max(0, Math.min(deck.cols - 1, left));
    top = Math.max(0, Math.min(deck.rows - 1, top));
    return {minX:left, minY:top, maxX:Math.min(deck.cols, left + width), maxY:Math.min(deck.rows, top + height), color:query.get('fg') || ''};
  }
  function applyCanvasLink(index, input, value) {
    value = value.trim();
    const internal = /^#([1-9][0-9]*)$/.exec(value);
    let external = false;
    if (value && !internal) {
      try { const parsed = new URL(value); external = parsed.protocol === 'http:' || parsed.protocol === 'https:'; } catch (_err) {}
    }
    if (value && !internal && !external) {
      input.setCustomValidity('Use an https:// URL or #slide number');
      input.reportValidity();
      return false;
    }
    updateCanvasElement(index, element => {
      const query = new URLSearchParams(element.query || '');
      query.delete('link');
      query.delete('slide');
      if (internal) query.set('slide', internal[1]);
      else if (external) query.set('link', value);
      element.query = query.toString();
    });
    return true;
  }
  function closeCanvasLinkDialog() {
    if (!activeCanvasLinkDialog) return;
    activeCanvasLinkDialog.remove();
    activeCanvasLinkDialog = null;
  }
  function openCanvasLinkDialog(index) {
    closeCanvasLinkDialog();
    const element = canvasElementAt(index);
    if (!element) return;
    const query = new URLSearchParams(element.query || '');
    const dialog = document.createElement('form');
    dialog.className = 'keynope-link-dialog';
    const heading = document.createElement('h3');
    heading.textContent = 'Link text';
    const modeLabel = document.createElement('label');
    modeLabel.textContent = 'Link to';
    const mode = document.createElement('select');
    for (const [value, label] of [['url','URL'],['slide','Slide']]) {
      const option = document.createElement('option');
      option.value = value;
      option.textContent = label;
      option.selected = query.has('slide') ? value === 'slide' : value === 'url';
      mode.appendChild(option);
    }
    modeLabel.appendChild(mode);
    const urlLabel = document.createElement('label');
    urlLabel.textContent = 'URL';
    const urlInput = document.createElement('input');
    urlInput.type = 'url';
    urlInput.placeholder = 'https://example.com';
    urlInput.value = query.get('link') || '';
    urlLabel.appendChild(urlInput);
    const slideLabel = document.createElement('label');
    slideLabel.textContent = 'Slide';
    const slideSelect = document.createElement('select');
    (editorState.slides || []).forEach((_slide, slideIndex) => {
      const option = document.createElement('option');
      option.value = String(slideIndex + 1);
      option.textContent = slideTitle(editorState.slides[slideIndex], slideIndex);
      option.selected = query.get('slide') === option.value;
      slideSelect.appendChild(option);
    });
    slideLabel.appendChild(slideSelect);
    const refreshMode = focus => {
      urlLabel.hidden = mode.value !== 'url';
      slideLabel.hidden = mode.value !== 'slide';
      if (focus) requestAnimationFrame(() => (mode.value === 'url' ? urlInput : slideSelect).focus());
    };
    mode.addEventListener('change', () => refreshMode(true));
    refreshMode(false);
    const actions = document.createElement('div');
    actions.className = 'keynope-editor-actions';
    const apply = canvasTool('Apply', '', () => {
      const value = mode.value === 'slide' ? '#' + slideSelect.value : urlInput.value;
      if (applyCanvasLink(index, urlInput, value)) closeCanvasLinkDialog();
    });
    apply.type = 'button';
    const clear = canvasTool('Clear', '', () => { applyCanvasLink(index, urlInput, ''); closeCanvasLinkDialog(); });
    const cancel = canvasTool('Cancel', '', closeCanvasLinkDialog);
    actions.append(apply, clear, cancel);
    dialog.append(heading, modeLabel, urlLabel, slideLabel, actions);
    dialog.addEventListener('submit', event => {
      event.preventDefault();
      const value = mode.value === 'slide' ? '#' + slideSelect.value : urlInput.value;
      if (applyCanvasLink(index, urlInput, value)) closeCanvasLinkDialog();
    });
    dialog.addEventListener('keydown', event => { event.stopPropagation(); if (event.key === 'Escape') closeCanvasLinkDialog(); });
    document.body.appendChild(dialog);
    const rect = dialog.getBoundingClientRect();
    dialog.style.left = Math.max(8, Math.floor((innerWidth - rect.width) / 2)) + 'px';
    dialog.style.top = Math.max(60, Math.floor((innerHeight - rect.height) / 3)) + 'px';
    activeCanvasLinkDialog = dialog;
    requestAnimationFrame(() => (mode.value === 'url' ? urlInput : slideSelect).focus());
  }
  function canvasLinkTool(index, query) {
    const linked = query.has('slide') || query.has('link');
    const button = canvasTool('🔗', linked ? 'active keynope-icon-button' : 'keynope-icon-button', () => openCanvasLinkDialog(index));
    button.title = linked ? 'Edit link' : 'Add link';
    button.setAttribute('aria-label', button.title);
    return button;
  }
  function renderEditorTopbar() {
    const slide = editorState && editorState.slides && editorState.slides[editorState.current];
    const index = editorState ? editorState.selected : -1;
    const element = slide && index >= 0 ? slide.elements[index] : null;
    if (activeCanvasVisualMenu && (!element || activeCanvasVisualMenu.index !== index)) closeCanvasVisualMenu();
    const contextual = !suppressSelectionTopbar && (!!activeInlineEditor || !!element);
    mainTopbar.hidden = contextual;
    selectionTopbar.hidden = !contextual;
    selectionTopbar.replaceChildren();
    if (!contextual) return;
    const label = document.createElement('span');
    label.className = 'keynope-topbar-label';
    label.textContent = activeInlineEditor ? 'Editing text' : ((element.kind || 'element') + ' selected');
    selectionTopbar.appendChild(label);
    if (activeInlineEditor) {
      selectionTopbar.appendChild(canvasTool('Commit', '', () => activeInlineEditor && activeInlineEditor.finish(true)));
      selectionTopbar.appendChild(canvasTool('Cancel', '', () => activeInlineEditor && activeInlineEditor.finish(false)));
      return;
    }
    const done = canvasTool('✓', '', () => editorAction({action: 'select-element', element: -1}).catch(() => {}));
    done.title = 'Done';
    done.setAttribute('aria-label', 'Done');
    selectionTopbar.appendChild(done);
    const query = new URLSearchParams(element.query || '');
    const selectedText = ['heading','text','text-image','bullet','code'].includes(element.kind);
    const rotatableText = ['heading','text','text-image','bullet'].includes(element.kind);
    const positionable = selectedText || element.kind === 'shape' || element.kind === 'image';
    if (selectedText) {
      const edit = canvasTool('✎', '', () => beginInlineEdit(index));
      edit.title = 'Edit text';
      edit.setAttribute('aria-label', 'Edit text');
      selectionTopbar.appendChild(edit);
      if (element.kind !== 'code') appendCanvasTextKindTools(selectionTopbar, index, element);
      selectionTopbar.appendChild(canvasTool('−', '', () => changeCanvasTextSize(index, -1)));
      selectionTopbar.appendChild(canvasTool('+', '', () => changeCanvasTextSize(index, 1)));
      if (element.kind !== 'code') {
        const bold = canvasTool('B', (element.text || '').startsWith('**') && (element.text || '').endsWith('**') ? 'active' : '', () => toggleCanvasMarkdownStyle(index, '**'));
        bold.title = 'Bold';
        bold.setAttribute('aria-label', 'Bold');
        const highlight = canvasTool('H', (element.text || '').startsWith('*') && (element.text || '').endsWith('*') ? 'active' : '', () => toggleCanvasMarkdownStyle(index, '*'));
        highlight.title = 'Highlight';
        highlight.setAttribute('aria-label', 'Highlight');
        selectionTopbar.append(bold, highlight);
      }
    }
    if (positionable) {
      for (const [alignment, symbol, title] of [['left','≡←','Align left'],['center','≡','Align centre'],['right','→≡','Align right']]) {
        const button = canvasTool(symbol, query.get('align') === alignment ? 'active' : '', () => setCanvasAlignment(index, alignment));
        button.title = title;
        button.setAttribute('aria-label', title);
        selectionTopbar.appendChild(button);
      }
    }
    if (selectedText) {
      if (rotatableText) {
        const rotate = canvasTool('⟳', 'keynope-icon-button keynope-rotate-button', () => rotateCanvasText(index));
        rotate.innerHTML = '<span class="keynope-rotate-icon" aria-hidden="true">⟳</span><span class="keynope-rotate-label">ROTATE</span>';
        rotate.title = 'Rotate';
        rotate.setAttribute('aria-label', 'Rotate');
        selectionTopbar.appendChild(rotate);
      }
      if (element.kind !== 'code') selectionTopbar.appendChild(canvasStyleSelect(index, element));
      selectionTopbar.appendChild(canvasColourTool(index, element, ''));
      selectionTopbar.appendChild(canvasLinkTool(index, query));
    }
    if (element.kind === 'shape') selectionTopbar.appendChild(canvasColourTool(index, element, ''));
    if (selectedText || element.kind === 'shape' || element.kind === 'image') selectionTopbar.appendChild(canvasTransparencyTool(index, query));
    if (element.kind === 'shape') appendCanvasShapeKindTools(selectionTopbar, index, query);
    if (element.kind === 'image') selectionTopbar.appendChild(canvasVisualMenu(index, element));
    selectionTopbar.appendChild(canvasOutlineTool(index, element, query));
    selectionTopbar.appendChild(canvasDuplicateTool(index));
    selectionTopbar.appendChild(canvasTool('Back', '', () => editorAction({action: 'move-element', element: index, kind: 'backward'}).catch(() => {})));
    selectionTopbar.appendChild(canvasTool('Front', '', () => editorAction({action: 'move-element', element: index, kind: 'forward'}).catch(() => {})));
    selectionTopbar.appendChild(canvasDeleteTool(index));
  }

  renderEditorCanvasOverlay = () => {
    if (!editorState || !deck.pages || !deck.pages.length) return;
    const page = deck.pages[pageIndex];
    if (!page || page.slide !== editorState.current) return;
    canvasOverlay.style.width = presenterCanvas.style.width;
    canvasOverlay.style.height = presenterCanvas.style.height;
    canvasOverlay.replaceChildren();
    const groups = new Map();
    const resolvedToRaw = editorElementIndexMaps().resolvedToRaw;
    for (const line of page.lines || []) {
      if (!Number.isInteger(line.element) || line.element < 0) continue;
      const rawElement = resolvedToRaw.get(line.element);
      if (rawElement == null) continue;
      let group = groups.get(rawElement);
      if (!group) group = {minX: deck.cols, minY: deck.rows, maxX: 0, maxY: 0, color: ''};
      for (const part of line.parts || []) {
        const width = [...(part.text || '')].length;
        group.minX = Math.min(group.minX, part.col || 0);
        group.maxX = Math.max(group.maxX, (part.col || 0) + width);
        group.minY = Math.min(group.minY, line.row || 0);
        group.maxY = Math.max(group.maxY, (line.row || 0) + 1);
        if (!group.color && part.color) group.color = part.color;
      }
      groups.set(rawElement, group);
    }
    const authoredElements = editorState.slides[editorState.current].elements || [];
    authoredElements.forEach((element, index) => {
      const query = new URLSearchParams(element.query || '');
      if (element.kind === 'shape' && query.get('transparent') === '1') {
        const bounds = canvasShapeSelectionBounds(element, page.page);
        if (bounds) groups.set(index, bounds);
      }
    });
    for (const [index, bounds] of groups) {
      if (index >= (editorState.slides[editorState.current].elements || []).length) continue;
      const hit = document.createElement('div');
      hit.className = 'keynope-canvas-element' + ((index === editorState.selected || (editorState.selection || []).includes(index)) ? ' active' : '');
      hit.dataset.element = String(index);
      hit.dataset.color = bounds.color || '';
      hit.style.left = (bounds.minX / deck.cols * 100) + '%';
      hit.style.top = (bounds.minY / deck.rows * 100) + '%';
      hit.style.width = (Math.max(1, bounds.maxX - bounds.minX) / deck.cols * 100) + '%';
      hit.style.height = (Math.max(1, bounds.maxY - bounds.minY) / deck.rows * 100) + '%';
      hit.title = 'Element ' + (index + 1);
      hit.addEventListener('dblclick', event => {
        event.preventDefault();
        event.stopPropagation();
        beginInlineEdit(index, hit);
      });
      hit.addEventListener('contextmenu', event => {
        event.preventDefault();
        event.stopPropagation();
        const open = () => showElementContextMenu(index, event.clientX, event.clientY);
        if (editorState.selected === index) open();
        else editorAction({action: 'select-element', element: index}).then(open).catch(() => {});
      });
      hit.addEventListener('pointerdown', event => {
        event.preventDefault();
        event.stopPropagation();
        const originX = event.clientX;
        const originY = event.clientY;
        const start = {...bounds};
        const resizeHandle = event.target.closest && event.target.closest('.keynope-resize-handle');
        const resizeCorner = resizeHandle ? resizeHandle.dataset.corner : '';
        const resizing = resizeCorner !== '';
        const sourceElement = {...editorState.slides[editorState.current].elements[index]};
        const fittingText = resizing && ['heading','text','text-image','bullet','code'].includes(sourceElement.kind);
        const resizingVisual = resizing && (sourceElement.kind === 'shape' || sourceElement.kind === 'image');
        let pendingFit = null;
        let fitting = false;
        let lastFit = null;
        let fitWaiters = [];
        let pendingVisualPreview = null;
        let visualPreviewFrame = 0;
        hit.setPointerCapture(event.pointerId);
        const resizedBounds = (dx, dy) => {
          let minX = start.minX, minY = start.minY, maxX = start.maxX, maxY = start.maxY;
          if (resizeCorner.includes('w')) minX = Math.min(maxX - 1, minX + dx);
          if (resizeCorner.includes('e')) maxX = Math.max(minX + 1, maxX + dx);
          if (resizeCorner.includes('n')) minY = Math.min(maxY - 1, minY + dy);
          if (resizeCorner.includes('s')) maxY = Math.max(minY + 1, maxY + dy);
          minX = Math.max(0, minX); minY = Math.max(0, minY);
          maxX = Math.min(deck.cols, maxX); maxY = Math.min(deck.rows, maxY);
          return {minX, minY, maxX, maxY};
        };
        const fitElementForBounds = next => {
          const element = {...sourceElement};
          const query = new URLSearchParams(element.query || '');
          for (const key of ['right','right_pct','bottom','row_delta','align','left','width','height']) query.delete(key);
          query.set('left_pct', Math.max(0, Math.min(1, next.minX / deck.cols)).toFixed(6));
          query.set('top', String(next.minY));
          element.query = query.toString();
          return element;
        };
        const resizedElementForBounds = next => {
          const element = {...sourceElement};
          const query = new URLSearchParams(element.query || '');
          for (const key of ['right','right_pct','bottom','row_delta','align','left']) query.delete(key);
          query.set('left_pct', Math.max(0, Math.min(1, next.minX / deck.cols)).toFixed(6));
          query.set('top', String(next.minY));
          query.set('width', String(Math.max(1, next.maxX - next.minX)));
          query.set('height', String(Math.max(1, next.maxY - next.minY)));
          element.query = query.toString();
          return element;
        };
        const queueVisualPreview = next => {
          pendingVisualPreview = next;
          if (visualPreviewFrame) return;
          visualPreviewFrame = requestAnimationFrame(() => {
            visualPreviewFrame = 0;
            const bounds = pendingVisualPreview;
            pendingVisualPreview = null;
            if (bounds) previewCanvasMutation(index, resizedElementForBounds(bounds), false);
          });
        };
        const pumpTextFit = async () => {
          if (fitting) return;
          fitting = true;
          while (pendingFit) {
            const task = pendingFit;
            pendingFit = null;
            try {
              const result = await fitCanvasTextElement(index, fitElementForBounds(task.bounds), task.bounds.maxX - task.bounds.minX, task.bounds.maxY - task.bounds.minY);
              lastFit = {key: task.key, element: result.element};
              replaceEditorPreviewPages(editorState.current, result.pages);
              drawFrame();
            } catch (_err) {}
          }
          fitting = false;
          const waiters = fitWaiters;
          fitWaiters = [];
          waiters.forEach(resolve => resolve());
        };
        const queueTextFit = next => {
          const key = [next.minX,next.minY,next.maxX,next.maxY].join(':');
          if (pendingFit && pendingFit.key === key || lastFit && lastFit.key === key && !fitting) return key;
          pendingFit = {key, bounds: next};
          pumpTextFit();
          return key;
        };
        const finishTextFit = async next => {
          const key = queueTextFit(next);
          while (fitting || pendingFit) await new Promise(resolve => fitWaiters.push(resolve));
          return lastFit && lastFit.key === key ? lastFit.element : null;
        };
        const move = moveEvent => {
          const rect = canvasOverlay.getBoundingClientRect();
          const dx = Math.round((moveEvent.clientX - originX) * deck.cols / Math.max(1, rect.width));
          const dy = Math.round((moveEvent.clientY - originY) * deck.rows / Math.max(1, rect.height));
          if (resizing) {
            const next = resizedBounds(dx, dy);
            hit.style.left = (next.minX / deck.cols * 100) + '%';
            hit.style.top = (next.minY / deck.rows * 100) + '%';
            hit.style.width = ((next.maxX - next.minX) / deck.cols * 100) + '%';
            hit.style.height = ((next.maxY - next.minY) / deck.rows * 100) + '%';
            if (fittingText) queueTextFit(next);
            else if (resizingVisual) queueVisualPreview(next);
          } else {
            hit.style.left = ((start.minX + dx) / deck.cols * 100) + '%';
            hit.style.top = ((start.minY + dy) / deck.rows * 100) + '%';
          }
        };
        const up = async upEvent => {
          hit.removeEventListener('pointermove', move);
          hit.removeEventListener('pointerup', up);
          if (visualPreviewFrame) cancelAnimationFrame(visualPreviewFrame);
          visualPreviewFrame = 0;
          pendingVisualPreview = null;
          const rect = canvasOverlay.getBoundingClientRect();
          const dx = Math.round((upEvent.clientX - originX) * deck.cols / Math.max(1, rect.width));
          const dy = Math.round((upEvent.clientY - originY) * deck.rows / Math.max(1, rect.height));
          if (!dx && !dy) {
            if (resizing) return;
            if (pendingCanvasSelection) clearTimeout(pendingCanvasSelection);
            pendingCanvasSelection = setTimeout(() => {
              pendingCanvasSelection = null;
              editorAction({action: 'select-element', element: index, name: event.shiftKey ? 'toggle' : ''}).catch(() => {});
            }, 240);
            return;
          }
          if (fittingText) {
            const fitted = await finishTextFit(resizedBounds(dx, dy));
            if (fitted) editorAction({action: 'update-element', element: index, elementData: fitted}).catch(() => {});
            return;
          }
          const element = {...editorState.slides[editorState.current].elements[index]};
          const query = new URLSearchParams(element.query || '');
          if (resizing) {
            const next = resizedBounds(dx, dy);
            query.delete('right'); query.delete('right_pct'); query.delete('bottom'); query.delete('row_delta'); query.delete('align'); query.delete('left');
            query.set('left_pct', Math.max(0, Math.min(1, next.minX / deck.cols)).toFixed(6));
            query.set('top', String(next.minY));
            query.set('width', String(Math.max(1, next.maxX - next.minX)));
            query.set('height', String(Math.max(1, next.maxY - next.minY)));
          } else {
            query.delete('right'); query.delete('right_pct'); query.delete('bottom'); query.delete('row_delta'); query.delete('align'); query.delete('left');
            query.set('left_pct', Math.max(0, Math.min(1, (start.minX + dx) / deck.cols)).toFixed(6));
            query.set('top', String(Math.max(0, start.minY + dy)));
          }
          element.query = query.toString();
          previewCanvasMutation(index, element);
          editorAction({action: 'update-element', element: index, elementData: element}).catch(() => {});
        };
        hit.addEventListener('pointermove', move);
        hit.addEventListener('pointerup', up);
      });
      if (index === editorState.selected) {
        for (const corner of ['nw','ne','sw','se']) {
          const handle = document.createElement('span');
          handle.className = 'keynope-resize-handle ' + corner;
          handle.dataset.corner = corner;
          handle.setAttribute('aria-label', 'Resize from ' + corner);
          hit.appendChild(handle);
        }
      }
      canvasOverlay.appendChild(hit);
    }
  };

  function slideTitle(slide, index) {
    const element = (slide.elements || []).find(item => item.text && (item.kind === 'heading' || item.kind === 'text'));
    return (index + 1) + '. ' + (element ? element.text.split('\n')[0] : 'Untitled slide');
  }
  function closeSlideContextMenu() {
    slideContextMenu.classList.remove('open');
    slideContextMenu.replaceChildren();
  }
  function showSlideContextMenu(index, clientX, clientY) {
    if (!editorState || index < 0 || index >= editorState.slides.length) return;
    const slide = editorState.slides[index];
    slideContextMenu.replaceChildren();
    const heading = document.createElement('h3');
    heading.textContent = 'Slide ' + (index + 1) + ' settings';
    slideContextMenu.appendChild(heading);
    const updateSlide = () => editorAction({action: 'update-slide', slideData: slide}).catch(() => {});
    if (editorState.masterMode) {
      const currentName = index === 0 ? (editorState.masters.base.name || 'Base Master') : ((editorState.masters.layouts[index - 1] && editorState.masters.layouts[index - 1].name) || ('Master ' + index));
      heading.textContent = currentName + ' settings';
      editorField(slideContextMenu, 'Name', currentName, false, value => {
        editorAction({action: 'rename-master', name: value}).catch(() => {});
      });
    }
    if (!editorState.masterMode && editorState.masters && Array.isArray(editorState.masters.layouts)) {
      const layoutField = document.createElement('label');
      layoutField.className = 'keynope-editor-field';
      const caption = document.createElement('span');
      caption.textContent = 'Layout';
      const select = document.createElement('select');
      for (const layout of editorState.masters.layouts) {
        const option = document.createElement('option');
        option.value = layout.id;
        option.textContent = layout.name || layout.id;
        option.selected = layout.id === (slide.layoutId || '');
        select.appendChild(option);
      }
      select.addEventListener('change', () => {
        closeSlideContextMenu();
        editorAction({action: 'set-layout', kind: select.value}).catch(() => {});
      });
      layoutField.append(caption, select);
      slideContextMenu.appendChild(layoutField);
    }
    editorSelect(slideContextMenu, 'Effect', slide.effect || '', ['none','matrix','stars','plasma','glitch','digital-snow','radar','neural','circuit','data-storm','flame','warp','scanline','fireworks','explosion'], value => { slide.effect = value; slide.effectSet = true; updateSlide(); });
    editorSelect(slideContextMenu, 'Background', slide.background || '', ['none','soft-plasma','aurora','topography','waves','mesh','constellation','ribbons','diagonal-flow','blueprint'], value => { slide.background = value; slide.backgroundSet = true; updateSlide(); });
    editorColor(slideContextMenu, 'Foreground', slide.fg || '', '#f3efe0', value => { slide.fg = value; slide.fgSet = true; updateSlide(); });
    editorColor(slideContextMenu, 'Background colour', slide.bg || '', '#000000', value => { slide.bg = value; slide.bgSet = true; updateSlide(); });
    editorColor(slideContextMenu, 'Header colour', slide.headerFg || '', '#ffffff', value => { slide.headerFg = value; slide.headerFgSet = true; updateSlide(); });
    const reset = document.createElement('button');
    reset.type = 'button';
    reset.textContent = 'Use master appearance';
    reset.addEventListener('click', () => {
      slide.effect = ''; slide.effectSet = false;
      slide.background = ''; slide.backgroundSet = false;
      slide.fg = ''; slide.fgSet = false;
      slide.bg = ''; slide.bgSet = false;
      slide.headerFg = ''; slide.headerFgSet = false;
      closeSlideContextMenu();
      updateSlide();
    });
    slideContextMenu.appendChild(reset);
    slideContextMenu.classList.add('open');
    const rect = slideContextMenu.getBoundingClientRect();
    slideContextMenu.style.left = Math.max(8, Math.min(innerWidth - rect.width - 8, clientX)) + 'px';
    slideContextMenu.style.top = Math.max(8, Math.min(innerHeight - rect.height - 8, clientY)) + 'px';
  }
  function showElementContextMenu(index, clientX, clientY) {
    const slide = editorState && editorState.slides && editorState.slides[editorState.current];
    const element = slide && slide.elements[index];
    if (!element) return;
    slideContextMenu.replaceChildren();
    const heading = document.createElement('h3');
    heading.textContent = (element.kind || 'Element') + ' actions';
    slideContextMenu.appendChild(heading);
    const actions = document.createElement('div');
    actions.className = 'keynope-editor-actions';
    const query = new URLSearchParams(element.query || '');
    const selectedText = ['heading','text','text-image','bullet','code'].includes(element.kind);
    const rotatableText = ['heading','text','text-image','bullet'].includes(element.kind);
    const positionable = selectedText || element.kind === 'shape' || element.kind === 'image';
    if (selectedText) {
      actions.appendChild(canvasTool('✎', '', () => beginInlineEdit(index)));
      if (element.kind !== 'code') appendCanvasTextKindTools(actions, index, element);
      actions.appendChild(canvasTool('−', '', () => changeCanvasTextSize(index, -1)));
      actions.appendChild(canvasTool('+', '', () => changeCanvasTextSize(index, 1)));
      if (element.kind !== 'code') {
        actions.appendChild(canvasTool('Bold', '', () => toggleCanvasMarkdownStyle(index, '**')));
        actions.appendChild(canvasTool('Highlight', '', () => toggleCanvasMarkdownStyle(index, '*')));
      }
    }
    if (positionable) {
      actions.appendChild(canvasTool('Left', '', () => setCanvasAlignment(index, 'left')));
      actions.appendChild(canvasTool('Centre', '', () => setCanvasAlignment(index, 'center')));
      actions.appendChild(canvasTool('Right', '', () => setCanvasAlignment(index, 'right')));
    }
    if (rotatableText) actions.appendChild(canvasTool('⟳', '', () => rotateCanvasText(index)));
    if (selectedText && element.kind !== 'code') actions.appendChild(canvasStyleSelect(index, element));
    if (selectedText || element.kind === 'shape') actions.appendChild(canvasColourTool(index, element, ''));
    if (selectedText) actions.appendChild(canvasLinkTool(index, query));
    if (selectedText || element.kind === 'shape' || element.kind === 'image') actions.appendChild(canvasTransparencyTool(index, query));
    if (element.kind === 'shape') appendCanvasShapeKindTools(actions, index, query);
    actions.appendChild(canvasOutlineTool(index, element, query));
    actions.appendChild(canvasDuplicateTool(index));
    actions.appendChild(canvasTool('Back', '', () => editorAction({action: 'move-element', element: index, kind: 'backward'}).catch(() => {})));
    actions.appendChild(canvasTool('Front', '', () => editorAction({action: 'move-element', element: index, kind: 'forward'}).catch(() => {})));
    actions.appendChild(canvasDeleteTool(index));
    slideContextMenu.appendChild(actions);
    if (element.kind === 'image') {
      const visualControls = document.createElement('div');
      visualControls.className = 'keynope-context-visual';
      const visualHeading = document.createElement('h4');
      visualHeading.textContent = 'Adjust Image';
      visualControls.appendChild(visualHeading);
      appendCanvasVisualControls(visualControls, index, element);
      slideContextMenu.appendChild(visualControls);
    }
    slideContextMenu.classList.add('open');
    const rect = slideContextMenu.getBoundingClientRect();
    slideContextMenu.style.left = Math.max(8, Math.min(innerWidth - rect.width - 8, clientX)) + 'px';
    slideContextMenu.style.top = Math.max(8, Math.min(innerHeight - rect.height - 8, clientY)) + 'px';
  }
  document.addEventListener('pointerdown', event => {
    if (!slideContextMenu.contains(event.target)) closeSlideContextMenu();
  }, true);
  stage.addEventListener('contextmenu', event => {
    event.preventDefault();
    event.stopPropagation();
    if (activeInlineEditor) activeInlineEditor.finish(true);
    showSlideContextMenu(editorState ? editorState.current : -1, event.clientX, event.clientY);
  });
  function renderEditorPanels() {
    if (!editorState || !editorState.slides || !editorState.slides.length) return;
    slidesPanel.replaceChildren();
    const slidesHeader = document.createElement('div');
    slidesHeader.className = 'keynope-slides-header';
    const slidesHeading = document.createElement('h3');
    slidesHeading.textContent = editorState.masterMode ? 'Master Slides' : 'Slides';
    slidesHeader.append(slidesHeading, masterModeButton);
    slidesPanel.appendChild(slidesHeader);
    addSlideButton.title = editorState.masterMode ? 'Add master' : 'Add slide';
    addSlideButton.setAttribute('aria-label', addSlideButton.title);
    cloneSlideButton.title = editorState.masterMode ? 'Clone master' : 'Clone slide';
    cloneSlideButton.setAttribute('aria-label', cloneSlideButton.title);
    deleteSlideButton.title = editorState.masterMode ? 'Delete master' : 'Delete slide';
    deleteSlideButton.setAttribute('aria-label', deleteSlideButton.title);
    deleteSlideButton.disabled = !!editorState.masterMode && editorState.current === 0;
    masterModeButton.textContent = 'M';
    masterModeButton.title = editorState.masterMode ? 'Exit master slides' : 'Master slides';
    masterModeButton.setAttribute('aria-label', masterModeButton.title);
    masterModeButton.classList.toggle('active', !!editorState.masterMode);
    editorState.slides.forEach((slide, index) => {
      const button = document.createElement('button');
      button.className = 'keynope-slide-item' + (index === editorState.current ? ' active' : '');
      button.textContent = editorState.masterMode
        ? (index === 0 ? 'Base Master' : ((editorState.masters.layouts[index - 1] && editorState.masters.layouts[index - 1].name) || ('Master ' + index)))
        : slideTitle(slide, index);
      button.addEventListener('click', () => editorAction({action: 'select-slide', slide: index}).catch(() => {}));
      button.addEventListener('contextmenu', event => {
        event.preventDefault();
        event.stopPropagation();
        const open = () => showSlideContextMenu(index, event.clientX, event.clientY);
        if (editorState.current === index) open();
        else editorAction({action: 'select-slide', slide: index}).then(open).catch(() => {});
      });
      slidesPanel.appendChild(button);
    });

    inspector.replaceChildren();
    const inspectorHeading = document.createElement('h2');
    inspectorHeading.textContent = 'Inspector';
    inspector.appendChild(inspectorHeading);
    const slide = editorState.slides[editorState.current];
    if (!slide) {
      renderEditorTopbar();
      return;
    }
    renderEditorTopbar();
    renderSpeakerNotes(slide);
    const slideSection = document.createElement('section');
    slideSection.className = 'keynope-editor-section';
    const slideHeading = document.createElement('h3');
    slideHeading.textContent = 'Slide';
    slideSection.appendChild(slideHeading);
    const updateSlide = () => editorAction({action: 'update-slide', slideData: slide}).catch(() => {});
    if (editorState.masters && Array.isArray(editorState.masters.layouts)) {
      const layoutField = document.createElement('label');
      layoutField.className = 'keynope-editor-field';
      const layoutCaption = document.createElement('span');
      layoutCaption.textContent = 'Layout';
      const layoutSelect = document.createElement('select');
      for (const layout of editorState.masters.layouts) {
        const option = document.createElement('option');
        option.value = layout.id;
        option.textContent = layout.name;
        option.selected = layout.id === (slide.layoutId || '');
        layoutSelect.appendChild(option);
      }
      layoutSelect.addEventListener('change', () => editorAction({action: 'set-layout', kind: layoutSelect.value}).catch(() => {}));
      layoutField.append(layoutCaption, layoutSelect);
      slideSection.appendChild(layoutField);
    }
    editorSelect(slideSection, 'Effect', slide.effect || '', ['none','matrix','stars','plasma','glitch','digital-snow','radar','neural','circuit','data-storm','flame','warp','scanline','fireworks','explosion'], value => { slide.effect = value; slide.effectSet = true; updateSlide(); });
    editorSelect(slideSection, 'Background', slide.background || '', ['none','soft-plasma','aurora','topography','waves','mesh','constellation','ribbons','diagonal-flow','blueprint'], value => { slide.background = value; slide.backgroundSet = true; updateSlide(); });
    editorColor(slideSection, 'Foreground', slide.fg || '', '#f3efe0', value => { slide.fg = value; slide.fgSet = true; updateSlide(); });
    editorColor(slideSection, 'Background colour', slide.bg || '', '#000000', value => { slide.bg = value; slide.bgSet = true; updateSlide(); });
    editorColor(slideSection, 'Header colour', slide.headerFg || '', '#ffffff', value => { slide.headerFg = value; slide.headerFgSet = true; updateSlide(); });
    const elementsSection = document.createElement('section');
    elementsSection.className = 'keynope-editor-section';
    const elementsHeading = document.createElement('h3');
    elementsHeading.textContent = 'Elements';
    elementsSection.appendChild(elementsHeading);
    (slide.elements || []).forEach((element, index) => {
      const button = document.createElement('button');
      button.className = 'keynope-element-item' + ((index === editorState.selected || (editorState.selection || []).includes(index)) ? ' active' : '');
      button.textContent = (index + 1) + '. ' + element.kind + (element.text ? ' — ' + element.text.split('\n')[0] : '');
      button.addEventListener('click', event => editorAction({action: 'select-element', element: index, name: event.shiftKey ? 'toggle' : ''}).catch(() => {}));
      elementsSection.appendChild(button);
    });
    if ((editorState.selection || []).length > 1) {
      editorButton('Delete selected', {action: 'delete-selection'}, elementsSection);
    }

    const selected = editorState.selected;
    if (selected >= 0 && selected < (slide.elements || []).length) {
      const element = slide.elements[selected];
      const elementSection = document.createElement('section');
      elementSection.className = 'keynope-editor-section';
      const heading = document.createElement('h3');
      heading.textContent = 'Selected ' + element.kind;
      elementSection.appendChild(heading);
      const updateElement = () => editorAction({action: 'update-element', element: selected, elementData: element}).catch(() => {});
      const help = document.createElement('p');
      help.className = 'keynope-editor-help';
      help.textContent = 'Drag on the canvas to move. Drag any blue corner to resize in that direction. Double-click or press Enter to edit text.';
      elementSection.appendChild(help);
      const actions = document.createElement('div');
      actions.className = 'keynope-editor-actions';
      if (['heading','text','bullet','code'].includes(element.kind)) {
        actions.appendChild(canvasTool('Edit text', '', () => beginInlineEdit(selected)));
      }
      actions.appendChild(canvasTool('Duplicate', '', () => editorAction({action: 'duplicate-element', element: selected}).catch(() => {})));
      const remove = canvasTool('Delete', 'danger', () => editorAction({action: 'delete-element', element: selected}).catch(() => {}));
      actions.appendChild(remove);
      elementSection.appendChild(actions);
      if (['heading','text','bullet','code'].includes(element.kind)) {
        editorField(elementSection, 'Text', element.text || '', true, value => { element.text = value; updateElement(); });
      }
      editorSelect(elementSection, 'Type', element.kind || 'text', ['heading','text','bullet','code','shape','image'], value => { element.kind = value; updateElement(); });
      if (element.kind === 'heading') {
        editorNumber(elementSection, 'Heading level', element.level || 1, {min: 1, max: 6, step: 1}, value => { element.level = Number(value || 1); updateElement(); });
      }
      if (element.kind === 'image') {
        editorField(elementSection, 'Image path', element.path || '', false, value => { element.path = value; updateElement(); });
      }
      const query = new URLSearchParams(element.query || '');
      const setQuery = (name, value) => {
        if (value === '' || value == null) query.delete(name); else query.set(name, String(value));
        element.query = query.toString();
        updateElement();
      };
      const positionHeading = document.createElement('p');
      positionHeading.className = 'keynope-editor-subheading';
      positionHeading.textContent = 'Position & size';
      elementSection.appendChild(positionHeading);
      const leftValue = query.has('left_pct') ? (Number(query.get('left_pct')) * 100).toFixed(1) : '';
      editorNumber(elementSection, 'Left (%)', leftValue, {min: 0, max: 100, step: .5}, value => setQuery('left_pct', value === '' ? '' : Math.max(0, Math.min(100, Number(value))) / 100));
      editorNumber(elementSection, 'Top (rows)', query.get('top') || '', {min: 0, step: 1}, value => setQuery('top', value));
      editorNumber(elementSection, 'Width (columns)', query.get('width') || '', {min: 1, step: 1}, value => setQuery('width', value));
      editorNumber(elementSection, 'Height (rows)', query.get('height') || '', {min: 1, step: 1}, value => setQuery('height', value));
      editorNumber(elementSection, 'Scale', query.get('scale') || '1', {min: .1, max: 10, step: .1}, value => setQuery('scale', value));
      if (element.kind === 'shape') {
        editorSelect(elementSection, 'Shape', query.get('shape') || 'rectangle', ['rectangle','square','circle','triangle','diamond'], value => setQuery('shape', value));
      }
      const appearanceHeading = document.createElement('p');
      appearanceHeading.className = 'keynope-editor-subheading';
      appearanceHeading.textContent = 'Appearance';
      elementSection.appendChild(appearanceHeading);
      editorColor(elementSection, 'Colour', query.get('fg') || '', '#f3efe0', value => setQuery('fg', value));
      const advanced = document.createElement('details');
      const advancedSummary = document.createElement('summary');
      advancedSummary.textContent = 'Advanced properties';
      advanced.appendChild(advancedSummary);
      editorField(advanced, 'Query', element.query || '', true, value => { element.query = value; updateElement(); });
      elementSection.appendChild(advanced);
      const layersHeading = document.createElement('p');
      layersHeading.className = 'keynope-editor-subheading';
      layersHeading.textContent = 'Layer';
      elementSection.appendChild(layersHeading);
      const layers = document.createElement('div');
      layers.className = 'keynope-editor-actions';
      for (const [label, direction] of [['To back','back'],['Backward','backward'],['Forward','forward'],['To front','front']]) {
        editorButton(label, {action: 'move-element', element: selected, kind: direction}, layers);
      }
      elementSection.appendChild(layers);
      inspector.appendChild(elementSection);
    }
    inspector.appendChild(elementsSection);
    inspector.appendChild(slideSection);
    if (editorState.masters) {
      const mastersSection = document.createElement('section');
      mastersSection.className = 'keynope-editor-section';
      const heading = document.createElement('h3');
      heading.textContent = 'Masters & Layouts';
      mastersSection.appendChild(heading);
      editorButton('+ Layout', {action: 'add-layout', name: 'New Layout'}, mastersSection);
      const layouts = [editorState.masters.base].concat(editorState.masters.layouts || []);
      for (const layout of layouts) {
        if (!layout) continue;
        const details = document.createElement('details');
        const summary = document.createElement('summary');
        summary.textContent = layout.name || layout.id;
        details.appendChild(summary);
        let name = layout.name || '';
        const masterSlide = layout.slide || {elements: []};
        if (!Array.isArray(masterSlide.elements)) masterSlide.elements = [];
        editorField(details, 'Name', name, false, value => { name = value; });
        editorSelect(details, 'Effect', masterSlide.effect || '', ['none','matrix','stars','plasma','glitch','digital-snow','radar','neural','circuit','data-storm','flame','warp','scanline','fireworks','explosion'], value => { masterSlide.effect = value; masterSlide.effectSet = true; });
        editorSelect(details, 'Background', masterSlide.background || '', ['none','soft-plasma','aurora','topography','waves','mesh','constellation','ribbons','diagonal-flow','blueprint'], value => { masterSlide.background = value; masterSlide.backgroundSet = true; });
        editorColor(details, 'Foreground', masterSlide.fg || '', '#f3efe0', value => { masterSlide.fg = value; masterSlide.fgSet = true; });
        editorColor(details, 'Background colour', masterSlide.bg || '', '#000000', value => { masterSlide.bg = value; masterSlide.bgSet = true; });
        editorColor(details, 'Header colour', masterSlide.headerFg || '', '#ffffff', value => { masterSlide.headerFg = value; masterSlide.headerFgSet = true; });
        const masterElementsHeading = document.createElement('h3');
        masterElementsHeading.textContent = 'Elements';
        details.appendChild(masterElementsHeading);
        masterSlide.elements.forEach((element, elementIndex) => {
          const elementDetails = document.createElement('details');
          const elementSummary = document.createElement('summary');
          elementSummary.textContent = (elementIndex + 1) + '. ' + (element.kind || 'element') + (element.text ? ' — ' + element.text.split('\n')[0] : '');
          elementDetails.appendChild(elementSummary);
          editorField(elementDetails, 'Text', element.text || '', true, value => { element.text = value; });
          editorField(elementDetails, 'Kind', element.kind || '', false, value => { element.kind = value; });
          editorField(elementDetails, 'Level', element.level || 0, false, value => { element.level = Number(value || 0); });
          editorField(elementDetails, 'Image path', element.path || '', false, value => { element.path = value; });
          editorField(elementDetails, 'Properties', element.query || '', true, value => { element.query = value; });
          const remove = document.createElement('button');
          remove.textContent = 'Remove element';
          remove.addEventListener('click', () => { masterSlide.elements.splice(elementIndex, 1); renderEditorPanels(); });
          elementDetails.appendChild(remove);
          details.appendChild(elementDetails);
        });
        const addMasterElement = document.createElement('button');
        addMasterElement.textContent = '+ Element';
        addMasterElement.addEventListener('click', () => {
          masterSlide.elements.push({kind: 'text', text: 'Text', id: '', query: ''});
          renderEditorPanels();
        });
        details.appendChild(addMasterElement);
        const save = document.createElement('button');
        save.textContent = 'Save layout';
        save.addEventListener('click', () => {
          editorAction({action: 'update-layout', kind: layout.id, name, slideData: masterSlide}).catch(() => {});
        });
        details.appendChild(save);
        if (layout.id !== 'base') editorButton('Delete', {action: 'delete-layout', kind: layout.id}, details);
        mastersSection.appendChild(details);
      }
      inspector.appendChild(mastersSection);
    }
    if (editorStatus) {
      if (selected >= 0 && selected < (slide.elements || []).length) {
        const selectedElement = slide.elements[selected];
        editorStatus.textContent = selectedElement.kind + ' selected · Enter commit · Esc deselect · arrows move · ⌘D duplicate · Delete remove';
      } else {
        editorStatus.textContent = 'Select an element · double-click text to edit · drag to move';
      }
    }
    requestAnimationFrame(renderEditorCanvasOverlay);
  }
  async function syncEditorState() {
    try {
      const response = await fetch('/api/editor/state', {cache: 'no-store'});
      if (!response.ok) return;
      const next = await response.json();
      if (next.version === editorStateVersion) return;
      editorState = next;
      keynopeEditorMasterMode = !!editorState.masterMode;
      editorStateVersion = next.version;
      renderEditorPanels();
      await syncEditorWorkspace();
    } catch (_err) {}
  }
  async function syncEditorWorkspace() {
    const sequence = ++editorWorkspaceSequence;
    if (!editorState || !editorState.masterMode) {
      if (editorNormalPages) {
        deck.pages = editorNormalPages;
        editorNormalPages = null;
        const next = deck.pages.findIndex(page => page.slide === editorState.current && page.page === 0);
        pageIndex = next >= 0 ? next : 0;
        render();
        requestAnimationFrame(renderEditorCanvasOverlay);
      }
      return;
    }
    if (!editorNormalPages) editorNormalPages = (deck.pages || []).slice();
    try {
      const response = await fetch('/api/editor/workspace?cols=' + encodeURIComponent(deck.cols) + '&rows=' + encodeURIComponent(deck.rows), {cache: 'no-store'});
      if (!response.ok) return;
      const pages = await response.json();
      if (sequence !== editorWorkspaceSequence || !editorState.masterMode || !Array.isArray(pages) || !pages.length) return;
      deck.pages = pages;
      pageIndex = Math.max(0, deck.pages.findIndex(page => page.slide === editorState.current && page.page === 0));
      render();
      requestAnimationFrame(renderEditorCanvasOverlay);
    } catch (_err) {}
  }
  const toolbar = document.createElement('div');
  toolbar.className = 'keynope-app-toolbar';
  const timerInputBlocker = document.createElement('div');
  timerInputBlocker.className = 'keynope-input-blocker';
  timerInputBlocker.hidden = true;
  timerInputBlocker.addEventListener('pointerdown', event => { event.preventDefault(); event.stopPropagation(); });
  timerInputBlocker.addEventListener('contextmenu', event => { event.preventDefault(); event.stopPropagation(); });
  document.body.appendChild(timerInputBlocker);
  notesToggleButton = document.createElement('button');
  notesToggleButton.type = 'button';
  notesToggleButton.textContent = 'Notes';
  notesToggleButton.addEventListener('click', () => setSpeakerNotesVisible(!editorSpeakerNotesVisible, !editorSpeakerNotesVisible));
  toolbar.appendChild(notesToggleButton);
  setSpeakerNotesVisible(false, false);
  const timerButton = document.createElement('button');
  timerButton.type = 'button';
  timerButton.textContent = 'Timer';
  function openEditorTimer() {
    presenterTimerMode = 'config';
    presenterTimerInput = '';
    presenterTimerEndMS = 0;
    refreshEditorPresenterControls();
    drawFrame();
  }
  function startEditorTimer() {
    const digits = String(presenterTimerInput || '').replace(/\D/g, '').slice(-4).padStart(4, '0');
    const seconds = Number(digits.slice(0, 2)) * 60 + Number(digits.slice(2));
    if (seconds <= 0) return;
    editorAction({action: 'start-timer', value: seconds}).catch(() => {});
  }
  function cancelEditorTimerInput() {
    presenterTimerMode = '';
    presenterTimerInput = '';
    presenterTimerEndMS = 0;
    refreshEditorPresenterControls();
    drawFrame();
  }
  timerButton.addEventListener('click', () => {
    if (presenterTimerMode === 'running') editorAction({action: 'stop-timer'}).catch(() => {});
    else if (presenterTimerMode === 'config') cancelEditorTimerInput();
    else openEditorTimer();
  });
  refreshEditorPresenterControls = () => {
    const running = presenterTimerMode === 'running';
    const active = presenterTimerMode === 'config' || running;
    timerButton.textContent = active ? 'Stop Timer' : 'Timer';
    timerButton.classList.toggle('active', active);
    timerButton.setAttribute('aria-pressed', active ? 'true' : 'false');
    timerInputBlocker.hidden = !active;
    canvasOverlay.hidden = active;
    document.documentElement.setAttribute('data-keynope-timer-active', active ? 'true' : 'false');
    for (const button of toolbar.querySelectorAll('button')) button.disabled = active && button !== timerButton;
    for (const button of slidesPanel.querySelectorAll('button')) button.disabled = active;
    speakerNotesInput.disabled = active;
  };
  toolbar.append(timerButton);
  editorStatus = document.createElement('span');
  editorStatus.className = 'keynope-editor-status';
  editorStatus.textContent = 'Select an element · double-click text to edit · drag to move';
  toolbar.appendChild(editorStatus);
  const controls = [
    ['← Previous', {action: 'previous-slide'}],
    ['Next →', {action: 'next-slide'}]
  ];
  for (const [label, action] of controls) {
    const button = document.createElement('button');
    button.type = 'button';
    button.textContent = label;
    button.addEventListener('click', event => {
      event.preventDefault();
      event.stopPropagation();
      editorAction(action).catch(() => {});
    });
    toolbar.appendChild(button);
  }
  const presentButton = document.createElement('button');
  presentButton.textContent = 'Present';
  presentButton.addEventListener('click', () => {
    const handler = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.keynopePresenter;
    if (handler) handler.postMessage({action: 'show-main'});
  });
  toolbar.appendChild(presentButton);
  const externalButton = document.createElement('button');
  externalButton.textContent = 'Present External';
  externalButton.addEventListener('click', () => {
    const handler = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.keynopePresenter;
    if (handler) handler.postMessage({action: 'show-external'});
  });
  toolbar.appendChild(externalButton);
  document.body.appendChild(toolbar);
  function nudgeSelected(dx, dy) {
    if (!editorState || editorState.selected < 0) return false;
    const index = editorState.selected;
    const slide = editorState.slides[editorState.current];
    const element = slide && slide.elements[index];
    const hit = canvasOverlay.querySelector('[data-element="' + index + '"]');
    if (!element || !hit) return false;
    const updated = {...element};
    const query = new URLSearchParams(updated.query || '');
    const renderedLeft = parseFloat(hit.style.left || '0') / 100;
    const renderedTop = Math.round(parseFloat(hit.style.top || '0') * deck.rows / 100);
    query.delete('right'); query.delete('right_pct'); query.delete('bottom'); query.delete('row_delta'); query.delete('align'); query.delete('left');
    query.set('left_pct', Math.max(0, Math.min(1, Number(query.get('left_pct') || renderedLeft) + dx / deck.cols)).toFixed(6));
    query.set('top', String(Math.max(0, Number(query.get('top') || renderedTop) + dy)));
    updated.query = query.toString();
    previewCanvasMutation(index, updated);
    editorAction({action: 'update-element', element: index, elementData: updated}).catch(() => {});
    return true;
  }
  function cycleCanvasSelection(reverse) {
    if (!editorState || !editorState.slides || !editorState.slides[editorState.current]) return false;
    const selectableKinds = new Set(['heading','text','text-image','bullet','code','shape','image','page-number']);
    const indices = (editorState.slides[editorState.current].elements || [])
      .map((element, index) => selectableKinds.has(element.kind) ? index : -1)
      .filter(index => index >= 0);
    if (!indices.length) return false;
    const position = indices.indexOf(editorState.selected);
    const next = position < 0
      ? (reverse ? indices[indices.length - 1] : indices[0])
      : indices[(position + (reverse ? -1 : 1) + indices.length) % indices.length];
    editorAction({action: 'select-element', element: next}).catch(() => {});
    return true;
  }
  addEventListener('keydown', e => {
    if (presenterTimerMode === 'config' && !e.metaKey && !e.ctrlKey && !e.altKey) {
      e.preventDefault();
      e.stopImmediatePropagation();
      if (/^[0-9]$/.test(e.key) && presenterTimerInput.length < 4) {
        presenterTimerInput += e.key;
        drawFrame();
      } else if (e.key === 'Backspace') {
        presenterTimerInput = presenterTimerInput.slice(0, -1);
        drawFrame();
      } else if (e.key === 'Enter') {
        startEditorTimer();
      } else if (e.key === 'Escape' || e.key.toLowerCase() === 'q') {
        cancelEditorTimerInput();
      }
      return;
    }
    if (presenterTimerMode === 'running' && !e.metaKey && !e.ctrlKey && !e.altKey) {
      e.preventDefault();
      e.stopImmediatePropagation();
      if (e.key === 'Escape' || e.key.toLowerCase() === 'q') editorAction({action: 'stop-timer'}).catch(() => {});
      return;
    }
    if (e.target && /^(INPUT|TEXTAREA|SELECT)$/.test(e.target.tagName)) return;
    if (!e.metaKey && !e.ctrlKey && !e.altKey && e.key === 'Tab') {
      e.preventDefault();
      e.stopImmediatePropagation();
      cycleCanvasSelection(e.shiftKey);
      return;
    }
    if (e.key === 'Escape' && slideContextMenu.classList.contains('open')) {
      e.preventDefault();
      e.stopImmediatePropagation();
      closeSlideContextMenu();
      return;
    }
    if (!e.metaKey && !e.ctrlKey && !e.altKey && e.key.toLowerCase() === 'm') {
      e.preventDefault();
      e.stopImmediatePropagation();
      editorAction({action: 'toggle-master-mode'}).catch(() => {});
      return;
    }
    if ((e.metaKey || e.ctrlKey) && (e.key.toLowerCase() === 'z' || e.key.toLowerCase() === 'y')) {
      e.preventDefault();
      e.stopImmediatePropagation();
      const redo = e.key.toLowerCase() === 'y' || (e.key.toLowerCase() === 'z' && e.shiftKey);
      editorAction({action: redo ? 'redo' : 'undo'}).catch(() => {});
      return;
    }
    if (!e.metaKey && !e.ctrlKey && !e.altKey && e.key === '2') {
      e.preventDefault();
      e.stopImmediatePropagation();
      setSpeakerNotesVisible(!editorSpeakerNotesVisible, false);
      return;
    }
    if (!e.metaKey && !e.ctrlKey && !e.altKey && e.key === '0') {
      e.preventDefault();
      e.stopImmediatePropagation();
      if (presenterTimerMode === 'running') editorAction({action: 'stop-timer'}).catch(() => {});
      else openEditorTimer();
      return;
    }
    if (!e.metaKey && !e.ctrlKey && !e.altKey && editorState && editorState.selected >= 0) {
      if (e.key === '<' || e.key === '=' || e.key === '>') {
        e.preventDefault();
        e.stopImmediatePropagation();
        setCanvasAlignment(editorState.selected, e.key === '<' ? 'left' : e.key === '>' ? 'right' : 'center');
        return;
      }
      if (e.key.toLowerCase() === 'o') {
        e.preventDefault();
        e.stopImmediatePropagation();
        cycleCanvasOutline(editorState.selected);
        return;
      }
    }
    if (e.key === 'Escape' && editorState && editorState.selected >= 0) {
      e.preventDefault();
      e.stopImmediatePropagation();
      if (pendingCanvasSelection) {
        clearTimeout(pendingCanvasSelection);
        pendingCanvasSelection = null;
      }
      editorAction({action: 'select-element', element: -1}).catch(() => {});
      return;
    }
    if (e.key === 'Enter' && editorState && editorState.selected >= 0) {
      e.preventDefault();
      e.stopImmediatePropagation();
      editorAction({action: 'select-element', element: -1}).catch(() => {});
      return;
    }
    if ((e.key === 'Delete' || e.key === 'Backspace') && editorState && editorState.selected >= 0) {
      e.preventDefault();
      e.stopImmediatePropagation();
      const action = (editorState.selection || []).length > 1 ? {action: 'delete-selection'} : {action: 'delete-element', element: editorState.selected};
      editorAction(action).catch(() => {});
      return;
    }
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'd' && editorState && editorState.selected >= 0) {
      e.preventDefault();
      e.stopImmediatePropagation();
      editorAction({action: 'duplicate-element', element: editorState.selected}).catch(() => {});
      return;
    }
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault();
      e.stopImmediatePropagation();
      if (!nudgeSelected((e.key === 'ArrowLeft' ? -1 : 1) * (e.shiftKey ? 5 : 1), 0)) {
        editorAction({action: e.key === 'ArrowLeft' ? 'previous-slide' : 'next-slide'}).catch(() => {});
      }
      return;
    }
    if (e.key === 'ArrowUp' || e.key === 'ArrowDown') {
      if (!editorState || editorState.selected < 0) return;
      e.preventDefault();
      e.stopImmediatePropagation();
      nudgeSelected(0, (e.key === 'ArrowUp' ? -1 : 1) * (e.shiftKey ? 5 : 1));
      return;
    }
    const sequence = keynopeTerminalSequence(e);
    if (sequence == null) return;
    e.preventDefault();
    e.stopImmediatePropagation();
    sendKeynopeAppInput(sequence);
  }, true);
  setInterval(syncPresenterState, 60);
  syncPresenterState();
  setInterval(syncEditorState, 150);
  syncEditorState();
} else if (window.KEYNOPE_PRESENTER) {
  setInterval(syncPresenterState, 120);
  syncPresenterState();
}
resize();
render();
tick();
</script>
</body>
</html>
`
}

func slideStyleComment(slide Slide) string {
	var fields []string
	if slide.FGSet || (slide.FG != "" && slide.FG != "37") {
		value := slide.FG
		if value == "" {
			fields = append(fields, "fg=none")
		} else {
			fields = append(fields, "fg="+ansiCodeName(value, false))
		}
	}
	if slide.BGSet || (slide.BG != "" && slide.BG != "40") {
		value := slide.BG
		if value == "" {
			fields = append(fields, "bg=none")
		} else {
			fields = append(fields, "bg="+ansiCodeName(value, true))
		}
	}
	if slide.HeaderFGSet || slide.HeaderFG != "" {
		value := slide.HeaderFG
		if value == "" {
			fields = append(fields, "header=none")
		} else {
			fields = append(fields, "header="+ansiCodeName(value, false))
		}
	}
	if len(fields) == 0 {
		return ""
	}
	return "<!-- " + strings.Join(fields, " ") + " -->"
}

func ansiCodeName(code string, background bool) string {
	names := []string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"}
	base := 30
	if background {
		base = 40
	}
	if parsed, err := strconv.Atoi(code); err == nil && parsed >= base && parsed < base+len(names) {
		return names[parsed-base]
	}
	parts := strings.Split(code, ";")
	if len(parts) == 5 && (parts[0] == "38" || parts[0] == "48") && parts[1] == "2" {
		r, _ := strconv.Atoi(parts[2])
		g, _ := strconv.Atoi(parts[3])
		b, _ := strconv.Atoi(parts[4])
		return fmt.Sprintf("#%02x%02x%02x", r, g, b)
	}
	return code
}

func splitSlides(text string) []string {
	var parts []string
	var cur []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "---" {
			parts = append(parts, strings.Join(cur, "\n"))
			cur = nil
			continue
		}
		cur = append(cur, line)
	}
	parts = append(parts, strings.Join(cur, "\n"))
	return parts
}

var effectRE = regexp.MustCompile(`<!--\s*effect=([a-zA-Z0-9_-]+)\s*-->`)
var backgroundRE = regexp.MustCompile(`<!--\s*background=([a-zA-Z0-9_-]+)\s*-->`)
var notesRE = regexp.MustCompile(`<!--\s*notes=base64:([A-Za-z0-9+/=]+)\s*-->`)
var layoutRE = regexp.MustCompile(`<!--\s*layout=([a-zA-Z0-9_-]+)\s*-->`)
var pageNumberRE = regexp.MustCompile(`<!--\s*page-number=(show|hide)\s*-->`)
var masterSlotRE = regexp.MustCompile(`<!--\s*master-slot=([a-zA-Z0-9_-]+)(?:\s+placeholder=(true))?\s*-->`)
var keynopeMetaRE = regexp.MustCompile(`<!--\s*keynope\s+width=([0-9]+)\s+height=([0-9]+)\s*-->`)
var commentRE = regexp.MustCompile(`<!--\s*(.*?)\s*-->`)
var imageRE = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)
var shapeRE = regexp.MustCompile(`^\[shape(?::([a-zA-Z0-9_-]+))?\]$`)

func parseSlide(text, base string) Slide {
	slide := Slide{}
	lines := strings.Split(text, "\n")
	inCode := false
	var code []string
	var paragraph []string
	pendingQuery := ""
	pendingMasterSlot := ""
	pendingPlaceholder := false

	flushParagraph := func() {
		if len(paragraph) > 0 {
			slide.Elements = append(slide.Elements, Element{Kind: "text", Text: strings.Join(paragraph, " "), Query: pendingQuery, MasterSlotID: pendingMasterSlot, Placeholder: pendingPlaceholder})
			pendingQuery = ""
			pendingMasterSlot = ""
			pendingPlaceholder = false
			paragraph = nil
		}
	}
	flushCode := func() {
		if len(code) > 0 {
			slide.Elements = append(slide.Elements, Element{Kind: "code", Text: strings.Join(code, "\n"), Query: pendingQuery, MasterSlotID: pendingMasterSlot, Placeholder: pendingPlaceholder})
			pendingQuery = ""
			pendingMasterSlot = ""
			pendingPlaceholder = false
			code = nil
		}
	}

	for lineIndex, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if match := effectRE.FindStringSubmatch(trimmed); match != nil {
			slide.EffectSet = true
			if match[1] != "none" {
				slide.Effect = match[1]
			}
			continue
		}
		if match := backgroundRE.FindStringSubmatch(trimmed); match != nil {
			slide.BackgroundSet = true
			if match[1] != "none" {
				slide.Background = match[1]
			}
			continue
		}
		if match := layoutRE.FindStringSubmatch(trimmed); match != nil {
			slide.LayoutID = match[1]
			continue
		}
		if match := pageNumberRE.FindStringSubmatch(trimmed); match != nil {
			slide.PageNumber = match[1]
			continue
		}
		if match := masterSlotRE.FindStringSubmatch(trimmed); match != nil {
			flushParagraph()
			pendingMasterSlot = match[1]
			pendingPlaceholder = len(match) > 2 && match[2] == "true"
			continue
		}
		if match := notesRE.FindStringSubmatch(trimmed); match != nil {
			if decoded, err := base64.StdEncoding.DecodeString(match[1]); err == nil {
				slide.Notes = string(decoded)
			}
			continue
		}
		if nextNonEmptyLine(lines, lineIndex) == "" && applySlideStyle(trimmed, &slide) {
			continue
		}
		if query, ok := textPlacementComment(trimmed); ok {
			flushParagraph()
			pendingQuery = query
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			flushParagraph()
			if inCode {
				flushCode()
				inCode = false
			} else {
				inCode = true
			}
			continue
		}
		if inCode {
			code = append(code, line)
			continue
		}
		if trimmed == "" {
			flushParagraph()
			continue
		}
		if match := imageRE.FindStringSubmatch(trimmed); match != nil {
			flushParagraph()
			src, query := splitImageSource(os.ExpandEnv(match[1]))
			if !filepath.IsAbs(src) {
				src = filepath.Join(base, src)
			}
			src = filepath.Clean(src)
			slide.Elements = append(slide.Elements, Element{Kind: "image", Path: src, Query: query, MasterSlotID: pendingMasterSlot, Placeholder: pendingPlaceholder})
			pendingQuery = ""
			pendingMasterSlot = ""
			pendingPlaceholder = false
			continue
		}
		if match := shapeRE.FindStringSubmatch(trimmed); match != nil {
			flushParagraph()
			query := pendingQuery
			if match[1] != "" {
				query = setQueryValue(query, "shape", strings.ToLower(match[1]))
			}
			slide.Elements = append(slide.Elements, Element{Kind: "shape", Query: query, MasterSlotID: pendingMasterSlot, Placeholder: pendingPlaceholder})
			pendingMasterSlot = ""
			pendingPlaceholder = false
			pendingQuery = ""
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			flushParagraph()
			slide.Elements = append(slide.Elements, Element{Kind: "heading", Level: 2, Text: strings.TrimSpace(trimmed[3:]), Query: pendingQuery, MasterSlotID: pendingMasterSlot, Placeholder: pendingPlaceholder})
			pendingQuery = ""
			pendingMasterSlot = ""
			pendingPlaceholder = false
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			flushParagraph()
			slide.Elements = append(slide.Elements, Element{Kind: "heading", Level: 1, Text: strings.TrimSpace(trimmed[2:]), Query: pendingQuery, MasterSlotID: pendingMasterSlot, Placeholder: pendingPlaceholder})
			pendingQuery = ""
			pendingMasterSlot = ""
			pendingPlaceholder = false
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			flushParagraph()
			slide.Elements = append(slide.Elements, Element{Kind: "bullet", Text: strings.TrimSpace(trimmed[2:]), Query: pendingQuery, MasterSlotID: pendingMasterSlot, Placeholder: pendingPlaceholder})
			pendingQuery = ""
			pendingMasterSlot = ""
			pendingPlaceholder = false
			continue
		}
		paragraph = append(paragraph, trimmed)
	}
	flushParagraph()
	flushCode()
	return slide
}

func nextNonEmptyLine(lines []string, index int) string {
	for i := index + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(strings.TrimRight(lines[i], "\r"))
		if trimmed != "" {
			return trimmed
		}
		return ""
	}
	return ""
}

func applySlideStyle(line string, slide *Slide) bool {
	match := commentRE.FindStringSubmatch(line)
	if match == nil {
		return false
	}
	fields := strings.Fields(match[1])
	if len(fields) == 0 {
		return false
	}
	for _, field := range fields {
		key, _, ok := strings.Cut(field, "=")
		if !ok {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "fg", "bg", "header":
		default:
			return false
		}
	}
	applied := false
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "fg":
			value = strings.Trim(value, `"'`)
			if strings.EqualFold(value, "none") {
				slide.FG = ""
				slide.FGSet = true
				applied = true
			} else if code, ok := ansiFG(value); ok {
				slide.FG = code
				slide.FGSet = true
				applied = true
			}
		case "bg":
			value = strings.Trim(value, `"'`)
			if strings.EqualFold(value, "none") {
				slide.BG = ""
				slide.BGSet = true
				applied = true
			} else if code, ok := ansiBG(value); ok {
				slide.BG = code
				slide.BGSet = true
				applied = true
			}
		case "header":
			value = strings.Trim(value, `"'`)
			if strings.EqualFold(value, "none") {
				slide.HeaderFG = ""
				slide.HeaderFGSet = true
				applied = true
			} else if code, ok := ansiFG(value); ok {
				slide.HeaderFG = code
				slide.HeaderFGSet = true
				applied = true
			}
		}
	}
	return applied
}

func textPlacementComment(line string) (string, bool) {
	match := commentRE.FindStringSubmatch(line)
	if match == nil {
		return "", false
	}
	values := url.Values{}
	if parsed, err := url.ParseQuery(match[1]); err == nil {
		for _, key := range []string{"top", "bottom", "left", "right", "left_pct", "right_pct", "row_delta", "align", "width", "height", "stretch", "transparent", "orientation", "render", "source", "scale", "text-size", "fg", "bg", "header", "color", "glyph", "shape", "outline", "brightness", "contrast", "saturation", "sharpness", "alpha", "link", "slide", "master-clear"} {
			for _, value := range parsed[key] {
				addPlacementValue(values, key, value)
			}
		}
	}
	for _, field := range strings.Fields(match[1]) {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		addPlacementValue(values, key, value)
	}
	if len(values) == 0 {
		return "", false
	}
	return values.Encode(), true
}

func addPlacementValue(values url.Values, key, value string) {
	key = strings.ToLower(strings.TrimSpace(key))
	value = strings.Trim(value, `"'`)
	switch key {
	case "top", "bottom", "left", "right", "width", "height":
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 {
			if key == "width" || key == "height" {
				parsed = max(1, parsed)
			}
			values.Set(key, strconv.Itoa(parsed))
		}
	case "row_delta":
		if parsed, err := strconv.Atoi(value); err == nil {
			values.Set(key, strconv.Itoa(parsed))
		}
	case "stretch":
		if value == "1" || value == "true" || value == "yes" {
			values.Set("stretch", "1")
		}
	case "transparent":
		if value == "1" || value == "true" || value == "yes" || value == "on" {
			values.Set("transparent", "1")
		}
	case "orientation":
		switch strings.ToLower(value) {
		case "cw", "clockwise", "right", "90":
			values.Set("orientation", "cw")
		case "down", "180":
			values.Set("orientation", "down")
		case "ccw", "counterclockwise", "counter-clockwise", "left", "270":
			values.Set("orientation", "ccw")
		}
	case "outline":
		switch strings.ToLower(value) {
		case "1", "true", "yes", "on":
			values.Set("outline", "1")
		case "dark", "darkgray", "darkgrey", "gray", "grey":
			values.Set("outline", "dark")
		}
	case "left_pct", "right_pct":
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			values.Set(key, fmt.Sprintf("%.6f", clampFloat(parsed, 0, 1)))
		}
	case "align":
		if value == "left" || value == "center" || value == "right" {
			values.Set(key, value)
		}
	case "render":
		if value == "text-image" {
			values.Set(key, value)
		}
	case "source":
		switch value {
		case "text", "heading1", "heading2", "bitmap":
			values.Set(key, value)
		}
	case "scale":
		if parsed, err := strconv.ParseFloat(value, 64); err == nil && parsed > 0 {
			values.Set(key, fmt.Sprintf("%.2f", parsed))
		}
	case "text-size":
		if parsed, err := strconv.Atoi(value); err == nil {
			values.Set(key, strconv.Itoa(max(textSizeMin, min(textSizeMax, parsed))))
		}
	case "fg", "color":
		if _, ok := ansiFG(value); ok {
			values.Set("fg", normalizeColourValue(value))
		}
	case "bg":
		if _, ok := ansiBG(value); ok {
			values.Set("bg", normalizeColourValue(value))
		}
	case "header":
		if _, ok := ansiFG(value); ok {
			values.Set("header", normalizeColourValue(value))
		}
	case "glyph":
		switch strings.ToLower(value) {
		case "blocks", "block":
			values.Set("glyph", "blocks")
		case "braille", "shade", "ascii", "dense":
			values.Set("glyph", strings.ToLower(value))
		}
	case "shape":
		switch strings.ToLower(value) {
		case "subject", "contrast", "saturation", "luma", "alpha", "circle", "square", "triangle", "diamond":
			values.Set("shape", strings.ToLower(value))
		}
	case "brightness", "contrast", "saturation", "sharpness":
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			values.Set(key, fmt.Sprintf("%.1f", parsed))
		}
	case "alpha":
		if parsed, err := strconv.Atoi(value); err == nil {
			values.Set("alpha", strconv.Itoa(max(0, min(255, parsed))))
		}
	case "link":
		if link, ok := normalizeLinkValue(value, 0); ok {
			setLinkValues(values, link)
		}
	case "slide":
		if slide, err := strconv.Atoi(value); err == nil && slide >= 1 {
			values.Del("link")
			values.Set("slide", strconv.Itoa(slide))
		}
	case "master-clear":
		var allowed []string
		for _, candidate := range strings.Split(value, ",") {
			candidate = strings.TrimSpace(candidate)
			if isMasterOverrideQueryKey(candidate) {
				allowed = append(allowed, candidate)
			}
		}
		if len(allowed) > 0 {
			values.Set("master-clear", strings.Join(allowed, ","))
		}
	}
}

func placementCommentText(query string) string {
	values, err := url.ParseQuery(query)
	if err != nil {
		return query
	}
	var fields []string
	for _, key := range []string{"top", "bottom", "left", "right", "left_pct", "right_pct", "row_delta", "align", "width", "height", "stretch", "transparent", "orientation", "render", "source", "scale", "text-size", "fg", "bg", "header", "glyph", "shape", "outline", "brightness", "contrast", "saturation", "sharpness", "alpha", "slide", "link", "master-clear"} {
		if value := values.Get(key); value != "" {
			if key == "link" {
				fields = append(fields, key+"="+url.QueryEscape(value))
			} else {
				fields = append(fields, key+"="+value)
			}
		}
	}
	return strings.Join(fields, " ")
}

func normalizeColourValue(value string) string {
	value = strings.TrimSpace(strings.Trim(value, `"'`))
	if strings.HasPrefix(value, "#") {
		if hex, ok := normalizeHexColour(value); ok {
			return hex
		}
	}
	return value
}

type linkTarget struct {
	Value string
	URL   string
	Slide int
}

func sanitizeLinkURL(input string) (string, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", false
	}
	for _, r := range input {
		if r < 32 || r == 127 {
			return "", false
		}
	}
	parsed, err := url.Parse(input)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	parsed.Scheme = scheme
	if parsed.RawQuery != "" {
		query, err := url.ParseQuery(parsed.RawQuery)
		if err != nil {
			return "", false
		}
		parsed.RawQuery = query.Encode()
	}
	return parsed.String(), true
}

func normalizeLinkValue(input string, slideCount int) (string, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", false
	}
	slideRef := strings.TrimPrefix(input, "#")
	if slideRef != "" {
		if slide, err := strconv.Atoi(slideRef); err == nil {
			if slide < 1 {
				return "", false
			}
			if slideCount > 0 && slide > slideCount {
				return "", false
			}
			return fmt.Sprintf("#%d", slide), true
		}
	}
	return sanitizeLinkURL(input)
}

func setLinkValues(values url.Values, value string) {
	values.Del("link")
	values.Del("slide")
	if strings.HasPrefix(value, "#") {
		if slide, err := strconv.Atoi(strings.TrimPrefix(value, "#")); err == nil && slide >= 1 {
			values.Set("slide", strconv.Itoa(slide))
		}
		return
	}
	values.Set("link", value)
}

func parseLinkTarget(input string, slideCount int) (linkTarget, bool) {
	value, ok := normalizeLinkValue(input, slideCount)
	if !ok {
		return linkTarget{}, false
	}
	if strings.HasPrefix(value, "#") {
		slide, err := strconv.Atoi(strings.TrimPrefix(value, "#"))
		if err != nil || slide < 1 || slideCount > 0 && slide > slideCount {
			return linkTarget{}, false
		}
		return linkTarget{Value: value, Slide: slide - 1}, true
	}
	return linkTarget{Value: value, URL: value, Slide: -1}, true
}

func linkTargetFromQuery(query string, slideCount int) (linkTarget, bool) {
	values, err := url.ParseQuery(query)
	if err != nil {
		return linkTarget{}, false
	}
	if raw := values.Get("slide"); raw != "" {
		return parseLinkTarget("#"+raw, slideCount)
	}
	return parseLinkTarget(values.Get("link"), slideCount)
}

func elementLink(query string) string {
	target, ok := linkTargetFromQuery(query, 0)
	if !ok {
		return ""
	}
	return target.Value
}

func setElementLink(element *Element, link string, slideCount int) bool {
	if element == nil {
		return false
	}
	values, _ := url.ParseQuery(element.Query)
	if strings.TrimSpace(link) == "" {
		values.Del("link")
		values.Del("slide")
		element.Query = values.Encode()
		return true
	}
	sanitized, ok := normalizeLinkValue(link, slideCount)
	if !ok {
		return false
	}
	setLinkValues(values, sanitized)
	element.Query = values.Encode()
	return true
}

func openElementLink(target linkTarget) bool {
	if target.URL == "" {
		return false
	}
	return exec.Command("open", target.URL).Start() == nil
}

func textRenderMode(query string) string {
	values, err := url.ParseQuery(query)
	if err != nil {
		return ""
	}
	return values.Get("render")
}

func ansiFG(name string) (string, bool) {
	return ansiColour(name, false)
}

func ansiBG(name string) (string, bool) {
	return ansiColour(name, true)
}

func ansiColour(name string, background bool) (string, bool) {
	if code, ok := ansiHexColour(name, background); ok {
		return code, true
	}
	base := 30
	if background {
		base = 40
	}
	colours := map[string]int{
		"black":   0,
		"red":     1,
		"green":   2,
		"yellow":  3,
		"blue":    4,
		"magenta": 5,
		"purple":  5,
		"cyan":    6,
		"white":   7,
	}
	value, ok := colours[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return "", false
	}
	return strconv.Itoa(base + value), true
}

func ansiHexColour(name string, background bool) (string, bool) {
	r, g, b, ok := parseHexColour(name)
	if !ok {
		return "", false
	}
	prefix := "38"
	if background {
		prefix = "48"
	}
	return fmt.Sprintf("%s;2;%d;%d;%d", prefix, r, g, b), true
}

func normalizeHexColour(value string) (string, bool) {
	r, g, b, ok := parseHexColour(value)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("#%02x%02x%02x", r, g, b), true
}

func parseHexColour(value string) (int, int, int, bool) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "#")
	if len(value) == 3 {
		value = strings.Repeat(value[0:1], 2) + strings.Repeat(value[1:2], 2) + strings.Repeat(value[2:3], 2)
	}
	if len(value) != 6 {
		return 0, 0, 0, false
	}
	parsed, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return int((parsed >> 16) & 0xff), int((parsed >> 8) & 0xff), int(parsed & 0xff), true
}

func splitImageSource(src string) (string, string) {
	before, after, ok := strings.Cut(src, "?")
	if !ok {
		return src, ""
	}
	return before, after
}

func authoredRenderSize(width, height int) (int, int) {
	if authoredTerminalWidth > 0 && authoredTerminalHeight > 0 {
		return authoredTerminalWidth, authoredTerminalHeight
	}
	return width, height
}

func terminalScaleX(width, height int) float64 {
	authorWidth, _ := authoredRenderSize(width, height)
	if authorWidth <= 0 || width <= 0 {
		return 1
	}
	return float64(width) / float64(authorWidth)
}

func terminalScaleY(width, height int) float64 {
	_, authorHeight := authoredRenderSize(width, height)
	if authorHeight <= 0 || height <= 0 {
		return 1
	}
	return float64(height) / float64(authorHeight)
}

func displayLines(slide Slide, width, height, page int) []Line {
	pages := displayPages(slide, width, height)
	if page < 0 || page >= len(pages) {
		return nil
	}
	return append([]Line(nil), pages[page]...)
}

func displayPages(slide Slide, width, height int) [][]Line {
	lines := displayLayout(slide, width, height)
	var repeatedPageNumber []Line
	var content []Line
	for _, line := range lines {
		if line.Element >= 0 && line.Element < len(slide.Elements) && slide.Elements[line.Element].Kind == "page-number" {
			repeatedPageNumber = append(repeatedPageNumber, line)
			continue
		}
		content = append(content, line)
	}
	pages := paginateLayout(content, height)
	if len(repeatedPageNumber) == 0 {
		return pages
	}
	for index := range pages {
		pageNumber := append([]Line(nil), repeatedPageNumber...)
		pages[index] = append(pageNumber, pages[index]...)
	}
	return pages
}

func authoredStep(direction string, step, width, height, authorWidth, authorHeight int) int {
	if step <= 0 {
		return 1
	}
	switch direction {
	case "left", "right":
		if width > 0 && authorWidth > 0 {
			return max(1, int(math.Round(float64(step)*float64(authorWidth)/float64(width))))
		}
	case "up", "down":
		if height > 0 && authorHeight > 0 {
			return max(1, int(math.Round(float64(step)*float64(authorHeight)/float64(height))))
		}
	}
	return step
}

func displayLayout(slide Slide, width, height int) []Line {
	return layout(scaleSlideForTerminal(slide, width, height), width, height)
}

func scaleSlideForTerminal(slide Slide, width, height int) Slide {
	authorWidth, authorHeight := authoredRenderSize(width, height)
	if authorWidth <= 0 || authorHeight <= 0 || width <= 0 || height <= 0 || authorWidth == width && authorHeight == height {
		return slide
	}
	scaleX := float64(width) / float64(authorWidth)
	scaleY := float64(height) / float64(authorHeight)
	scaled := slide
	scaled.Elements = append([]Element(nil), slide.Elements...)
	for i := range scaled.Elements {
		scaled.Elements[i] = scaleElementForTerminal(scaled.Elements[i], scaleX, scaleY)
	}
	return scaled
}

func scaleElementForTerminal(element Element, scaleX, scaleY float64) Element {
	element.Query = scalePlacementQueryForTerminal(element.Query, scaleX, scaleY)
	switch {
	case element.Kind == "image":
		element.Query = scaleQueryFloatForTerminal(element.Query, "scale", 1.0, scaleX, 0.1, 1.0)
	case element.Kind == "shape":
		element.Query = scaleQueryIntForTerminal(element.Query, "width", 12, scaleX, 1, 1000)
		element.Query = scaleQueryIntForTerminal(element.Query, "height", 6, scaleY, 1, 1000)
	case rendersAsTextImage(element):
		element.Query = scaleQueryFloatForTerminal(element.Query, "scale", 1.0, scaleX, 0.1, 10.0)
	case shouldAutoScaleTextElement(element, scaleX):
		source, scale := textImageSourceAndScale(textSize(element))
		element.Query = setTextRenderQuery(element.Query, "text-image", source, clampFloat(scale*scaleX, 0.1, 10.0), textSize(element))
	}
	return element
}

func shouldAutoScaleTextElement(element Element, scaleX float64) bool {
	if math.Abs(scaleX-1) < 0.02 || rendersAsTextImage(element) {
		return false
	}
	return element.Kind == "heading" || element.Kind == "text" || element.Kind == "bullet" || element.Kind == "page-number"
}

func scalePlacementQueryForTerminal(query string, scaleX, scaleY float64) string {
	values, err := url.ParseQuery(query)
	if err != nil {
		return query
	}
	for _, key := range []string{"left", "right", "width"} {
		if raw := values.Get(key); raw != "" {
			if value, err := strconv.Atoi(raw); err == nil {
				low := 0
				if key == "width" {
					low = 1
				}
				values.Set(key, strconv.Itoa(max(low, int(math.Round(float64(value)*scaleX)))))
			}
		}
	}
	for _, key := range []string{"top", "bottom", "row_delta", "height"} {
		if raw := values.Get(key); raw != "" {
			if value, err := strconv.Atoi(raw); err == nil {
				scaled := int(math.Round(float64(value) * scaleY))
				if key == "height" {
					scaled = max(1, scaled)
				}
				values.Set(key, strconv.Itoa(scaled))
			}
		}
	}
	return values.Encode()
}

func scaleQueryFloatForTerminal(query, key string, defaultValue, factor, low, high float64) string {
	values, err := url.ParseQuery(query)
	if err != nil {
		return query
	}
	value := defaultValue
	if raw := values.Get(key); raw != "" {
		if parsed, err := strconv.ParseFloat(raw, 64); err == nil && parsed > 0 {
			value = parsed
		}
	}
	values.Set(key, fmt.Sprintf("%.3f", clampFloat(value*factor, low, high)))
	return values.Encode()
}

func scaleQueryIntForTerminal(query, key string, defaultValue int, factor float64, low, high int) string {
	values, err := url.ParseQuery(query)
	if err != nil {
		return query
	}
	value := defaultValue
	if raw := values.Get(key); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			value = parsed
		}
	}
	values.Set(key, strconv.Itoa(max(low, min(high, int(math.Round(float64(value)*factor))))))
	return values.Encode()
}

func renderSlide(slide Slide, width, height, page int, view ViewState) {
	flushTerminalFrame(func() {
		termPrintf("\033[%sm\033[2J\033[H", slideBG(slide))
		backgroundFrame := captureTerminalOutput(func() {
			drawStaticBackground(slide.Background, width, height, slideBG(slide))
		})
		termPrint(backgroundFrame)
		lines := displayLines(slide, width, height, page)
		backgroundLines := ansiFrameToExportLines(backgroundFrame, width, height, ansiCSSColour(slideFG(slide)))
		drawTransparentShapeBackdrop(lines, backgroundLines, width, height, slide)
		drawOverlayLines(lines, width, height, slide)
		drawLinkUnderlines(lines, width, height, slide)
		drawViewChrome(width, height, view)
		drawViewOverlays(width, height, view)
		termPrint("\033[0m")
	})
}

func repaintFrameBackground(bg string, width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	blank := strings.Repeat(" ", width)
	for row := 1; row <= height; row++ {
		termPrintf("\033[%sm\033[%d;1H%s", bg, row, blank)
	}
	termPrint("\033[H")
}

var terminalFrame *strings.Builder

func flushTerminalFrame(draw func()) {
	var buffer strings.Builder
	terminalFrame = &buffer
	draw()
	terminalFrame = nil
	if activePresenter != nil {
		width, height := terminalAuthoredSize()
		activePresenter.PublishTerminalFrame(buffer.String(), width, height)
	}
	if !nativeAppModeActive {
		fmt.Print("\033[?2026h" + buffer.String() + "\033[?2026l")
	}
}

func captureTerminalOutput(draw func()) string {
	var buffer strings.Builder
	previous := terminalFrame
	terminalFrame = &buffer
	draw()
	terminalFrame = previous
	return buffer.String()
}

func termPrint(args ...any) {
	if terminalFrame != nil {
		fmt.Fprint(terminalFrame, args...)
		return
	}
	fmt.Print(args...)
}

func termPrintf(format string, args ...any) {
	if terminalFrame != nil {
		fmt.Fprintf(terminalFrame, format, args...)
		return
	}
	fmt.Printf(format, args...)
}

func playAnimatedSlide(slide Slide, width, height, page int, view ViewState) string {
	ticker := time.NewTicker(70 * time.Millisecond)
	defer ticker.Stop()
	for {
		if action := pollKey(); action != "" {
			return action
		}
		renderSlide(slide, width, height, page, view)
		<-ticker.C
	}
}

func playStaticSlide(slide Slide, width, height, page int, view ViewState) string {
	if view.TimerMode == "" {
		renderSlide(slide, width, height, page, view)
		return waitKey()
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		if action := pollKey(); action != "" {
			return action
		}
		renderSlide(slide, width, height, page, view)
		<-ticker.C
	}
}

func slideHasAnimatedImage(slide Slide) bool {
	for _, element := range slide.Elements {
		if element.Kind != "image" {
			continue
		}
		ext := strings.ToLower(filepath.Ext(element.Path))
		if ext == ".gif" || ext == ".webp" {
			return true
		}
	}
	return false
}

func prewarmImageCache(slides []Slide, width, height int) {
	prewarmingImageCache = true
	defer func() { prewarmingImageCache = false }()
	for _, slide := range slides {
		if slideHasAnimatedImage(slide) {
			_ = displayLayout(slide, width, height)
		}
	}
}

func drawOverlayLines(lines []Line, width, height int, slide Slide) {
	drawOverlayLineSet(lines, width, height, slide, nil)
}

func drawOverlayLineSet(lines []Line, width, height int, slide Slide, rows map[int]bool) {
	for index, line := range lines {
		row := line.Row
		if rows != nil && !rows[row] {
			continue
		}
		if row < 0 || row >= height {
			continue
		}
		if line.Text == "" {
			continue
		}
		if transparencyMaskLine(line, slide) {
			continue
		}
		transparency := transparentShapeCellsFrom(lines, index+1, width, height, slide)
		switch line.Role {
		case "heading":
			drawStyledLine(row+1, line.Col+1, line.Text, width, lineFG(slide, line, true), slideBG(slide), false, transparency)
			continue
		case "code":
			drawStyledLine(row+1, line.Col+1, line.Text, width, codeLineFG(slide, line), codeLineBG(line), true, transparency)
			continue
		case "image":
			drawStyledLine(row+1, line.Col+1, line.Text, width, slideFG(slide), slideBG(slide), false, transparency)
			continue
		default:
			drawStyledLine(row+1, line.Col+1, line.Text, width, lineFG(slide, line, false), slideBG(slide), false, transparency)
		}
	}
}

func drawTransparentShapeBackdrop(lines []Line, backdrop []exportLine, width, height int, slide Slide) {
	transparency := transparentShapeCells(lines, width, height, slide)
	if len(transparency) == 0 {
		return
	}
	for row, cells := range transparency {
		if row < 0 || row >= height || len(cells) == 0 {
			continue
		}
		cols := make([]int, 0, len(cells))
		for col := range cells {
			cols = append(cols, col)
		}
		sort.Ints(cols)
		start := cols[0]
		prev := cols[0]
		bg := cells[start]
		flush := func(end int, runBG string) {
			termPrintf("\033[0;%s;%sm\033[%d;%dH%s", slideFG(slide), runBG, row+1, start+1, strings.Repeat(" ", end-start+1))
		}
		for _, col := range cols[1:] {
			if col == prev+1 && cells[col] == bg {
				prev = col
				continue
			}
			flush(prev, bg)
			start = col
			prev = col
			bg = cells[col]
		}
		flush(prev, bg)
	}
	for _, line := range backdrop {
		if line.Row < 0 || line.Row >= height {
			continue
		}
		for _, part := range line.Parts {
			fg := cssColourToFG(part.Color, slideFG(slide))
			for offset, r := range []rune(part.Text) {
				col := part.Col + offset
				if r == ' ' || col < 0 || col >= width {
					continue
				}
				rowCells := transparency[line.Row]
				if rowCells == nil {
					continue
				}
				bg := rowCells[col]
				if bg == "" {
					continue
				}
				termPrintf("\033[0;%s;%sm\033[%d;%dH%s", darkenFGForTransparency(fg), bg, line.Row+1, col+1, string(r))
			}
		}
	}
}

func drawOverlayRows(lines []Line, width, height int, slide Slide, rows map[int]bool) {
	if len(rows) == 0 {
		return
	}
	blank := strings.Repeat(" ", max(0, width))
	for row := range rows {
		if row < 0 || row >= height {
			continue
		}
		termPrintf("\033[%sm\033[%d;1H%s", slideBG(slide), row+1, blank)
	}
	drawOverlayLineSet(lines, width, height, slide, rows)
}

func lineStyle(slide Slide, line Line, heading bool) string {
	underline := "0"
	if elementLink(line.Query) != "" && line.Role != "image" {
		underline = "4"
	}
	if fg := elementFG(line.Query, heading); fg != "" {
		return fmt.Sprintf("\033[%s;%s;%sm", underline, fg, slideBG(slide))
	}
	if underline == "4" {
		if heading {
			return fmt.Sprintf("\033[4;%s;%sm", slideHeaderFG(slide), slideBG(slide))
		}
		return fmt.Sprintf("\033[4;%s;%sm", slideFG(slide), slideBG(slide))
	}
	if heading {
		return slideHeaderStyle(slide)
	}
	return slideStyle(slide, false)
}

func lineFG(slide Slide, line Line, heading bool) string {
	if fg := elementFG(line.Query, heading); fg != "" {
		return fg
	}
	if heading {
		return slideHeaderFG(slide)
	}
	return slideFG(slide)
}

func codeBlockStyle(slide Slide, line Line) string {
	return fmt.Sprintf("\033[0;%s;%sm", codeLineFG(slide, line), codeLineBG(line))
}

func codeLineFG(slide Slide, line Line) string {
	fg := slideFG(slide)
	if elementFG := elementFG(line.Query, false); elementFG != "" {
		fg = elementFG
	}
	return fg
}

func codeLineBG(line Line) string {
	bg := "100"
	if elementBG := elementBG(line.Query); elementBG != "" {
		bg = elementBG
	}
	return bg
}

func transparentShapeCells(lines []Line, width, height int, slide Slide) map[int]map[int]string {
	return transparentShapeCellsFrom(lines, 0, width, height, slide)
}

func transparentShapeCellsFrom(lines []Line, start int, width, height int, slide Slide) map[int]map[int]string {
	cells := map[int]map[int]string{}
	if start < 0 {
		start = 0
	}
	if start >= len(lines) {
		return cells
	}
	for _, line := range lines[start:] {
		if !transparencyMaskLine(line, slide) || line.Row < 0 || line.Row >= height {
			continue
		}
		element := slide.Elements[line.Element]
		bg := transparentElementBG(slide, element, line.Role)
		plain := []rune(stripANSI(line.Text))
		for offset, r := range plain {
			if r == ' ' {
				continue
			}
			col := line.Col + offset
			if col < 0 || col >= width {
				continue
			}
			if cells[line.Row] == nil {
				cells[line.Row] = map[int]string{}
			}
			cells[line.Row][col] = bg
		}
	}
	return cells
}

func transparencyMaskLine(line Line, slide Slide) bool {
	if line.Element < 0 || line.Element >= len(slide.Elements) {
		return false
	}
	switch line.Role {
	case "shape", "heading", "body", "code", "image":
		return elementTransparent(slide.Elements[line.Element])
	default:
		return false
	}
}

func transparentElementBG(slide Slide, element Element, role string) string {
	heading := role == "heading"
	fg := elementFG(element.Query, heading)
	if fg == "" {
		if heading {
			fg = slideHeaderFG(slide)
		} else {
			fg = slideFG(slide)
		}
	}
	if bg, ok := fgCodeToBG(fg); ok {
		return bg
	}
	return slideBG(slide)
}

func drawStyledLine(row, startCol int, text string, width int, baseFG, baseBG string, solid bool, transparency map[int]map[int]string) {
	if startCol < 1 {
		if solid {
			text = runeSliceWithPadding(text, 1-startCol, displayWidth(text))
		}
		startCol = 1
	}
	if startCol > width {
		return
	}
	col := startCol
	normalFG := baseFG
	fg := baseFG
	bold := false
	for i := 0; i < len(text) && col <= width; {
		if text[i] == 0x1b {
			end := i + 1
			for end < len(text) && text[end] != 'm' {
				end++
			}
			if end < len(text) {
				sequence := text[i : end+1]
				if ansiSequenceBoldOff(sequence) {
					bold = false
					fg = normalFG
				}
				if ansiSequenceBoldOn(sequence) {
					bold = true
					fg = lightenFG(normalFG)
				}
				if parsed := ansiSequenceFG(sequence); parsed != "" {
					normalFG = parsed
					fg = parsed
					if bold {
						fg = lightenFG(normalFG)
					}
				}
				i = end + 1
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		if r != ' ' || solid {
			cellFG, cellBG := fg, baseBG
			if rowCells := transparency[row-1]; rowCells != nil {
				if bg := rowCells[col-1]; bg != "" {
					cellBG = bg
					cellFG = darkenFGForTransparency(cellFG)
				}
			}
			termPrintf("\033[0;%s;%sm\033[%d;%dH%s", cellFG, cellBG, row, col, string(r))
		}
		col++
		i += size
	}
}

func ansiSequenceFG(sequence string) string {
	sequence = strings.TrimPrefix(sequence, "\033[")
	sequence = strings.TrimSuffix(sequence, "m")
	fields := strings.Split(sequence, ";")
	for i := 0; i < len(fields); i++ {
		if (fields[i] == "38") && i+4 < len(fields) && fields[i+1] == "2" {
			return strings.Join(fields[i:i+5], ";")
		}
		if parsed, err := strconv.Atoi(fields[i]); err == nil && (parsed >= 30 && parsed <= 37 || parsed >= 90 && parsed <= 97) {
			return strconv.Itoa(parsed)
		}
	}
	return ""
}

func darkenFG(code string, factor float64) string {
	r, g, b, ok := ansiCodeRGB(code)
	if !ok {
		return code
	}
	r = int(math.Round(float64(r) * factor))
	g = int(math.Round(float64(g) * factor))
	b = int(math.Round(float64(b) * factor))
	return fmt.Sprintf("38;2;%d;%d;%d", max(0, min(255, r)), max(0, min(255, g)), max(0, min(255, b)))
}

func lightenFG(code string) string {
	r, g, b, ok := ansiCodeRGB(code)
	if !ok {
		return code
	}
	r = int(math.Round(float64(r) + (255-float64(r))*0.35))
	g = int(math.Round(float64(g) + (255-float64(g))*0.35))
	b = int(math.Round(float64(b) + (255-float64(b))*0.35))
	return fmt.Sprintf("38;2;%d;%d;%d", max(0, min(255, r)), max(0, min(255, g)), max(0, min(255, b)))
}

func darkenFGForTransparency(code string) string {
	r, g, b, ok := ansiCodeRGB(code)
	if !ok {
		return code
	}
	luma := float64(299*r+587*g+114*b) / 255000.0
	factor := 0.8 - math.Pow(clampFloat(luma, 0, 1), 3.0)*0.6
	return darkenFG(code, factor)
}

func fgCodeToBG(code string) (string, bool) {
	parts := strings.Split(code, ";")
	if len(parts) == 5 && parts[0] == "38" && parts[1] == "2" {
		parts[0] = "48"
		return strings.Join(parts, ";"), true
	}
	if parsed, err := strconv.Atoi(code); err == nil {
		switch {
		case parsed >= 30 && parsed <= 37:
			return strconv.Itoa(parsed + 10), true
		case parsed >= 90 && parsed <= 97:
			return strconv.Itoa(parsed + 10), true
		}
	}
	return "", false
}

func ansiCodeRGB(code string) (int, int, int, bool) {
	parts := strings.Split(code, ";")
	if len(parts) == 5 && (parts[0] == "38" || parts[0] == "48") && parts[1] == "2" {
		r, errR := strconv.Atoi(parts[2])
		g, errG := strconv.Atoi(parts[3])
		b, errB := strconv.Atoi(parts[4])
		if errR == nil && errG == nil && errB == nil {
			return r, g, b, true
		}
	}
	if parsed, err := strconv.Atoi(code); err == nil {
		colours := map[int][3]int{
			30: {0, 0, 0}, 31: {170, 0, 0}, 32: {0, 170, 0}, 33: {170, 170, 0},
			34: {0, 0, 170}, 35: {170, 0, 170}, 36: {0, 170, 170}, 37: {243, 239, 224},
			90: {85, 85, 85}, 91: {255, 85, 85}, 92: {85, 255, 85}, 93: {255, 255, 85},
			94: {85, 85, 255}, 95: {255, 85, 255}, 96: {85, 255, 255}, 97: {255, 255, 255},
		}
		if rgb, ok := colours[parsed]; ok {
			return rgb[0], rgb[1], rgb[2], true
		}
	}
	return 0, 0, 0, false
}

func cssColourToFG(css, fallback string) string {
	css = strings.TrimSpace(css)
	if strings.HasPrefix(css, "#") {
		if r, g, b, ok := parseHexColour(css); ok {
			return fmt.Sprintf("38;2;%d;%d;%d", r, g, b)
		}
	}
	if strings.HasPrefix(css, "rgb(") && strings.HasSuffix(css, ")") {
		fields := strings.Split(strings.TrimSuffix(strings.TrimPrefix(css, "rgb("), ")"), ",")
		if len(fields) == 3 {
			r, errR := strconv.Atoi(strings.TrimSpace(fields[0]))
			g, errG := strconv.Atoi(strings.TrimSpace(fields[1]))
			b, errB := strconv.Atoi(strings.TrimSpace(fields[2]))
			if errR == nil && errG == nil && errB == nil {
				return fmt.Sprintf("38;2;%d;%d;%d", max(0, min(255, r)), max(0, min(255, g)), max(0, min(255, b)))
			}
		}
	}
	return fallback
}

func lightenCSSColour(css string) string {
	fg := cssColourToFG(css, "37")
	return ansiCSSColour(lightenFG(fg))
}

func slideStyle(slide Slide, bold bool) string {
	weight := "0"
	if bold {
		weight = "1"
	}
	return fmt.Sprintf("\033[%s;%s;%sm", weight, slideFG(slide), slideBG(slide))
}

func slideHeaderStyle(slide Slide) string {
	return fmt.Sprintf("\033[0;%s;%sm", slideHeaderFG(slide), slideBG(slide))
}

func slideHeaderFG(slide Slide) string {
	if slide.HeaderFG != "" {
		return slide.HeaderFG
	}
	return slideFG(slide)
}

func slideFG(slide Slide) string {
	if slide.FG == "" {
		return "37"
	}
	return slide.FG
}

func slideBG(slide Slide) string {
	if slide.BG == "" {
		return "40"
	}
	return slide.BG
}

func elementFG(query string, heading bool) string {
	values, err := url.ParseQuery(query)
	if err != nil {
		return ""
	}
	if heading {
		if code, ok := ansiFG(values.Get("header")); ok {
			return code
		}
	}
	if code, ok := ansiFG(values.Get("fg")); ok {
		return code
	}
	return ""
}

func elementBG(query string) string {
	values, err := url.ParseQuery(query)
	if err != nil {
		return ""
	}
	if code, ok := ansiBG(values.Get("bg")); ok {
		return code
	}
	return ""
}

func drawTransparentTextAt(row, startCol int, text string, width int) {
	if startCol < 1 {
		startCol = 1
	}
	runes := []rune(crop(text, max(0, width-startCol+1)))
	for col := 0; col < len(runes); {
		for col < len(runes) && runes[col] == ' ' {
			col++
		}
		if col >= len(runes) {
			break
		}
		start := col
		for col < len(runes) && runes[col] != ' ' {
			col++
		}
		termPrintf("\033[%d;%dH%s", row, startCol+start, string(runes[start:col]))
	}
}

func drawSolidTextAt(row, startCol int, text string, width int) {
	if startCol < 1 {
		text = runeSliceWithPadding(text, 1-startCol, displayWidth(text))
		startCol = 1
	}
	if startCol > width {
		return
	}
	termPrintf("\033[%d;%dH%s", row, startCol, crop(text, max(0, width-startCol+1)))
}

func hasExplicitAlign(query string) bool {
	values, err := url.ParseQuery(query)
	if err != nil {
		return false
	}
	switch values.Get("align") {
	case "left", "center", "right":
		return true
	default:
		return false
	}
}

func drawTransparentANSI(row, startCol int, text string, width int) {
	if startCol < 1 {
		startCol = 1
	}
	col := startCol
	active := ""
	for i := 0; i < len(text) && col <= width; {
		if text[i] == 0x1b {
			end := i + 1
			for end < len(text) && text[end] != 'm' {
				end++
			}
			if end < len(text) {
				active += text[i : end+1]
				i = end + 1
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		if r != ' ' {
			termPrintf("\033[%d;%dH%s%c", row, col, active, r)
			active = ""
		}
		col++
		i += size
	}
	if active != "" {
		termPrint("\033[0m")
	}
}

func editState(states map[int]*EditState, slideIndex int) *EditState {
	state := states[slideIndex]
	if state == nil {
		state = &EditState{Selected: -1, LastSelected: -1, Cursor: map[int]int{}, MultiSelected: map[int]bool{}}
		states[slideIndex] = state
	}
	if state.Cursor == nil {
		state.Cursor = map[int]int{}
	}
	if state.MultiSelected == nil {
		state.MultiSelected = map[int]bool{}
	}
	return state
}

type editModeOptions struct {
	Master               bool
	MasterName           string
	Persist              func() bool
	TogglePageNumber     func(*Slide) string
	EditVisualProperties func(*Slide) bool
}

func playEditMode(deck *Deck, current int, width, height, page int, deckPath string, state *EditState, options editModeOptions) string {
	slides := &deck.Slides
	workingSlide := deck.ResolveSlide(current, true)
	slide := &workingSlide
	entryPage := page
	modeOriginal := cloneSlide(*slide)
	selected := state.Selected
	if selected < 0 || selected >= len(slide.Elements) || !isSelectableElement(slide.Elements[selected]) {
		selected = -1
	}
	multiSelected := state.MultiSelected
	if multiSelected == nil {
		multiSelected = map[int]bool{}
		state.MultiSelected = multiSelected
	}
	pruneSelectionSet(multiSelected, *slide)
	cursor := state.Cursor
	if cursor == nil {
		cursor = map[int]int{}
		state.Cursor = cursor
	}
	if selected >= 0 && isEditableElement(slide.Elements[selected]) {
		ensureCursor(slide, cursor, selected)
	}
	state.Selected = selected
	mode := "select"
	status := ""
	textSelectionMoved := false
	imageSelectionMoved := false
	clipboard := state.Clipboard
	authorWidth, authorHeight := 0, 0
	axisScaleMode := ""
	axisScaleLastMode := ""
	notesOpen := state.ShowNotes
	notesEditing := false
	slideListOpen := state.ShowSlides
	notesCursor := max(0, min(len([]rune(slide.Notes)), state.NotesCursor))
	notesDraft := slide.Notes
	slideNavIndex := state.SlideNavIndex
	if slideNavIndex < 0 || slideNavIndex >= len(*slides) {
		slideNavIndex = current
	}
	slideNavScroll := max(0, state.SlideNavScroll)
	fullEditRedraw := true
	editDirty := true
	var currentLines []Line
	persist := func() bool {
		deck.StoreResolvedSlide(current, *slide)
		if options.Persist != nil {
			return options.Persist()
		}
		return persistDeck(deckPath, *deck)
	}
	commit := func(before Slide) {
		commitSlideSnapshot(state, before, *slide)
		persist()
		modeOriginal = cloneSlide(*slide)
		fullEditRedraw = true
		editDirty = true
		currentLines = nil
	}
	beginSelectionMove := func() {
		if !textSelectionMoved && !imageSelectionMoved {
			modeOriginal = cloneSlide(*slide)
		}
	}
	commitPendingSelectionMove := func() bool {
		if !textSelectionMoved && !imageSelectionMoved {
			return false
		}
		commit(modeOriginal)
		textSelectionMoved = false
		imageSelectionMoved = false
		return true
	}
	clearSelection := func() {
		selected = -1
		status = ""
		state.Selected = selected
		for index := range multiSelected {
			delete(multiSelected, index)
		}
		textSelectionMoved = false
		imageSelectionMoved = false
	}
	reconcileSelection := func() {
		pruneSelectionSet(multiSelected, *slide)
		if selected < 0 || selected >= len(slide.Elements) || !isSelectableElement(slide.Elements[selected]) {
			selected = -1
			status = ""
			if mode == "text" || mode == "move" {
				mode = "select"
			}
			return
		}
		if mode == "text" && !isEditableElement(slide.Elements[selected]) {
			selected = -1
			status = ""
			mode = "select"
			return
		}
		if isEditableElement(slide.Elements[selected]) {
			ensureCursor(slide, cursor, selected)
		}
		if elementPage, ok := pageForElement(*slide, width, height, selected); ok {
			page = elementPage
		}
	}
	activeIndices := func() []int {
		return activeSelectionIndices(multiSelected, selected, *slide)
	}
	removeSelected := func(copyToClipboard bool) bool {
		if selected < 0 || selected >= len(slide.Elements) {
			return false
		}
		before := cloneSlide(*slide)
		if copyToClipboard {
			element := clipboardElementFromSelection(*slide, selected, width, height)
			clipboard = &element
			state.Clipboard = clipboard
		}
		slide.Elements = append(slide.Elements[:selected], slide.Elements[selected+1:]...)
		delete(cursor, selected)
		shiftCursorKeys(cursor, selected, -1)
		clearSelection()
		commit(before)
		return true
	}
	removeSelection := func(copyToClipboard bool) bool {
		indices := activeIndices()
		if len(indices) == 0 {
			return false
		}
		if len(indices) == 1 {
			return removeSelected(copyToClipboard)
		}
		before := cloneSlide(*slide)
		if copyToClipboard {
			element := clipboardElementFromSelection(*slide, indices[len(indices)-1], width, height)
			clipboard = &element
			state.Clipboard = clipboard
		}
		sort.Sort(sort.Reverse(sort.IntSlice(indices)))
		for _, index := range indices {
			if index < 0 || index >= len(slide.Elements) {
				continue
			}
			slide.Elements = append(slide.Elements[:index], slide.Elements[index+1:]...)
			delete(cursor, index)
			shiftCursorKeys(cursor, index, -1)
		}
		clearSelection()
		commit(before)
		return true
	}
	fillImagePlaceholder := func(index int) bool {
		if index < 0 || index >= len(slide.Elements) || slide.Elements[index].Kind != "image" || !slide.Elements[index].Placeholder || slide.Elements[index].MasterSlotID == "" {
			return false
		}
		path, ok := chooseImageFile()
		if !ok {
			setUINotice("Image selection cancelled")
			return false
		}
		path, ok = copyImageToDeckDir(path, deckPath)
		if !ok {
			setUIError("Image copy failed")
			return false
		}
		slide.Elements[index].Path = path
		slide.Elements[index].Text = ""
		slide.Elements[index].Placeholder = false
		if slide.Elements[index].ID == "" {
			slide.Elements[index].ID = newStableID("slide-element")
		}
		return true
	}
	beginBatchMove := func(indices []int) {
		if len(indices) == 0 {
			return
		}
		beginSelectionMove()
		if selectionContainsText(*slide, indices) {
			textSelectionMoved = true
		}
		if selectionContainsPositioned(*slide, indices) {
			imageSelectionMoved = true
		}
	}
	moveBatch := func(direction string, step int) {
		indices := activeIndices()
		if len(indices) == 0 {
			return
		}
		beginBatchMove(indices)
		for _, index := range indices {
			moveSelectedElement(slide, index, direction, authorWidth, authorHeight, authoredStep(direction, step, width, height, authorWidth, authorHeight))
		}
		status = selectionStatus(*slide, selected, multiSelected)
		persist()
	}
	beginAxisScale := func(picked string) bool {
		if picked == "" || picked == axisScaleClose || selected < 0 || selected >= len(slide.Elements) || !isAxisScalableElement(slide.Elements[selected]) {
			axisScaleMode = ""
			return false
		}
		axisScaleLastMode = picked
		modeOriginal = cloneSlide(*slide)
		beginElementResize(slide, selected, authorWidth, authorHeight)
		mode = "resize"
		axisScaleMode = picked
		for index := range multiSelected {
			delete(multiSelected, index)
		}
		status = selectionStatus(*slide, selected, multiSelected)
		return true
	}
	var prevLines []Line
	prevSelected := -1
	prevMultiSelected := map[int]bool{}
	prevStatus := ""
	prevMode := ""
	lastEditWidth, lastEditHeight := -1, -1
	lastAnimationFrame := time.Now()
	animationFrame := 0
	var editMatrix *matrixEffect
	var editStars *starsEffect
	var editBursts *burstEffect
	for {
		width, height = terminalAuthoredSize()
		if width != lastEditWidth || height != lastEditHeight {
			lastEditWidth, lastEditHeight = width, height
			fullEditRedraw = true
			editDirty = true
		}
		if (slideListOpen || notesOpen || state.TimerMode != "" || slideHasAnimatedImage(*slide) || slide.Effect != "") && time.Since(lastAnimationFrame) >= 70*time.Millisecond {
			lastAnimationFrame = time.Now()
			animationFrame++
			editDirty = true
			fullEditRedraw = true
		}
		authorWidth, authorHeight = authoredRenderSize(width, height)
		reconcileSelection()
		initialEvent := false
		event := KeyEvent{}
		if queuedEditEvent != nil {
			event = *queuedEditEvent
			queuedEditEvent = nil
			initialEvent = true
		} else {
			if editDirty {
				flushTerminalFrame(func() {
					fastImageRender = true
					defer func() { fastImageRender = false }()
					currentLines = displayLines(*slide, width, height, page)
					if fullEditRedraw || len(prevLines) == 0 {
						repaintFrameBackground(slideBG(*slide), width, height)
						drawStaticBackground(slide.Background, width, height, slideBG(*slide))
						if slide.Effect != "" {
							if editMatrix == nil || editMatrix.width != width || editMatrix.height != height {
								editMatrix = newMatrix(width, height)
								editStars = newStars(width, height)
								editBursts = newBursts(slide.Effect, width, height)
							}
							drawEffectFrame(slide.Effect, width, height, animationFrame, editMatrix, editStars, editBursts, slideBG(*slide))
						}
						drawOverlayLines(currentLines, width, height, *slide)
						drawLinkUnderlines(currentLines, width, height, *slide)
					} else {
						rows := map[int]bool{height - 1: true}
						addSelectedElementRows(rows, prevLines, prevSelected, height)
						addSelectedElementRows(rows, currentLines, selected, height)
						addSelectionSetRows(rows, prevLines, prevMultiSelected, width, height)
						addSelectionSetRows(rows, currentLines, multiSelected, width, height)
						addSelectionUnderlineRow(rows, prevLines, prevSelected, width, height, prevStatus == "image selected")
						addSelectionUnderlineRow(rows, currentLines, selected, width, height, status == "image selected")
						if prevMode == "text" {
							addSelectedElementRows(rows, prevLines, prevSelected, height)
						}
						if mode == "text" {
							addSelectedElementRows(rows, currentLines, selected, height)
						}
						drawOverlayRows(currentLines, width, height, *slide, rows)
						drawLinkUnderlines(currentLines, width, height, *slide)
					}
					if (mode == "select" || mode == "move") && selectionSetCount(multiSelected) > 1 {
						drawSelectedSetHighlight(currentLines, multiSelected, selected, width, height)
						if selected >= 0 && selected < len(slide.Elements) && slide.Elements[selected].Kind == "image" {
							drawSelectedImageHighlight(currentLines, selected, width, height)
						} else if selected >= 0 {
							drawSelectedElementHighlight(currentLines, selected, width, height)
						}
					} else if (mode == "select" || mode == "move" || mode == "resize") && selected >= 0 && (status == "text selected" || status == "code text selected" || status == "shape selected" || status == "page number selected") {
						drawSelectedElementHighlight(currentLines, selected, width, height)
					} else if (mode == "select" || mode == "move" || mode == "resize") && selected >= 0 && status == "image selected" {
						drawSelectedImageHighlight(currentLines, selected, width, height)
					} else if mode == "text" && selected >= 0 && selected < len(slide.Elements) && isEditableElement(slide.Elements[selected]) {
						drawEditCursor(currentLines, selected, cursor[selected], width, height, *slide)
					}
					if slideListOpen {
						drawSlideNavigatorOverlay(deck.ResolvedSlides(), slideNavIndex, &slideNavScroll, width, height)
					}
					if notesOpen {
						drawSpeakerNotesPanel(notesDraft, notesCursor, notesEditing, width, height)
					}
					if state.TimerMode != "" {
						drawTimerOverlay(width, height, state.TimerMode, state.TimerInput, state.TimerDeadline)
					}
					if slideListOpen {
						drawSlideNavigatorToolbar(width, height, slideNavIndex, len(*slides))
					} else if notesOpen {
						drawSpeakerNotesToolbar(width, height, notesEditing)
					} else {
						drawEditToolbar(width, height, current, len(*slides), page, slidePageCount(*slide, width, height), mode, status, *slide, selected, axisScaleMode, options)
					}
					termPrint("\033[0m")
					prevLines = currentLines
					prevSelected = selected
					prevMultiSelected = cloneBoolMap(multiSelected)
					prevStatus = status
					prevMode = mode
					fullEditRedraw = false
					editDirty = false
				})
			}
			if slideListOpen {
				event = readSlideNavigatorKeyEvent()
			} else if notesOpen && notesEditing {
				event = readSpeakerNotesKeyEvent()
			} else if notesOpen {
				event = readSpeakerNotesViewKeyEvent()
			} else {
				event = readEditKeyEventForMode(mode)
			}
		}
		if event.Action == "" {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		editDirty = true
		if state.TimerMode != "" && applyTimerEvent(state, event) {
			fullEditRedraw = true
			continue
		}
		if event.Action == "text" && event.Text == "?" && mode != "text" && !slideListOpen && !notesOpen {
			title, items := editShortcutHelp(mode, status, *slide, selected, axisScaleMode, options.Master)
			replayed := playShortcutHelp(title, items, *slide, page, width, height, readEditKeyEvent)
			fullEditRedraw = true
			if shortcutHelpDismissed(replayed) {
				continue
			}
			event = replayed
		}
		if slideListOpen {
			switch event.Action {
			case "slide-list", "escape":
				slideListOpen = false
				state.ShowSlides = false
				fullEditRedraw = true
			case "up":
				slideNavIndex = max(0, slideNavIndex-1)
				fullEditRedraw = true
			case "down":
				slideNavIndex = min(len(*slides)-1, slideNavIndex+1)
				fullEditRedraw = true
			case "enter":
				state.ShowSlides = false
				state.SlideNavIndex = slideNavIndex
				state.SlideNavScroll = slideNavScroll
				state.Selected = selected
				state.Cursor = cursor
				state.Clipboard = clipboard
				return fmt.Sprintf("jump-slide:%d", slideNavIndex)
			case "mouse-click":
				if target, ok := slideNavigatorIndexAtPoint(event.X, event.Y, slideNavScroll, len(*slides), width, height); ok {
					slideNavIndex = target
					fullEditRedraw = true
				}
			}
			state.SlideNavIndex = slideNavIndex
			state.SlideNavScroll = slideNavScroll
			continue
		}
		if notesOpen && !notesEditing {
			switch event.Action {
			case "escape", "controls":
				notesOpen = false
				notesEditing = false
				notesDraft = slide.Notes
				notesCursor = min(notesCursor, len([]rune(notesDraft)))
				state.ShowNotes = false
				fullEditRedraw = true
				continue
			case "tab":
				notesCursor = min(notesCursor, len([]rune(notesDraft)))
				notesEditing = true
				fullEditRedraw = true
				continue
			case "enter":
				notesEditing = true
				fullEditRedraw = true
				continue
			case "mouse-click":
				if noteCursor, ok := notesCursorAtPoint(notesDraft, event.X, event.Y, width, height); ok {
					notesCursor = noteCursor
					notesEditing = true
					fullEditRedraw = true
					continue
				}
			}
		}
		if notesOpen && notesEditing {
			switch event.Action {
			case "escape":
				notesOpen = false
				notesEditing = false
				notesDraft = slide.Notes
				notesCursor = min(notesCursor, len([]rune(notesDraft)))
				state.ShowNotes = false
				fullEditRedraw = true
			case "left":
				notesCursor = max(0, notesCursor-1)
				fullEditRedraw = true
			case "right":
				notesCursor = min(len([]rune(notesDraft)), notesCursor+1)
				fullEditRedraw = true
			case "up":
				notesCursor = moveNotesCursorVertical(notesDraft, notesCursor, -1)
				fullEditRedraw = true
			case "down":
				notesCursor = moveNotesCursorVertical(notesDraft, notesCursor, 1)
				fullEditRedraw = true
			case "enter":
				before := cloneSlide(*slide)
				slide.Notes = notesDraft
				notesEditing = false
				state.NotesCursor = notesCursor
				commitSlideSnapshot(state, before, *slide)
				persist()
				modeOriginal = cloneSlide(*slide)
				fullEditRedraw = true
			case "backspace":
				if notesCursor > 0 {
					runes := []rune(notesDraft)
					runes = append(runes[:notesCursor-1], runes[notesCursor:]...)
					notesDraft = string(runes)
					notesCursor--
				}
				fullEditRedraw = true
			case "insert-newline":
				notesDraft, notesCursor, _ = insertNotesText(notesDraft, notesCursor, "\n", width, height)
				fullEditRedraw = true
			case "text":
				notesDraft, notesCursor, _ = insertNotesText(notesDraft, notesCursor, event.Text, width, height)
				fullEditRedraw = true
			}
			state.NotesCursor = notesCursor
			continue
		}
		ctx := interactionContextFor(mode, *slide, selected, multiSelected, status)
		ctx.Master = options.Master
		if !actionAllowedInContext(ctx, event.Action) {
			continue
		}
		switch event.Action {
		case "next":
			return "next-slide"
		case "prev":
			if current > 0 {
				return fmt.Sprintf("jump-slide:%d", current-1)
			}
			return ""
		case "present", "controls":
			return ""
		case "export":
			exportHTMLWithNotice(deckPath, deck.ResolvedSlides(), width, height)
		case "jump":
			target, _ := playJumpMode(deck.ResolvedSlides(), current, page, width, height)
			return fmt.Sprintf("jump-slide:%d", target)
		case "search":
			query := ""
			target, _ := playSearchMode(deck.ResolvedSlides(), current, page, width, height, &query)
			return fmt.Sprintf("jump-slide:%d", target)
		case "slide-list":
			if mode == "text" {
				insertTextAtCursor(slide, selected, cursor, "1")
				break
			}
			commitPendingSelectionMove()
			slideListOpen = true
			notesOpen = false
			state.ShowSlides = true
			state.ShowNotes = false
			slideNavIndex = current
			slideNavScroll = 0
			fullEditRedraw = true
		case "speaker-notes":
			if mode == "text" {
				insertTextAtCursor(slide, selected, cursor, "2")
				break
			}
			commitPendingSelectionMove()
			notesOpen = true
			notesEditing = false
			slideListOpen = false
			state.ShowNotes = true
			state.ShowSlides = false
			notesDraft = slide.Notes
			notesCursor = len([]rune(slide.Notes))
			fullEditRedraw = true
		case "timer":
			if mode == "text" {
				insertTextAtCursor(slide, selected, cursor, "0")
				break
			}
			state.TimerMode = "config"
			state.TimerInput = ""
			state.TimerDeadline = time.Time{}
			fullEditRedraw = true
		case "page-number":
			if options.TogglePageNumber != nil && mode != "text" {
				commitPendingSelectionMove()
				before := cloneSlide(*slide)
				label := options.TogglePageNumber(slide)
				clearSelection()
				commit(before)
				setUINotice("Page number: " + label)
				fullEditRedraw = true
				currentLines = nil
			}
		case "visual-properties":
			if options.EditVisualProperties != nil && mode != "text" {
				commitPendingSelectionMove()
				if options.EditVisualProperties(slide) {
					clearSelection()
					persist()
					modeOriginal = cloneSlide(*slide)
					setUINotice("Visual properties updated")
				}
				fullEditRedraw = true
				currentLines = nil
			}
		case "shift-mouse-click":
			commitPendingSelectionMove()
			lines := currentLines
			if len(lines) == 0 {
				lines = displayLines(*slide, width, height, page)
				currentLines = lines
			}
			elementIndex := elementAtPoint(lines, event.X, event.Y)
			if elementIndex < 0 || elementIndex >= len(slide.Elements) {
				break
			}
			selected = elementIndex
			state.LastSelected = selected
			toggleSelection(multiSelected, selected)
			mode = "select"
			status = selectionStatus(*slide, selected, multiSelected)
			if isEditableElement(slide.Elements[selected]) {
				ensureCursor(slide, cursor, selected)
			}
			textSelectionMoved = false
			imageSelectionMoved = false
		case "mouse-click":
			commitPendingSelectionMove()
			lines := currentLines
			if len(lines) == 0 {
				lines = displayLines(*slide, width, height, page)
				currentLines = lines
			}
			elementIndex := elementAtPoint(lines, event.X, event.Y)
			if elementIndex < 0 || elementIndex >= len(slide.Elements) {
				if initialEvent {
					return ""
				}
				break
			}
			if isPositionedElement(slide.Elements[elementIndex]) {
				selected = elementIndex
				state.LastSelected = selected
				for index := range multiSelected {
					delete(multiSelected, index)
				}
				mode = "select"
				status = selectionStatus(*slide, selected, multiSelected)
				textSelectionMoved = false
				imageSelectionMoved = false
				continue
			}
			if isEditableElement(slide.Elements[elementIndex]) {
				if slide.Elements[elementIndex].Kind == "code" && codeTextAtPoint(lines, elementIndex, event.X, event.Y) {
					selected = elementIndex
					state.LastSelected = selected
					for index := range multiSelected {
						delete(multiSelected, index)
					}
					ensureCursor(slide, cursor, selected)
					cursor[selected] = cursorForClick(slide.Elements[selected], event.X, event.Y, lines, selected, width)
					mode = "select"
					status = "code text selected"
					textSelectionMoved = false
					imageSelectionMoved = false
					continue
				}
				if mode == "select" && selected == elementIndex && status == "text selected" {
					normalizeFlowRelativePlacements(slide, authorWidth, authorHeight)
					modeOriginal = cloneSlide(*slide)
					materializePlaceholder(slide, selected, cursor)
					cursor[selected] = cursorForClick(slide.Elements[selected], event.X, event.Y, lines, selected, width)
					mode = "text"
					status = ""
				} else {
					selected = elementIndex
					state.LastSelected = selected
					for index := range multiSelected {
						delete(multiSelected, index)
					}
					ensureCursor(slide, cursor, selected)
					mode = "select"
					status = "text selected"
					textSelectionMoved = false
					imageSelectionMoved = false
				}
			}
		case "quit":
			commitPendingSelectionMove()
			state.Selected = selected
			state.Cursor = cursor
			return "quit"
		case "escape":
			if mode == "select" {
				commitPendingSelectionMove()
				clearSelection()
				state.Selected = -1
				state.Cursor = cursor
				return ""
			}
			*slide = modeOriginal
			mode = "select"
			axisScaleMode = ""
			status = selectionStatus(*slide, selected, multiSelected)
			fullEditRedraw = true
			editDirty = true
			currentLines = nil
			continue
		case "save":
			state.Selected = selected
			state.Cursor = cursor
			if persist() {
				setUINotice("Saved")
			}
			modeOriginal = cloneSlide(*slide)
			return ""
		case "undo":
			commitPendingSelectionMove()
			if undoSlideSnapshot(state, slide) {
				persist()
				modeOriginal = cloneSlide(*slide)
				clearSelection()
			}
		case "redo":
			commitPendingSelectionMove()
			if redoSlideSnapshot(state, slide) {
				persist()
				modeOriginal = cloneSlide(*slide)
				clearSelection()
			}
		case "tab", "shift-tab":
			commitPendingSelectionMove()
			lines := currentLines
			if len(lines) == 0 {
				lines = displayLines(*slide, width, height, page)
				currentLines = lines
			}
			if selected < 0 && state.LastSelected >= 0 && state.LastSelected < len(slide.Elements) && isSelectableElement(slide.Elements[state.LastSelected]) {
				selected = state.LastSelected
			} else {
				direction := 1
				if event.Action == "shift-tab" {
					direction = -1
				}
				selected = selectableElementByPosition(*slide, lines, selected, direction)
			}
			applySelectionState(slide, cursor, selected, &mode, &status)
			textSelectionMoved = false
			imageSelectionMoved = false
			if selected >= 0 {
				state.LastSelected = selected
			}
			for index := range multiSelected {
				delete(multiSelected, index)
			}
		case "copy":
			commitPendingSelectionMove()
			if selected >= 0 && selected < len(slide.Elements) {
				element := clipboardElementFromSelection(*slide, selected, width, height)
				clipboard = &element
				state.Clipboard = clipboard
				status = selectionStatus(*slide, selected, multiSelected)
				setUINotice("Copied " + elementKindLabel(element))
			}
		case "cut":
			if mode == "select" {
				commitPendingSelectionMove()
				if removeSelection(true) {
					setUINotice("Cut selection")
				}
			}
		case "paste":
			commitPendingSelectionMove()
			if clipboard != nil {
				before := cloneSlide(*slide)
				if initialEvent {
					page = entryPage
				}
				element := elementForPastePage(*clipboard, page, width, height)
				insertAt := insertElementAfter(slide, selected, element)
				shiftCursorKeys(cursor, insertAt, 1)
				selected = insertAt
				ensureCursor(slide, cursor, selected)
				mode = "select"
				applySelectionState(slide, cursor, selected, &mode, &status)
				state.LastSelected = selected
				for index := range multiSelected {
					delete(multiSelected, index)
				}
				commit(before)
				setUINotice("Pasted " + elementKindLabel(element))
				fullEditRedraw = true
				currentLines = nil
			} else {
				setUINotice("Nothing to paste")
			}
		case "up":
			if mode == "select" && selected >= 0 && selected < len(slide.Elements) {
				moveBatch(event.Action, 1)
			} else if mode == "resize" && selected >= 0 && selected < len(slide.Elements) && isAxisScalableElement(slide.Elements[selected]) {
				resizeElementByAxisMode(slide, selected, event.Action, authorWidth, authorHeight, axisScaleMode)
			} else if mode == "text" && selected >= 0 && selected < len(slide.Elements) && slide.Elements[selected].Kind == "code" {
				cursor[selected] = moveCodeCursorVertical(slide.Elements[selected], cursor[selected], -1, width)
			}
		case "down":
			if mode == "select" && selected >= 0 && selected < len(slide.Elements) {
				moveBatch(event.Action, 1)
			} else if mode == "resize" && selected >= 0 && selected < len(slide.Elements) && isAxisScalableElement(slide.Elements[selected]) {
				resizeElementByAxisMode(slide, selected, event.Action, authorWidth, authorHeight, axisScaleMode)
			} else if mode == "text" && selected >= 0 && selected < len(slide.Elements) && slide.Elements[selected].Kind == "code" {
				cursor[selected] = moveCodeCursorVertical(slide.Elements[selected], cursor[selected], 1, width)
			}
		case "left":
			if mode == "select" && selected >= 0 && selected < len(slide.Elements) {
				moveBatch(event.Action, 1)
			} else if mode == "resize" && selected >= 0 && selected < len(slide.Elements) && isAxisScalableElement(slide.Elements[selected]) {
				resizeElementByAxisMode(slide, selected, event.Action, authorWidth, authorHeight, axisScaleMode)
			} else if mode == "text" && selected >= 0 && selected < len(slide.Elements) && cursor[selected] > 0 {
				cursor[selected]--
			}
		case "right":
			if mode == "select" && selected >= 0 && selected < len(slide.Elements) {
				moveBatch(event.Action, 1)
			} else if mode == "resize" && selected >= 0 && selected < len(slide.Elements) && isAxisScalableElement(slide.Elements[selected]) {
				resizeElementByAxisMode(slide, selected, event.Action, authorWidth, authorHeight, axisScaleMode)
			} else if mode == "text" && selected >= 0 && selected < len(slide.Elements) {
				cursor[selected] = min(cursor[selected]+1, len([]rune(slide.Elements[selected].Text)))
			}
		case "shift-up", "shift-down", "shift-left", "shift-right":
			if mode == "select" && selected >= 0 && selected < len(slide.Elements) {
				direction := strings.TrimPrefix(event.Action, "shift-")
				moveBatch(direction, 10)
			}
		case "shape-toggle":
			if mode == "text" {
				insertTextAtCursor(slide, selected, cursor, "g")
				break
			}
			if mode == "select" && selected >= 0 && selected < len(slide.Elements) && isAxisScalableElement(slide.Elements[selected]) {
				if picked, ok := playAxisScalePicker(*slide, page, width, height, axisScaleLastMode); ok {
					commitPendingSelectionMove()
					if picked == axisScaleClose {
						axisScaleLastMode = ""
					} else {
						beginAxisScale(picked)
					}
				}
				status = selectionStatus(*slide, selected, multiSelected)
				fullEditRedraw = true
			}
		case "move":
			if mode == "text" {
				insertTextAtCursor(slide, selected, cursor, "m")
				break
			}
		case "toggle-selection":
			if mode == "text" {
				insertTextAtCursor(slide, selected, cursor, " ")
				break
			}
			if mode == "select" && selected >= 0 && selected < len(slide.Elements) {
				commitPendingSelectionMove()
				toggleSelection(multiSelected, selected)
				status = selectionStatus(*slide, selected, multiSelected)
			}
		case "align-left", "align-center", "align-right":
			if mode == "select" && selected >= 0 && selected < len(slide.Elements) && (status == "text selected" || status == "image selected" || status == "shape selected") {
				commitPendingSelectionMove()
				before := cloneSlide(*slide)
				for _, index := range activeIndices() {
					if index >= 0 && index < len(slide.Elements) && isEditableElement(slide.Elements[index]) {
						normalizeTextPlacement(slide, index, authorWidth, authorHeight)
					}
					alignElement(&slide.Elements[index], strings.TrimPrefix(event.Action, "align-"))
				}
				commit(before)
			} else if mode == "select" && selectionSetCount(multiSelected) > 1 {
				commitPendingSelectionMove()
				before := cloneSlide(*slide)
				for _, index := range activeIndices() {
					if index >= 0 && index < len(slide.Elements) && isEditableElement(slide.Elements[index]) {
						normalizeTextPlacement(slide, index, authorWidth, authorHeight)
					}
					alignElement(&slide.Elements[index], strings.TrimPrefix(event.Action, "align-"))
				}
				commit(before)
			}
		case "layer-back", "layer-front":
			if mode == "text" {
				if event.Action == "layer-back" {
					insertTextAtCursor(slide, selected, cursor, "[")
				} else {
					insertTextAtCursor(slide, selected, cursor, "]")
				}
				break
			}
			if mode == "select" && selected >= 0 && selected < len(slide.Elements) && (status == "image selected" || status == "shape selected") {
				commitPendingSelectionMove()
				before := cloneSlide(*slide)
				layer := "front"
				if event.Action == "layer-back" {
					layer = "back"
				}
				setElementLayer(&slide.Elements[selected], layer)
				commit(before)
				fullEditRedraw = true
			}
		case "color":
			if mode == "select" && selected >= 0 && selected < len(slide.Elements) && (status == "text selected" || status == "code text selected" || status == "shape selected" || status == "page number selected") {
				commitPendingSelectionMove()
				before := cloneSlide(*slide)
				if colour, ok := playTextColorPicker(*slide, selected, width, height, page); ok {
					if status == "code text selected" {
						setElementTextColour(&slide.Elements[selected], colour)
					} else {
						setElementColour(&slide.Elements[selected], colour)
					}
					selected = -1
					status = ""
					commit(before)
				}
				fullEditRedraw = true
			} else if mode == "text" {
				insertTextAtCursor(slide, selected, cursor, "c")
			}
		case "outline":
			if mode == "text" {
				insertTextAtCursor(slide, selected, cursor, "o")
				break
			}
			if mode == "select" && selected >= 0 && selected < len(slide.Elements) && (strings.Contains(status, " selected") || selectionSetCount(multiSelected) > 0) {
				commitPendingSelectionMove()
				before := cloneSlide(*slide)
				for _, index := range activeIndices() {
					if index >= 0 && index < len(slide.Elements) && isSelectableElement(slide.Elements[index]) {
						toggleElementOutline(&slide.Elements[index])
					}
				}
				commit(before)
				status = selectionStatus(*slide, selected, multiSelected)
				fullEditRedraw = true
			}
		case "placeholder-role":
			if options.Master && mode == "select" && selected >= 0 && selected < len(slide.Elements) {
				commitPendingSelectionMove()
				before := cloneSlide(*slide)
				if role, ok := playPlaceholderRolePicker(*slide, page, width, height, slide.Elements[selected].PlaceholderRole); ok {
					applyPlaceholderRole(&slide.Elements[selected], role)
					commit(before)
					status = selectionStatus(*slide, selected, multiSelected)
				}
				fullEditRedraw = true
			}
		case "toggle-bold", "toggle-highlight":
			if mode == "select" && selected >= 0 && selected < len(slide.Elements) && status == "text selected" {
				commitPendingSelectionMove()
				before := cloneSlide(*slide)
				ensureCursor(slide, cursor, selected)
				marker := "**"
				label := "Bold"
				if event.Action == "toggle-highlight" {
					marker = "*"
					label = "Highlight"
				}
				removing := hasMarkdownStyleWrapper(slide.Elements[selected].Text, marker)
				slide.Elements[selected].Text = toggleMarkdownStyle(slide.Elements[selected].Text, marker)
				markerWidth := len([]rune(marker))
				if removing {
					cursor[selected] = max(0, cursor[selected]-markerWidth)
				} else {
					cursor[selected] += markerWidth
				}
				ensureCursor(slide, cursor, selected)
				commit(before)
				setUINotice(label + " toggled")
				status = selectionStatus(*slide, selected, multiSelected)
				fullEditRedraw = true
				currentLines = nil
			}
		case "rotate", "rotate-ccw":
			if mode == "text" {
				if event.Action == "rotate-ccw" {
					insertTextAtCursor(slide, selected, cursor, "R")
				} else {
					insertTextAtCursor(slide, selected, cursor, "r")
				}
				break
			}
			if mode == "select" && selected >= 0 && selected < len(slide.Elements) && status == "text selected" {
				commitPendingSelectionMove()
				before := cloneSlide(*slide)
				for _, index := range activeIndices() {
					if index >= 0 && index < len(slide.Elements) && isRotatableTextElement(slide.Elements[index]) {
						if event.Action == "rotate-ccw" {
							rotateTextOrientationCounterClockwise(&slide.Elements[index])
						} else {
							rotateTextOrientation(&slide.Elements[index])
						}
						ensureCursor(slide, cursor, index)
					}
				}
				commit(before)
				status = selectionStatus(*slide, selected, multiSelected)
				fullEditRedraw = true
			}
		case "transparency":
			if mode == "text" {
				insertTextAtCursor(slide, selected, cursor, "/")
				break
			}
			if mode == "select" && selected >= 0 && selected < len(slide.Elements) && status == "shape selected" {
				commitPendingSelectionMove()
				before := cloneSlide(*slide)
				toggleShapeTransparency(&slide.Elements[selected])
				commit(before)
				status = selectionStatus(*slide, selected, multiSelected)
				fullEditRedraw = true
			}
		case "style", "settings":
			if mode == "select" && selected >= 0 && selected < len(slide.Elements) && status == "image selected" {
				commitPendingSelectionMove()
				before := cloneSlide(*slide)
				if query, ok := playImageSettingsDialog(*slide, selected, width, height, page); ok {
					slide.Elements[selected].Query = query
					commit(before)
				}
				fullEditRedraw = true
			} else if mode == "select" && selected >= 0 && selected < len(slide.Elements) && (status == "text selected" || status == "page number selected") {
				commitPendingSelectionMove()
				before := cloneSlide(*slide)
				normalizeFlowRelativePlacements(slide, authorWidth, authorHeight)
				preview := cloneSlide(*slide)
				ensureTextImageRender(&preview.Elements[selected])
				if query, ok := playImageSettingsDialog(preview, selected, width, height, page); ok {
					ensureTextImageRender(&slide.Elements[selected])
					slide.Elements[selected].Query = query
					ensureCursor(slide, cursor, selected)
					commit(before)
				}
				fullEditRedraw = true
			} else if mode == "select" && selected >= 0 && selected < len(slide.Elements) && status == "shape selected" {
				commitPendingSelectionMove()
				before := cloneSlide(*slide)
				if shape, ok := playShapePicker(*slide, width, height, page, shapeName(slide.Elements[selected])); ok {
					slide.Elements[selected].Query = setQueryValue(slide.Elements[selected].Query, "shape", shape)
					commit(before)
				}
				fullEditRedraw = true
			} else if mode == "text" {
				insertTextAtCursor(slide, selected, cursor, "s")
			} else if mode == "select" {
				commitPendingSelectionMove()
				if shape, ok := playShapePicker(*slide, width, height, page, ""); ok {
					before := cloneSlide(*slide)
					top, left := insertedTextPlacementAnchor(before, selected, width, height, page)
					insertAt := insertElementAfter(slide, selected, newShapeElement(shape))
					initializeInsertedShapePlacement(slide, insertAt, top, left, width)
					selected = insertAt
					setSingleSelection(multiSelected, selected)
					state.LastSelected = selected
					mode = "select"
					status = "shape selected"
					commit(before)
				}
				fullEditRedraw = true
			}
		case "link":
			if mode == "select" && selected >= 0 && selected < len(slide.Elements) && (status == "text selected" || status == "image selected" || status == "shape selected" || selectionSetCount(multiSelected) > 0) {
				commitPendingSelectionMove()
				before := cloneSlide(*slide)
				currentLink := elementLink(slide.Elements[selected].Query)
				if link, ok := playLinkInput(*slide, width, height, page, currentLink, len(*slides)); ok {
					applied := false
					for _, index := range activeIndices() {
						if index >= 0 && index < len(slide.Elements) && isSelectableElement(slide.Elements[index]) {
							if setElementLink(&slide.Elements[index], link, len(*slides)) {
								applied = true
							}
						}
					}
					if applied {
						commit(before)
						status = selectionStatus(*slide, selected, multiSelected)
					}
				}
				fullEditRedraw = true
			} else if mode == "text" {
				insertTextAtCursor(slide, selected, cursor, "l")
			}
		case "promote":
			if mode == "resize" && selected >= 0 && selected < len(slide.Elements) && isAxisScalableElement(slide.Elements[selected]) {
				break
			}
			if mode == "select" && selected >= 0 && selected < len(slide.Elements) && (status == "text selected" || status == "shape selected" || status == "page number selected" || selectionSetCount(multiSelected) > 1) {
				commitPendingSelectionMove()
				before := cloneSlide(*slide)
				for _, index := range activeIndices() {
					if isEditableElement(slide.Elements[index]) {
						normalizeFlowRelativePlacements(slide, authorWidth, authorHeight)
						changeTextLevel(&slide.Elements[index], 1)
						ensureCursor(slide, cursor, index)
					} else if slide.Elements[index].Kind == "image" {
						withFastImageRender(func() {
							normalizeImagePlacement(slide, index, authorWidth, authorHeight)
						})
						scaleImageElement(&slide.Elements[index], 0.1)
					} else if slide.Elements[index].Kind == "shape" {
						resizeShapeElement(&slide.Elements[index], 1)
					} else if slide.Elements[index].Kind == "page-number" {
						changeTextLevel(&slide.Elements[index], 1)
					}
				}
				commit(before)
			} else if mode == "select" && selected >= 0 && selected < len(slide.Elements) && status == "image selected" {
				beginSelectionMove()
				withFastImageRender(func() {
					normalizeImagePlacement(slide, selected, authorWidth, authorHeight)
				})
				scaleImageElement(&slide.Elements[selected], 0.1)
				imageSelectionMoved = true
				persist()
			}
		case "demote":
			if mode == "resize" && selected >= 0 && selected < len(slide.Elements) && isAxisScalableElement(slide.Elements[selected]) {
				break
			}
			if mode == "select" && selected >= 0 && selected < len(slide.Elements) && (status == "text selected" || status == "shape selected" || status == "page number selected" || selectionSetCount(multiSelected) > 1) {
				commitPendingSelectionMove()
				before := cloneSlide(*slide)
				for _, index := range activeIndices() {
					if isEditableElement(slide.Elements[index]) {
						normalizeFlowRelativePlacements(slide, authorWidth, authorHeight)
						changeTextLevel(&slide.Elements[index], -1)
						ensureCursor(slide, cursor, index)
					} else if slide.Elements[index].Kind == "image" {
						withFastImageRender(func() {
							normalizeImagePlacement(slide, index, authorWidth, authorHeight)
						})
						scaleImageElement(&slide.Elements[index], -0.1)
					} else if slide.Elements[index].Kind == "shape" {
						resizeShapeElement(&slide.Elements[index], -1)
					} else if slide.Elements[index].Kind == "page-number" {
						changeTextLevel(&slide.Elements[index], -1)
					}
				}
				commit(before)
			} else if mode == "select" && selected >= 0 && selected < len(slide.Elements) && status == "image selected" {
				beginSelectionMove()
				withFastImageRender(func() {
					normalizeImagePlacement(slide, selected, authorWidth, authorHeight)
				})
				scaleImageElement(&slide.Elements[selected], -0.1)
				imageSelectionMoved = true
				persist()
			}
		case "backspace":
			if mode == "select" {
				commitPendingSelectionMove()
				if removeSelection(false) {
					setUINotice("Deleted selection")
				}
			} else if mode == "text" && selected >= 0 && selected < len(slide.Elements) {
				selected = editBackspace(slide, selected, cursor)
			}
		case "insert-newline":
			if mode == "text" && selected >= 0 && selected < len(slide.Elements) && slide.Elements[selected].Kind == "code" {
				insertTextAtCursor(slide, selected, cursor, "\n")
			}
		case "enter":
			if mode == "move" || mode == "resize" {
				commit(modeOriginal)
				mode = "select"
				axisScaleMode = ""
				status = selectionStatus(*slide, selected, multiSelected)
				if selected >= 0 && selected < len(slide.Elements) && isAxisScalableElement(slide.Elements[selected]) {
					if picked, ok := playAxisScalePicker(*slide, page, width, height, axisScaleLastMode); ok {
						if picked == axisScaleClose {
							axisScaleLastMode = ""
						} else {
							beginAxisScale(picked)
						}
					}
					fullEditRedraw = true
				}
				break
			}
			if mode == "select" {
				if selected >= 0 && selected < len(slide.Elements) && (status == "text selected" || status == "code text selected") {
					if textSelectionMoved {
						commitPendingSelectionMove()
						clearSelection()
						state.Cursor = cursor
						state.Clipboard = clipboard
						return ""
					}
					normalizeFlowRelativePlacements(slide, authorWidth, authorHeight)
					ensureCursor(slide, cursor, selected)
					modeOriginal = cloneSlide(*slide)
					materializePlaceholder(slide, selected, cursor)
					mode = "text"
					status = ""
					break
				}
				if selected >= 0 && selected < len(slide.Elements) && (status == "image selected" || status == "shape selected") {
					if status == "image selected" && slide.Elements[selected].Placeholder && slide.Elements[selected].MasterSlotID != "" {
						before := cloneSlide(*slide)
						if fillImagePlaceholder(selected) {
							commit(before)
							status = "image selected"
						}
						break
					}
					if imageSelectionMoved {
						commitPendingSelectionMove()
					}
					clearSelection()
					state.Cursor = cursor
					state.Clipboard = clipboard
					return ""
				}
				if initialEvent {
					return ""
				}
			} else if mode == "text" && selected >= 0 && selected < len(slide.Elements) {
				selected = finalizeTextEdit(slide, selected, cursor)
				commit(modeOriginal)
				mode = "select"
				clearSelection()
				state.Cursor = cursor
				state.Clipboard = clipboard
				return ""
			}
		case "edit-selected":
			if mode == "text" && selected >= 0 && selected < len(slide.Elements) {
				insertTextAtCursor(slide, selected, cursor, "e")
				break
			}
			if mode == "select" && selected >= 0 && selected < len(slide.Elements) && status == "text selected" {
				normalizeFlowRelativePlacements(slide, authorWidth, authorHeight)
				ensureCursor(slide, cursor, selected)
				modeOriginal = cloneSlide(*slide)
				materializePlaceholder(slide, selected, cursor)
				mode = "text"
				status = ""
			}
		case "insert-image":
			if mode != "select" {
				if mode == "text" && selected >= 0 && selected < len(slide.Elements) {
					insertTextAtCursor(slide, selected, cursor, "i")
				}
				break
			}
			if selected >= 0 && selected < len(slide.Elements) && slide.Elements[selected].Kind == "image" && slide.Elements[selected].Placeholder && slide.Elements[selected].MasterSlotID != "" {
				before := cloneSlide(*slide)
				if fillImagePlaceholder(selected) {
					commit(before)
					status = "image selected"
				}
				break
			}
			imageOriginal := cloneSlide(*slide)
			path, ok := chooseImageFile()
			if !ok {
				setUINotice("Image insert cancelled")
				break
			}
			path, ok = copyImageToDeckDir(path, deckPath)
			if !ok {
				setUIError("Image copy failed")
				break
			}
			imageIndex := insertImageElement(slide, selected, path)
			initializeInsertedImagePlacement(slide, imageIndex)
			result := playImagePlacementMode(slide, current, len(*slides), imageIndex, width, height, page, imageOriginal, &clipboard, persist)
			state.Clipboard = clipboard
			if result.Action == "quit" {
				return "quit"
			}
			if result.Action == "save" {
				state.Selected = selected
				state.Cursor = cursor
				commitSlideSnapshot(state, imageOriginal, *slide)
				if persist() {
					setUINotice("Image inserted")
				}
				return ""
			}
			selected = firstEditableElement(*slide)
			state.Selected = selected
			state.Cursor = cursor
			state.Clipboard = clipboard
			return ""
		case "insert-text":
			if mode == "text" && selected >= 0 && selected < len(slide.Elements) {
				insertTextAtCursor(slide, selected, cursor, "t")
				break
			}
			commitPendingSelectionMove()
			before := cloneSlide(*slide)
			top, left := insertedTextPlacementAnchor(before, selected, width, height, page)
			insertAt := insertElementAfter(slide, selected, Element{Kind: "text", Text: "Your text here", Placeholder: true})
			initializeInsertedTextPlacement(slide, insertAt, top, left, width)
			selected = insertAt
			setSingleSelection(multiSelected, selected)
			state.LastSelected = selected
			ensureCursor(slide, cursor, selected)
			mode = "select"
			status = "text selected"
			commit(before)
		case "shape-picker":
			if mode == "text" && selected >= 0 && selected < len(slide.Elements) {
				insertTextAtCursor(slide, selected, cursor, "s")
				break
			}
			commitPendingSelectionMove()
			if shape, ok := playShapePicker(*slide, width, height, page, ""); ok {
				before := cloneSlide(*slide)
				top, left := insertedTextPlacementAnchor(before, selected, width, height, page)
				insertAt := insertElementAfter(slide, selected, newShapeElement(shape))
				initializeInsertedShapePlacement(slide, insertAt, top, left, width)
				selected = insertAt
				setSingleSelection(multiSelected, selected)
				state.LastSelected = selected
				mode = "select"
				status = "shape selected"
				commit(before)
			}
			fullEditRedraw = true
		case "text":
			if mode == "select" {
				break
			}
			if selected < 0 || selected >= len(slide.Elements) {
				slide.Elements = append(slide.Elements, Element{Kind: "text"})
				selected = len(slide.Elements) - 1
			}
			ensureCursor(slide, cursor, selected)
			insertTextAtCursor(slide, selected, cursor, event.Text)
			status = ""
		}
		state.Selected = selected
		state.Cursor = cursor
		state.Clipboard = clipboard
	}
}

func drawEditCursor(lines []Line, selected, cursor, width, height int, slide Slide) {
	startRow := -1
	startCol := 0
	for _, line := range lines {
		if line.Element != selected {
			continue
		}
		if startRow == -1 || line.Row < startRow {
			startRow = line.Row
			startCol = line.Col
		} else if line.Row == startRow {
			startCol = min(startCol, line.Col)
		}
	}
	if startRow == -1 {
		return
	}
	if selected >= 0 && selected < len(slide.Elements) && slide.Elements[selected].Kind == "code" && !rendersAsTextImage(slide.Elements[selected]) {
		visualLine, col := codeCursorVisualLineCol(slide.Elements[selected].Text, codeCharsPerVisualLine(width-startCol), cursor)
		glyphWidth := editGlyphWidth(slide.Elements[selected])
		row := startRow + codeBlockPadY + visualLine*4 + 3
		if row < 0 || row >= height {
			return
		}
		col = min(max(0, startCol+codeBlockPadX+col*glyphWidth), max(0, width-1))
		cursorWidth := min(glyphWidth, max(1, width-col))
		termPrint("\033[0;33m")
		termPrintf("\033[%d;%dH%s", row+1, col+1, strings.Repeat("▀", cursorWidth))
		return
	}
	prefixRows := editableRowsForElementPrefix(slide.Elements[selected], width, cursor)
	if len(prefixRows) == 0 {
		prefixRows = []string{""}
	}
	glyphWidth := editGlyphWidth(slide.Elements[selected])
	row := startRow + max(1, len(prefixRows))
	if rendersAsTextImage(slide.Elements[selected]) {
		fullRows := editableRowsForElementPrefix(slide.Elements[selected], width, len([]rune(slide.Elements[selected].Text)))
		row = startRow + max(1, len(fullRows))
	}
	if row < 0 || row >= height {
		return
	}
	col := 0
	if cursor > 0 {
		col = editCursorColumn(slide.Elements[selected], prefixRows, cursor)
	}
	col += startCol
	col = min(max(0, col), max(0, width-1))
	cursorWidth := min(glyphWidth, max(1, width-col))
	termPrint("\033[0;33m")
	termPrintf("\033[%d;%dH%s", row+1, col+1, strings.Repeat("▀", cursorWidth))
}

func drawSelectedElementHighlight(lines []Line, selected, width, height int) {
	drawSelectionUnderline(lines, selected, width, height, false)
}

func addSelectedElementRows(rows map[int]bool, lines []Line, selected, height int) {
	if selected < 0 {
		return
	}
	maxRow := -1
	for _, line := range lines {
		if line.Element != selected || line.Row < 0 || line.Row >= height {
			continue
		}
		rows[line.Row] = true
		maxRow = max(maxRow, line.Row)
	}
	if maxRow >= 0 && maxRow+1 < height {
		rows[maxRow+1] = true
	}
}

func addSelectionUnderlineRow(rows map[int]bool, lines []Line, selected, width, height int, imageOnly bool) {
	row := selectionUnderlineRow(lines, selected, width, height, imageOnly)
	if row >= 0 {
		rows[row] = true
	}
}

func addSelectionSetRows(rows map[int]bool, lines []Line, selection map[int]bool, width, height int) {
	for selected := range selection {
		addSelectedElementRows(rows, lines, selected, height)
		addSelectionUnderlineRow(rows, lines, selected, width, height, false)
		addSelectionUnderlineRow(rows, lines, selected, width, height, true)
	}
}

func selectionUnderlineRow(lines []Line, selected, width, height int, imageOnly bool) int {
	minCol := width
	maxCol := -1
	maxRow := -1
	for _, line := range lines {
		if line.Element != selected || line.Row < 0 || line.Row >= height {
			continue
		}
		if imageOnly && line.Role != "image" {
			continue
		}
		if !imageOnly && line.Role == "image" {
			continue
		}
		left, right, ok := visibleTextBounds(line.Text)
		if !ok {
			continue
		}
		minCol = min(minCol, line.Col+left)
		maxCol = max(maxCol, line.Col+right)
		maxRow = max(maxRow, line.Row)
	}
	if maxCol < minCol || maxRow < 0 {
		return -1
	}
	row := maxRow + 1
	if row >= height {
		row = maxRow
	}
	return row
}

func drawSelectionUnderline(lines []Line, selected, width, height int, imageOnly bool) {
	drawSelectionUnderlineColor(lines, selected, width, height, imageOnly, "33")
}

func drawSelectionUnderlineColor(lines []Line, selected, width, height int, imageOnly bool, color string) {
	minCol := width
	maxCol := -1
	maxRow := -1
	for _, line := range lines {
		if line.Element != selected || line.Row < 0 || line.Row >= height {
			continue
		}
		if imageOnly && line.Role != "image" {
			continue
		}
		if !imageOnly && line.Role == "image" {
			continue
		}
		left, right, ok := visibleTextBounds(line.Text)
		if !ok {
			continue
		}
		minCol = min(minCol, line.Col+left)
		maxCol = max(maxCol, line.Col+right)
		maxRow = max(maxRow, line.Row)
	}
	if maxCol < minCol || maxRow < 0 {
		return
	}
	row := maxRow + 1
	if row >= height {
		row = maxRow
	}
	start := max(0, min(width-1, minCol))
	end := max(0, min(width-1, maxCol))
	if end < start {
		return
	}
	termPrintf("\033[0;%sm", color)
	termPrintf("\033[%d;%dH%s", row+1, start+1, strings.Repeat("▀", end-start+1))
}

func drawLinkUnderlines(lines []Line, width, height int, slide Slide) {
	type group struct {
		lines  []Line
		height int
	}
	groups := map[int]*group{}
	for _, line := range lines {
		if line.Role == "image" || line.Role == "outline" || line.Element < 0 || line.Element >= len(slide.Elements) || line.Row < 0 || line.Row >= height {
			continue
		}
		if elementLink(line.Query) == "" {
			continue
		}
		g := groups[line.Element]
		if g == nil {
			g = &group{height: linkGlyphHeight(slide.Elements[line.Element])}
			groups[line.Element] = g
		}
		g.lines = append(g.lines, line)
	}
	for elementIndex, group := range groups {
		if len(group.lines) == 0 {
			continue
		}
		sort.SliceStable(group.lines, func(i, j int) bool {
			if group.lines[i].Row != group.lines[j].Row {
				return group.lines[i].Row < group.lines[j].Row
			}
			return group.lines[i].Col < group.lines[j].Col
		})
		element := slide.Elements[elementIndex]
		fg := slideFG(slide)
		if group.lines[0].Role == "heading" {
			fg = slideHeaderFG(slide)
		}
		if elementColour := elementFG(element.Query, group.lines[0].Role == "heading"); elementColour != "" {
			fg = elementColour
		}
		for startIndex := 0; startIndex < len(group.lines); startIndex += max(1, group.height) {
			endIndex := min(len(group.lines), startIndex+max(1, group.height))
			minCol := width
			maxCol := -1
			maxRow := -1
			for _, line := range group.lines[startIndex:endIndex] {
				left, right, ok := visibleTextBounds(line.Text)
				if !ok {
					continue
				}
				minCol = min(minCol, line.Col+left)
				maxCol = max(maxCol, line.Col+right)
				maxRow = max(maxRow, line.Row)
			}
			if maxCol < minCol || maxRow < 0 {
				continue
			}
			row := maxRow + 1
			if row >= height {
				row = maxRow
			}
			start := max(0, min(width-1, minCol))
			end := max(0, min(width-1, maxCol))
			if end < start {
				continue
			}
			termPrintf("\033[0;%s;%sm\033[%d;%dH%s", fg, slideBG(slide), row+1, start+1, strings.Repeat("▔", end-start+1))
		}
	}
}

func linkGlyphHeight(element Element) int {
	if rendersAsTextImage(element) {
		rows := renderElementRows(element, 10000)
		return max(1, len(rows))
	}
	if element.Kind == "heading" {
		scale := 1
		if element.Level == 1 {
			scale = 2
		}
		return 8 * scale
	}
	return 4
}

func visibleTextBounds(text string) (int, int, bool) {
	plain := []rune(stripANSI(text))
	left := -1
	right := -1
	for i, r := range plain {
		if r == ' ' {
			continue
		}
		if left == -1 {
			left = i
		}
		right = i
	}
	if left == -1 {
		return 0, 0, false
	}
	return left, right, true
}

func elementAtPoint(lines []Line, x, y int) int {
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if line.Element < 0 || line.Row != y {
			continue
		}
		lineWidth := maxLineDisplayWidth([]string{line.Text})
		if x >= line.Col && x < line.Col+max(1, lineWidth) {
			return line.Element
		}
	}
	return -1
}

func codeTextAtPoint(lines []Line, elementIndex, x, y int) bool {
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if line.Element != elementIndex || line.Role != "code" || line.Row != y {
			continue
		}
		left, right, ok := visibleTextBounds(line.Text)
		if !ok {
			continue
		}
		return x >= line.Col+left && x <= line.Col+right
	}
	return false
}

func linkAtPoint(slide Slide, lines []Line, x, y, slideCount int) (linkTarget, bool) {
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if line.Element < 0 || line.Element >= len(slide.Elements) || line.Row != y {
			continue
		}
		target, ok := linkTargetFromQuery(slide.Elements[line.Element].Query, slideCount)
		if !ok {
			continue
		}
		lineWidth := maxLineDisplayWidth([]string{line.Text})
		if x < line.Col || x >= line.Col+max(1, lineWidth) {
			continue
		}
		if line.Role == "image" {
			return target, true
		}
		if line.Role == "outline" {
			continue
		}
		left, right, ok := visibleTextBounds(line.Text)
		if !ok || x < line.Col+left || x > line.Col+right {
			continue
		}
		return target, true
	}
	return linkTarget{}, false
}

type positionedElement struct {
	index int
	row   int
	col   int
}

func selectableElementByPosition(slide Slide, lines []Line, current, direction int) int {
	positions := map[int]positionedElement{}
	for _, line := range lines {
		if line.Element < 0 || line.Element >= len(slide.Elements) || !isSelectableElement(slide.Elements[line.Element]) {
			continue
		}
		if line.Row < 0 {
			continue
		}
		position, ok := positions[line.Element]
		if !ok || line.Row < position.row || line.Row == position.row && line.Col < position.col {
			positions[line.Element] = positionedElement{index: line.Element, row: line.Row, col: line.Col}
		}
	}
	if len(positions) == 0 {
		return -1
	}
	ordered := make([]positionedElement, 0, len(positions))
	for _, position := range positions {
		ordered = append(ordered, position)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].row != ordered[j].row {
			return ordered[i].row < ordered[j].row
		}
		if ordered[i].col != ordered[j].col {
			return ordered[i].col < ordered[j].col
		}
		return ordered[i].index < ordered[j].index
	})
	for i, position := range ordered {
		if position.index == current {
			if direction < 0 {
				return ordered[(i-1+len(ordered))%len(ordered)].index
			}
			return ordered[(i+1)%len(ordered)].index
		}
	}
	if direction < 0 {
		return ordered[len(ordered)-1].index
	}
	return ordered[0].index
}

func applySelectionState(slide *Slide, cursor map[int]int, selected int, mode, status *string) {
	if selected < 0 || selected >= len(slide.Elements) {
		return
	}
	*mode = "select"
	if slide.Elements[selected].Kind == "image" {
		*status = "image selected"
		return
	}
	if slide.Elements[selected].Kind == "shape" {
		*status = "shape selected"
		return
	}
	if slide.Elements[selected].Kind == "page-number" {
		*status = "page number selected"
		return
	}
	*status = "text selected"
	ensureCursor(slide, cursor, selected)
}

func pruneSelectionSet(selection map[int]bool, slide Slide) {
	for index := range selection {
		if index < 0 || index >= len(slide.Elements) || !isSelectableElement(slide.Elements[index]) {
			delete(selection, index)
		}
	}
}

func selectionSetCount(selection map[int]bool) int {
	count := 0
	for _, ok := range selection {
		if ok {
			count++
		}
	}
	return count
}

func cloneBoolMap(values map[int]bool) map[int]bool {
	out := map[int]bool{}
	for key, value := range values {
		if value {
			out[key] = true
		}
	}
	return out
}

func activeSelectionIndices(selection map[int]bool, selected int, slide Slide) []int {
	pruneSelectionSet(selection, slide)
	var indices []int
	if selectionSetCount(selection) > 0 {
		for index := range selection {
			if index >= 0 && index < len(slide.Elements) && isSelectableElement(slide.Elements[index]) {
				indices = append(indices, index)
			}
		}
		sort.Ints(indices)
		return indices
	}
	if selected >= 0 && selected < len(slide.Elements) && isSelectableElement(slide.Elements[selected]) {
		return []int{selected}
	}
	return nil
}

func setSingleSelection(selection map[int]bool, selected int) {
	for index := range selection {
		delete(selection, index)
	}
	if selected >= 0 {
		selection[selected] = true
	}
}

func toggleSelection(selection map[int]bool, selected int) {
	if selected < 0 {
		return
	}
	if selection[selected] {
		delete(selection, selected)
	} else {
		selection[selected] = true
	}
}

func selectionStatus(slide Slide, selected int, selection map[int]bool) string {
	count := selectionSetCount(selection)
	if count > 1 {
		return fmt.Sprintf("%d selected", count)
	}
	if count == 1 {
		for index := range selection {
			selected = index
			break
		}
	}
	if selected < 0 || selected >= len(slide.Elements) {
		return ""
	}
	if slide.Elements[selected].Kind == "image" {
		return "image selected"
	}
	if slide.Elements[selected].Kind == "shape" {
		return "shape selected"
	}
	if slide.Elements[selected].Kind == "page-number" {
		return "page number selected"
	}
	return "text selected"
}

func selectionContainsText(slide Slide, indices []int) bool {
	for _, index := range indices {
		if index >= 0 && index < len(slide.Elements) && isEditableElement(slide.Elements[index]) {
			return true
		}
	}
	return false
}

func selectionContainsImage(slide Slide, indices []int) bool {
	for _, index := range indices {
		if index >= 0 && index < len(slide.Elements) && slide.Elements[index].Kind == "image" {
			return true
		}
	}
	return false
}

func selectionContainsPositioned(slide Slide, indices []int) bool {
	for _, index := range indices {
		if index >= 0 && index < len(slide.Elements) && isPositionedElement(slide.Elements[index]) {
			return true
		}
	}
	return false
}

func cursorForClick(element Element, x, y int, lines []Line, elementIndex, width int) int {
	startRow := -1
	startCol := 0
	for _, line := range lines {
		if line.Element != elementIndex {
			continue
		}
		if startRow == -1 || line.Row < startRow {
			startRow = line.Row
			startCol = line.Col
		}
		if line.Row == y {
			startCol = line.Col
		}
	}
	if startRow < 0 {
		return len([]rune(element.Text))
	}
	x = max(0, x-startCol)
	switch element.Kind {
	case "heading":
		return max(0, min(len([]rune(element.Text)), x/max(1, editGlyphWidth(element))))
	case "code":
		lineOffset := max(0, y-startRow-codeBlockPadY) / 4
		col := max(0, x-codeBlockPadX) / max(1, editGlyphWidth(element))
		return codeCursorIndexForVisualLineCol(element.Text, codeCharsPerVisualLine(width-startCol), lineOffset, col)
	default:
		lineOffset := max(0, y-startRow)
		bodyCol := max(0, x/4)
		charsPerLine := max(1, width/4)
		return max(0, min(len([]rune(element.Text)), lineOffset*charsPerLine+bodyCol))
	}
}

func drawEditToolbar(width, height, slideIndex, slideCount, page, pageCount int, mode, status string, slide Slide, selected int, axisScaleMode string, options editModeOptions) {
	if height <= 0 || width <= 0 {
		return
	}
	slideLabel := slideNumberLabel(ViewState{
		SlideIndex: slideIndex,
		SlideCount: slideCount,
		Page:       page,
		PageCount:  pageCount,
	})
	contextLabel := editContextLabel(mode, status, slide, selected, axisScaleMode)
	if options.Master {
		contextLabel = "MASTER · " + options.MasterName
		if mode == "text" {
			contextLabel += " / Editing"
		}
	}
	right := []toolbarSegment{
		{Long: contextLabel, Short: shortEditContextLabel(contextLabel), Required: true, Priority: 0},
	}
	if presenter, short := presenterStatusLabels(); presenter != "" {
		right = append(right, toolbarSegment{Long: presenter, Short: short, Required: true, Priority: 1})
	}
	if notice := currentUINotice(); notice != "" {
		right = append(right, toolbarSegment{Long: notice, Short: "status", Priority: 4})
	}
	right = append(right, toolbarSegment{Long: slideLabel, Short: slideLabel, Required: true, Priority: 0})
	if mode == "resize" && selected >= 0 && selected < len(slide.Elements) && isAxisScalableElement(slide.Elements[selected]) {
		drawAdaptiveToolbarLine(width, height, "43", legacyToolbarSegments(axisScaleToolbar(axisScaleMode)), right)
		return
	}
	ctx := interactionContextFor(mode, slide, selected, nil, status)
	ctx.Master = options.Master
	specs := editActionSpecs(ctx)
	if ctx.Mode == editorModeText && (selected < 0 || selected >= len(slide.Elements) || slide.Elements[selected].Kind != "code") {
		specs = filterActionSpecs(specs, "insert-newline")
	}
	prefix := strings.ToUpper(string(ctx.Selection))
	if ctx.Selection == selectionNone {
		prefix = "EDIT"
	}
	if ctx.Mode == editorModeText {
		prefix = "TEXT"
		if ctx.Selection == selectionCode {
			prefix = "CODE"
		}
	} else if ctx.Mode == editorModeMove {
		prefix = "MOVE"
	}
	left := []toolbarSegment{{Long: prefix, Short: prefix, Required: true, Priority: 0}}
	left = append(left, toolbarSegmentsFromActions(specs)...)
	if ctx.Mode != editorModeText {
		left = append(left, toolbarSegment{Long: "? shortcuts", Short: "?", Required: true, Priority: 0})
	}
	drawAdaptiveToolbarLine(width, height, "43", left, right)
}

func shortEditContextLabel(label string) string {
	replacer := strings.NewReplacer(
		"SELECT · ", "SEL:",
		"EDIT · ", "ED:",
		"AXIS SCALE · ", "AXIS:",
	)
	return replacer.Replace(label)
}

func editContextLabel(mode, status string, slide Slide, selected int, axisScaleMode string) string {
	switch mode {
	case "text":
		if selected >= 0 && selected < len(slide.Elements) && slide.Elements[selected].Kind == "code" {
			return "EDIT · Code"
		}
		return "EDIT · Text"
	case "resize":
		return "AXIS SCALE · " + titleCaseASCII(axisScaleModeLabel(axisScaleMode))
	case "move":
		return "MOVE"
	}
	if count := selectionCountFromStatus(status); count > 1 {
		return fmt.Sprintf("SELECT · %d / %s active", count, selectedElementKindLabel(slide, selected))
	}
	switch status {
	case "text selected":
		return "SELECT · Text"
	case "code text selected":
		return "SELECT · Code"
	case "image selected":
		return "SELECT · Image"
	case "shape selected":
		return "SELECT · Shape"
	case "":
		return "EDIT"
	default:
		return "EDIT"
	}
}

func selectionCountFromStatus(status string) int {
	fields := strings.Fields(status)
	if len(fields) != 2 || fields[1] != "selected" {
		return 0
	}
	count, _ := strconv.Atoi(fields[0])
	return count
}

func selectedElementKindLabel(slide Slide, selected int) string {
	if selected < 0 || selected >= len(slide.Elements) {
		return "Element"
	}
	return titleCaseASCII(elementKindLabel(slide.Elements[selected]))
}

func titleCaseASCII(text string) string {
	if text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:]
}

func drawToolbarLine(width, height int, bgCode, controls, label string) {
	if width <= 0 || height <= 0 {
		return
	}
	drawAdaptiveToolbarLine(width, height, bgCode, legacyToolbarSegments(controls), []toolbarSegment{{Long: label, Short: shortToolbarLabel(label), Required: true}})
}

type toolbarSegment struct {
	Long     string
	Short    string
	Required bool
	Priority int
}

func legacyToolbarSegments(controls string) []toolbarSegment {
	fields := strings.Split(strings.TrimSpace(controls), "  ")
	segments := make([]toolbarSegment, 0, len(fields))
	for index, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		required := index == 0 || strings.Contains(field, "Enter") || strings.Contains(field, "Esc") || strings.Contains(field, "? shortcuts")
		priority := 3
		if required {
			priority = 0
		} else if strings.Contains(strings.ToLower(field), "arrow") {
			priority = 1
		}
		segments = append(segments, toolbarSegment{Long: field, Short: compactToolbarText(field), Required: required, Priority: priority})
	}
	return segments
}

func compactToolbarText(text string) string {
	replacements := []struct{ old, new string }{
		{"Space/→ next", "→"}, {"← prev", "←"}, {"Shift-arrows jump 10", "⇧arrows"},
		{"Shift-arrows ×10", "⇧arrows"}, {"arrows cursor", "arrows"}, {"arrows move", "arrows"},
		{"Enter commit", "Enter"}, {"Enter save", "Enter"}, {"Esc cancel", "Esc"},
		{"? shortcuts", "?"}, {"speaker notes", "notes"},
	}
	for _, replacement := range replacements {
		if text == replacement.old {
			return replacement.new
		}
	}
	return text
}

func shortToolbarLabel(label string) string {
	if index := strings.Index(label, "  "); index >= 0 {
		return label[:index]
	}
	return label
}

func toolbarSegmentsFromActions(specs []actionSpec) []toolbarSegment {
	segments := make([]toolbarSegment, 0, len(specs))
	for _, spec := range specs {
		if !spec.Core || strings.TrimSpace(spec.Toolbar) == "" {
			continue
		}
		short := spec.Short
		if short == "" {
			short = compactToolbarText(spec.Toolbar)
		}
		segments = append(segments, toolbarSegment{
			Long: spec.Toolbar, Short: short, Required: spec.Priority == 0, Priority: spec.Priority,
		})
	}
	return segments
}

func drawAdaptiveToolbarLine(width, height int, bgCode string, left, right []toolbarSegment) {
	if width <= 0 || height <= 0 {
		return
	}
	leftText, rightText := fitToolbarSegments(width, left, right)
	termPrintf("\033[0;30;%sm\033[%d;1H%s", bgCode, height, strings.Repeat(" ", width))
	if leftText != "" {
		termPrintf("\033[0;30;%sm\033[%d;1H%s", bgCode, height, leftText)
	}
	if rightText != "" {
		col := max(1, width-displayWidth(rightText)+1)
		termPrintf("\033[0;30;%sm\033[%d;%dH%s", bgCode, height, col, rightText)
	}
}

func fitToolbarSegments(width int, left, right []toolbarSegment) (string, string) {
	left = append([]toolbarSegment(nil), left...)
	right = append([]toolbarSegment(nil), right...)
	leftVisible := make([]bool, len(left))
	rightVisible := make([]bool, len(right))
	leftShort := make([]bool, len(left))
	rightShort := make([]bool, len(right))
	for index := range leftVisible {
		leftVisible[index] = strings.TrimSpace(left[index].Long) != ""
	}
	for index := range rightVisible {
		rightVisible[index] = strings.TrimSpace(right[index].Long) != ""
	}
	render := func(segments []toolbarSegment, visible, short []bool) string {
		var parts []string
		for index, segment := range segments {
			if !visible[index] {
				continue
			}
			value := segment.Long
			if short[index] && segment.Short != "" {
				value = segment.Short
			}
			if value = strings.TrimSpace(value); value != "" {
				parts = append(parts, value)
			}
		}
		return strings.Join(parts, "  ")
	}
	fits := func() bool {
		leftText := render(left, leftVisible, leftShort)
		rightText := render(right, rightVisible, rightShort)
		gap := 0
		if leftText != "" && rightText != "" {
			gap = 2
		}
		return displayWidth(leftText)+gap+displayWidth(rightText) <= width
	}
	type candidate struct{ right, index, priority int }
	var candidates []candidate
	for index, segment := range left {
		if segment.Short != "" && segment.Short != segment.Long {
			candidates = append(candidates, candidate{index: index, priority: segment.Priority})
		}
	}
	for index, segment := range right {
		if segment.Short != "" && segment.Short != segment.Long {
			candidates = append(candidates, candidate{right: 1, index: index, priority: segment.Priority})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].priority > candidates[j].priority })
	for _, candidate := range candidates {
		if fits() {
			break
		}
		if candidate.right == 1 {
			rightShort[candidate.index] = true
		} else {
			leftShort[candidate.index] = true
		}
	}
	for priority := 9; priority >= 0 && !fits(); priority-- {
		for index := len(left) - 1; index >= 0 && !fits(); index-- {
			if leftVisible[index] && !left[index].Required && left[index].Priority == priority {
				leftVisible[index] = false
			}
		}
		for index := 0; index < len(right) && !fits(); index++ {
			if rightVisible[index] && !right[index].Required && right[index].Priority == priority {
				rightVisible[index] = false
			}
		}
	}
	leftText := render(left, leftVisible, leftShort)
	rightText := render(right, rightVisible, rightShort)
	if displayWidth(leftText)+displayWidth(rightText)+2 > width {
		leftText = ""
	}
	for index := 0; index < len(right)-1 && displayWidth(rightText) > width; index++ {
		if rightVisible[index] {
			rightVisible[index] = false
			rightText = render(right, rightVisible, rightShort)
		}
	}
	if displayWidth(rightText) > width {
		rightText = ""
	}
	return leftText, rightText
}

const (
	axisScaleStretchHorizontal = "stretch-horizontal"
	axisScaleShrinkHorizontal  = "shrink-horizontal"
	axisScaleStretchVertical   = "stretch-vertical"
	axisScaleShrinkVertical    = "shrink-vertical"
	axisScaleClose             = "close-menu"
)

func axisScaleModeLabel(mode string) string {
	switch mode {
	case axisScaleStretchHorizontal:
		return "stretch horizontal"
	case axisScaleShrinkHorizontal:
		return "shrink horizontal"
	case axisScaleStretchVertical:
		return "stretch vertical"
	case axisScaleShrinkVertical:
		return "shrink vertical"
	default:
		return "axis scale"
	}
}

func axisScaleToolbar(mode string) string {
	switch mode {
	case axisScaleStretchHorizontal:
		return " STRETCH HORIZONTAL  ← stretch left side  → stretch right side  Enter commit  Esc cancel "
	case axisScaleShrinkHorizontal:
		return " SHRINK HORIZONTAL  ← shrink right side  → shrink left side  Enter commit  Esc cancel "
	case axisScaleStretchVertical:
		return " STRETCH VERTICAL  ↑ stretch up  ↓ stretch bottom  Enter commit  Esc cancel "
	case axisScaleShrinkVertical:
		return " SHRINK VERTICAL  ↑ shrink bottom  ↓ shrink top  Enter commit  Esc cancel "
	default:
		return " AXIS SCALE  Enter commit  Esc cancel "
	}
}

type shortcutHelpItem struct {
	Key  string
	Text string
}

func playShortcutHelp(title string, items []shortcutHelpItem, slide Slide, page, width, height int, readEvent func() KeyEvent) KeyEvent {
	if len(items) == 0 {
		return KeyEvent{}
	}
	var result KeyEvent
	renderer := &liveSlideRenderer{}
	runOverlayLoop(overlayLoopSpec{
		Draw: func(frame int) {
			width, height = terminalAuthoredSize()
			renderer.draw(slide, width, height, page, frame, func(lines []Line) { drawShortcutHelp(title, items, width, height) })
		},
		Read: readEvent,
		Handle: func(event KeyEvent) overlayDecision {
			result = event
			return overlayDecision{Disposition: overlayPassthrough, Event: event}
		},
	})
	return result
}

func shortcutHelpDismissed(event KeyEvent) bool {
	if event.Action == "" || event.Action == "controls" || event.Action == "escape" || event.Action == "enter" || event.Action == "shortcuts" {
		return true
	}
	return event.Action == "text" && event.Text == "?"
}

func drawShortcutHelp(title string, items []shortcutHelpItem, width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	boxW := min(max(44, width/2), max(28, width-4))
	boxH := min(height-2, len(items)+5)
	x := max(0, (width-boxW)/2)
	y := max(0, (height-boxH)/2)
	for row := 0; row < boxH; row++ {
		termPrintf("\033[0;37;40m\033[%d;%dH%s", y+row+1, x+1, strings.Repeat(" ", boxW))
	}
	termPrintf("\033[1;37;40m\033[%d;%dH%s", y+2, x+3, crop(title, max(0, boxW-4)))
	row := y + 4
	keyW := 0
	for _, item := range items {
		keyW = max(keyW, displayWidth(item.Key))
	}
	keyW = min(keyW, 16)
	for _, item := range items {
		if row >= y+boxH {
			break
		}
		termPrintf("\033[1;36;40m\033[%d;%dH%s", row, x+3, padRight(crop(item.Key, keyW), keyW))
		termPrintf("\033[0;37;40m\033[%d;%dH%s", row, x+5+keyW, crop(item.Text, max(0, boxW-keyW-7)))
		row++
	}
	termPrintf("\033[0;90;40m\033[%d;%dH%s", y+boxH-1, x+3, crop("esc/enter return", max(0, boxW-4)))
	termPrint("\033[0m")
}

type axisScaleOption struct {
	Title string
	Mode  string
	Help  string
}

func playAxisScalePicker(slide Slide, page, width, height int, initialMode string) (string, bool) {
	options := []axisScaleOption{
		{Title: "Stretch horizontal", Mode: axisScaleStretchHorizontal, Help: "left stretches left side, right stretches right side"},
		{Title: "Shrink horizontal", Mode: axisScaleShrinkHorizontal, Help: "left shrinks right side, right shrinks left side"},
		{Title: "Stretch vertical", Mode: axisScaleStretchVertical, Help: "up stretches up, down stretches bottom"},
		{Title: "Shrink vertical", Mode: axisScaleShrinkVertical, Help: "up shrinks bottom, down shrinks top"},
		{},
		{Title: "Close menu", Mode: axisScaleClose, Help: "return to selection"},
	}
	selected := axisScaleOptionIndex(options, initialMode)
	result := ""
	renderer := &liveSlideRenderer{}
	decision := runOverlayLoop(overlayLoopSpec{
		Draw: func(frame int) {
			width, height = terminalAuthoredSize()
			renderer.draw(slide, width, height, page, frame, func(lines []Line) {
				drawAxisScalePicker(width, height, options, selected)
			})
		},
		Read: readStartupKeyEvent,
		Handle: func(event KeyEvent) overlayDecision {
			switch event.Action {
			case "escape", "quit":
				return overlayDecision{Disposition: overlayCancel}
			case "enter":
				result = options[selected].Mode
				return overlayDecision{Disposition: overlayCommit}
			case "up":
				selected = previousAxisScaleOption(options, selected)
			case "down", "tab":
				selected = nextAxisScaleOption(options, selected)
			}
			return overlayDecision{Disposition: overlayContinue}
		},
	})
	return result, decision.Disposition == overlayCommit
}

func axisScaleOptionIndex(options []axisScaleOption, mode string) int {
	for index, option := range options {
		if option.Title != "" && option.Mode == mode {
			return index
		}
	}
	return 0
}

func previousAxisScaleOption(options []axisScaleOption, selected int) int {
	for i := 0; i < len(options); i++ {
		selected = (selected + len(options) - 1) % len(options)
		if options[selected].Title != "" {
			return selected
		}
	}
	return 0
}

func nextAxisScaleOption(options []axisScaleOption, selected int) int {
	for i := 0; i < len(options); i++ {
		selected = (selected + 1) % len(options)
		if options[selected].Title != "" {
			return selected
		}
	}
	return 0
}

func drawAxisScalePicker(width, height int, options []axisScaleOption, selected int) {
	if width <= 0 || height <= 0 {
		return
	}
	boxW := min(max(62, width/2), max(32, width-4))
	boxH := min(height-2, 6+len(options)*2)
	x := max(0, (width-boxW)/2)
	y := max(0, (height-boxH)/2)
	for row := 0; row < boxH; row++ {
		termPrintf("\033[0;37;40m\033[%d;%dH%s", y+row+1, x+1, strings.Repeat(" ", boxW))
	}
	termPrintf("\033[1;37;40m\033[%d;%dH%s", y+2, x+3, crop("Axis scaling", max(0, boxW-4)))
	row := y + 4
	for index, option := range options {
		if option.Title == "" {
			termPrintf("\033[0;90;40m\033[%d;%dH%s", row, x+3, strings.Repeat("-", max(0, boxW-6)))
			row++
			continue
		}
		prefix := "  "
		mode := "\033[0;37;40m"
		if index == selected {
			prefix = "> "
			mode = "\033[7;37;40m"
		}
		termPrintf("%s\033[%d;%dH%s\033[0m", mode, row, x+3, padRight(crop(prefix+option.Title, max(0, boxW-6)), max(0, boxW-6)))
		if row+1 < y+boxH {
			termPrintf("\033[0;90;40m\033[%d;%dH%s", row+1, x+5, crop(option.Help, max(0, boxW-8)))
		}
		row += 2
	}
	termPrintf("\033[0;90;40m\033[%d;%dH%s", y+boxH-1, x+3, crop("up/down select  enter confirm  esc cancel", max(0, boxW-4)))
	termPrint("\033[0m")
}

func mainShortcutHelp() []shortcutHelpItem {
	return shortcutItemsFromActionSpecs(mainActionSpecs())
}

func editShortcutHelp(mode, status string, slide Slide, selected int, axisScaleMode string, master bool) (string, []shortcutHelpItem) {
	if mode == "resize" {
		switch axisScaleMode {
		case axisScaleStretchHorizontal:
			return "Stretch horizontal", []shortcutHelpItem{
				{"Left", "stretch left side to the left"},
				{"Right", "stretch right side to the right"},
				{"Enter", "commit axis scaling"},
				{"Esc", "cancel axis scaling"},
			}
		case axisScaleShrinkHorizontal:
			return "Shrink horizontal", []shortcutHelpItem{
				{"Left", "shrink the right side"},
				{"Right", "shrink the left side"},
				{"Enter", "commit axis scaling"},
				{"Esc", "cancel axis scaling"},
			}
		case axisScaleStretchVertical:
			return "Stretch vertical", []shortcutHelpItem{
				{"Up", "stretch up"},
				{"Down", "stretch bottom"},
				{"Enter", "commit axis scaling"},
				{"Esc", "cancel axis scaling"},
			}
		case axisScaleShrinkVertical:
			return "Shrink vertical", []shortcutHelpItem{
				{"Up", "shrink bottom"},
				{"Down", "shrink top"},
				{"Enter", "commit axis scaling"},
				{"Esc", "cancel axis scaling"},
			}
		default:
			return "Axis scaling", []shortcutHelpItem{
				{"Enter", "commit axis scaling"},
				{"Esc", "cancel axis scaling"},
			}
		}
	}
	ctx := interactionContextFor(mode, slide, selected, nil, status)
	ctx.Master = master
	specs := editActionSpecs(ctx)
	if ctx.Mode == editorModeText && (selected < 0 || selected >= len(slide.Elements) || slide.Elements[selected].Kind != "code") {
		specs = filterActionSpecs(specs, "insert-newline")
	}
	title := "Edit shortcuts"
	switch {
	case ctx.Mode == editorModeMove:
		title = "Move shortcuts"
	case ctx.Mode == editorModeText && ctx.Selection == selectionCode:
		title = "Code edit shortcuts"
	case ctx.Mode == editorModeText:
		title = "Text edit shortcuts"
	case ctx.Selection == selectionText:
		title = "Selected text shortcuts"
	case ctx.Selection == selectionCode:
		title = "Selected code shortcuts"
	case ctx.Selection == selectionImage:
		title = "Selected image shortcuts"
	case ctx.Selection == selectionShape:
		title = "Selected shape shortcuts"
	case ctx.Selection == selectionMulti:
		title = "Multi-selection shortcuts"
	}
	if ctx.Selection == selectionNone && ctx.Mode == editorModeSelect {
		specs = append(specs,
			action("1 / 2", "slides or notes", "", "", false, 4, "slide-list", "speaker-notes"),
			action("/ / j", "search or jump", "", "", false, 4, "search", "jump"),
			action("x", "export HTML", "", "", false, 4, "export"),
			action("q", "quit", "", "", false, 4, "quit"),
		)
	}
	return title, shortcutItemsFromActionSpecs(specs)
}

func shortcutItemsFromActionSpecs(specs []actionSpec) []shortcutHelpItem {
	items := make([]shortcutHelpItem, 0, len(specs))
	for _, spec := range specs {
		if spec.Key == "" || spec.Help == "" {
			continue
		}
		items = append(items, shortcutHelpItem{Key: spec.Key, Text: spec.Help})
	}
	return items
}

func filterActionSpecs(specs []actionSpec, actionName string) []actionSpec {
	out := make([]actionSpec, 0, len(specs))
	for _, spec := range specs {
		if actionSpecsAllow([]actionSpec{spec}, actionName) {
			continue
		}
		out = append(out, spec)
	}
	return out
}

const speakerNotesPanelHeight = 6

func drawSpeakerNotesPanel(notes string, cursor int, editing bool, width, height int) {
	x, y, w, h := notesPanelRect(width, height)
	if w <= 0 || h <= 0 {
		return
	}
	for row := 0; row < h; row++ {
		termPrintf("\033[0;30;47m\033[%d;%dH%s", y+row+1, x+1, strings.Repeat(" ", w))
	}
	title := " Speaker notes "
	termPrintf("\033[1;30;47m\033[%d;%dH%s", y+1, x+2, crop(title, max(0, w-2)))
	lines := strings.Split(notes, "\n")
	bodyRows := max(1, h-2)
	cursorRow, cursorCol := notesCursorLineCol(notes, cursor)
	scroll := max(0, cursorRow-bodyRows+1)
	for i := 0; i < bodyRows; i++ {
		lineIndex := scroll + i
		text := ""
		if lineIndex < len(lines) {
			text = lines[lineIndex]
		}
		termPrintf("\033[0;37;40m\033[%d;%dH%s", y+2+i, x+2, crop(text, max(0, w-4)))
	}
	if editing && cursorRow >= scroll && cursorRow < scroll+bodyRows {
		cursorX := x + 2 + min(cursorCol, max(0, w-5))
		cursorY := y + 2 + cursorRow - scroll
		if cursorY+1 < y+h {
			termPrintf("\033[0;90;40m\033[%d;%dH▀\033[0m", cursorY+1, cursorX)
		} else {
			termPrintf("\033[0;90;40m\033[%d;%dH▄\033[0m", cursorY, cursorX)
		}
	}
}

func drawSlideNavigatorToolbar(width, height, selected, count int) {
	label := fmt.Sprintf("%d/%d", min(selected+1, count), count)
	drawAdaptiveToolbarLine(width, height, "43",
		legacyToolbarSegments(" SLIDES  ↑/↓ select  Enter go  1/Esc close "),
		[]toolbarSegment{{Long: "SLIDES · Overview", Short: "SLIDES", Required: true}, {Long: label, Short: label, Required: true}},
	)
}

func drawSpeakerNotesToolbar(width, height int, editing bool) {
	if editing {
		drawAdaptiveToolbarLine(width, height, "43",
			legacyToolbarSegments(" NOTES  Enter commit  Shift-Enter newline  arrows cursor  Esc cancel "),
			[]toolbarSegment{{Long: "NOTES · Editing", Short: "NOTES", Required: true}},
		)
		return
	}
	drawAdaptiveToolbarLine(width, height, "43",
		legacyToolbarSegments(" NOTES  Enter edit  Tab select field  Esc close  slide controls active "),
		[]toolbarSegment{{Long: "NOTES · Viewing", Short: "NOTES", Required: true}},
	)
}

func timerInputDuration(input string) time.Duration {
	input = strings.TrimSpace(input)
	if input == "" {
		return 0
	}
	for len(input) < 4 {
		input = "0" + input
	}
	minutes, _ := strconv.Atoi(input[:len(input)-2])
	seconds, _ := strconv.Atoi(input[len(input)-2:])
	if seconds > 59 {
		seconds = 59
	}
	return time.Duration(minutes*60+seconds) * time.Second
}

func applyTimerEvent(state *EditState, event KeyEvent) bool {
	if state == nil || state.TimerMode == "" {
		return false
	}
	if event.Action == "escape" || event.Action == "controls" {
		state.TimerMode = ""
		state.TimerInput = ""
		state.TimerDeadline = time.Time{}
		return true
	}
	if event.Action == "timer" {
		if state.TimerMode == "config" {
			if len(state.TimerInput) < 4 {
				state.TimerInput += "0"
			}
		} else {
			state.TimerMode = "config"
			state.TimerInput = ""
			state.TimerDeadline = time.Time{}
		}
		return true
	}
	if state.TimerMode != "config" {
		return false
	}
	switch event.Action {
	case "slide-list":
		if len(state.TimerInput) < 4 {
			state.TimerInput += "1"
		}
	case "speaker-notes":
		if len(state.TimerInput) < 4 {
			state.TimerInput += "2"
		}
	case "backspace":
		if state.TimerInput != "" {
			state.TimerInput = state.TimerInput[:len(state.TimerInput)-1]
		}
	case "enter":
		if duration := timerInputDuration(state.TimerInput); duration > 0 {
			state.TimerMode = "running"
			state.TimerDeadline = time.Now().Add(duration)
		}
	case "text":
		for _, r := range event.Text {
			if r >= '0' && r <= '9' && len(state.TimerInput) < 4 {
				state.TimerInput += string(r)
			}
		}
	default:
		return false
	}
	return true
}

func timerDisplayText(mode, input string, deadline time.Time) (string, bool) {
	if mode == "config" {
		padded := input
		for len(padded) < 4 {
			padded = "0" + padded
		}
		if len(padded) > 4 {
			padded = padded[len(padded)-4:]
		}
		return padded[:2] + ":" + padded[2:], false
	}
	remaining := time.Until(deadline)
	done := remaining <= 0
	if done {
		remaining = 0
	}
	total := int(math.Ceil(remaining.Seconds()))
	minutes := total / 60
	seconds := total % 60
	if minutes > 99 {
		minutes = 99
		seconds = 59
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds), done
}

func drawTimerOverlay(width, height int, mode, input string, deadline time.Time) {
	text, done := timerDisplayText(mode, input, deadline)
	if done && time.Now().UnixMilli()/400%2 == 0 {
		return
	}
	scale := timerFontScale(width, height, text)
	gap := max(1, scale)
	contentW, contentH := timerFontTextSize(text, scale, gap)
	panelW := contentW + 4
	panelH := contentH + 4
	x := max(0, (width-panelW)/2)
	y := max(0, (height-panelH)/2)
	for row := 0; row < panelH && y+row < height; row++ {
		termPrintf("\033[0;37;40m\033[%d;%dH%s", y+row+1, x+1, strings.Repeat(" ", min(panelW, width-x)))
	}
	title := " TIMER "
	if mode == "config" {
		title = " TIMER SETUP "
	}
	termPrintf("\033[1;37;40m\033[%d;%dH%s", y+1, x+2, crop(title, max(0, panelW-4)))
	color := "\033[1;31;40m"
	if done {
		color = "\033[1;37;41m"
	}
	if mode == "config" {
		drawTimerFontSetup(x+2, max(0, y+1), text, input, scale, gap)
	} else {
		drawTimerFontText(x+2, max(0, y+1), text, scale, gap, color)
	}
	if mode == "config" {
		hint := "type MMSS, Enter start, Esc cancel"
		termPrintf("\033[0;37;40m\033[%d;%dH%s", y+panelH, x+2, crop(hint, max(0, panelW-4)))
	}
	termPrint("\033[0m")
}

func timerFontScale(width, height int, text string) int {
	if width <= 0 || height <= 0 {
		return 1
	}
	for scale := 2; scale >= 1; scale-- {
		w, h := timerFontTextSize(text, scale, max(1, scale))
		if w+4 <= width && h+4 <= height {
			return scale
		}
	}
	return 1
}

func timerFontTextSize(text string, scale, gap int) (int, int) {
	width, height := 0, 0
	for i, ch := range text {
		rows := timerFontGlyphRows(ch, scale)
		if i > 0 {
			width += gap
		}
		width += maxLineDisplayWidth(rows)
		height = max(height, len(rows))
	}
	return width, height
}

func timerFontGlyphRows(ch rune, scale int) []string {
	rows := renderFull(string(ch), scale)
	if len(rows) == 0 {
		return []string{""}
	}
	return rows
}

func drawTimerFontText(x, y int, text string, scale, gap int, color string) {
	cursor := x
	for _, ch := range text {
		rows := timerFontGlyphRows(ch, scale)
		drawTimerFontGlyph(cursor, y, rows, color)
		cursor += maxLineDisplayWidth(rows) + gap
	}
}

func drawTimerFontSetup(x, y int, text, input string, scale, gap int) {
	typed := max(0, min(4, len([]rune(input))))
	typedFrom := 4 - typed
	digitIndex := 0
	cursor := x
	for _, ch := range text {
		rows := timerFontGlyphRows(ch, scale)
		color := "\033[1;90;40m"
		if ch != ':' && digitIndex >= typedFrom {
			color = "\033[1;36;40m"
		}
		drawTimerFontGlyph(cursor, y, rows, color)
		cursor += maxLineDisplayWidth(rows) + gap
		if ch != ':' {
			digitIndex++
		}
	}
}

func drawTimerFontGlyph(x, y int, rows []string, color string) {
	for row, line := range rows {
		termPrintf("%s\033[%d;%dH%s", color, y+row+1, x+1, line)
	}
}

func notesPanelRect(width, height int) (int, int, int, int) {
	h := min(speakerNotesPanelHeight, max(1, height-1))
	return 0, max(0, height-h-1), width, h
}

func notesCapacity(width, height int) (int, int) {
	_, _, w, h := notesPanelRect(width, height)
	return max(1, h-2), max(1, w-4)
}

func insertNotesText(notes string, cursor int, text string, width, height int) (string, int, bool) {
	maxLines, maxCols := notesCapacity(width, height)
	changed := false
	runes := []rune(notes)
	cursor = max(0, min(len(runes), cursor))
	for _, ch := range text {
		if ch == '\r' {
			continue
		}
		row, col := notesCursorLineCol(string(runes), cursor)
		lines := strings.Split(string(runes), "\n")
		if ch == '\n' {
			if len(lines) >= maxLines {
				continue
			}
		} else {
			if row >= len(lines) || col >= maxCols || len([]rune(lines[row])) >= maxCols {
				continue
			}
		}
		runes = append(runes[:cursor], append([]rune{ch}, runes[cursor:]...)...)
		cursor++
		changed = true
	}
	return string(runes), cursor, changed
}

func notesCursorAtPoint(notes string, x, y, width, height int) (int, bool) {
	panelX, panelY, panelW, panelH := notesPanelRect(width, height)
	if x < panelX+1 || x >= panelX+panelW-1 || y < panelY+1 || y >= panelY+panelH {
		return 0, false
	}
	lines := strings.Split(notes, "\n")
	bodyRows := max(1, panelH-2)
	cursorRow, _ := notesCursorLineCol(notes, len([]rune(notes)))
	scroll := max(0, cursorRow-bodyRows+1)
	lineIndex := min(len(lines)-1, max(0, scroll+y-(panelY+1)))
	col := max(0, x-(panelX+1))
	return notesOffsetForLineCol(notes, lineIndex, col), true
}

func notesCursorLineCol(notes string, cursor int) (int, int) {
	runes := []rune(notes)
	cursor = max(0, min(len(runes), cursor))
	row, col := 0, 0
	for i := 0; i < cursor; i++ {
		if runes[i] == '\n' {
			row++
			col = 0
		} else {
			col++
		}
	}
	return row, col
}

func notesOffsetForLineCol(notes string, row, col int) int {
	runes := []rune(notes)
	curRow, curCol := 0, 0
	for i, r := range runes {
		if curRow == row && curCol >= col {
			return i
		}
		if r == '\n' {
			if curRow == row {
				return i
			}
			curRow++
			curCol = 0
		} else {
			curCol++
		}
	}
	return len(runes)
}

func moveNotesCursorVertical(notes string, cursor, delta int) int {
	row, col := notesCursorLineCol(notes, cursor)
	targetRow := max(0, row+delta)
	return notesOffsetForLineCol(notes, targetRow, col)
}

func drawSlideNavigatorOverlay(slides []Slide, selected int, scroll *int, width, height int) {
	x, y, w, h := slideNavigatorRect(width, height)
	if w <= 0 || h <= 0 || len(slides) == 0 {
		return
	}
	itemH := slideNavigatorItemHeight()
	visible := max(1, h/itemH)
	if selected < *scroll {
		*scroll = selected
	}
	if selected >= *scroll+visible {
		*scroll = selected - visible + 1
	}
	*scroll = max(0, min(max(0, len(slides)-visible), *scroll))
	for row := 0; row < h; row++ {
		termPrintf("\033[0;37;40m\033[%d;%dH%s", y+row+1, x+1, strings.Repeat(" ", w))
	}
	for slot := 0; slot < visible; slot++ {
		index := *scroll + slot
		if index >= len(slides) {
			break
		}
		itemY := y + slot*itemH
		drawSlideNavigatorItem(slides[index], index, selected == index, x, itemY, w, itemH)
	}
}

func slideNavigatorRect(width, height int) (int, int, int, int) {
	w := min(max(20, width/6), max(1, width/3))
	h := max(1, height-1)
	return 0, 0, w, h
}

func slideNavigatorItemHeight() int {
	return 7
}

func drawSlideNavigatorItem(slide Slide, index int, selected bool, x, y, w, h int) {
	bg := "40"
	fg := "37"
	if selected {
		bg = "47"
		fg = "30"
	}
	for row := 0; row < h; row++ {
		termPrintf("\033[0;%s;%sm\033[%d;%dH%s", fg, bg, y+row+1, x+1, strings.Repeat(" ", w))
	}
	label := fmt.Sprintf("%d %s", index+1, slideNavigatorTitle(slide, max(0, w-4)))
	termPrintf("\033[1;%s;%sm\033[%d;%dH%s", fg, bg, y+1, x+2, crop(label, max(0, w-2)))
	thumbW := max(4, w-4)
	thumbH := max(2, h-2)
	lines := displayLines(slide, thumbW, thumbH, 0)
	for _, line := range lines {
		if line.Row < 0 || line.Row >= thumbH {
			continue
		}
		text := crop(line.Text, thumbW)
		if text == "" {
			continue
		}
		termPrintf("\033[0;%s;%sm\033[%d;%dH%s", fg, bg, y+2+line.Row, x+2+max(0, min(thumbW-1, line.Col)), crop(text, max(0, thumbW-line.Col)))
	}
}

func slideNavigatorTitle(slide Slide, maxChars int) string {
	for _, element := range slide.Elements {
		if element.Kind == "heading" && strings.TrimSpace(element.Text) != "" {
			return crop(strings.TrimSpace(element.Text), maxChars)
		}
	}
	for _, element := range slide.Elements {
		if element.Kind != "image" && element.Kind != "shape" && strings.TrimSpace(element.Text) != "" {
			return crop(strings.TrimSpace(element.Text), maxChars)
		}
	}
	return ""
}

func slideNavigatorIndexAtPoint(x, y, scroll, slideCount, width, height int) (int, bool) {
	panelX, panelY, panelW, panelH := slideNavigatorRect(width, height)
	if x < panelX || x >= panelX+panelW || y < panelY || y >= panelY+panelH {
		return 0, false
	}
	index := scroll + (y-panelY)/slideNavigatorItemHeight()
	if index < 0 || index >= slideCount {
		return 0, false
	}
	return index, true
}

var availableEffects = []string{
	"none",
	"matrix",
	"stars",
	"plasma",
	"glitch",
	"digital-snow",
	"radar",
	"neural",
	"circuit",
	"data-storm",
	"flame",
	"warp",
	"scanline",
	"fireworks",
	"explosion",
}

var availableBackgrounds = []string{
	"none",
	"soft-plasma",
	"aurora",
	"topography",
	"waves",
	"mesh",
	"constellation",
	"ribbons",
	"diagonal-flow",
	"blueprint",
}

var availableShapes = []string{
	"circle",
	"square",
	"triangle",
	"diamond",
}

type overlayDisposition int

const (
	overlayContinue overlayDisposition = iota
	overlayCommit
	overlayCancel
	overlayPassthrough
)

type overlayDecision struct {
	Disposition overlayDisposition
	Event       KeyEvent
}

type overlayLoopSpec struct {
	Draw   func(frame int)
	Read   func() KeyEvent
	Handle func(KeyEvent) overlayDecision
}

func runOverlayLoop(spec overlayLoopSpec) overlayDecision {
	if spec.Draw == nil || spec.Read == nil || spec.Handle == nil {
		return overlayDecision{Disposition: overlayCancel}
	}
	ticker := time.NewTicker(70 * time.Millisecond)
	defer ticker.Stop()
	frame := 0
	dirty := true
	for {
		if dirty {
			spec.Draw(frame)
			dirty = false
		}
		if event := spec.Read(); event.Action != "" {
			decision := spec.Handle(event)
			if decision.Disposition != overlayContinue {
				return decision
			}
			dirty = true
		}
		select {
		case <-ticker.C:
			frame++
			dirty = true
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

type liveSlideRenderer struct {
	effect    string
	width     int
	height    int
	matrix    *matrixEffect
	stars     *starsEffect
	fireworks *burstEffect
}

func (renderer *liveSlideRenderer) draw(slide Slide, width, height, page, frame int, overlay func([]Line)) {
	if renderer.effect != slide.Effect || renderer.width != width || renderer.height != height || renderer.matrix == nil {
		renderer.effect = slide.Effect
		renderer.width = width
		renderer.height = height
		renderer.matrix = newMatrix(width, height)
		renderer.stars = newStars(width, height)
		renderer.fireworks = newBursts(slide.Effect, width, height)
	}
	flushTerminalFrame(func() {
		termPrintf("\033[%sm\033[2J\033[H", slideBG(slide))
		drawStaticBackground(slide.Background, width, height, slideBG(slide))
		if slide.Effect != "" {
			drawEffectFrame(slide.Effect, width, height, frame, renderer.matrix, renderer.stars, renderer.fireworks, slideBG(slide))
		}
		lines := displayLines(slide, width, height, page)
		drawOverlayLines(lines, width, height, slide)
		drawLinkUnderlines(lines, width, height, slide)
		if overlay != nil {
			overlay(lines)
		}
		termPrint("\033[0m")
	})
}

func playEffectPicker(slide Slide, width, height int) (string, bool) {
	current := slide.Effect
	if current == "" {
		current = "none"
	}
	selected := 0
	for i, effect := range availableEffects {
		if effect == current {
			selected = i
			break
		}
	}
	frame := 0
	lastEffect := ""
	var matrix *matrixEffect
	var stars *starsEffect
	var fireworks *burstEffect
	result := ""
	decision := runOverlayLoop(overlayLoopSpec{
		Draw: func(tick int) {
			width, height = terminalAuthoredSize()
			previewEffect := availableEffects[selected]
			if previewEffect != lastEffect || matrix == nil || matrix.width != width || matrix.height != height {
				matrix = newMatrix(width, height)
				stars = newStars(width, height)
				fireworks = newBursts(previewEffect, width, height)
				lastEffect = previewEffect
				frame = 0
			}
			flushTerminalFrame(func() {
				termPrintf("\033[%sm\033[2J\033[H", slideBG(slide))
				drawStaticBackground(slide.Background, width, height, slideBG(slide))
				if previewEffect != "none" {
					drawEffectFrame(previewEffect, width, height, frame, matrix, stars, fireworks, slideBG(slide))
				}
				drawOverlayLines(displayLines(slide, width, height, 0), width, height, slide)
				drawEffectPicker(width, height, selected)
				termPrint("\033[0m")
			})
			frame++
		},
		Read: readEffectPickerKeyEvent,
		Handle: func(event KeyEvent) overlayDecision {
			switch event.Action {
			case "escape", "quit":
				return overlayDecision{Disposition: overlayCancel}
			case "enter":
				result = availableEffects[selected]
				return overlayDecision{Disposition: overlayCommit}
			case "up", "left":
				selected = (selected - 1 + len(availableEffects)) % len(availableEffects)
			case "down", "right":
				selected = (selected + 1) % len(availableEffects)
			case "mouse-click":
				if index, ok := effectAtPoint(event.X, event.Y, width, height); ok {
					if index == selected {
						result = availableEffects[selected]
						return overlayDecision{Disposition: overlayCommit}
					}
					selected = index
				}
			}
			return overlayDecision{Disposition: overlayContinue}
		},
	})
	return result, decision.Disposition == overlayCommit
}

func playShapePicker(slide Slide, width, height, page int, current string) (string, bool) {
	selected := 0
	for i, shape := range availableShapes {
		if shape == current {
			selected = i
			break
		}
	}
	result := ""
	renderer := &liveSlideRenderer{}
	decision := runOverlayLoop(overlayLoopSpec{
		Draw: func(frame int) {
			width, height = terminalAuthoredSize()
			renderer.draw(slide, width, height, page, frame, func(lines []Line) { drawShapePicker(width, height, selected) })
		},
		Read: readEffectPickerKeyEvent,
		Handle: func(event KeyEvent) overlayDecision {
			switch event.Action {
			case "escape", "quit":
				return overlayDecision{Disposition: overlayCancel}
			case "enter":
				result = availableShapes[selected]
				return overlayDecision{Disposition: overlayCommit}
			case "up", "left":
				selected = (selected - 1 + len(availableShapes)) % len(availableShapes)
			case "down", "right":
				selected = (selected + 1) % len(availableShapes)
			case "mouse-click":
				if index, ok := shapeAtPoint(event.X, event.Y, width, height); ok {
					if index == selected {
						result = availableShapes[selected]
						return overlayDecision{Disposition: overlayCommit}
					}
					selected = index
				}
			}
			return overlayDecision{Disposition: overlayContinue}
		},
	})
	return result, decision.Disposition == overlayCommit
}

func playBackgroundPicker(slide Slide, width, height int) (string, bool) {
	current := slide.Background
	if current == "" {
		current = "none"
	}
	selected := 0
	for i, background := range availableBackgrounds {
		if background == current {
			selected = i
			break
		}
	}
	result := ""
	renderer := &liveSlideRenderer{}
	decision := runOverlayLoop(overlayLoopSpec{
		Draw: func(frame int) {
			width, height = terminalAuthoredSize()
			preview := slide
			previewBackground := availableBackgrounds[selected]
			if previewBackground == "none" {
				preview.Background = ""
			} else {
				preview.Background = previewBackground
			}
			renderer.draw(preview, width, height, 0, frame, func(lines []Line) { drawBackgroundPicker(width, height, selected) })
		},
		Read: readEffectPickerKeyEvent,
		Handle: func(event KeyEvent) overlayDecision {
			switch event.Action {
			case "escape", "quit":
				return overlayDecision{Disposition: overlayCancel}
			case "enter":
				result = availableBackgrounds[selected]
				return overlayDecision{Disposition: overlayCommit}
			case "up", "left":
				selected = (selected - 1 + len(availableBackgrounds)) % len(availableBackgrounds)
			case "down", "right":
				selected = (selected + 1) % len(availableBackgrounds)
			case "mouse-click":
				if index, ok := backgroundAtPoint(event.X, event.Y, width, height); ok {
					if index == selected {
						result = availableBackgrounds[selected]
						return overlayDecision{Disposition: overlayCommit}
					}
					selected = index
				}
			}
			return overlayDecision{Disposition: overlayContinue}
		},
	})
	return result, decision.Disposition == overlayCommit
}

func drawEffectPicker(width, height, selected int) {
	panelWidth := min(width, 44)
	panelHeight := min(height, len(availableEffects)+5)
	left := max(0, (width-panelWidth)/2)
	top := max(0, (height-panelHeight)/2)
	panelBG := "48;2;18;18;18"
	for row := 0; row < panelHeight; row++ {
		termPrintf("\033[0;37;%sm\033[%d;%dH%s", panelBG, top+row+1, left+1, strings.Repeat(" ", panelWidth))
	}
	termPrintf("\033[0;37;%sm\033[%d;%dH%s", panelBG, top+1, left+3, "Effect")
	for i, effect := range availableEffects {
		if i+3 >= panelHeight {
			break
		}
		bg := panelBG
		fg := "37"
		if i == selected {
			bg = "43"
			fg = "30"
		}
		termPrintf("\033[0;%s;%sm\033[%d;%dH%s", fg, bg, top+3+i, left+3, padRight(crop(effect, panelWidth-6), panelWidth-6))
	}
	help := " arrows select  Enter save  Esc cancel "
	termPrintf("\033[0;30;43m\033[%d;%dH%s", top+panelHeight-1, left+3, padRight(crop(help, panelWidth-6), panelWidth-6))
}

func drawBackgroundPicker(width, height, selected int) {
	panelWidth := min(width, 44)
	panelHeight := min(height, len(availableBackgrounds)+5)
	left := max(0, (width-panelWidth)/2)
	top := max(0, (height-panelHeight)/2)
	panelBG := "48;2;18;18;18"
	for row := 0; row < panelHeight; row++ {
		termPrintf("\033[0;37;%sm\033[%d;%dH%s", panelBG, top+row+1, left+1, strings.Repeat(" ", panelWidth))
	}
	termPrintf("\033[0;37;%sm\033[%d;%dH%s", panelBG, top+1, left+3, "Background")
	for i, background := range availableBackgrounds {
		if i+3 >= panelHeight {
			break
		}
		bg := panelBG
		fg := "37"
		if i == selected {
			bg = "43"
			fg = "30"
		}
		termPrintf("\033[0;%s;%sm\033[%d;%dH%s", fg, bg, top+3+i, left+3, padRight(crop(background, panelWidth-6), panelWidth-6))
	}
	help := " arrows select  Enter save  Esc cancel "
	termPrintf("\033[0;30;43m\033[%d;%dH%s", top+panelHeight-1, left+3, padRight(crop(help, panelWidth-6), panelWidth-6))
}

func drawShapePicker(width, height, selected int) {
	panelWidth := min(width, 52)
	panelHeight := min(height, len(availableShapes)*3+5)
	left := max(0, (width-panelWidth)/2)
	top := max(0, (height-panelHeight)/2)
	panelBG := "48;2;18;18;18"
	for row := 0; row < panelHeight; row++ {
		termPrintf("\033[0;37;%sm\033[%d;%dH%s", panelBG, top+row+1, left+1, strings.Repeat(" ", panelWidth))
	}
	termPrintf("\033[0;37;%sm\033[%d;%dH%s", panelBG, top+1, left+3, "Shapes")
	for i, shape := range availableShapes {
		itemTop := top + 3 + i*3
		if itemTop >= top+panelHeight-2 {
			break
		}
		bg := panelBG
		fg := "37"
		if i == selected {
			bg = "43"
			fg = "30"
		}
		preview := shapePreview(shape)
		label := fmt.Sprintf("%-10s %s", shape, preview)
		termPrintf("\033[0;%s;%sm\033[%d;%dH%s", fg, bg, itemTop, left+3, padRight(crop(label, panelWidth-6), panelWidth-6))
	}
	help := " arrows select  Enter insert  Esc cancel "
	termPrintf("\033[0;30;43m\033[%d;%dH%s", top+panelHeight-1, left+3, padRight(crop(help, panelWidth-6), panelWidth-6))
}

func shapePreview(shape string) string {
	switch shape {
	case "circle":
		return "  ███  "
	case "square":
		return "  ████ "
	case "triangle":
		return "   ▲   "
	case "diamond":
		return "   ◆   "
	default:
		return "  ███  "
	}
}

func effectAtPoint(x, y, width, height int) (int, bool) {
	panelWidth := min(width, 44)
	panelHeight := min(height, len(availableEffects)+5)
	left := max(0, (width-panelWidth)/2)
	top := max(0, (height-panelHeight)/2)
	index := y - (top + 2)
	if x < left+2 || x >= left+panelWidth-2 || index < 0 || index >= len(availableEffects) || index+3 >= panelHeight {
		return 0, false
	}
	return index, true
}

func backgroundAtPoint(x, y, width, height int) (int, bool) {
	panelWidth := min(width, 44)
	panelHeight := min(height, len(availableBackgrounds)+5)
	left := max(0, (width-panelWidth)/2)
	top := max(0, (height-panelHeight)/2)
	index := y - (top + 2)
	if x < left+2 || x >= left+panelWidth-2 || index < 0 || index >= len(availableBackgrounds) || index+3 >= panelHeight {
		return 0, false
	}
	return index, true
}

func shapeAtPoint(x, y, width, height int) (int, bool) {
	panelWidth := min(width, 52)
	panelHeight := min(height, len(availableShapes)*3+5)
	left := max(0, (width-panelWidth)/2)
	top := max(0, (height-panelHeight)/2)
	index := (y - (top + 2)) / 3
	itemY := top + 2 + index*3
	if x < left+2 || x >= left+panelWidth-2 || y != itemY || index < 0 || index >= len(availableShapes) || itemY >= top+panelHeight-2 {
		return 0, false
	}
	return index, true
}

func readEffectPickerKeyEvent() KeyEvent {
	var buf [32]byte
	n, _ := inputRead(buf[:])
	if n == 0 {
		return KeyEvent{}
	}
	b := buf[:n]
	if event := parseMouseEvent(b); event.Action != "" {
		return event
	}
	if event := editEscapeEvent(b); event.Action != "" {
		return event
	}
	if bytes.Contains(b, []byte{'\r'}) || bytes.Contains(b, []byte{'\n'}) {
		return KeyEvent{Action: "enter"}
	}
	if bytes.Contains(b, []byte{'q'}) {
		return KeyEvent{Action: "quit"}
	}
	return KeyEvent{}
}

type pickerColour struct {
	Hex     string
	FG, BG  string
	R, G, B int
}

func playTextColorPicker(slide Slide, selected, width, height, page int) (string, bool) {
	return playColorPicker(slide, currentElementHexForElement(slide.Elements[selected]), selected, width, height, page)
}

func playSlideColorPicker(slide Slide, current string, width, height int) (string, bool) {
	return playColorPicker(slide, current, -1, width, height, 0)
}

func playColorPicker(slide Slide, current string, selected, width, height, page int) (string, bool) {
	palette := colourPickerPalette()
	index := nearestPickerColourIndex(current, palette)
	field := palette[index].Hex
	result := ""
	renderer := &liveSlideRenderer{}
	decision := runOverlayLoop(overlayLoopSpec{
		Draw: func(frame int) {
			width, height = terminalAuthoredSize()
			withFastImageRender(func() {
				renderer.draw(slide, width, height, page, frame, func(lines []Line) {
					if selected >= 0 && selected < len(slide.Elements) {
						if slide.Elements[selected].Kind == "image" {
							drawSelectedImageHighlight(lines, selected, width, height)
						} else {
							drawSelectedElementHighlight(lines, selected, width, height)
						}
					}
					drawColorPicker(width, height, palette, index, field)
				})
			})
		},
		Read: readColorPickerKeyEvent,
		Handle: func(event KeyEvent) overlayDecision {
			switch event.Action {
			case "escape", "quit":
				return overlayDecision{Disposition: overlayCancel}
			case "enter":
				if hex, ok := normalizeHexColour(field); ok {
					result = hex
					return overlayDecision{Disposition: overlayCommit}
				}
			case "left":
				if index%8 > 0 {
					index--
					field = palette[index].Hex
				}
			case "right":
				if index%8 < 7 {
					index++
					field = palette[index].Hex
				}
			case "up":
				if index >= 8 {
					index -= 8
					field = palette[index].Hex
				}
			case "down":
				if index < 56 {
					index += 8
					field = palette[index].Hex
				}
			case "backspace":
				if len(field) > 0 {
					field = field[:len(field)-1]
				}
			case "mouse-click":
				if clicked, ok := colorPickerIndexAt(event.X, event.Y, width, height); ok {
					index = clicked
					field = palette[index].Hex
					result = field
					return overlayDecision{Disposition: overlayCommit}
				}
			case "text":
				for _, r := range event.Text {
					if r == '#' && !strings.Contains(field, "#") {
						field = "#" + field
						continue
					}
					if len(field) < 7 && ((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
						field += strings.ToLower(string(r))
					}
				}
			}
			return overlayDecision{Disposition: overlayContinue}
		},
	})
	return result, decision.Disposition == overlayCommit
}

func colourPickerPalette() []pickerColour {
	levels := []int{0x00, 0x55, 0xaa, 0xff}
	out := make([]pickerColour, 0, 64)
	for r := 0; r < 4; r++ {
		for g := 0; g < 4; g++ {
			for b := 0; b < 4; b++ {
				red, green, blue := levels[r], levels[g], levels[b]
				hex := fmt.Sprintf("#%02x%02x%02x", red, green, blue)
				fg := "37"
				if red+green+blue > 384 {
					fg = "30"
				}
				out = append(out, pickerColour{
					Hex: hex,
					FG:  fg,
					BG:  fmt.Sprintf("48;2;%d;%d;%d", red, green, blue),
					R:   red,
					G:   green,
					B:   blue,
				})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		left := colourLuminance(out[i])
		right := colourLuminance(out[j])
		if left != right {
			return left < right
		}
		if out[i].R != out[j].R {
			return out[i].R < out[j].R
		}
		if out[i].G != out[j].G {
			return out[i].G < out[j].G
		}
		return out[i].B < out[j].B
	})
	return out
}

func colourLuminance(colour pickerColour) int {
	return 299*colour.R + 587*colour.G + 114*colour.B
}

func drawColorPicker(width, height int, palette []pickerColour, selected int, field string) {
	gridWidth, gridHeight := colorPickerGridSize()
	panelWidth := min(width, gridWidth+4)
	panelHeight := min(height, gridHeight+7)
	panelLeft := max(0, (width-panelWidth)/2)
	panelTop := max(0, (height-panelHeight)/2)
	left := min(width-1, panelLeft+2)
	top := min(height-1, panelTop+1)
	panelBG := "48;2;18;18;18"
	for row := 0; row < panelHeight; row++ {
		termPrintf("\033[0;37;%sm\033[%d;%dH%s", panelBG, panelTop+row+1, panelLeft+1, strings.Repeat(" ", panelWidth))
	}
	title := " COLOR "
	termPrintf("\033[0;37;%sm\033[%d;%dH%s", panelBG, panelTop+1, left+1, padRight(title, min(gridWidth, max(0, width-left))))
	for i, colour := range palette {
		row := i / 8
		col := i % 8
		x := left + col*colorPickerCellStrideX()
		y := top + row*colorPickerCellStrideY()
		if x+colorPickerCellWidth() > width || y >= height {
			continue
		}
		borderFG := "38;2;230;230;230"
		borderBG := "48;2;72;72;72"
		if i == selected {
			borderFG = "38;2;0;0;0"
			borderBG = "48;2;255;170;0"
		}
		termPrintf("\033[0;%s;%sm\033[%d;%dH▐", borderFG, borderBG, y+1, x+1)
		termPrintf("\033[0;%s;%sm  ", colour.FG, colour.BG)
		termPrintf("\033[0;%s;%sm▌", borderFG, borderBG)
	}
	fieldLabel := " HTML " + field
	termPrintf("\033[0;30;43m\033[%d;%dH%s", top+gridHeight+2, left+1, padRight(crop(fieldLabel, gridWidth), gridWidth))
	help := " arrows/mouse select  type hex  Enter choose  Esc cancel "
	termPrintf("\033[0;30;43m\033[%d;%dH%s", top+gridHeight+3, left+1, padRight(crop(help, gridWidth), gridWidth))
}

func colorPickerIndexAt(x, y, width, height int) (int, bool) {
	gridWidth, gridHeight := colorPickerGridSize()
	panelWidth := min(width, gridWidth+4)
	panelHeight := min(height, gridHeight+7)
	panelLeft := max(0, (width-panelWidth)/2)
	panelTop := max(0, (height-panelHeight)/2)
	left := min(width-1, panelLeft+2)
	top := min(height-1, panelTop+1)
	if x < left || x >= left+gridWidth || y < top || y >= top+gridHeight {
		return 0, false
	}
	localX := x - left
	localY := y - top
	if localX%colorPickerCellStrideX() >= colorPickerCellWidth() || localY%colorPickerCellStrideY() >= colorPickerCellHeight() {
		return 0, false
	}
	col := localX / colorPickerCellStrideX()
	row := localY / colorPickerCellStrideY()
	index := row*8 + col
	if index < 0 || index >= 64 {
		return 0, false
	}
	return index, true
}

func colorPickerCellWidth() int {
	return 4
}

func colorPickerCellHeight() int {
	return 1
}

func colorPickerCellStrideX() int {
	return colorPickerCellWidth() + 1
}

func colorPickerCellStrideY() int {
	return colorPickerCellHeight() + 1
}

func colorPickerGridSize() (int, int) {
	return 8*colorPickerCellStrideX() - 1, 8*colorPickerCellStrideY() - 1
}

func currentElementHexForElement(element Element) string {
	values, err := url.ParseQuery(element.Query)
	if err != nil {
		return ""
	}
	value := values.Get("fg")
	if element.Kind == "heading" {
		value = values.Get("header")
		if value == "" {
			value = values.Get("fg")
		}
	}
	if hex, ok := normalizeHexColour(value); ok {
		return hex
	}
	if code, ok := ansiFG(value); ok {
		return ansiCodeName(code, false)
	}
	return ""
}

func nearestPickerColourIndex(hex string, palette []pickerColour) int {
	r, g, b, ok := parseHexColour(hex)
	if !ok {
		return 63
	}
	bestIndex := 0
	bestDistance := int(^uint(0) >> 1)
	for index, colour := range palette {
		dr, dg, db := r-colour.R, g-colour.G, b-colour.B
		distance := dr*dr + dg*dg + db*db
		if distance < bestDistance {
			bestIndex = index
			bestDistance = distance
		}
	}
	return bestIndex
}

func setElementColour(element *Element, colour string) {
	if element == nil {
		return
	}
	if hex, ok := normalizeHexColour(colour); ok {
		if element.Kind == "heading" {
			element.Query = removeImageQueryKeys(element.Query, "fg")
			element.Query = setQueryValue(element.Query, "header", hex)
			return
		}
		if element.Kind == "code" {
			element.Query = setQueryValue(element.Query, "bg", hex)
			return
		}
		element.Query = setQueryValue(element.Query, "fg", hex)
	}
}

func setElementTextColour(element *Element, colour string) {
	if element == nil {
		return
	}
	if hex, ok := normalizeHexColour(colour); ok {
		element.Query = setQueryValue(element.Query, "fg", hex)
	}
}

func cloneSlide(slide Slide) Slide {
	copySlide := slide
	copySlide.Elements = append([]Element(nil), slide.Elements...)
	return copySlide
}

func cloneSlides(slides []Slide) []Slide {
	out := make([]Slide, len(slides))
	for i, slide := range slides {
		out[i] = cloneSlide(slide)
	}
	return out
}

const editHistoryLimit = 100

func commitSlideSnapshot(state *EditState, before, after Slide) bool {
	if state == nil || reflect.DeepEqual(before, after) {
		return false
	}
	snapshot := SlideSnapshot{Before: cloneSlide(before), After: cloneSlide(after)}
	state.Undo = append(state.Undo, snapshot)
	if len(state.Undo) > editHistoryLimit {
		state.Undo = append([]SlideSnapshot(nil), state.Undo[len(state.Undo)-editHistoryLimit:]...)
	}
	state.Redo = nil
	return true
}

func undoSlideSnapshot(state *EditState, slide *Slide) bool {
	if state == nil || slide == nil || len(state.Undo) == 0 {
		return false
	}
	snapshot := state.Undo[len(state.Undo)-1]
	state.Undo = state.Undo[:len(state.Undo)-1]
	state.Redo = append(state.Redo, snapshot)
	if len(state.Redo) > editHistoryLimit {
		state.Redo = append([]SlideSnapshot(nil), state.Redo[len(state.Redo)-editHistoryLimit:]...)
	}
	*slide = cloneSlide(snapshot.Before)
	return true
}

func redoSlideSnapshot(state *EditState, slide *Slide) bool {
	if state == nil || slide == nil || len(state.Redo) == 0 {
		return false
	}
	snapshot := state.Redo[len(state.Redo)-1]
	state.Redo = state.Redo[:len(state.Redo)-1]
	state.Undo = append(state.Undo, snapshot)
	if len(state.Undo) > editHistoryLimit {
		state.Undo = append([]SlideSnapshot(nil), state.Undo[len(state.Undo)-editHistoryLimit:]...)
	}
	*slide = cloneSlide(snapshot.After)
	return true
}

func commitDeckSnapshot(state *EditState, before, after []Slide, beforeIndex, afterIndex int) bool {
	if state == nil || reflect.DeepEqual(before, after) {
		return false
	}
	snapshot := DeckSnapshot{
		Before:      cloneSlides(before),
		After:       cloneSlides(after),
		BeforeIndex: beforeIndex,
		AfterIndex:  afterIndex,
	}
	state.DeckUndo = append(state.DeckUndo, snapshot)
	if len(state.DeckUndo) > editHistoryLimit {
		state.DeckUndo = append([]DeckSnapshot(nil), state.DeckUndo[len(state.DeckUndo)-editHistoryLimit:]...)
	}
	state.DeckRedo = nil
	return true
}

func commitFullDeckSnapshot(state *EditState, before, after Deck, beforeIndex, afterIndex int) bool {
	if state == nil || reflect.DeepEqual(before, after) {
		return false
	}
	snapshot := DeckSnapshot{
		Before: cloneSlides(before.Slides), After: cloneSlides(after.Slides),
		BeforeMasters: before.Masters.Clone(), AfterMasters: after.Masters.Clone(), HasMasters: true,
		BeforeIndex: beforeIndex, AfterIndex: afterIndex,
	}
	state.DeckUndo = append(state.DeckUndo, snapshot)
	if len(state.DeckUndo) > editHistoryLimit {
		state.DeckUndo = append([]DeckSnapshot(nil), state.DeckUndo[len(state.DeckUndo)-editHistoryLimit:]...)
	}
	state.DeckRedo = nil
	return true
}

func undoDeckSnapshot(state *EditState, deck *Deck) (int, bool) {
	if state == nil || deck == nil || len(state.DeckUndo) == 0 {
		return 0, false
	}
	snapshot := state.DeckUndo[len(state.DeckUndo)-1]
	state.DeckUndo = state.DeckUndo[:len(state.DeckUndo)-1]
	state.DeckRedo = append(state.DeckRedo, snapshot)
	if len(state.DeckRedo) > editHistoryLimit {
		state.DeckRedo = append([]DeckSnapshot(nil), state.DeckRedo[len(state.DeckRedo)-editHistoryLimit:]...)
	}
	deck.Slides = cloneSlides(snapshot.Before)
	if snapshot.HasMasters {
		deck.Masters = snapshot.BeforeMasters.Clone()
	}
	return max(0, min(len(deck.Slides)-1, snapshot.BeforeIndex)), true
}

func redoDeckSnapshot(state *EditState, deck *Deck) (int, bool) {
	if state == nil || deck == nil || len(state.DeckRedo) == 0 {
		return 0, false
	}
	snapshot := state.DeckRedo[len(state.DeckRedo)-1]
	state.DeckRedo = state.DeckRedo[:len(state.DeckRedo)-1]
	state.DeckUndo = append(state.DeckUndo, snapshot)
	if len(state.DeckUndo) > editHistoryLimit {
		state.DeckUndo = append([]DeckSnapshot(nil), state.DeckUndo[len(state.DeckUndo)-editHistoryLimit:]...)
	}
	deck.Slides = cloneSlides(snapshot.After)
	if snapshot.HasMasters {
		deck.Masters = snapshot.AfterMasters.Clone()
	}
	return max(0, min(len(deck.Slides)-1, snapshot.AfterIndex)), true
}

func insertElementAfter(slide *Slide, selected int, element Element) int {
	insertAt := selected + 1
	if selected < 0 || selected >= len(slide.Elements) {
		slide.Elements = append(slide.Elements, element)
		return len(slide.Elements) - 1
	}
	slide.Elements = append(slide.Elements, Element{})
	copy(slide.Elements[insertAt+1:], slide.Elements[insertAt:])
	slide.Elements[insertAt] = element
	return insertAt
}

func insertImageElement(slide *Slide, selected int, path string) int {
	element := Element{Kind: "image", Path: filepath.Clean(path)}
	return insertElementAfter(slide, selected, element)
}

func newShapeElement(shape string) Element {
	if shape == "" {
		shape = "circle"
	}
	query := ""
	query = setQueryValue(query, "shape", shape)
	switch shape {
	case "square":
		query = setShapeSize(query, 8, 4)
	case "triangle":
		query = setShapeSize(query, 12, 6)
	case "diamond":
		query = setShapeSize(query, 11, 6)
	default:
		query = setShapeSize(query, 10, 6)
	}
	return Element{Kind: "shape", Query: query}
}

func placeholderSlide() Slide {
	return Slide{Elements: []Element{
		{Kind: "heading", Level: 1, Text: "Your header here", Placeholder: true},
		{Kind: "text", Text: "Your text here", Placeholder: true},
	}}
}

func chooseImageFile() (string, bool) {
	script := `POSIX path of (choose file of type {"public.image"} with prompt "Select image")`
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return "", false
	}
	path := strings.TrimSpace(string(out))
	return path, path != ""
}

func copyImageToDeckDir(sourcePath, deckPath string) (string, bool) {
	deckDir := filepath.Dir(deckPath)
	sourceAbs, err := filepath.Abs(sourcePath)
	if err != nil {
		return "", false
	}
	sourceAbs = filepath.Clean(sourceAbs)
	if rel, err := filepath.Rel(deckDir, sourceAbs); err == nil && !strings.HasPrefix(rel, "..") {
		return sourceAbs, true
	}
	base := filepath.Base(sourceAbs)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	target := filepath.Join(deckDir, base)
	for i := 2; fileExists(target); i++ {
		target = filepath.Join(deckDir, fmt.Sprintf("%s-%d%s", name, i, ext))
	}
	in, err := os.Open(sourceAbs)
	if err != nil {
		return "", false
	}
	defer in.Close()
	out, err := os.Create(target)
	if err != nil {
		return "", false
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return "", false
	}
	if out.Close() != nil {
		return "", false
	}
	return target, true
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

type imagePlacementResult struct {
	Accepted bool
	Action   string
}

func playImagePlacementMode(slide *Slide, slideIndex, slideCount, imageIndex, width, height, page int, original Slide, clipboard **Element, persist func() bool) imagePlacementResult {
	width, height = terminalAuthoredSize()
	withFastImageRender(func() {
		normalizeImagePlacement(slide, imageIndex, width, height)
	})
	if imageIndex < 0 || imageIndex >= len(slide.Elements) {
		return imagePlacementResult{Accepted: true}
	}
	result := imagePlacementResult{}
	renderer := &liveSlideRenderer{}
	decision := runOverlayLoop(overlayLoopSpec{
		Draw: func(frame int) {
			if imageIndex < 0 || imageIndex >= len(slide.Elements) {
				return
			}
			width, height = terminalAuthoredSize()
			background := cloneSlide(*slide)
			background.Elements = append(background.Elements[:imageIndex], background.Elements[imageIndex+1:]...)
			withFastImageRender(func() {
				renderer.draw(background, width, height, page, frame, func(lines []Line) {
					drawImagePlacementPreview(slide.Elements[imageIndex], width, height, page)
					drawImagePlacementToolbar(width, height, slideIndex, slideCount, page, slidePageCount(background, width, height), imageScale(slide.Elements[imageIndex].Query))
				})
			})
		},
		Read: readImagePlacementKeyEvent,
		Handle: func(event KeyEvent) overlayDecision {
			if imageIndex < 0 || imageIndex >= len(slide.Elements) {
				result = imagePlacementResult{Accepted: true}
				return overlayDecision{Disposition: overlayCommit}
			}
			switch event.Action {
			case "quit":
				result = imagePlacementResult{Action: "quit"}
				return overlayDecision{Disposition: overlayPassthrough, Event: event}
			case "escape":
				*slide = original
				return overlayDecision{Disposition: overlayCancel}
			case "save":
				withFastImageRender(func() { normalizeImagePlacement(slide, imageIndex, width, height) })
				persist()
				result = imagePlacementResult{Accepted: true, Action: "save"}
				return overlayDecision{Disposition: overlayCommit}
			case "copy":
				withFastImageRender(func() { normalizeImagePlacement(slide, imageIndex, width, height) })
				element := clipboardElementFromSelection(*slide, imageIndex, width, height)
				*clipboard = &element
			case "cut":
				withFastImageRender(func() { normalizeImagePlacement(slide, imageIndex, width, height) })
				element := clipboardElementFromSelection(*slide, imageIndex, width, height)
				*clipboard = &element
				slide.Elements = append(slide.Elements[:imageIndex], slide.Elements[imageIndex+1:]...)
				persist()
				result = imagePlacementResult{Accepted: true}
				return overlayDecision{Disposition: overlayCommit}
			case "paste":
				if clipboard != nil && *clipboard != nil && (*clipboard).Kind == "image" {
					element := elementForPastePage(**clipboard, page, width, height)
					imageIndex = insertElementAfter(slide, imageIndex, element)
					persist()
					original = cloneSlide(*slide)
				}
			case "backspace":
				slide.Elements = append(slide.Elements[:imageIndex], slide.Elements[imageIndex+1:]...)
				persist()
				result = imagePlacementResult{Accepted: true}
				return overlayDecision{Disposition: overlayCommit}
			case "up", "down", "left", "right":
				withFastImageRender(func() { normalizeImagePlacement(slide, imageIndex, width, height) })
				moveImageElement(&slide.Elements[imageIndex], event.Action, width, height, page, 1)
			case "shift-up", "shift-down", "shift-left", "shift-right":
				withFastImageRender(func() { normalizeImagePlacement(slide, imageIndex, width, height) })
				moveImageElement(&slide.Elements[imageIndex], strings.TrimPrefix(event.Action, "shift-"), width, height, page, 10)
			case "align-left", "align-center", "align-right":
				alignElement(&slide.Elements[imageIndex], strings.TrimPrefix(event.Action, "align-"))
			case "settings":
				if query, ok := playImageSettingsDialog(*slide, imageIndex, width, height, page); ok {
					slide.Elements[imageIndex].Query = query
					persist()
					original = cloneSlide(*slide)
				}
			case "scale-up":
				scaleImageElement(&slide.Elements[imageIndex], 0.1)
				persist()
				original = cloneSlide(*slide)
			case "scale-down":
				scaleImageElement(&slide.Elements[imageIndex], -0.1)
				persist()
				original = cloneSlide(*slide)
			}
			return overlayDecision{Disposition: overlayContinue}
		},
	})
	if decision.Disposition == overlayPassthrough && decision.Event.Action == "quit" {
		return imagePlacementResult{Action: "quit"}
	}
	return result
}

func withFastImageRender(fn func()) {
	previous := fastImageRender
	fastImageRender = true
	defer func() { fastImageRender = previous }()
	fn()
}

func drawSelectedImageHighlight(lines []Line, imageIndex, width, height int) {
	drawSelectionUnderline(lines, imageIndex, width, height, true)
}

func drawSelectedSetHighlight(lines []Line, selection map[int]bool, active, width, height int) {
	for selected := range selection {
		if selected == active {
			continue
		}
		imageOnly := false
		for _, line := range lines {
			if line.Element == selected && line.Role == "image" {
				imageOnly = true
				break
			}
		}
		drawSelectionUnderlineColor(lines, selected, width, height, imageOnly, "36")
	}
}

func drawImagePlacementPreview(element Element, width, height, page int) {
	element = scaleElementForTerminal(element, terminalScaleX(width, height), terminalScaleY(width, height))
	placement := parseImagePlacement(element.Query)
	rows := renderFastASCIIImage(element.Path, element.Query, width, height)
	if len(rows) == 0 {
		return
	}
	imageWidth := maxLineDisplayWidth(rows)
	row, col := 0, 0
	if placement.top != nil {
		row = *placement.top - page*max(1, height)
	} else if placement.bottom != nil {
		row = height - len(rows) - *placement.bottom
	}
	if placement.hasHorizontalOffset() {
		col = placementLeftCol(placement, width, imageWidth)
	} else {
		switch placement.align {
		case "center":
			col = (width - imageWidth) / 2
		case "right":
			col = rightAlignedCol(width, imageWidth, 0)
		}
	}
	col = clampBlockCol(col, width, imageWidth)
	termPrint("\033[43m")
	for offset, text := range rows {
		if row+offset < 0 || row+offset >= height {
			continue
		}
		if strings.Contains(text, "\033[") {
			drawTransparentANSI(row+offset+1, col+1, text, width)
		} else {
			drawTransparentTextAt(row+offset+1, col+1, text, width)
		}
	}
	termPrint("\033[0m")
}

func drawImagePlacementToolbar(width, height, slideIndex, slideCount, page, pageCount int, scale float64) {
	if height <= 0 || width <= 0 {
		return
	}
	label := fmt.Sprintf("%s  scale %.1f", slideNumberLabel(ViewState{
		SlideIndex: slideIndex,
		SlideCount: slideCount,
		Page:       page,
		PageCount:  pageCount,
	}), scale)
	controls := " IMAGE  arrows move  Shift-arrows jump 10  +/- scale  s settings  < left  = center  > right  Enter save  Esc cancel  Backspace delete  q quit "
	drawToolbarLine(width, height, "43", controls, label)
}

type imageSettingField struct {
	Key    string
	Label  string
	Values []string
	Min    float64
	Max    float64
	Step   float64
}

var imageSettingFields = []imageSettingField{
	{Key: "glyph", Label: "Glyph", Values: []string{"blocks", "braille", "shade", "ascii", "dense"}},
	{Key: "shape", Label: "Shape", Values: []string{"subject", "contrast", "saturation", "luma", "alpha"}},
	{Key: "brightness", Label: "Brightness", Min: 0.2, Max: 2.0, Step: 0.1},
	{Key: "contrast", Label: "Contrast", Min: 0.2, Max: 2.0, Step: 0.1},
	{Key: "saturation", Label: "Saturation", Min: 0.0, Max: 2.0, Step: 0.1},
	{Key: "sharpness", Label: "Sharpness", Min: 0.2, Max: 2.0, Step: 0.1},
	{Key: "alpha", Label: "Alpha threshold", Min: 0, Max: 255, Step: 16},
}

var textSettingFields = []imageSettingField{
	{Key: "glyph", Label: "Glyph", Values: []string{"blocks", "braille", "shade", "ascii", "dense"}},
}

func playImageSettingsDialog(slide Slide, imageIndex, width, height, page int) (string, bool) {
	if imageIndex < 0 || imageIndex >= len(slide.Elements) {
		return "", false
	}
	selected := 0
	textSettings := slide.Elements[imageIndex].Kind != "image"
	fields := imageSettingFields
	title := " IMAGE SETTINGS "
	if textSettings {
		fields = textSettingFields
		title = " TEXT GLYPH "
	}
	query := normalizeImageSettingsQuery(slide.Elements[imageIndex].Query)
	if textSettings {
		query = normalizeTextSettingsQuery(slide.Elements[imageIndex].Query)
	}
	result := ""
	renderer := &liveSlideRenderer{}
	decision := runOverlayLoop(overlayLoopSpec{
		Draw: func(frame int) {
			width, height = terminalAuthoredSize()
			preview := cloneSlide(slide)
			preview.Elements[imageIndex].Query = query
			withFastImageRender(func() {
				renderer.draw(preview, width, height, page, frame, func(lines []Line) {
					if preview.Elements[imageIndex].Kind == "image" {
						drawSelectedImageHighlight(lines, imageIndex, width, height)
					} else {
						drawSelectedElementHighlight(lines, imageIndex, width, height)
					}
					drawImageSettingsDialog(width, height, query, selected, fields, title)
				})
			})
		},
		Read: readImageSettingsKeyEvent,
		Handle: func(event KeyEvent) overlayDecision {
			switch event.Action {
			case "quit", "escape":
				return overlayDecision{Disposition: overlayCancel}
			case "enter":
				if textSettings {
					result = compactTextSettingsQuery(query)
				} else {
					result = compactImageSettingsQuery(query)
				}
				return overlayDecision{Disposition: overlayCommit}
			case "up":
				selected = (selected - 1 + len(fields)) % len(fields)
			case "down":
				selected = (selected + 1) % len(fields)
			case "left":
				query = changeImageSetting(query, fields[selected], -1)
			case "right":
				query = changeImageSetting(query, fields[selected], 1)
			case "reset":
				query = resetImageSetting(query, fields[selected])
			case "mouse-click":
				if index, ok := imageSettingsFieldAt(event.X, event.Y, width, height, fields); ok {
					if index == selected {
						query = changeImageSetting(query, fields[selected], 1)
					} else {
						selected = index
					}
				}
			}
			return overlayDecision{Disposition: overlayContinue}
		},
	})
	return result, decision.Disposition == overlayCommit
}

func drawImageSettingsDialog(width, height int, query string, selected int, fields []imageSettingField, title string) {
	panelWidth := min(width, 74)
	panelHeight := min(height, len(fields)+6)
	left := max(0, (width-panelWidth)/2)
	top := max(0, (height-panelHeight)/2)
	panelBG := "48;2;18;18;18"
	for row := 0; row < panelHeight; row++ {
		termPrintf("\033[0;37;%sm\033[%d;%dH%s", panelBG, top+row+1, left+1, strings.Repeat(" ", panelWidth))
	}
	termPrintf("\033[0;37;%sm\033[%d;%dH%s", panelBG, top+1, left+3, title)
	for i, field := range fields {
		bg := panelBG
		fg := "37"
		if i == selected {
			bg = "43"
			fg = "30"
		}
		value := imageSettingDisplayValue(query, field)
		line := fmt.Sprintf("%-16s %s", field.Label, value)
		termPrintf("\033[0;%s;%sm\033[%d;%dH%s", fg, bg, top+3+i, left+3, padRight(crop(line, panelWidth-6), panelWidth-6))
	}
	help := " arrows adjust  r reset field  Enter save  Esc cancel "
	termPrintf("\033[0;30;43m\033[%d;%dH%s", top+panelHeight-1, left+3, padRight(crop(help, panelWidth-6), panelWidth-6))
}

func imageSettingsFieldAt(x, y, width, height int, fields []imageSettingField) (int, bool) {
	panelWidth := min(width, 74)
	panelHeight := min(height, len(fields)+6)
	left := max(0, (width-panelWidth)/2)
	top := max(0, (height-panelHeight)/2)
	index := y - (top + 2)
	if x < left+2 || x >= left+panelWidth-2 || index < 0 || index >= len(fields) {
		return 0, false
	}
	return index, true
}

func readImageSettingsKeyEvent() KeyEvent {
	var buf [32]byte
	n, _ := inputRead(buf[:])
	if n == 0 {
		return KeyEvent{}
	}
	b := buf[:n]
	if event := parseMouseEvent(b); event.Action != "" {
		return event
	}
	if event := editEscapeEvent(b); event.Action != "" {
		return event
	}
	if bytes.Contains(b, []byte{'\r'}) || bytes.Contains(b, []byte{'\n'}) {
		return KeyEvent{Action: "enter"}
	}
	if bytes.Contains(b, []byte{'q'}) {
		return KeyEvent{Action: "quit"}
	}
	if bytes.Contains(b, []byte{'r'}) {
		return KeyEvent{Action: "reset"}
	}
	if bytes.Contains(b, []byte{27, '[', 'A'}) {
		return KeyEvent{Action: "up"}
	}
	if bytes.Contains(b, []byte{27, '[', 'B'}) {
		return KeyEvent{Action: "down"}
	}
	if bytes.Contains(b, []byte{27, '[', 'C'}) {
		return KeyEvent{Action: "right"}
	}
	if bytes.Contains(b, []byte{27, '[', 'D'}) {
		return KeyEvent{Action: "left"}
	}
	return KeyEvent{}
}

func normalizeImageSettingsQuery(query string) string {
	values, _ := url.ParseQuery(query)
	if values.Get("glyph") == "" {
		values.Set("glyph", "blocks")
	}
	if values.Get("shape") == "" {
		values.Set("shape", "subject")
	}
	for _, key := range []string{"brightness", "contrast", "saturation", "sharpness"} {
		if values.Get(key) == "" {
			values.Set(key, "1.0")
		}
	}
	if values.Get("alpha") == "" {
		values.Set("alpha", "96")
	}
	return values.Encode()
}

func normalizeTextSettingsQuery(query string) string {
	values, _ := url.ParseQuery(query)
	if values.Get("glyph") == "" {
		values.Set("glyph", "blocks")
	}
	for _, key := range []string{"shape", "brightness", "contrast", "saturation", "sharpness", "alpha"} {
		values.Del(key)
	}
	return values.Encode()
}

func compactImageSettingsQuery(query string) string {
	values, _ := url.ParseQuery(query)
	defaults := map[string]string{
		"glyph":      "blocks",
		"shape":      "subject",
		"brightness": "1.0",
		"contrast":   "1.0",
		"saturation": "1.0",
		"sharpness":  "1.0",
		"alpha":      "96",
	}
	for key, value := range defaults {
		if values.Get(key) == value {
			values.Del(key)
		}
	}
	return values.Encode()
}

func compactTextSettingsQuery(query string) string {
	values, _ := url.ParseQuery(query)
	if values.Get("glyph") == "blocks" {
		values.Del("glyph")
	}
	for _, key := range []string{"shape", "brightness", "contrast", "saturation", "sharpness", "alpha"} {
		values.Del(key)
	}
	return values.Encode()
}

func imageSettingValue(query string, field imageSettingField) string {
	values, _ := url.ParseQuery(query)
	if len(field.Values) > 0 {
		value := values.Get(field.Key)
		if value == "" {
			value = field.Values[0]
		}
		return value
	}
	if field.Key == "alpha" {
		value := values.Get(field.Key)
		if value == "" {
			value = "96"
		}
		return value
	}
	value := values.Get(field.Key)
	if value == "" {
		value = "1.0"
	}
	return value
}

func imageSettingDisplayValue(query string, field imageSettingField) string {
	value := imageSettingValue(query, field)
	if len(field.Values) > 0 {
		return value
	}
	current, _ := strconv.ParseFloat(value, 64)
	steps := 18
	position := 0
	if field.Max > field.Min {
		position = int(math.Round((current - field.Min) / (field.Max - field.Min) * float64(steps-1)))
	}
	position = max(0, min(steps-1, position))
	var bar strings.Builder
	bar.WriteByte('[')
	for i := 0; i < steps; i++ {
		if i == position {
			bar.WriteRune('█')
		} else if i < position {
			bar.WriteRune('▓')
		} else {
			bar.WriteRune('░')
		}
	}
	bar.WriteByte(']')
	return fmt.Sprintf("%s %s", bar.String(), value)
}

func changeImageSetting(query string, field imageSettingField, direction int) string {
	values, _ := url.ParseQuery(query)
	if len(field.Values) > 0 {
		current := imageSettingValue(query, field)
		index := 0
		for i, value := range field.Values {
			if value == current {
				index = i
				break
			}
		}
		index = (index + direction + len(field.Values)) % len(field.Values)
		values.Set(field.Key, field.Values[index])
		return values.Encode()
	}
	current, _ := strconv.ParseFloat(imageSettingValue(query, field), 64)
	next := clampFloat(current+float64(direction)*field.Step, field.Min, field.Max)
	if field.Key == "alpha" {
		values.Set(field.Key, strconv.Itoa(int(math.Round(next))))
	} else {
		values.Set(field.Key, fmt.Sprintf("%.1f", next))
	}
	return values.Encode()
}

func resetImageSetting(query string, field imageSettingField) string {
	values, _ := url.ParseQuery(query)
	switch field.Key {
	case "glyph":
		values.Set(field.Key, "blocks")
	case "shape":
		values.Set(field.Key, "subject")
	case "alpha":
		values.Set(field.Key, "96")
	default:
		values.Set(field.Key, "1.0")
	}
	return values.Encode()
}

func normalizeImagePlacement(slide *Slide, imageIndex, width, height int) {
	lines := layout(*slide, width, height)
	top, left := 0, 0
	right := 0
	found := false
	for _, line := range lines {
		if line.Role != "image" || line.Element != imageIndex {
			continue
		}
		if !found {
			top, left = line.Row, line.Col
			found = true
		}
		top = min(top, line.Row)
		left = min(left, line.Col)
		right = max(right, line.Col+displayWidth(stripANSI(line.Text))-1)
	}
	if imageIndex < 0 || imageIndex >= len(slide.Elements) {
		return
	}
	if !found {
		return
	}
	query := setImageQueryInt(slide.Elements[imageIndex].Query, "top", max(0, top))
	query = setPlacementHorizontalPct(query, left, right, width)
	query = removeImageQueryKeys(query, "align", "left", "right", "bottom", "row_delta")
	slide.Elements[imageIndex].Query = query
}

func normalizeTextPlacement(slide *Slide, elementIndex, width, height int) {
	if elementIndex < 0 || elementIndex >= len(slide.Elements) {
		return
	}
	lines := layout(*slide, width, height)
	top, left := 0, 0
	right := 0
	found := false
	for _, line := range lines {
		if line.Element != elementIndex || line.Role == "image" || line.Role == "outline" {
			continue
		}
		if !found {
			top, left = line.Row, line.Col
			found = true
		}
		top = min(top, line.Row)
		left = min(left, line.Col)
		right = max(right, line.Col+displayWidth(stripANSI(line.Text))-1)
	}
	if !found {
		return
	}
	query := slide.Elements[elementIndex].Query
	query = setPlacementHorizontalPct(query, left, right, width)
	query = setImageQueryInt(query, "top", max(0, top))
	query = removeImageQueryKeys(query, "align", "left", "right", "bottom", "row_delta")
	slide.Elements[elementIndex].Query = query
}

func moveTextElement(slide *Slide, elementIndex int, direction string, width, height, step int) {
	if elementIndex < 0 || elementIndex >= len(slide.Elements) {
		return
	}
	normalizeTextPlacement(slide, elementIndex, width, height)
	placement := parseImagePlacement(slide.Elements[elementIndex].Query)
	top, left := 0, 0
	if placement.top != nil {
		top = *placement.top
	}
	if placement.leftPct != nil {
		left = int(math.Round(*placement.leftPct * float64(max(0, width-1))))
	} else if placement.left != nil {
		left = *placement.left
	}
	switch direction {
	case "up":
		top -= step
	case "down":
		top += step
	case "left":
		left -= step
	case "right":
		left += step
	}
	top = max(0, min(max(0, height-1), top))
	left = max(0, min(max(0, width-1), left))
	query := setImageQueryInt(slide.Elements[elementIndex].Query, "top", top)
	query = setPlacementHorizontalPct(query, left, left, width)
	query = removeImageQueryKeys(query, "align", "left", "right", "bottom", "row_delta")
	slide.Elements[elementIndex].Query = query
}

func moveSelectedElement(slide *Slide, elementIndex int, direction string, width, height, step int) {
	if elementIndex < 0 || elementIndex >= len(slide.Elements) {
		return
	}
	if isPositionedElement(slide.Elements[elementIndex]) || isEditableElement(slide.Elements[elementIndex]) {
		withFastImageRender(func() {
			if slide.Elements[elementIndex].Kind == "image" {
				normalizeImagePlacement(slide, elementIndex, width, height)
			} else {
				normalizeTextPlacement(slide, elementIndex, width, height)
			}
		})
		moveElementByBounds(slide, elementIndex, direction, width, height, step)
	}
}

func moveElementByBounds(slide *Slide, elementIndex int, direction string, width, height, step int) {
	top, left, right, ok := elementBounds(*slide, elementIndex, width, height)
	if !ok {
		return
	}
	switch direction {
	case "up":
		top -= step
	case "down":
		top += step
	case "left":
		left -= step
		right -= step
	case "right":
		left += step
		right += step
	}
	top = max(0, min(max(0, height-1), top))
	blockWidth := max(1, right-left+1)
	left = clampBlockCol(left, width, blockWidth)
	right = left + blockWidth - 1
	query := slide.Elements[elementIndex].Query
	query = setPlacementHorizontalPct(query, left, right, width)
	query = setImageQueryInt(query, "top", top)
	query = removeImageQueryKeys(query, "align", "left", "right", "bottom", "row_delta")
	slide.Elements[elementIndex].Query = query
}

func elementBounds(slide Slide, elementIndex, width, height int) (int, int, int, bool) {
	lines := layout(slide, width, height)
	top, left, right := 0, 0, 0
	found := false
	for _, line := range lines {
		if line.Element != elementIndex || line.Role == "outline" || line.Row < 0 || line.Row >= height {
			continue
		}
		lineRight := line.Col + displayWidth(stripANSI(line.Text)) - 1
		if !found {
			top, left, right = line.Row, line.Col, lineRight
			found = true
			continue
		}
		top = min(top, line.Row)
		left = min(left, line.Col)
		right = max(right, lineRight)
	}
	return top, left, right, found
}

func elementFullBounds(slide Slide, elementIndex, width, height int) (int, int, int, bool) {
	top, bottom, left, _, ok := elementFullBox(slide, elementIndex, width, height)
	return top, bottom, left, ok
}

func elementFullBox(slide Slide, elementIndex, width, height int) (int, int, int, int, bool) {
	lines := layout(slide, width, height)
	top, bottom, left, right := 0, 0, 0, 0
	found := false
	for _, line := range lines {
		if line.Element != elementIndex || line.Role == "outline" {
			continue
		}
		lineRight := line.Col + displayWidth(stripANSI(line.Text)) - 1
		lineLeft := min(line.Col, lineRight)
		if !found {
			top, bottom, left, right = line.Row, line.Row, lineLeft, max(line.Col, lineRight)
			found = true
			continue
		}
		top = min(top, line.Row)
		bottom = max(bottom, line.Row)
		left = min(left, lineLeft)
		right = max(right, max(line.Col, lineRight))
	}
	return top, bottom, left, right, found
}

func naturalElementTop(slide Slide, elementIndex, width, height int, query string) (int, bool) {
	if elementIndex < 0 || elementIndex >= len(slide.Elements) {
		return 0, false
	}
	clone := cloneSlide(slide)
	clone.Elements[elementIndex].Query = removeImageQueryKeys(query, "top", "bottom", "row_delta")
	top, _, _, ok := elementBounds(clone, elementIndex, width, height)
	return top, ok
}

func setPlacementRowDeltaForTop(slide Slide, elementIndex int, query string, top, width, height int) string {
	if elementIndex < 0 || elementIndex >= len(slide.Elements) || width <= 0 || height <= 0 {
		return removeImageQueryKeys(query, "top", "bottom", "row_delta")
	}
	return setPlacementRowDelta(query, max(0, top))
}

func normalizeFlowRelativePlacements(slide *Slide, width, height int) {
	if slide == nil {
		return
	}
	original := cloneSlide(*slide)
	for index, element := range slide.Elements {
		placement := parseImagePlacement(element.Query)
		if !placement.hasHorizontalOffset() || placement.rowDelta != nil || !placement.hasVerticalOffset() {
			continue
		}
		top, _, _, ok := elementBounds(original, index, width, height)
		if !ok {
			continue
		}
		query := setPlacementRowDeltaForTop(original, index, element.Query, top, width, height)
		query = removeImageQueryKeys(query, "top", "bottom")
		slide.Elements[index].Query = query
	}
}

func alignElement(element *Element, align string) {
	if element == nil {
		return
	}
	switch align {
	case "left", "center", "right":
	default:
		return
	}
	values, _ := url.ParseQuery(element.Query)
	values.Set("align", align)
	values.Del("left")
	values.Del("right")
	values.Del("left_pct")
	values.Del("right_pct")
	element.Query = values.Encode()
}

func initializeInsertedImagePlacement(slide *Slide, imageIndex int) {
	if imageIndex < 0 || imageIndex >= len(slide.Elements) {
		return
	}
	query := slide.Elements[imageIndex].Query
	query = setImageQueryInt(query, "top", 1)
	query = setImageQueryInt(query, "left", 1)
	query = setImageQueryFloat(query, "scale", 1.0)
	query = removeImageQueryKeys(query, "align", "left_pct", "right", "right_pct", "bottom", "row_delta")
	slide.Elements[imageIndex].Query = query
}

func insertedTextPlacementAnchor(slide Slide, selected, width, height, page int) (int, int) {
	top := max(0, page*max(1, height)+1)
	left := 1
	if selected < 0 || selected >= len(slide.Elements) {
		return top, left
	}
	minRow, maxRow, minCol, ok := elementFullBounds(slide, selected, width, height)
	if !ok {
		return top, left
	}
	pageTop := page * max(1, height)
	pageBottom := pageTop + max(1, height) - 1
	if maxRow < pageTop || minRow > pageBottom {
		return top, left
	}
	top = min(pageBottom, maxRow+1)
	left = max(0, minCol)
	return top, left
}

func initializeInsertedTextPlacement(slide *Slide, elementIndex, top, left, width int) {
	if elementIndex < 0 || elementIndex >= len(slide.Elements) {
		return
	}
	query := slide.Elements[elementIndex].Query
	query = setImageQueryInt(query, "top", max(0, top))
	query = setPlacementHorizontalPct(query, left, left+max(0, displayWidth(slide.Elements[elementIndex].Text)-1), width)
	query = removeImageQueryKeys(query, "align", "left", "right", "bottom", "row_delta")
	slide.Elements[elementIndex].Query = query
}

func initializeInsertedShapePlacement(slide *Slide, elementIndex, top, left, width int) {
	if elementIndex < 0 || elementIndex >= len(slide.Elements) {
		return
	}
	shapeWidth, _ := shapeSize(slide.Elements[elementIndex])
	query := slide.Elements[elementIndex].Query
	query = setImageQueryInt(query, "top", max(0, top))
	query = setPlacementHorizontalPct(query, left, left+shapeWidth-1, width)
	query = removeImageQueryKeys(query, "align", "left", "right", "bottom", "row_delta")
	slide.Elements[elementIndex].Query = query
}

func clipboardElementFromSelection(slide Slide, elementIndex, width, height int) Element {
	if elementIndex < 0 || elementIndex >= len(slide.Elements) {
		return Element{}
	}
	element := slide.Elements[elementIndex]
	top, _, left, right, ok := elementFullBox(slide, elementIndex, width, height)
	if !ok {
		return element
	}
	element.Query = setImageQueryInt(element.Query, "top", top)
	element.Query = setPlacementHorizontalPct(element.Query, left, right, width)
	element.Query = removeImageQueryKeys(element.Query, "align", "left", "right", "bottom", "row_delta")
	return element
}

func elementForPastePage(element Element, page, width, height int) Element {
	if height <= 0 {
		return element
	}
	page = max(0, page)
	placement := parseImagePlacement(element.Query)
	localTop := 0
	switch {
	case placement.top != nil:
		localTop = positiveMod(*placement.top, height)
	case placement.bottom != nil:
		localTop = max(0, height-elementLayoutHeight(element, width, height)-*placement.bottom)
	case placement.rowDelta != nil:
		localTop = max(0, *placement.rowDelta)
	default:
		if page == 0 {
			return element
		}
	}
	element.Query = setImageQueryInt(element.Query, "top", page*height+localTop)
	element.Query = removeImageQueryKeys(element.Query, "bottom", "row_delta")
	return element
}

func elementLayoutHeight(element Element, width, height int) int {
	if element.Kind == "image" {
		return max(1, len(renderASCIIImage(element.Path, element.Query, width, height)))
	}
	return max(1, len(layoutElementRows(element, width)))
}

func positiveMod(value, base int) int {
	if base <= 0 {
		return value
	}
	out := value % base
	if out < 0 {
		out += base
	}
	return out
}

func moveImageElement(element *Element, direction string, width, height, page, step int) {
	placement := parseImagePlacement(element.Query)
	top, left := 0, 0
	pageTop := page * max(1, height)
	if placement.top != nil {
		top = *placement.top - pageTop
	}
	rows := renderASCIIImage(element.Path, element.Query, width, height)
	imageWidth := max(1, maxLineDisplayWidth(rows))
	left = placementLeftCol(placement, width, imageWidth)
	right := left + imageWidth - 1
	switch direction {
	case "up":
		top -= step
	case "down":
		top += step
	case "left":
		left -= step
		right -= step
	case "right":
		left += step
		right += step
	}
	top = max(0, min(max(0, height-1), top))
	left = clampBlockCol(left, width, imageWidth)
	right = left + imageWidth - 1
	query := setImageQueryInt(element.Query, "top", pageTop+top)
	query = setPlacementHorizontalPct(query, left, right, width)
	query = removeImageQueryKeys(query, "align", "left", "right", "bottom", "row_delta")
	element.Query = query
}

func scaleImageElement(element *Element, delta float64) {
	scale := clampFloat(imageScale(element.Query)+delta, 0.1, 1.0)
	element.Query = setImageQueryFloat(element.Query, "scale", scale)
}

func resizeShapeElement(element *Element, delta int) {
	if element == nil || element.Kind != "shape" {
		return
	}
	w, h := shapeSize(*element)
	w = max(1, w+delta*2)
	h = max(1, h+delta)
	element.Query = setShapeSize(element.Query, w, h)
}

func beginElementResize(slide *Slide, elementIndex, width, height int) {
	if slide == nil || elementIndex < 0 || elementIndex >= len(slide.Elements) {
		return
	}
	element := &slide.Elements[elementIndex]
	switch {
	case element.Kind == "image":
		w, h := measuredElementSize(*slide, elementIndex, width, height, 16, 8)
		element.Query = setImageResizeSize(element.Query, w, h)
		top, _, left, _, ok := elementFullBox(*slide, elementIndex, width, height)
		if ok {
			element.Query = setElementTopLeft(element.Query, top, left)
		}
	case isEditableElement(*element):
		w, _ := measuredElementSize(*slide, elementIndex, width, height, 20, 1)
		element.Query = setImageQueryInt(element.Query, "width", max(4, w))
		top, _, left, _, ok := elementFullBox(*slide, elementIndex, width, height)
		if ok {
			element.Query = setElementTopLeft(element.Query, top, left)
		}
	}
}

func resizeElementUniform(slide *Slide, elementIndex, width, height, delta int) {
	if slide == nil || elementIndex < 0 || elementIndex >= len(slide.Elements) {
		return
	}
	element := &slide.Elements[elementIndex]
	switch {
	case element.Kind == "shape":
		resizeShapeElement(element, delta)
	case element.Kind == "image":
		w, h := measuredElementSize(*slide, elementIndex, width, height, 16, 8)
		element.Query = setImageResizeSize(element.Query, max(1, w+delta*2), max(1, h+delta))
	case isEditableElement(*element):
		changeTextLevel(element, delta)
	}
}

func resizeElementByDirection(slide *Slide, elementIndex int, direction string, width, height int, shrink bool) {
	if slide == nil || elementIndex < 0 || elementIndex >= len(slide.Elements) {
		return
	}
	element := &slide.Elements[elementIndex]
	if element.Kind == "shape" {
		stretchShapeElement(slide, elementIndex, direction, width, height, shrink)
		return
	}
	top, bottom, left, right, ok := elementFullBox(*slide, elementIndex, width, height)
	if !ok {
		top, bottom, left, right = 0, 0, 0, 0
	}
	switch {
	case element.Kind == "image":
		w, h := max(1, right-left+1), max(1, bottom-top+1)
		left, top, w, h = resizeBoxByDirection(left, top, w, h, width, height, direction, shrink, 2, 1, 1, 1)
		element.Query = setImageResizeSize(element.Query, w, h)
		element.Query = setElementTopLeft(element.Query, top, left)
	case isEditableElement(*element):
		w := max(4, right-left+1)
		h := max(1, bottom-top+1)
		switch direction {
		case "left", "right":
			left, _, w, _ = resizeBoxByDirection(left, top, w, h, width, height, direction, shrink, 4, 1, 4, 1)
			element.Query = setImageQueryInt(element.Query, "width", w)
			element.Query = setElementTopLeft(element.Query, top, left)
		case "up", "down":
			sizeDelta := 1
			if shrink {
				sizeDelta = -1
			}
			if direction == "up" || direction == "down" {
				oldTop, oldBottom := top, bottom
				changeTextLevel(element, sizeDelta)
				newTop, newBottom, _, _, ok := elementFullBox(*slide, elementIndex, width, height)
				if !ok {
					return
				}
				newH := max(1, newBottom-newTop+1)
				switch {
				case !shrink && direction == "up":
					if oldTop <= 0 && newH > h {
						changeTextLevel(element, -sizeDelta)
						return
					}
					top = oldBottom - newH + 1
				case shrink && direction == "down":
					top = oldTop + (h - newH)
				default:
					top = oldTop
				}
				if top < 0 || top+newH > height {
					changeTextLevel(element, -sizeDelta)
					return
				}
				element.Query = setElementTopLeft(element.Query, top, left)
			}
		}
	}
}

func resizeElementByAxisMode(slide *Slide, elementIndex int, direction string, width, height int, mode string) {
	switch mode {
	case axisScaleStretchHorizontal:
		if direction == "left" || direction == "right" {
			resizeElementByDirection(slide, elementIndex, direction, width, height, false)
		}
	case axisScaleShrinkHorizontal:
		if direction == "left" || direction == "right" {
			resizeElementByDirection(slide, elementIndex, direction, width, height, true)
		}
	case axisScaleStretchVertical:
		if direction == "up" || direction == "down" {
			resizeElementByDirection(slide, elementIndex, direction, width, height, false)
		}
	case axisScaleShrinkVertical:
		if direction == "up" || direction == "down" {
			resizeElementByDirection(slide, elementIndex, direction, width, height, true)
		}
	}
}

func resizeBoxByDirection(left, top, w, h, viewportW, viewportH int, direction string, shrink bool, stepX, stepY, minW, minH int) (int, int, int, int) {
	stepX = max(1, stepX)
	stepY = max(1, stepY)
	minW = max(1, minW)
	minH = max(1, minH)
	right := left + w - 1
	bottom := top + h - 1
	if shrink {
		switch direction {
		case "right":
			if w > minW {
				left += min(stepX, w-minW)
				w -= min(stepX, w-minW)
			}
		case "left":
			w = max(minW, w-stepX)
		case "down":
			if h > minH {
				top += min(stepY, h-minH)
				h -= min(stepY, h-minH)
			}
		case "up":
			h = max(minH, h-stepY)
		}
		return max(0, left), max(0, top), max(minW, w), max(minH, h)
	}
	switch direction {
	case "right":
		if right+stepX < viewportW {
			w += stepX
		}
	case "left":
		if left-stepX >= 0 {
			left -= stepX
			w += stepX
		}
	case "down":
		if bottom+stepY < viewportH {
			h += stepY
		}
	case "up":
		if top-stepY >= 0 {
			top -= stepY
			h += stepY
		}
	}
	return max(0, left), max(0, top), max(minW, w), max(minH, h)
}

func measuredElementSize(slide Slide, elementIndex, width, height, fallbackW, fallbackH int) (int, int) {
	top, bottom, left, right, ok := elementFullBox(slide, elementIndex, width, height)
	if ok {
		return max(1, right-left+1), max(1, bottom-top+1)
	}
	if elementIndex >= 0 && elementIndex < len(slide.Elements) {
		return elementSizeQuery(slide.Elements[elementIndex], fallbackW, fallbackH)
	}
	return fallbackW, fallbackH
}

func elementSizeQuery(element Element, fallbackW, fallbackH int) (int, int) {
	values, _ := url.ParseQuery(element.Query)
	w := fallbackW
	h := fallbackH
	if parsed, err := strconv.Atoi(values.Get("width")); err == nil && parsed > 0 {
		w = parsed
	}
	if parsed, err := strconv.Atoi(values.Get("height")); err == nil && parsed > 0 {
		h = parsed
	}
	return max(1, w), max(1, h)
}

func setElementSize(query string, width, height int) string {
	query = setImageQueryInt(query, "width", max(1, width))
	query = setImageQueryInt(query, "height", max(1, height))
	return query
}

func setElementTopLeft(query string, top, left int) string {
	query = setImageQueryInt(query, "top", max(0, top))
	query = setImageQueryInt(query, "left", max(0, left))
	return removeImageQueryKeys(query, "align", "left_pct", "right", "right_pct", "bottom", "row_delta")
}

func setImageResizeSize(query string, width, height int) string {
	query = setElementSize(query, width, height)
	query = setQueryValue(query, "stretch", "1")
	query = removeImageQueryKeys(query, "scale")
	return query
}

func stretchShapeElement(slide *Slide, elementIndex int, direction string, width, height int, shrink bool) {
	if slide == nil || elementIndex < 0 || elementIndex >= len(slide.Elements) || slide.Elements[elementIndex].Kind != "shape" {
		return
	}
	normalizeTextPlacement(slide, elementIndex, width, height)
	element := &slide.Elements[elementIndex]
	top, _, left, _, ok := elementFullBox(*slide, elementIndex, width, height)
	if !ok {
		top = 0
		left = 0
	}
	w, h := shapeSize(*element)
	left, top, w, h = resizeBoxByDirection(left, top, w, h, width, height, direction, shrink, 1, 1, 1, 1)
	element.Query = setShapeSize(element.Query, w, h)
	element.Query = setElementTopLeft(element.Query, max(0, top), max(0, left))
}

func shapeSize(element Element) (int, int) {
	values, _ := url.ParseQuery(element.Query)
	w := max(1, intQueryDefault(values, "width", 12))
	h := max(1, intQueryDefault(values, "height", 6))
	return w, h
}

func setShapeSize(query string, width, height int) string {
	query = setImageQueryInt(query, "width", max(1, width))
	query = setImageQueryInt(query, "height", max(1, height))
	return query
}

func imageScale(query string) float64 {
	values, err := url.ParseQuery(query)
	if err != nil {
		return 1.0
	}
	if parsed, err := strconv.ParseFloat(values.Get("scale"), 64); err == nil && parsed > 0 {
		return clampFloat(parsed, 0.1, 1.0)
	}
	return 1.0
}

func setImageQueryInt(query, key string, value int) string {
	values, _ := url.ParseQuery(query)
	values.Set(key, strconv.Itoa(value))
	return values.Encode()
}

func setPlacementHorizontalPct(query string, left, right, width int) string {
	values, _ := url.ParseQuery(query)
	denominator := max(1, width-1)
	values.Del("left")
	values.Del("right")
	values.Del("left_pct")
	values.Del("right_pct")
	left = max(0, left)
	right = max(left, right)
	rightGap := max(0, width-1-right-rightEdgeGutter(width, right-left+1))
	if rightGap < left {
		percent := clampFloat(float64(rightGap)/float64(denominator), 0, 1)
		values.Set("right_pct", fmt.Sprintf("%.6f", percent))
	} else {
		percent := clampFloat(float64(left)/float64(denominator), 0, 1)
		values.Set("left_pct", fmt.Sprintf("%.6f", percent))
	}
	return values.Encode()
}

func setPlacementRowDelta(query string, delta int) string {
	values, _ := url.ParseQuery(query)
	values.Del("top")
	values.Del("bottom")
	values.Del("row_delta")
	if delta != 0 {
		values.Set("row_delta", strconv.Itoa(delta))
	}
	return values.Encode()
}

func setImageQueryFloat(query, key string, value float64) string {
	values, _ := url.ParseQuery(query)
	values.Set(key, fmt.Sprintf("%.1f", clampFloat(value, 0.1, 1.0)))
	return values.Encode()
}

func setQueryValue(query, key, value string) string {
	values, _ := url.ParseQuery(query)
	values.Set(key, value)
	return values.Encode()
}

func toggleElementOutline(element *Element) {
	if element == nil || !isSelectableElement(*element) {
		return
	}
	values, _ := url.ParseQuery(element.Query)
	switch values.Get("outline") {
	case "":
		values.Set("outline", "1")
	case "1":
		values.Set("outline", "dark")
	default:
		values.Del("outline")
	}
	element.Query = values.Encode()
}

func elementHasOutline(element Element) bool {
	values, err := url.ParseQuery(element.Query)
	return err == nil && values.Get("outline") != ""
}

func toggleShapeTransparency(element *Element) {
	if element == nil || element.Kind != "shape" {
		return
	}
	values, _ := url.ParseQuery(element.Query)
	if values.Get("transparent") == "1" {
		values.Del("transparent")
	} else {
		values.Set("transparent", "1")
	}
	element.Query = values.Encode()
}

func elementTransparent(element Element) bool {
	values, err := url.ParseQuery(element.Query)
	return err == nil && values.Get("transparent") == "1"
}

func outlineLineQuery(query string) string {
	values, _ := url.ParseQuery(query)
	switch values.Get("outline") {
	case "dark":
		values.Set("fg", "#555555")
	default:
		values.Set("fg", "#f3efe0")
	}
	values.Del("link")
	values.Del("slide")
	return values.Encode()
}

func removeImageQueryKeys(query string, keys ...string) string {
	values, _ := url.ParseQuery(query)
	for _, key := range keys {
		values.Del(key)
	}
	return values.Encode()
}

func playSearchMode(slides []Slide, current, page, width, height int, lastQuery *string) (int, int) {
	query := ""
	if lastQuery != nil {
		query = *lastQuery
	}
	originSlide, originPage := current, page
	status := ""
	if query != "" {
		if matchSlide, matchPage, ok := findForwardMatch(slides, width, height, originSlide, originPage, query); ok {
			current, page = matchSlide, matchPage
		} else {
			status = "not found"
		}
	}
	renderer := &liveSlideRenderer{}
	runOverlayLoop(overlayLoopSpec{
		Draw: func(frame int) {
			width, height = terminalAuthoredSize()
			renderer.draw(slides[current], width, height, page, frame, func(lines []Line) {
				drawSearchHighlights(lines, slides[current], width, height, query)
				drawSearchField(width, height, query, status)
			})
		},
		Read: readSearchKeyEvent,
		Handle: func(event KeyEvent) overlayDecision {
			switch event.Action {
			case "escape":
				if lastQuery != nil {
					*lastQuery = ""
				}
				return overlayDecision{Disposition: overlayCancel}
			case "enter":
				if query == "" {
					return overlayDecision{Disposition: overlayCommit}
				}
				if lastQuery != nil {
					*lastQuery = query
				}
				if matchSlide, matchPage, ok := findForwardMatch(slides, width, height, current, page, query); ok {
					current, page = matchSlide, matchPage
					status = ""
				} else {
					status = "not found"
				}
				return overlayDecision{Disposition: overlayContinue}
			case "backspace":
				if len(query) > 0 {
					rs := []rune(query)
					query = string(rs[:len(rs)-1])
				}
			case "text":
				query += event.Text
			}
			status = ""
			if query == "" {
				return overlayDecision{Disposition: overlayContinue}
			}
			if lastQuery != nil {
				*lastQuery = query
			}
			if matchSlide, matchPage, ok := findForwardMatch(slides, width, height, originSlide, originPage, query); ok {
				current, page = matchSlide, matchPage
			} else {
				status = "not found"
				current, page = originSlide, originPage
			}
			return overlayDecision{Disposition: overlayContinue}
		},
	})
	return current, page
}

func playLinkInput(slide Slide, width, height, page int, current string, slideCount int) (string, bool) {
	query := current
	status := ""
	result := ""
	renderer := &liveSlideRenderer{}
	decision := runOverlayLoop(overlayLoopSpec{
		Draw: func(frame int) {
			width, height = terminalAuthoredSize()
			renderer.draw(slide, width, height, page, frame, func(lines []Line) { drawLinkField(width, height, query, status) })
		},
		Read: readLinkInputKeyEvent,
		Handle: func(event KeyEvent) overlayDecision {
			switch event.Action {
			case "escape":
				return overlayDecision{Disposition: overlayCancel}
			case "enter":
				if strings.TrimSpace(query) == "" {
					return overlayDecision{Disposition: overlayCommit}
				}
				if link, ok := normalizeLinkValue(query, slideCount); ok {
					result = link
					return overlayDecision{Disposition: overlayCommit}
				}
				status = "invalid URL or slide"
			case "backspace":
				rs := []rune(query)
				if len(rs) > 0 {
					query = string(rs[:len(rs)-1])
					status = ""
				}
			case "text":
				query += event.Text
				status = ""
			}
			return overlayDecision{Disposition: overlayContinue}
		},
	})
	return result, decision.Disposition == overlayCommit
}

func drawLinkField(width, height int, query, status string) {
	if height <= 0 {
		return
	}
	prompt := "link " + query
	if status != "" {
		prompt += "  " + status
	}
	prompt = crop(prompt, width)
	termPrintf("\033[0;30;43m\033[%d;1H%s", height, padRight(prompt, width))
}

func playJumpMode(slides []Slide, current, page, width, height int) (int, int) {
	query := ""
	status := ""
	targetSlide, targetPage := current, page
	renderer := &liveSlideRenderer{}
	runOverlayLoop(overlayLoopSpec{
		Draw: func(frame int) {
			width, height = terminalAuthoredSize()
			renderer.draw(slides[current], width, height, page, frame, func(lines []Line) { drawJumpField(width, height, query, status) })
		},
		Read: readJumpKeyEvent,
		Handle: func(event KeyEvent) overlayDecision {
			switch event.Action {
			case "escape":
				return overlayDecision{Disposition: overlayCancel}
			case "enter":
				if query != "" {
					targetSlide, targetPage = resolveJumpTarget(slides, width, height, query)
				}
				return overlayDecision{Disposition: overlayCommit}
			case "backspace":
				if len(query) > 0 {
					rs := []rune(query)
					query = string(rs[:len(rs)-1])
				}
				status = ""
			case "text":
				query = appendJumpInput(query, event.Text)
				status = ""
			}
			return overlayDecision{Disposition: overlayContinue}
		},
	})
	return targetSlide, targetPage
}

func appendJumpInput(query, text string) string {
	for _, r := range text {
		if r >= '0' && r <= '9' {
			query += string(r)
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			if query == "" || jumpHasSuffix(query) {
				continue
			}
			query += strings.ToLower(string(r))
		}
	}
	return query
}

func jumpHasSuffix(query string) bool {
	for _, r := range query {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

func resolveJumpTarget(slides []Slide, width, height int, query string) (int, int) {
	numberPart := query
	suffix := rune(0)
	for index, r := range query {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			numberPart = query[:index]
			suffix = []rune(strings.ToLower(string(r)))[0]
			break
		}
	}
	target, err := strconv.Atoi(numberPart)
	if err != nil {
		target = 1
	}
	target = max(1, min(len(slides), target))
	slideIndex := target - 1
	pageCount := max(1, slidePageCount(slides[slideIndex], width, height))
	page := 0
	if suffix != 0 {
		page = int(suffix - 'a')
		page = max(0, min(pageCount-1, page))
	}
	return slideIndex, page
}

func drawSearchHighlights(lines []Line, slide Slide, width, height int, query string) {
	if query == "" {
		return
	}
	startRows := map[int]int{}
	for _, line := range lines {
		if line.Element < 0 || line.Row < 0 || line.Row >= height {
			continue
		}
		if _, ok := startRows[line.Element]; !ok || line.Row < startRows[line.Element] {
			startRows[line.Element] = line.Row
		}
	}
	needle := strings.ToLower(query)
	for elementIndex, startRow := range startRows {
		if elementIndex < 0 || elementIndex >= len(slide.Elements) {
			continue
		}
		element := slide.Elements[elementIndex]
		haystack := strings.ToLower(element.Text)
		searchFrom := 0
		for {
			matchAt := strings.Index(haystack[searchFrom:], needle)
			if matchAt < 0 {
				break
			}
			matchStart := searchFrom + matchAt
			matchEnd := matchStart + len([]rune(query))
			drawRenderedMatch(element, width, height, startRow, matchStart, matchEnd)
			searchFrom = matchStart + max(1, len([]rune(query)))
		}
	}
}

func drawRenderedMatch(element Element, width, height, startRow, matchStart, matchEnd int) {
	renderedRows := editableRowsForElementPrefix(element, width, len([]rune(element.Text)))
	if len(renderedRows) == 0 {
		return
	}
	if element.Kind == "heading" && !rendersAsTextImage(element) {
		drawRenderedHeadingMatch(element, renderedRows, width, height, startRow, matchStart, matchEnd)
		return
	}
	startOffset, startCol := renderedOffsetForCursor(element, width, matchStart)
	endOffset, endCol := renderedOffsetForCursor(element, width, matchEnd)
	if endOffset < startOffset {
		return
	}
	startCol++
	endCol++
	for offset := startOffset; offset <= endOffset && offset < len(renderedRows); offset++ {
		row := startRow + offset
		if row < 0 || row >= height {
			continue
		}
		from := 0
		if offset == startOffset {
			from = startCol
		}
		to := displayWidth(renderedRows[offset])
		if offset == endOffset {
			to = endCol
		}
		if to <= from {
			continue
		}
		segment := runeSliceWithPadding(renderedRows[offset], from, to)
		if segment == "" {
			continue
		}
		termPrintf("\033[0;30;43m\033[%d;%dH%s", row+1, from+1, segment)
	}
}

func drawRenderedHeadingMatch(element Element, renderedRows []string, width, height, startRow, matchStart, matchEnd int) {
	from := matchStart*editGlyphWidth(element) + 1
	to := matchEnd*editGlyphWidth(element) + 1
	if to <= from {
		return
	}
	from = min(max(0, from), max(0, width-1))
	to = min(max(from, to), width)
	for offset, rowText := range renderedRows {
		row := startRow + offset
		if row < 0 || row >= height {
			continue
		}
		segment := runeSliceWithPadding(rowText, from, to)
		if segment == "" {
			continue
		}
		termPrintf("\033[0;30;43m\033[%d;%dH%s", row+1, from+1, segment)
	}
}

func renderedOffsetForCursor(element Element, width, cursor int) (int, int) {
	cursor = max(0, min(cursor, len([]rune(element.Text))))
	if element.Kind == "heading" && !rendersAsTextImage(element) {
		return 0, cursor * editGlyphWidth(element)
	}
	if element.Kind == "code" && !rendersAsTextImage(element) {
		visualLine, col := codeCursorVisualLineCol(element.Text, codeCharsPerVisualLine(width), cursor)
		return codeBlockPadY + visualLine*4 + 3, codeBlockPadX + col*editGlyphWidth(element)
	}
	rows := editableRowsForElementPrefix(element, width, cursor)
	if len(rows) == 0 {
		return 0, 0
	}
	return len(rows) - 1, displayWidth(strings.TrimRight(rows[len(rows)-1], " "))
}

func runeSliceWithPadding(text string, start, end int) string {
	if end <= start {
		return ""
	}
	rs := []rune(text)
	if len(rs) < end {
		rs = append(rs, []rune(strings.Repeat(" ", end-len(rs)))...)
	}
	start = max(0, min(start, len(rs)))
	end = max(start, min(end, len(rs)))
	return string(rs[start:end])
}

func drawSearchField(width, height int, query, status string) {
	if height <= 0 {
		return
	}
	prompt := "/" + query
	if status != "" {
		prompt += "  " + status
	}
	prompt = crop(prompt, width)
	termPrintf("\033[0;30;43m\033[%d;1H%s", height, padRight(prompt, width))
}

func drawJumpField(width, height int, query, status string) {
	if height <= 0 {
		return
	}
	prompt := "jump " + query
	if status != "" {
		prompt += "  " + status
	}
	prompt = crop(prompt, width)
	termPrintf("\033[0;30;43m\033[%d;1H%s", height, padRight(prompt, width))
}

func drawViewChrome(width, height int, view ViewState) {
	if height <= 0 || width <= 0 || view.SlideCount <= 0 {
		return
	}
	right := []toolbarSegment{}
	if presenter, short := presenterStatusLabels(); presenter != "" {
		right = append(right, toolbarSegment{Long: presenter, Short: short, Required: true, Priority: 1})
	}
	if notice := currentUINotice(); notice != "" {
		right = append(right, toolbarSegment{Long: notice, Short: "status", Priority: 4})
	}
	slideLabel := slideNumberLabel(view)
	right = append(right, toolbarSegment{Long: slideLabel, Short: slideLabel, Required: true, Priority: 0})
	if view.Chrome {
		left := toolbarSegmentsFromActions(mainActionSpecs())
		left = append(left, toolbarSegment{Long: "? shortcuts", Short: "?", Required: true, Priority: 0})
		drawAdaptiveToolbarLine(width, height, "47", left, right)
		return
	}
	label := slideLabel
	col := max(1, width-displayWidth(label)+1)
	termPrintf("\033[0;37;%sm\033[%d;%dH%s", slideBGCodeOnly(view), height, col, label)
}

func presenterStatusLabels() (string, string) {
	if !presenterModeActive {
		return "", ""
	}
	if activePresenter == nil {
		return "PRES: Unavailable", "P:!"
	}
	available, target, live := activePresenter.Status()
	if !available {
		return "PRES: Unavailable", "P:!"
	}
	if target == "none" || target == "" {
		return "PRES: None", "P:-"
	}
	targetLabel := "Main"
	shortTarget := "M"
	if target == "external" {
		targetLabel = "External"
		shortTarget = "E"
	}
	signal := "Signal off"
	shortSignal := "O"
	if live {
		signal = "Live"
		shortSignal = "L"
	}
	return "PRES: " + targetLabel + " / " + signal, "P:" + shortTarget + "/" + shortSignal
}

func drawViewOverlays(width, height int, view ViewState) {
	if view.ShowSlides {
		scroll := view.SlideNavScroll
		drawSlideNavigatorOverlay(view.Slides, view.SlideNavIndex, &scroll, width, height)
		drawSlideNavigatorToolbar(width, height, view.SlideNavIndex, view.SlideCount)
	}
	if view.ShowNotes && view.SlideIndex >= 0 && view.SlideIndex < len(view.Slides) {
		activeSlide := view.Slides[view.SlideIndex]
		notes := activeSlide.Notes
		drawSpeakerNotesPanel(notes, len([]rune(notes)), false, width, height)
		drawSpeakerNotesToolbar(width, height, false)
	}
	if view.TimerMode != "" {
		drawTimerOverlay(width, height, view.TimerMode, view.TimerInput, view.TimerDeadline)
	}
}

func slideNumberLabel(view ViewState) string {
	slideNo := strconv.Itoa(view.SlideIndex + 1)
	if view.PageCount > 1 {
		slideNo += string(rune('a' + max(0, view.Page)))
	}
	return fmt.Sprintf("%s/%d", slideNo, view.SlideCount)
}

func slideBGCodeOnly(view ViewState) string {
	return "40"
}

func findForwardMatch(slides []Slide, width, height, originSlide, originPage int, query string) (int, int, bool) {
	height = max(1, height)
	positions := slidePagePositions(slides, width, height)
	if len(positions) == 0 {
		return 0, 0, false
	}
	origin := 0
	for i, pos := range positions {
		if pos.slide == originSlide && pos.page == originPage {
			origin = i
			break
		}
	}
	needle := strings.ToLower(query)
	for step := 1; step < len(positions); step++ {
		pos := positions[(origin+step)%len(positions)]
		if strings.Contains(strings.ToLower(pageContentText(slides[pos.slide], width, height, pos.page)), needle) {
			return pos.slide, pos.page, true
		}
	}
	return 0, 0, false
}

type slidePagePosition struct {
	slide int
	page  int
}

func slidePagePositions(slides []Slide, width, height int) []slidePagePosition {
	height = max(1, height)
	var positions []slidePagePosition
	for slideIndex, slide := range slides {
		pages := max(1, slidePageCount(slide, width, height))
		for page := 0; page < pages; page++ {
			positions = append(positions, slidePagePosition{slide: slideIndex, page: page})
		}
	}
	return positions
}

func slidePageCount(slide Slide, width, height int) int {
	return max(1, len(displayPages(slide, width, max(1, height))))
}

func pageContentText(slide Slide, width, height, page int) string {
	seen := map[int]bool{}
	var parts []string
	for _, line := range displayLines(slide, width, height, page) {
		if line.Row < 0 || line.Row >= height || line.Element < 0 || line.Role == "image" || seen[line.Element] {
			continue
		}
		seen[line.Element] = true
		element := slide.Elements[line.Element]
		switch element.Kind {
		case "heading", "text", "bullet", "code":
			parts = append(parts, element.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func padRight(text string, width int) string {
	rs := []rune(text)
	if len(rs) >= width {
		return string(rs[:width])
	}
	return text + strings.Repeat(" ", width-len(rs))
}

func editableRowsForElementPrefix(element Element, width, cursor int) []string {
	runes := []rune(element.Text)
	cursor = max(0, min(cursor, len(runes)))
	text := string(runes[:cursor])
	if rendersAsTextImage(element) {
		copyElement := element
		copyElement.Text = text
		return renderTextImageElement(copyElement, width)
	}
	switch element.Kind {
	case "heading":
		scale := 1
		if element.Level == 1 {
			scale = 2
		}
		return renderFull(text, scale)
	case "bullet":
		return renderBulletWrapped(text, width)
	case "code":
		copyElement := element
		copyElement.Text = text
		return renderCodeBlockRows(copyElement, width)
	default:
		return renderBodyWrapped(text, width, "", "")
	}
}

func editGlyphWidth(element Element) int {
	if rendersAsTextImage(element) {
		values, _ := url.ParseQuery(element.Query)
		scale := 1.0
		if parsed, err := strconv.ParseFloat(values.Get("scale"), 64); err == nil && parsed > 0 {
			scale = parsed
		}
		if values.Get("source") == "bitmap" {
			return max(1, int(math.Round(4*scale)))
		}
		if values.Get("source") == "heading1" {
			return max(1, int(math.Round(16*scale)))
		}
		if values.Get("source") == "heading2" {
			return max(1, int(math.Round(8*scale)))
		}
		return max(1, int(math.Round(4*scale)))
	}
	if element.Kind == "heading" {
		if element.Level == 1 {
			return 16
		}
		return 8
	}
	if element.Kind == "code" {
		return 4
	}
	return 4
}

func editCursorColumn(element Element, prefixRows []string, cursor int) int {
	if rendersAsTextImage(element) {
		return maxLineDisplayWidth(prefixRows)
	}
	if element.Kind == "heading" {
		return cursor * editGlyphWidth(element)
	}
	if element.Kind == "code" {
		_, col := codeCursorLineCol(element.Text, cursor)
		return codeBlockPadX + col*editGlyphWidth(element)
	}
	return displayWidth(strings.TrimRight(prefixRows[len(prefixRows)-1], " "))
}

func codeCursorLineCol(text string, cursor int) (int, int) {
	runes := []rune(text)
	cursor = max(0, min(cursor, len(runes)))
	line := 0
	col := 0
	for i := 0; i < cursor; i++ {
		if runes[i] == '\n' {
			line++
			col = 0
		} else {
			col++
		}
	}
	return line, col
}

func codeCursorIndexForLineCol(text string, targetLine, targetCol int) int {
	targetLine = max(0, targetLine)
	targetCol = max(0, targetCol)
	runes := []rune(text)
	line := 0
	col := 0
	for i, r := range runes {
		if line == targetLine && col >= targetCol {
			return i
		}
		if r == '\n' {
			if line == targetLine {
				return i
			}
			line++
			col = 0
			continue
		}
		col++
	}
	return len(runes)
}

func codeCharsPerVisualLine(width int) int {
	return max(1, max(1, width-codeBlockPadX*2)/4)
}

func codeCursorVisualLineCol(text string, charsPerLine, cursor int) (int, int) {
	charsPerLine = max(1, charsPerLine)
	line, col := codeCursorLineCol(text, cursor)
	visualLine := 0
	for index, rawLine := range strings.Split(text, "\n") {
		if index == line {
			return visualLine + col/charsPerLine, col % charsPerLine
		}
		visualLine += max(1, (len([]rune(rawLine))+charsPerLine-1)/charsPerLine)
	}
	return visualLine, 0
}

func codeCursorIndexForVisualLineCol(text string, charsPerLine, targetVisualLine, targetCol int) int {
	charsPerLine = max(1, charsPerLine)
	targetVisualLine = max(0, targetVisualLine)
	targetCol = max(0, targetCol)
	offset := 0
	visualLine := 0
	for _, rawLine := range strings.Split(text, "\n") {
		runes := []rune(rawLine)
		chunkCount := max(1, (len(runes)+charsPerLine-1)/charsPerLine)
		if targetVisualLine < visualLine+chunkCount {
			chunk := targetVisualLine - visualLine
			col := min(len(runes), chunk*charsPerLine+targetCol)
			return offset + col
		}
		offset += len(runes)
		if offset < len([]rune(text)) {
			offset++
		}
		visualLine += chunkCount
	}
	return len([]rune(text))
}

func moveCodeCursorVertical(element Element, cursor, delta, width int) int {
	charsPerLine := codeCharsPerVisualLine(width)
	visualLine, col := codeCursorVisualLineCol(element.Text, charsPerLine, cursor)
	return codeCursorIndexForVisualLineCol(element.Text, charsPerLine, visualLine+delta, col)
}

func firstEditableElement(slide Slide) int {
	for i, element := range slide.Elements {
		if isEditableElement(element) {
			return i
		}
	}
	return -1
}

func previousEditableElement(slide Slide, selected int) int {
	for i := selected - 1; i >= 0; i-- {
		if isEditableElement(slide.Elements[i]) {
			return i
		}
	}
	return selected
}

func nextEditableElement(slide Slide, selected int) int {
	start := selected + 1
	if selected < 0 {
		start = 0
	}
	for i := start; i < len(slide.Elements); i++ {
		if isEditableElement(slide.Elements[i]) {
			return i
		}
	}
	return selected
}

func isEditableElement(element Element) bool {
	return element.Kind == "heading" || element.Kind == "text" || element.Kind == "text-image" || element.Kind == "bullet" || element.Kind == "code"
}

func isRotatableTextElement(element Element) bool {
	return element.Kind == "heading" || element.Kind == "text" || element.Kind == "text-image" || element.Kind == "bullet"
}

func isSelectableElement(element Element) bool {
	return !element.Inherited && (isEditableElement(element) || isPositionedElement(element))
}

func isAxisScalableElement(element Element) bool {
	return element.Kind == "image" || element.Kind == "shape"
}

type editorMode string

const (
	editorModeMain      editorMode = "main"
	editorModeSelect    editorMode = "select"
	editorModeText      editorMode = "text"
	editorModeAxisScale editorMode = "axis-scale"
	editorModeMove      editorMode = "move"
)

type selectionKind string

const (
	selectionNone  selectionKind = "none"
	selectionText  selectionKind = "text"
	selectionCode  selectionKind = "code"
	selectionImage selectionKind = "image"
	selectionShape selectionKind = "shape"
	selectionPage  selectionKind = "page number"
	selectionMulti selectionKind = "multi"
)

type interactionContext struct {
	Mode      editorMode
	Selection selectionKind
	Master    bool
}

type actionSpec struct {
	Actions  []string
	Key      string
	Help     string
	Toolbar  string
	Short    string
	Core     bool
	Priority int
}

func action(keys, help, toolbar, short string, core bool, priority int, actions ...string) actionSpec {
	return actionSpec{Actions: actions, Key: keys, Help: help, Toolbar: toolbar, Short: short, Core: core, Priority: priority}
}

func interactionContextFor(mode string, slide Slide, selected int, selection map[int]bool, status string) interactionContext {
	ctx := interactionContext{Mode: editorModeSelect, Selection: selectionNone}
	switch mode {
	case "text":
		ctx.Mode = editorModeText
	case "resize":
		ctx.Mode = editorModeAxisScale
	case "move":
		ctx.Mode = editorModeMove
	}
	if selectionSetCount(selection) > 1 || strings.HasSuffix(status, " selected") && status != "text selected" && status != "code text selected" && status != "image selected" && status != "shape selected" && status != "page number selected" {
		ctx.Selection = selectionMulti
		return ctx
	}
	if selected < 0 || selected >= len(slide.Elements) {
		return ctx
	}
	if status == "code text selected" {
		ctx.Selection = selectionCode
		return ctx
	}
	switch slide.Elements[selected].Kind {
	case "image":
		ctx.Selection = selectionImage
	case "shape":
		ctx.Selection = selectionShape
	case "page-number":
		ctx.Selection = selectionPage
	case "code":
		ctx.Selection = selectionCode
	default:
		ctx.Selection = selectionText
	}
	return ctx
}

func mainActionSpecs() []actionSpec {
	return []actionSpec{
		action("Space / ->", "next page or slide", "Space/→ next", "→ next", true, 0, "next"),
		action("<-", "previous page or slide", "← prev", "←", true, 0, "prev"),
		action("p", "toggle presentation signal", "p present", "p", true, 1, "present", "controls"),
		action("0", "countdown timer", "0 timer", "0", true, 2, "timer"),
		action("1", "slide overview", "1 slides", "1", true, 3, "slide-list"),
		action("2", "speaker notes", "2 notes", "2", true, 3, "speaker-notes"),
		action("Tab", "select slide element", "", "", false, 4, "tab", "shift-tab"),
		action("n / c / d", "new, clone, or delete slide", "", "", false, 4, "insert-slide", "clone-slide", "delete-slide"),
		action("M / L", "master view or change slide layout", "", "", false, 4, "master-view", "layout-picker"),
		action("#", "cycle page number inherit, show, or hide", "", "", false, 4, "page-number"),
		action("v", "visual properties", "", "", false, 4, "visual-properties"),
		action("t / i / s", "insert text, image, or shape", "", "", false, 4, "insert-text", "insert-image", "shape-picker"),
		action("e / b", "effects or backgrounds", "", "", false, 5, "effect-picker", "background-picker"),
		action("/ / j", "search or jump", "", "", false, 5, "search", "jump"),
		action("x", "export HTML", "", "", false, 5, "export"),
		action("Ctrl-Z / Ctrl-Y", "undo or redo", "", "", false, 5, "undo", "redo"),
		action("q", "quit", "", "", false, 5, "quit"),
	}
}

func editActionSpecs(ctx interactionContext) []actionSpec {
	if ctx.Mode == editorModeText {
		return []actionSpec{
			action("Type", "insert text", "", "", false, 0, "text"),
			action("Arrows", "move cursor", "arrows cursor", "arrows", true, 0, "up", "down", "left", "right"),
			action("Shift-Enter", "insert newline in code", "Shift-Enter newline", "⇧Enter", true, 1, "insert-newline"),
			action("Enter", "commit edit", "Enter commit", "Enter", true, 0, "enter"),
			action("Esc", "cancel edit", "Esc cancel", "Esc", true, 0, "escape"),
		}
	}
	if ctx.Mode == editorModeAxisScale {
		return []actionSpec{
			action("Arrows", "scale the selected axis", "arrows scale", "arrows", true, 0, "up", "down", "left", "right"),
			action("Enter", "commit axis scaling", "Enter commit", "Enter", true, 0, "enter"),
			action("Esc", "cancel axis scaling", "Esc cancel", "Esc", true, 0, "escape"),
		}
	}
	if ctx.Mode == editorModeMove {
		return []actionSpec{
			action("Arrows", "move element", "arrows move", "arrows", true, 0, "up", "down", "left", "right"),
			action("Shift-arrows", "jump 10 cells", "Shift-arrows ×10", "⇧arrows", true, 1, "shift-up", "shift-down", "shift-left", "shift-right"),
			action("Enter", "commit move", "Enter commit", "Enter", true, 0, "enter"),
			action("Esc", "cancel move", "Esc cancel", "Esc", true, 0, "escape"),
		}
	}
	var specs []actionSpec
	if ctx.Selection == selectionNone {
		specs = append(specs,
			action("Tab / Shift-Tab", "select next or previous element", "Tab select", "Tab", true, 0, "tab", "shift-tab"),
			action("0", "countdown timer", "0 timer", "0", true, 2, "timer"),
			action("1", "slide overview", "1 slides", "1", true, 3, "slide-list"),
			action("2", "speaker notes", "2 notes", "2", true, 3, "speaker-notes"),
			action("Ctrl-V", "paste element", "", "", false, 4, "paste"),
			action("t / i / s", "insert text, image, or shape", "", "", false, 3, "insert-text", "insert-image", "style"),
		)
	}
	if ctx.Master {
		specs = append(specs,
			action("#", "cycle page-number policy", "# page number", "#", true, 2, "page-number"),
			action("v", "visual properties", "v properties", "v", true, 2, "visual-properties"),
		)
	}
	if ctx.Selection == selectionText || ctx.Selection == selectionImage || ctx.Selection == selectionShape || ctx.Selection == selectionPage || ctx.Selection == selectionMulti {
		specs = append(specs,
			action("Arrows", "move selection", "arrows move", "arrows", true, 0, "up", "down", "left", "right"),
			action("Shift-arrows", "jump 10 cells", "Shift-arrows ×10", "⇧arrows", false, 3, "shift-up", "shift-down", "shift-left", "shift-right"),
		)
	}
	if ctx.Selection == selectionText || ctx.Selection == selectionImage || ctx.Selection == selectionShape || ctx.Selection == selectionPage {
		specs = append(specs, action("Space", "add or remove from multi-selection", "", "", false, 4, "toggle-selection"))
	}
	if ctx.Selection == selectionText || ctx.Selection == selectionCode {
		specs = append(specs, action("Enter", "edit text", "Enter edit", "Enter", true, 0, "enter", "edit-selected"))
	}
	if ctx.Selection == selectionText || ctx.Selection == selectionImage || ctx.Selection == selectionShape || ctx.Selection == selectionPage {
		specs = append(specs, action("+ / -", "increase or decrease size", "+/- size", "+/-", true, 1, "promote", "demote"))
	}
	if ctx.Selection == selectionImage || ctx.Selection == selectionShape {
		specs = append(specs, action("g", "axis scaling", "g axis scale", "g scale", true, 1, "shape-toggle"))
	}
	if ctx.Selection == selectionText {
		specs = append(specs,
			action("Shift-B", "toggle bold Markdown", "", "", false, 3, "toggle-bold"),
			action("Shift-H", "toggle highlighted Markdown", "", "", false, 3, "toggle-highlight"),
			action("r / R", "rotate clockwise or counter-clockwise", "", "", false, 3, "rotate", "rotate-ccw"),
		)
	}
	if ctx.Selection == selectionText || ctx.Selection == selectionCode || ctx.Selection == selectionShape || ctx.Selection == selectionPage {
		specs = append(specs, action("c", "change color", "c color", "c", true, 2, "color"))
	}
	if ctx.Selection == selectionText || ctx.Selection == selectionImage || ctx.Selection == selectionShape || ctx.Selection == selectionPage {
		specs = append(specs, action("s", "change style", "s style", "s", true, 2, "style", "settings"))
	}
	if ctx.Selection == selectionText || ctx.Selection == selectionCode || ctx.Selection == selectionImage || ctx.Selection == selectionShape || ctx.Selection == selectionPage {
		specs = append(specs, action("o", "cycle outline", "o outline", "o", true, 2, "outline"))
	}
	if ctx.Master && (ctx.Selection == selectionText || ctx.Selection == selectionCode || ctx.Selection == selectionImage || ctx.Selection == selectionShape) {
		specs = append(specs, action("p", "set placeholder role", "p placeholder", "p", true, 2, "placeholder-role"))
	}
	if ctx.Selection == selectionImage || ctx.Selection == selectionShape {
		specs = append(specs, action("[ / ]", "send back or bring front", "", "", false, 3, "layer-back", "layer-front"))
	}
	if ctx.Selection == selectionText || ctx.Selection == selectionImage || ctx.Selection == selectionShape {
		specs = append(specs, action("l", "set link", "", "", false, 3, "link"))
	}
	if ctx.Selection == selectionShape {
		specs = append(specs, action("/", "toggle see-through shape", "/ see-through", "/", true, 2, "transparency"))
	}
	if ctx.Selection == selectionMulti {
		specs = append(specs, action("< / = / >", "align left, center, or right", "align", "align", true, 2, "align-left", "align-center", "align-right"))
	}
	if ctx.Selection != selectionNone && ctx.Selection != selectionCode && ctx.Selection != selectionPage {
		specs = append(specs,
			action("Ctrl-C / X / V", "copy, cut, or paste", "", "", false, 4, "copy", "cut", "paste"),
			action("Backspace", "delete selection", "", "", false, 4, "backspace"),
		)
	}
	return specs
}

func actionSpecsAllow(specs []actionSpec, actionName string) bool {
	for _, spec := range specs {
		for _, candidate := range spec.Actions {
			if candidate == actionName {
				return true
			}
		}
	}
	return false
}

func globalEditAction(actionName string) bool {
	switch actionName {
	case "escape", "quit", "save", "undo", "redo", "mouse-click", "shift-mouse-click", "slide-list", "speaker-notes", "timer", "export", "jump", "search", "present", "controls", "next", "prev", "tab", "shift-tab":
		return true
	default:
		return false
	}
}

func actionAllowedInContext(ctx interactionContext, actionName string) bool {
	if ctx.Mode == editorModeText {
		return actionSpecsAllow(editActionSpecs(ctx), actionName) || actionName == "backspace" || actionName == "paste" || actionName == "quit"
	}
	return globalEditAction(actionName) || actionSpecsAllow(editActionSpecs(ctx), actionName)
}

func elementKindLabel(element Element) string {
	switch element.Kind {
	case "heading":
		return "heading"
	case "bullet":
		return "bullet"
	case "code":
		return "code block"
	case "image":
		return "image"
	case "shape":
		return "shape"
	case "page-number":
		return "page number"
	default:
		return "text"
	}
}

func isPositionedElement(element Element) bool {
	return element.Kind == "image" || element.Kind == "shape" || element.Kind == "page-number"
}

const (
	codeBlockPadX = 2
	codeBlockPadY = 1

	textSizeMin     = -1
	textSizeNormal  = 0
	textSizeHeading = 10
	textSizeTitle   = 20
	textSizeMax     = 25
)

func changeTextLevel(element *Element, delta int) {
	if element == nil {
		return
	}
	size := textSize(*element)
	size += delta
	size = max(textSizeMin, min(textSizeMax, size))
	applyTextSize(element, size)
}

func textSize(element Element) int {
	values, _ := url.ParseQuery(element.Query)
	if parsed, err := strconv.Atoi(values.Get("text-size")); err == nil {
		return max(textSizeMin, min(textSizeMax, parsed))
	}
	if rendersAsTextImage(element) {
		scale := 1.0
		if parsed, err := strconv.ParseFloat(values.Get("scale"), 64); err == nil {
			scale = parsed
		}
		return sizeFromLegacyScale(values.Get("source"), scale)
	}
	if element.Kind == "heading" {
		if element.Level == 1 {
			return textSizeTitle
		}
		return textSizeHeading
	}
	return textSizeNormal
}

func sizeFromLegacyScale(source string, scale float64) int {
	if source == "bitmap" {
		if scale < 1 {
			return max(textSizeMin, min(textSizeNormal-1, int(math.Round((scale-1.0)/0.1))))
		}
		if scale < 2 {
			return max(textSizeNormal+1, min(textSizeHeading-1, int(math.Round((scale-1.0)*10))))
		}
		if scale < 4 {
			return max(textSizeHeading+1, min(textSizeTitle-1, textSizeHeading+int(math.Round((scale-2.0)/0.2))))
		}
		return max(textSizeTitle+1, min(textSizeMax, textSizeTitle+int(math.Round((scale-4.0)/0.2))))
	}
	switch source {
	case "heading1":
		return max(textSizeTitle+1, min(textSizeMax, textSizeTitle+int(math.Round((scale-2.0)/0.1))))
	case "heading2":
		if scale < 1 {
			return max(textSizeNormal+1, min(textSizeHeading-1, int(math.Round((scale-0.45)/0.055))))
		}
		return max(textSizeHeading+1, min(textSizeTitle-1, textSizeHeading+int(math.Round((scale-1.0)*10))))
	default:
		if scale < 1 {
			return max(textSizeMin, min(textSizeNormal-1, int(math.Round((scale-1.0)/0.07))))
		}
		return textSizeNormal
	}
}

func applyTextSize(element *Element, size int) {
	if size == nativeTextSize(*element) {
		element.Query = clearTextRenderQuery(element.Query)
		return
	}
	source, scale := textImageSourceAndScale(size)
	element.Query = setTextRenderQuery(element.Query, "text-image", source, scale, size)
}

func ensureTextImageRender(element *Element) {
	if element == nil || rendersAsTextImage(*element) {
		return
	}
	size := textSize(*element)
	source, scale := textImageSourceAndScale(size)
	element.Query = setTextRenderQuery(element.Query, "text-image", source, scale, size)
}

func nativeTextSize(element Element) int {
	if element.Kind == "heading" {
		if element.Level == 1 {
			return textSizeTitle
		}
		return textSizeHeading
	}
	return textSizeNormal
}

func textImageSourceAndScale(size int) (string, float64) {
	if size < textSizeNormal {
		return "bitmap", 1.0 + float64(size)*0.1
	}
	if size < textSizeHeading {
		return "bitmap", 1.0 + float64(size)/10.0
	}
	if size < textSizeTitle {
		return "bitmap", 2.0 + float64(size-textSizeHeading)*0.2
	}
	return "bitmap", 4.0 + float64(size-textSizeTitle)*0.2
}

func setTextRenderQuery(query, render, source string, scale float64, size int) string {
	values, _ := url.ParseQuery(query)
	values.Set("render", render)
	values.Set("source", source)
	values.Set("scale", fmt.Sprintf("%.2f", scale))
	values.Set("text-size", strconv.Itoa(size))
	return values.Encode()
}

func clearTextRenderQuery(query string) string {
	values, _ := url.ParseQuery(query)
	values.Del("render")
	values.Del("source")
	values.Del("scale")
	values.Del("text-size")
	return values.Encode()
}

func textOrientation(element Element) string {
	values, _ := url.ParseQuery(element.Query)
	switch strings.ToLower(values.Get("orientation")) {
	case "cw", "down", "ccw":
		return strings.ToLower(values.Get("orientation"))
	default:
		return ""
	}
}

func rotateTextOrientation(element *Element) {
	if element == nil || !isRotatableTextElement(*element) {
		return
	}
	values, _ := url.ParseQuery(element.Query)
	switch textOrientation(*element) {
	case "":
		values.Set("orientation", "cw")
	case "cw":
		values.Set("orientation", "down")
	case "down":
		values.Set("orientation", "ccw")
	default:
		values.Del("orientation")
	}
	element.Query = values.Encode()
}

func rotateTextOrientationCounterClockwise(element *Element) {
	if element == nil || !isRotatableTextElement(*element) {
		return
	}
	values, _ := url.ParseQuery(element.Query)
	switch textOrientation(*element) {
	case "":
		values.Set("orientation", "ccw")
	case "ccw":
		values.Set("orientation", "down")
	case "down":
		values.Set("orientation", "cw")
	default:
		values.Del("orientation")
	}
	element.Query = values.Encode()
}

func ensureCursor(slide *Slide, cursor map[int]int, selected int) {
	if selected < 0 || selected >= len(slide.Elements) {
		return
	}
	length := len([]rune(slide.Elements[selected].Text))
	if _, ok := cursor[selected]; !ok {
		cursor[selected] = length
		return
	}
	cursor[selected] = max(0, min(cursor[selected], length))
}

func insertTextAtCursor(slide *Slide, selected int, cursor map[int]int, text string) {
	if selected < 0 || selected >= len(slide.Elements) {
		return
	}
	materializePlaceholder(slide, selected, cursor)
	rs := []rune(slide.Elements[selected].Text)
	pos := max(0, min(cursor[selected], len(rs)))
	insert := []rune(text)
	next := make([]rune, 0, len(rs)+len(insert))
	next = append(next, rs[:pos]...)
	next = append(next, insert...)
	next = append(next, rs[pos:]...)
	slide.Elements[selected].Text = string(next)
	cursor[selected] = pos + len(insert)
}

func editBackspace(slide *Slide, selected int, cursor map[int]int) int {
	if selected < 0 || selected >= len(slide.Elements) {
		return selected
	}
	if slide.Elements[selected].Placeholder {
		materializePlaceholder(slide, selected, cursor)
		return selected
	}
	rs := []rune(slide.Elements[selected].Text)
	pos := max(0, min(cursor[selected], len(rs)))
	if pos > 0 {
		next := make([]rune, 0, len(rs)-1)
		next = append(next, rs[:pos-1]...)
		next = append(next, rs[pos:]...)
		slide.Elements[selected].Text = string(next)
		cursor[selected] = pos - 1
		return selected
	}
	if slide.Elements[selected].Kind == "bullet" {
		slide.Elements[selected].Kind = "text"
		cursor[selected] = 0
	}
	return selected
}

func materializePlaceholder(slide *Slide, selected int, cursor map[int]int) {
	if selected < 0 || selected >= len(slide.Elements) || !slide.Elements[selected].Placeholder {
		return
	}
	slide.Elements[selected].Placeholder = false
	slide.Elements[selected].Text = ""
	cursor[selected] = 0
}

func finalizeTextEdit(slide *Slide, selected int, cursor map[int]int) int {
	if selected < 0 || selected >= len(slide.Elements) {
		return selected
	}
	if slide.Elements[selected].Placeholder || strings.TrimSpace(slide.Elements[selected].Text) == "" {
		return removeEditableElement(slide, selected, cursor)
	}
	return selected
}

func removeEditableElement(slide *Slide, selected int, cursor map[int]int) int {
	if selected < 0 || selected >= len(slide.Elements) {
		return selected
	}
	slide.Elements = append(slide.Elements[:selected], slide.Elements[selected+1:]...)
	delete(cursor, selected)
	shiftCursorKeys(cursor, selected, -1)
	if len(slide.Elements) == 0 {
		return -1
	}
	if selected >= len(slide.Elements) {
		selected = len(slide.Elements) - 1
	}
	if !isEditableElement(slide.Elements[selected]) {
		selected = previousEditableElement(*slide, selected+1)
	}
	if selected >= 0 && selected < len(slide.Elements) && isEditableElement(slide.Elements[selected]) {
		ensureCursor(slide, cursor, selected)
		return selected
	}
	return -1
}

func editInsertLine(slide *Slide, selected int, cursor map[int]int) int {
	newElement := Element{Kind: "text"}
	insertAt := selected + 1
	if selected < 0 || selected >= len(slide.Elements) {
		slide.Elements = append(slide.Elements, newElement)
		selected = len(slide.Elements) - 1
		cursor[selected] = 0
		return selected
	}
	slide.Elements = append(slide.Elements, Element{})
	copy(slide.Elements[insertAt+1:], slide.Elements[insertAt:])
	slide.Elements[insertAt] = newElement
	shiftCursorKeys(cursor, insertAt, 1)
	cursor[insertAt] = 0
	return insertAt
}

func shiftCursorKeys(cursor map[int]int, start, delta int) {
	next := map[int]int{}
	for key, value := range cursor {
		if key >= start {
			next[key+delta] = value
		} else {
			next[key] = value
		}
	}
	for key := range cursor {
		delete(cursor, key)
	}
	for key, value := range next {
		cursor[key] = value
	}
}

func renderElementRows(element Element, width int) []string {
	orientation := textOrientation(element)
	if !isRotatableTextElement(element) || orientation == "" {
		return renderElementRowsBase(element, width)
	}
	return renderRotatedTextImageElement(element, width, orientation)
}

func renderElementRowsBase(element Element, width int) []string {
	width = constrainedElementWidth(element, width)
	if rendersAsTextImage(element) {
		return renderTextImageElement(element, width)
	}
	switch element.Kind {
	case "heading":
		scale := 1
		if element.Level == 1 {
			scale = 2
		}
		return renderFullWrapped(element.Text, width, scale)
	case "bullet":
		return renderBulletWrapped(element.Text, width)
	case "code":
		return renderCodeBlockRows(element, width)
	case "shape":
		return renderShapeRows(element, width)
	default:
		return renderBodyWrapped(element.Text, width, "", "")
	}
}

func rotateRowsClockwise(rows []string) []string {
	matrix, width := plainRowMatrix(rows)
	if width == 0 || len(matrix) == 0 {
		return rows
	}
	out := make([]string, 0, width)
	for col := 0; col < width; col++ {
		var b strings.Builder
		for row := len(matrix) - 1; row >= 0; row-- {
			b.WriteRune(matrix[row][col])
		}
		out = append(out, b.String())
	}
	return trimBlank(out)
}

func rotateRowsDown(rows []string) []string {
	matrix, width := plainRowMatrix(rows)
	if width == 0 || len(matrix) == 0 {
		return rows
	}
	out := make([]string, 0, len(matrix))
	for row := len(matrix) - 1; row >= 0; row-- {
		var b strings.Builder
		for col := width - 1; col >= 0; col-- {
			b.WriteRune(matrix[row][col])
		}
		out = append(out, b.String())
	}
	return trimBlank(out)
}

func rotateRowsCounterClockwise(rows []string) []string {
	matrix, width := plainRowMatrix(rows)
	if width == 0 || len(matrix) == 0 {
		return rows
	}
	out := make([]string, 0, width)
	for col := width - 1; col >= 0; col-- {
		var b strings.Builder
		for row := 0; row < len(matrix); row++ {
			b.WriteRune(matrix[row][col])
		}
		out = append(out, b.String())
	}
	return trimBlank(out)
}

func plainRowMatrix(rows []string) ([][]rune, int) {
	width := maxLineDisplayWidth(rows)
	if width == 0 {
		return nil, 0
	}
	matrix := make([][]rune, len(rows))
	for rowIndex, row := range rows {
		rs := []rune(stripANSI(row))
		if len(rs) < width {
			rs = append(rs, []rune(strings.Repeat(" ", width-len(rs)))...)
		}
		matrix[rowIndex] = rs
	}
	return matrix, width
}

func constrainedElementWidth(element Element, width int) int {
	if width <= 0 {
		return width
	}
	values, err := url.ParseQuery(element.Query)
	if err != nil {
		return width
	}
	if parsed, err := strconv.Atoi(values.Get("width")); err == nil && parsed > 0 {
		return max(1, min(width, parsed))
	}
	return width
}

func constrainedElementHeight(element Element, height int) int {
	if height <= 0 {
		return height
	}
	values, err := url.ParseQuery(element.Query)
	if err != nil {
		return height
	}
	if parsed, err := strconv.Atoi(values.Get("height")); err == nil && parsed > 0 {
		return max(1, min(height, parsed))
	}
	return height
}

func renderShapeRows(element Element, maxWidth int) []string {
	values, _ := url.ParseQuery(element.Query)
	shape := shapeName(element)
	w := max(1, intQueryDefault(values, "width", 12))
	h := max(1, intQueryDefault(values, "height", 6))
	if maxWidth > 0 {
		w = min(w, maxWidth)
	}
	rows := make([]string, h)
	for y := 0; y < h; y++ {
		cells := make([]rune, w)
		for x := 0; x < w; x++ {
			if shapeCellFilled(shape, x, y, w, h) {
				cells[x] = '█'
			} else {
				cells[x] = ' '
			}
		}
		rows[y] = string(cells)
	}
	return rows
}

func shapeCellFilled(shape string, x, y, w, h int) bool {
	if w <= 0 || h <= 0 {
		return false
	}
	switch shape {
	case "circle":
		cx := (float64(w) - 1) / 2
		cy := (float64(h) - 1) / 2
		rx := maxFloat(0.5, float64(w)/2)
		ry := maxFloat(0.5, float64(h)/2)
		dx := (float64(x) - cx) / rx
		dy := (float64(y) - cy) / ry
		return dx*dx+dy*dy <= 1.0
	case "triangle":
		if h == 1 {
			return true
		}
		center := (float64(w) - 1) / 2
		half := (float64(y) / float64(max(1, h-1))) * float64(w) / 2
		return math.Abs(float64(x)-center) <= half
	case "diamond":
		cx := (float64(w) - 1) / 2
		cy := (float64(h) - 1) / 2
		return math.Abs(float64(x)-cx)/maxFloat(0.5, float64(w)/2)+math.Abs(float64(y)-cy)/maxFloat(0.5, float64(h)/2) <= 1.0
	default:
		return true
	}
}

func shapeName(element Element) string {
	values, _ := url.ParseQuery(element.Query)
	switch strings.ToLower(values.Get("shape")) {
	case "circle", "square", "triangle", "diamond":
		return strings.ToLower(values.Get("shape"))
	default:
		return "circle"
	}
}

func intQueryDefault(values url.Values, key string, fallback int) int {
	if parsed, err := strconv.Atoi(values.Get(key)); err == nil {
		return parsed
	}
	return fallback
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func renderCodeBlockRows(element Element, width int) []string {
	if width <= 0 {
		return nil
	}
	glyphRows := renderCodeGlyphRows(element.Text, max(1, width-codeBlockPadX*2))
	contentWidth := max(1, maxLineDisplayWidth(glyphRows))
	blockWidth := min(width, contentWidth+codeBlockPadX*2)
	blank := strings.Repeat(" ", blockWidth)
	rows := make([]string, 0, len(glyphRows)+codeBlockPadY*2)
	for i := 0; i < codeBlockPadY; i++ {
		rows = append(rows, blank)
	}
	for _, row := range glyphRows {
		rows = append(rows, strings.Repeat(" ", codeBlockPadX)+padRight(crop(row, contentWidth), contentWidth)+strings.Repeat(" ", codeBlockPadX))
	}
	for i := 0; i < codeBlockPadY; i++ {
		rows = append(rows, blank)
	}
	return rows
}

func renderCodeGlyphRows(text string, width int) []string {
	charsPerLine := max(1, width/4)
	var rows []string
	for _, rawLine := range strings.Split(text, "\n") {
		chunks := fixedRuneChunks(rawLine, charsPerLine)
		for _, chunk := range chunks {
			rows = append(rows, renderQuad(chunk)...)
		}
	}
	if len(rows) == 0 {
		return renderQuad("")
	}
	return rows
}

func fixedRuneChunks(text string, width int) []string {
	width = max(1, width)
	runes := []rune(text)
	if len(runes) == 0 {
		return []string{""}
	}
	chunks := make([]string, 0, (len(runes)+width-1)/width)
	for len(runes) > 0 {
		n := min(width, len(runes))
		chunks = append(chunks, string(runes[:n]))
		runes = runes[n:]
	}
	return chunks
}

func layoutElementRows(element Element, width int) []string {
	rows := renderElementRows(element, width)
	if len(rows) == 0 && isEditableElement(element) {
		return []string{""}
	}
	return rows
}

func rendersAsTextImage(element Element) bool {
	return element.Kind == "text-image" || textRenderMode(element.Query) == "text-image"
}

func layout(slide Slide, width, height int) []Line {
	type block struct {
		lines   []Line
		image   bool
		path    string
		query   string
		element int
	}
	var blocks [][]Line
	var typedBlocks []block
	for elementIndex, e := range slide.Elements {
		switch e.Kind {
		case "heading":
			typedBlocks = append(typedBlocks, block{lines: roleLines(layoutElementRows(e, width), "heading", elementIndex, e.Query)})
		case "bullet":
			typedBlocks = append(typedBlocks, block{lines: roleLines(layoutElementRows(e, width), "body", elementIndex, e.Query)})
		case "text":
			typedBlocks = append(typedBlocks, block{lines: roleLines(layoutElementRows(e, width), "body", elementIndex, e.Query)})
		case "text-image":
			typedBlocks = append(typedBlocks, block{lines: roleLines(layoutElementRows(e, width), "body", elementIndex, e.Query)})
		case "code":
			typedBlocks = append(typedBlocks, block{lines: roleLines(layoutElementRows(e, width), "code", elementIndex, e.Query)})
		case "image":
			typedBlocks = append(typedBlocks, block{image: true, path: e.Path, query: e.Query, element: elementIndex})
		case "shape":
			typedBlocks = append(typedBlocks, block{lines: roleLines(layoutElementRows(e, width), "shape", elementIndex, e.Query)})
		case "page-number":
			typedBlocks = append(typedBlocks, block{lines: roleLines(layoutElementRows(e, width), "body", elementIndex, e.Query)})
		}
	}
	blocks = make([][]Line, 0, len(typedBlocks))
	for _, block := range typedBlocks {
		if block.image {
			blocks = append(blocks, []Line{{Text: block.path, Role: "image-placeholder", Query: block.query, Element: block.element}})
		} else {
			blocks = append(blocks, block.lines)
		}
	}

	var masterBackLines []Line
	var masterFrontLines []Line
	var backLines []Line
	var frontLines []Line
	currentRow := 0
	hasFlowBlock := false
	for _, block := range blocks {
		if len(block) == 0 {
			continue
		}
		isImage := len(block) == 1 && block[0].Role == "image-placeholder"
		positionedText := false
		if !isImage {
			placement := parseImagePlacement(block[0].Query)
			positionedText = placement.hasVerticalOffset()
		}
		if !isImage && !positionedText && hasFlowBlock {
			currentRow++
		}
		if isImage {
			placement := parseImagePlacement(block[0].Query)
			maxImageRows := height
			if placement.top != nil && placement.bottom != nil {
				maxImageRows = max(1, height-*placement.top-*placement.bottom)
			}
			imageElement := slide.Elements[block[0].Element]
			maxImageWidth := constrainedElementWidth(imageElement, width)
			maxImageRows = constrainedElementHeight(imageElement, maxImageRows)
			rows := renderASCIIImage(block[0].Text, block[0].Query, maxImageWidth, maxImageRows)
			if len(rows) == 0 && imageElement.Placeholder {
				rows = renderImagePlaceholderRows(imageElement, maxImageWidth, maxImageRows)
			}
			imageWidth := maxLineDisplayWidth(rows)
			row := currentRow
			if placement.top != nil {
				row = *placement.top
			} else if placement.bottom != nil {
				row = height - len(rows) - *placement.bottom
			} else if placement.rowDelta != nil {
				row = *placement.rowDelta
			}
			col := 0
			if placement.hasHorizontalOffset() {
				col = placementLeftCol(placement, width, imageWidth)
			} else {
				switch placement.align {
				case "center":
					col = (width - imageWidth) / 2
				case "right":
					col = rightAlignedCol(width, imageWidth, 0)
				}
			}
			if placement.top != nil {
				row = max(0, row)
			} else if placement.bottom != nil {
				row = max(0, min(height-1, row))
			} else {
				row = max(0, row)
			}
			col = clampBlockCol(col, width, imageWidth)
			element := slide.Elements[block[0].Element]
			var layerLines *[]Line
			switch {
			case element.Inherited && elementLayer(element) == "front":
				layerLines = &masterFrontLines
			case element.Inherited:
				layerLines = &masterBackLines
			case elementLayer(element) == "front":
				layerLines = &frontLines
			default:
				layerLines = &backLines
			}
			for offset, text := range rows {
				line := Line{Text: text, Role: "image", Row: row + offset, Col: col, Element: block[0].Element, Query: block[0].Query}
				*layerLines = append(*layerLines, line)
			}
			if elementHasOutline(slide.Elements[block[0].Element]) {
				*layerLines = append(*layerLines, outlineLinesForRows(rows, "image", block[0].Element, block[0].Query, row, col, width)...)
			}
		} else {
			placement := parseImagePlacement(block[0].Query)
			blockHeight := len(block)
			blockWidth := 0
			for _, line := range block {
				blockWidth = max(blockWidth, displayWidth(stripANSI(line.Text)))
			}
			row := currentRow
			if placement.top != nil {
				row = *placement.top
			} else if placement.bottom != nil {
				row = height - blockHeight - *placement.bottom
			} else if placement.rowDelta != nil {
				row = *placement.rowDelta
			}
			col := 0
			if placement.hasHorizontalOffset() {
				col = placementLeftCol(placement, width, blockWidth)
			} else {
				switch placement.align {
				case "center":
					col = (width - blockWidth) / 2
				case "right":
					col = rightAlignedCol(width, blockWidth, 0)
				}
			}
			if placement.top != nil {
				row = max(0, row)
			} else if placement.bottom != nil {
				row = max(0, min(max(0, height-1), row))
			} else {
				row = max(0, row)
			}
			col = clampBlockCol(col, width, blockWidth)
			availableWidth := max(1, width-col)
			if availableWidth < width {
				role := block[0].Role
				block = roleLines(layoutElementRows(slide.Elements[block[0].Element], availableWidth), role, block[0].Element, block[0].Query)
				blockHeight = len(block)
				if placement.bottom != nil {
					row = height - blockHeight - *placement.bottom
					row = max(0, min(max(0, height-1), row))
				}
			}
			element := slide.Elements[block[0].Element]
			var layerLines *[]Line
			switch {
			case element.Inherited && elementLayer(element) == "back":
				layerLines = &masterBackLines
			case element.Inherited:
				layerLines = &masterFrontLines
			case elementLayer(element) == "back":
				layerLines = &backLines
			default:
				layerLines = &frontLines
			}
			blockRows := make([]string, 0, len(block))
			for _, line := range block {
				blockRows = append(blockRows, line.Text)
				line.Row = row + (line.Row - block[0].Row)
				line.Col = col
				*layerLines = append(*layerLines, line)
			}
			if elementHasOutline(slide.Elements[block[0].Element]) {
				*layerLines = append(*layerLines, outlineLinesForRows(blockRows, block[0].Role, block[0].Element, block[0].Query, row, col, width)...)
			}
			if elementLink(block[0].Query) != "" {
				blockHeight++
			}
			if !placement.hasVerticalOffset() {
				currentRow += blockHeight
				hasFlowBlock = true
			}
		}
	}
	lines := append(masterBackLines, masterFrontLines...)
	lines = append(lines, backLines...)
	return append(lines, frontLines...)
}

func renderImagePlaceholderRows(element Element, maxWidth, maxHeight int) []string {
	values, _ := url.ParseQuery(element.Query)
	width := max(8, intQueryDefault(values, "width", 20))
	height := max(3, intQueryDefault(values, "height", 7))
	if maxWidth > 0 {
		width = min(width, maxWidth)
	}
	if maxHeight > 0 {
		height = min(height, maxHeight)
	}
	if width < 2 || height < 2 {
		return []string{strings.Repeat("#", max(1, width))}
	}
	rows := make([]string, height)
	rows[0] = "+" + strings.Repeat("-", width-2) + "+"
	for row := 1; row < height-1; row++ {
		rows[row] = "|" + strings.Repeat(" ", width-2) + "|"
	}
	rows[height-1] = rows[0]
	label := "IMAGE"
	if width > len(label)+2 {
		start := max(1, (width-len(label))/2)
		middle := height / 2
		runes := []rune(rows[middle])
		copy(runes[start:], []rune(label))
		rows[middle] = string(runes)
	}
	return rows
}

func resolveTextCollisions(lines []Line) []Line {
	type group struct {
		element int
		minRow  int
		maxRow  int
		minCol  int
	}
	groupsByElement := map[int]int{}
	var groups []group
	for _, line := range lines {
		if line.Role == "image" || parseImagePlacement(line.Query).hasVerticalOffset() {
			continue
		}
		lineRight := line.Col + displayWidth(stripANSI(line.Text)) - 1
		index, ok := groupsByElement[line.Element]
		if !ok {
			groups = append(groups, group{element: line.Element, minRow: line.Row, maxRow: line.Row, minCol: line.Col})
			groupsByElement[line.Element] = len(groups) - 1
			continue
		}
		g := &groups[index]
		g.minRow = min(g.minRow, line.Row)
		g.maxRow = max(g.maxRow, line.Row)
		g.minCol = min(g.minCol, min(line.Col, lineRight))
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].minRow != groups[j].minRow {
			return groups[i].minRow < groups[j].minRow
		}
		if groups[i].minCol != groups[j].minCol {
			return groups[i].minCol < groups[j].minCol
		}
		return groups[i].element < groups[j].element
	})
	deltas := map[int]int{}
	nextFreeRow := -1
	for _, group := range groups {
		delta := 0
		if nextFreeRow >= 0 && group.minRow < nextFreeRow {
			delta = nextFreeRow - group.minRow
		}
		if delta != 0 {
			deltas[group.element] = delta
		}
		nextFreeRow = max(nextFreeRow, group.maxRow+delta+1)
	}
	if len(deltas) == 0 {
		return lines
	}
	out := append([]Line(nil), lines...)
	for i := range out {
		if delta, ok := deltas[out[i].Element]; ok {
			out[i].Row += delta
		}
	}
	return out
}

func roleLines(rows []string, role string, elementIndex int, query string) []Line {
	out := make([]Line, 0, len(rows))
	for index, row := range rows {
		out = append(out, Line{Text: row, Role: role, Element: elementIndex, Query: query, Row: index})
	}
	return out
}

func outlineLinesForRows(rows []string, role string, elementIndex int, query string, baseRow, baseCol, width int) []Line {
	if len(rows) == 0 || width <= 0 {
		return nil
	}
	maxWidth := maxLineDisplayWidth(rows)
	if maxWidth <= 0 {
		return nil
	}
	filled := make([][]bool, len(rows))
	for y, row := range rows {
		filled[y] = rowFilledCells(row, role, maxWidth)
	}
	outline := map[[2]int]bool{}
	for y := range filled {
		for x, on := range filled[y] {
			if !on {
				continue
			}
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if dx == 0 && dy == 0 {
						continue
					}
					oy, ox := y+dy, x+dx
					if oy >= 0 && oy < len(filled) && ox >= 0 && ox < len(filled[oy]) && filled[oy][ox] {
						continue
					}
					outline[[2]int{oy, ox}] = true
				}
			}
		}
	}
	if len(outline) == 0 {
		return nil
	}
	byRow := map[int][]int{}
	for point := range outline {
		y, x := point[0], point[1]
		col := baseCol + x
		if col < 0 || col >= width {
			continue
		}
		byRow[y] = append(byRow[y], x)
	}
	if len(byRow) == 0 {
		return nil
	}
	rowKeys := make([]int, 0, len(byRow))
	for y := range byRow {
		rowKeys = append(rowKeys, y)
	}
	sort.Ints(rowKeys)
	query = outlineLineQuery(query)
	var out []Line
	for _, y := range rowKeys {
		xs := byRow[y]
		sort.Ints(xs)
		start := xs[0]
		prev := xs[0]
		flush := func(end int) {
			col := baseCol + start
			out = append(out, Line{
				Text:    strings.Repeat("█", end-start+1),
				Role:    "outline",
				Query:   query,
				Row:     baseRow + y,
				Col:     col,
				Element: elementIndex,
			})
		}
		for _, x := range xs[1:] {
			if x == prev+1 {
				prev = x
				continue
			}
			flush(prev)
			start = x
			prev = x
		}
		flush(prev)
	}
	return out
}

func rowFilledCells(row, role string, width int) []bool {
	out := make([]bool, width)
	if width <= 0 {
		return out
	}
	plain := []rune(stripANSI(row))
	limit := min(width, len(plain))
	if role == "code" {
		for i := 0; i < limit; i++ {
			out[i] = true
		}
		return out
	}
	for i := 0; i < limit; i++ {
		out[i] = plain[i] != ' '
	}
	return out
}

func pageLines(lines []Line, page, height int) []Line {
	if height <= 0 {
		return nil
	}
	var out []Line
	for _, line := range lines {
		row := line.Row - page*height
		if row < 0 || row >= height {
			continue
		}
		line.Row = row
		out = append(out, line)
	}
	return out
}

func pageForElement(slide Slide, width, height, element int) (int, bool) {
	if height <= 0 || element < 0 || element >= len(slide.Elements) {
		return 0, false
	}
	for page, lines := range displayPages(slide, width, height) {
		for _, line := range lines {
			if line.Element == element {
				return page, true
			}
		}
	}
	return 0, false
}

func isAbsolutePlacementQuery(query string) bool {
	placement := parseImagePlacement(query)
	return placement.hasVerticalOffset()
}

func paginateLayout(lines []Line, height int) [][]Line {
	type group struct {
		lines      []Line
		minRow     int
		maxRow     int
		minCol     int
		element    int
		order      int
		metricsSet bool
	}
	height = max(1, height)
	var groups []group
	groupByElement := map[int]int{}
	for order, line := range lines {
		if line.Element < 0 {
			continue
		}
		index, ok := groupByElement[line.Element]
		if !ok {
			groups = append(groups, group{element: line.Element, minRow: line.Row, maxRow: line.Row, minCol: line.Col, order: order})
			index = len(groups) - 1
			groupByElement[line.Element] = index
		}
		g := &groups[index]
		g.lines = append(g.lines, line)
		if line.Role == "outline" {
			continue
		}
		if !g.metricsSet {
			g.minRow = line.Row
			g.maxRow = line.Row
			g.minCol = line.Col
			g.metricsSet = true
			continue
		}
		g.minRow = min(g.minRow, line.Row)
		g.maxRow = max(g.maxRow, line.Row)
		g.minCol = min(g.minCol, line.Col)
	}
	if len(groups) == 0 {
		return [][]Line{{}}
	}
	var pages [][]Line
	ensurePage := func(page int) {
		for len(pages) <= page {
			pages = append(pages, nil)
		}
	}
	for _, group := range groups {
		groupHeight := group.maxRow - group.minRow + 1
		if groupHeight > height {
			basePage := max(0, group.minRow/height)
			for _, line := range group.lines {
				rowInGroup := line.Row - group.minRow
				pageIndex := basePage + max(0, rowInGroup)/height
				ensurePage(pageIndex)
				line.Row = rowInGroup % height
				pages[pageIndex] = append(pages[pageIndex], line)
			}
			continue
		}
		targetPage := max(0, group.minRow/height)
		targetRow := group.minRow % height
		if targetRow < 0 {
			targetRow = 0
		}
		if targetRow+groupHeight > height {
			targetPage++
			targetRow = 0
		}
		ensurePage(targetPage)
		for _, line := range group.lines {
			line.Row = targetRow + (line.Row - group.minRow)
			pages[targetPage] = append(pages[targetPage], line)
		}
	}
	if len(pages) == 0 {
		return [][]Line{{}}
	}
	return pages
}

func hasNextPage(slide Slide, width, height, page int) bool {
	return page+1 < slidePageCount(slide, width, height)
}

func clampPage(slide Slide, width, height, page int) int {
	if page < 0 {
		return 0
	}
	for page > 0 && !hasPage(slide, width, height, page) {
		page--
	}
	return page
}

func hasPage(slide Slide, width, height, page int) bool {
	return page >= 0 && page < slidePageCount(slide, width, height)
}

type imagePlacement struct {
	align       string
	top, bottom *int
	left, right *int
	rowDelta    *int
	leftPct     *float64
	rightPct    *float64
}

func parseImagePlacement(query string) imagePlacement {
	placement := imagePlacement{align: "left"}
	values, err := url.ParseQuery(query)
	if err != nil {
		return placement
	}
	switch values.Get("align") {
	case "center", "right", "left":
		placement.align = values.Get("align")
	}
	placement.top = intQuery(values, "top")
	placement.bottom = intQuery(values, "bottom")
	placement.left = intQuery(values, "left")
	placement.right = intQuery(values, "right")
	placement.rowDelta = signedIntQuery(values, "row_delta")
	placement.leftPct = floatQuery(values, "left_pct")
	placement.rightPct = floatQuery(values, "right_pct")
	return placement
}

func (placement imagePlacement) hasHorizontalOffset() bool {
	return placement.left != nil || placement.right != nil || placement.leftPct != nil || placement.rightPct != nil
}

func (placement imagePlacement) hasVerticalOffset() bool {
	return placement.top != nil || placement.bottom != nil || placement.rowDelta != nil
}

func (placement imagePlacement) hasAbsoluteOffset() bool {
	return placement.hasVerticalOffset()
}

func placementLeftCol(placement imagePlacement, width, contentWidth int) int {
	switch {
	case placement.leftPct != nil:
		return int(math.Round(*placement.leftPct * float64(max(0, width-1))))
	case placement.rightPct != nil:
		right := int(math.Round(*placement.rightPct * float64(max(0, width-1))))
		return rightAlignedCol(width, contentWidth, right)
	case placement.left != nil:
		return *placement.left
	case placement.right != nil:
		return rightAlignedCol(width, contentWidth, *placement.right)
	default:
		return 0
	}
}

func elementLayer(element Element) string {
	if element.Kind != "image" && element.Kind != "shape" {
		return "front"
	}
	values, _ := url.ParseQuery(element.Query)
	if values.Get("layer") == "front" {
		return "front"
	}
	return "back"
}

func setElementLayer(element *Element, layer string) {
	if element == nil || element.Kind != "image" && element.Kind != "shape" {
		return
	}
	switch layer {
	case "front", "back":
	default:
		return
	}
	values, _ := url.ParseQuery(element.Query)
	if layer == "back" {
		values.Del("layer")
	} else {
		values.Set("layer", layer)
	}
	element.Query = values.Encode()
}

func intQuery(values url.Values, name string) *int {
	value := values.Get(name)
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return nil
	}
	if parsed < 0 {
		parsed = 0
	}
	return &parsed
}

func signedIntQuery(values url.Values, name string) *int {
	value := values.Get(name)
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return nil
	}
	return &parsed
}

func floatQuery(values url.Values, name string) *float64 {
	value := values.Get(name)
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil
	}
	parsed = clampFloat(parsed, 0, 1)
	return &parsed
}

func rightAlignedCol(width, contentWidth, right int) int {
	return max(0, width-contentWidth-right-rightEdgeGutter(width, contentWidth))
}

func clampBlockCol(col, width, contentWidth int) int {
	if width <= 0 {
		return 0
	}
	contentWidth = max(1, contentWidth)
	if contentWidth >= width {
		return 0
	}
	return max(0, min(width-contentWidth-rightEdgeGutter(width, contentWidth), col))
}

func rightEdgeGutter(width, contentWidth int) int {
	if width <= 1 || contentWidth >= width {
		return 0
	}
	return 1
}

func maxLineDisplayWidth(rows []string) int {
	maxWidth := 0
	for _, row := range rows {
		maxWidth = max(maxWidth, displayWidth(stripANSI(row)))
	}
	return maxWidth
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(text string) string {
	return ansiPattern.ReplaceAllString(text, "")
}

func renderFull(text string, scale int) []string {
	return renderFullWithFont(text, scale, c64FullFont, 8)
}

func renderFullWithFont(text string, scale int, font map[rune][]string, glyphWidth int) []string {
	rows := renderFullRawWithFont(text, font, glyphWidth)
	rows = trimBlank(rows)
	if scale > 1 {
		rows = scaleRows(rows, scale)
	}
	return rows
}

func renderFullRawWithFont(text string, font map[rune][]string, glyphWidth int) []string {
	rows := make([]string, 8)
	for _, r := range text {
		glyph := font[r]
		if glyph == nil {
			glyph = font['?']
		}
		for i := 0; i < 8; i++ {
			rows[i] += padRunes(glyph[i], glyphWidth)
		}
	}
	return rows
}

func renderFullWrapped(text string, width, scale int) []string {
	if strings.Contains(text, "*") {
		return renderFullStyledWrapped(parseMarkdownStyledSpans(text), width, scale)
	}
	glyphWidth := max(1, 8*max(1, scale))
	chars := max(1, width/glyphWidth)
	chunks := wrapWords(text, chars)
	var out []string
	for _, chunk := range chunks {
		out = append(out, renderFull(chunk, scale)...)
	}
	return out
}

func renderFullStyledWrapped(spans []styledTextSpan, width, scale int) []string {
	scale = max(1, scale)
	lines := wrapStyledSpans(spans, width, 8*scale, 10*scale)
	var out []string
	for _, line := range lines {
		out = append(out, renderFullStyled(line, scale)...)
	}
	return out
}

func renderFullStyled(spans []styledTextSpan, scale int) []string {
	scale = max(1, scale)
	rows := make([]string, 8*scale)
	for _, span := range spans {
		font := c64FullFont
		glyphWidth := 8
		if span.Bold {
			font = c64BoldFont
			glyphWidth = 10
		}
		chunk := renderFullRawWithFont(span.Text, font, glyphWidth)
		if scale > 1 {
			chunk = scaleRows(chunk, scale)
		}
		for i := 0; i < len(rows); i++ {
			if i < len(chunk) {
				if span.Highlight {
					rows[i] += highlightVisibleText(chunk[i])
				} else {
					rows[i] += chunk[i]
				}
			}
		}
	}
	rows = trimBlank(rows)
	return rows
}

func highlightVisibleText(text string) string {
	left := len(text) - len(strings.TrimLeft(text, " "))
	right := len(strings.TrimRight(text, " "))
	if left >= right {
		return text
	}
	return text[:left] + "\033[1m" + text[left:right] + "\033[22m" + text[right:]
}

func renderBodyWrapped(text string, width int, firstPrefix, contPrefix string) []string {
	prefixWidth := max(displayWidth(firstPrefix), displayWidth(contPrefix))
	chars := max(1, (width-prefixWidth)/4)
	if strings.Contains(text, "*") {
		return renderStyledBodyWrapped(parseMarkdownStyledSpans(text), width, firstPrefix, contPrefix)
	}
	chunks := wrapWords(text, chars)
	var out []string
	for i, chunk := range chunks {
		prefix := firstPrefix
		if i > 0 {
			prefix = contPrefix
		}
		for _, row := range renderQuad(chunk) {
			out = append(out, crop(prefix+row, width))
		}
	}
	return out
}

func renderStyledBodyWrapped(spans []styledTextSpan, width int, firstPrefix, contPrefix string) []string {
	prefixWidth := max(displayWidth(firstPrefix), displayWidth(contPrefix))
	lines := wrapStyledSpans(spans, max(1, width-prefixWidth), 4, 5)
	var out []string
	for i, line := range lines {
		prefix := firstPrefix
		if i > 0 {
			prefix = contPrefix
		}
		for _, row := range renderQuadStyled(line) {
			out = append(out, cropANSIVisible(prefix+row, width))
		}
	}
	return out
}

func renderBulletWrapped(text string, width int) []string {
	blankPrefix := "        "
	prefixWidth := displayWidth(blankPrefix)
	chars := max(1, (width-prefixWidth)/4)
	if strings.Contains(text, "*") {
		return renderStyledBulletWrapped(parseMarkdownStyledSpans(text), width, blankPrefix)
	}
	chunks := wrapWords(text, chars)
	markerRows := renderQuad("·")
	var out []string
	for chunkIndex, chunk := range chunks {
		for rowIndex, row := range renderQuad(chunk) {
			prefix := blankPrefix
			if chunkIndex == 0 && rowIndex == 0 {
				prefix = padRunes(markerRows[0], prefixWidth)
			} else if chunkIndex == 0 && rowIndex < len(markerRows) {
				prefix = padRunes(markerRows[rowIndex], prefixWidth)
			}
			out = append(out, crop(prefix+row, width))
		}
	}
	return out
}

func renderStyledBulletWrapped(spans []styledTextSpan, width int, blankPrefix string) []string {
	prefixWidth := displayWidth(blankPrefix)
	lines := wrapStyledSpans(spans, max(1, width-prefixWidth), 4, 5)
	markerRows := renderQuad("·")
	var out []string
	for chunkIndex, chunk := range lines {
		for rowIndex, row := range renderQuadStyled(chunk) {
			prefix := blankPrefix
			if chunkIndex == 0 && rowIndex == 0 {
				prefix = padRunes(markerRows[0], prefixWidth)
			} else if chunkIndex == 0 && rowIndex < len(markerRows) {
				prefix = padRunes(markerRows[rowIndex], prefixWidth)
			}
			out = append(out, cropANSIVisible(prefix+row, width))
		}
	}
	return out
}

func renderQuad(text string) []string {
	rows := make([]string, 4)
	for _, r := range text {
		glyph := c64QuadFont[r]
		if glyph == nil {
			glyph = c64QuadFont['?']
		}
		for i := 0; i < 4; i++ {
			rows[i] += padRunes(glyph[i], 4)
		}
	}
	return trimBlank(rows)
}

type styledTextSpan struct {
	Text      string
	Bold      bool
	Highlight bool
}

func toggleMarkdownStyle(text, marker string) string {
	if text == "" || marker == "" {
		return text
	}
	if hasMarkdownStyleWrapper(text, marker) {
		return text[len(marker) : len(text)-len(marker)]
	}
	return marker + text + marker
}

func hasMarkdownStyleWrapper(text, marker string) bool {
	return marker != "" && len(text) >= len(marker)*2 && strings.HasPrefix(text, marker) && strings.HasSuffix(text, marker)
}

func parseMarkdownStyledSpans(text string) []styledTextSpan {
	var spans []styledTextSpan
	var b strings.Builder
	bold := false
	highlight := false
	flush := func() {
		if b.Len() == 0 {
			return
		}
		spans = appendStyledSpan(spans, styledTextSpan{Text: b.String(), Bold: bold, Highlight: highlight})
		b.Reset()
	}
	for i := 0; i < len(text); {
		if text[i] == '*' {
			run := markdownStarRun(text, i)
			consume := 0
			toggleBold := false
			toggleHighlight := false
			switch {
			case run >= 3 && (bold && highlight || hasMarkdownStarRun(text, i+run, 3, false)):
				consume = 3
				toggleBold = true
				toggleHighlight = true
			case run >= 2 && (bold || hasMarkdownStarRun(text, i+run, 2, false)):
				consume = 2
				toggleBold = true
			case run >= 1 && (highlight || hasMarkdownStarRun(text, i+run, 1, true)):
				consume = 1
				toggleHighlight = true
			}
			if consume > 0 {
				flush()
				if toggleBold {
					bold = !bold
				}
				if toggleHighlight {
					highlight = !highlight
				}
				i += consume
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		b.WriteRune(r)
		i += size
	}
	flush()
	return spans
}

func markdownStarRun(text string, start int) int {
	end := start
	for end < len(text) && text[end] == '*' {
		end++
	}
	return end - start
}

func hasMarkdownStarRun(text string, start, minimum int, odd bool) bool {
	for i := max(0, start); i < len(text); {
		if text[i] != '*' {
			_, size := utf8.DecodeRuneInString(text[i:])
			if size == 0 {
				return false
			}
			i += size
			continue
		}
		run := markdownStarRun(text, i)
		if run >= minimum && (!odd || run%2 == 1) {
			return true
		}
		i += run
	}
	return false
}

func appendStyledSpan(spans []styledTextSpan, span styledTextSpan) []styledTextSpan {
	if span.Text == "" {
		return spans
	}
	if len(spans) > 0 && spans[len(spans)-1].Bold == span.Bold && spans[len(spans)-1].Highlight == span.Highlight {
		spans[len(spans)-1].Text += span.Text
		return spans
	}
	return append(spans, span)
}

func wrapStyledSpans(spans []styledTextSpan, width, normalRuneWidth, boldRuneWidth int) [][]styledTextSpan {
	width = max(1, width)
	words := styledWords(spans)
	if len(words) == 0 {
		return [][]styledTextSpan{{}}
	}
	var lines [][]styledTextSpan
	var current []styledTextSpan
	currentLen := 0
	for _, word := range words {
		wordLen := styledSpansWidth(word, normalRuneWidth, boldRuneWidth)
		if wordLen > width {
			if currentLen > 0 {
				lines = append(lines, current)
				current = nil
				currentLen = 0
			}
			for wordLen > width {
				prefix, rest := splitStyledSpans(word, width, normalRuneWidth, boldRuneWidth)
				lines = append(lines, prefix)
				word = rest
				wordLen = styledSpansWidth(word, normalRuneWidth, boldRuneWidth)
			}
			if wordLen > 0 {
				current = word
				currentLen = wordLen
			}
			continue
		}
		if currentLen == 0 {
			current = word
			currentLen = wordLen
			continue
		}
		if currentLen+normalRuneWidth+wordLen <= width {
			current = appendStyledSpan(current, styledTextSpan{Text: " "})
			current = append(current, word...)
			currentLen += normalRuneWidth + wordLen
		} else {
			lines = append(lines, current)
			current = word
			currentLen = wordLen
		}
	}
	if currentLen > 0 {
		lines = append(lines, current)
	}
	if len(lines) == 0 {
		return [][]styledTextSpan{{}}
	}
	return lines
}

func styledWords(spans []styledTextSpan) [][]styledTextSpan {
	var words [][]styledTextSpan
	var current []styledTextSpan
	flush := func() {
		if styledSpansLen(current) == 0 {
			current = nil
			return
		}
		words = append(words, current)
		current = nil
	}
	for _, span := range spans {
		for _, r := range span.Text {
			if unicode.IsSpace(r) {
				flush()
				continue
			}
			current = appendStyledRune(current, r, span)
		}
	}
	flush()
	return words
}

func appendStyledRune(spans []styledTextSpan, r rune, style styledTextSpan) []styledTextSpan {
	style.Text = string(r)
	return appendStyledSpan(spans, style)
}

func styledSpansLen(spans []styledTextSpan) int {
	total := 0
	for _, span := range spans {
		total += len([]rune(span.Text))
	}
	return total
}

func styledSpansWidth(spans []styledTextSpan, normalRuneWidth, boldRuneWidth int) int {
	total := 0
	for _, span := range spans {
		runeWidth := normalRuneWidth
		if span.Bold {
			runeWidth = boldRuneWidth
		}
		total += len([]rune(span.Text)) * runeWidth
	}
	return total
}

func splitStyledSpans(spans []styledTextSpan, width, normalRuneWidth, boldRuneWidth int) ([]styledTextSpan, []styledTextSpan) {
	var left, right []styledTextSpan
	used := 0
	for _, span := range spans {
		runeWidth := normalRuneWidth
		if span.Bold {
			runeWidth = boldRuneWidth
		}
		for _, r := range span.Text {
			if used+runeWidth <= width || styledSpansLen(left) == 0 {
				left = appendStyledRune(left, r, span)
				used += runeWidth
			} else {
				right = appendStyledRune(right, r, span)
			}
		}
	}
	return left, right
}

func renderQuadStyled(spans []styledTextSpan) []string {
	rows := make([]string, 4)
	for _, span := range spans {
		chunk := renderQuadRaw(span.Text)
		if span.Bold {
			chunk = renderQuadBoldRaw(span.Text)
		}
		for i := 0; i < 4; i++ {
			if span.Highlight {
				rows[i] += "\033[1m" + chunk[i] + "\033[22m"
			} else {
				rows[i] += chunk[i]
			}
		}
	}
	return trimBlank(rows)
}

func renderQuadBoldRaw(text string) []string {
	const pixelWidth = 10
	rows := make([]string, 4)
	for _, r := range text {
		glyph := c64BoldFont[r]
		if glyph == nil {
			glyph = c64BoldFont['?']
		}
		mask := make([][]bool, 8)
		for y := range mask {
			mask[y] = make([]bool, pixelWidth)
			if y >= len(glyph) {
				continue
			}
			line := []rune(padRunes(glyph[y], pixelWidth))
			for x := 0; x < pixelWidth; x++ {
				mask[y][x] = line[x] != ' '
			}
		}
		quad := maskToQuadrantsPadded(mask)
		for y := range rows {
			rows[y] += padRunes(quad[y], pixelWidth/2)
		}
	}
	return rows
}

func renderQuadRaw(text string) []string {
	rows := make([]string, 4)
	for _, r := range text {
		glyph := c64QuadFont[r]
		if glyph == nil {
			glyph = c64QuadFont['?']
		}
		for i := 0; i < 4; i++ {
			rows[i] += padRunes(glyph[i], 4)
		}
	}
	return rows
}

func renderTextImageElement(element Element, width int) []string {
	values, _ := url.ParseQuery(element.Query)
	scale := 1.0
	if parsed, err := strconv.ParseFloat(values.Get("scale"), 64); err == nil && parsed > 0 {
		scale = parsed
	}
	if element.Kind == "bullet" {
		element.Text = "·  " + element.Text
	}
	var rows []string
	if strings.Contains(element.Text, "*") {
		rows = renderMarkdownBitmapTextImage(element, scale)
	} else if hasTextImageStyle(element.Query) {
		rows = renderStyledBitmapTextImage(element, scale)
	}
	if len(rows) == 0 {
		rows = renderBitmapTextImage(element.Text, scale)
	}
	for i, row := range rows {
		if strings.Contains(row, "\033[") {
			rows[i] = cropANSIVisible(row, width)
		} else {
			rows[i] = crop(row, width)
		}
	}
	return trimBlank(rows)
}

func renderMarkdownBitmapTextImage(element Element, factor float64) []string {
	spans := parseMarkdownStyledSpans(element.Text)
	chunks := make([][]string, 0, len(spans))
	maxRows := 0
	for _, span := range spans {
		font, glyphWidth := c64FullFont, 8
		if span.Bold {
			font, glyphWidth = c64BoldFont, 10
		}
		mask := scaledTextMaskWithFont(span.Text, factor, font, glyphWidth)
		var rows []string
		if hasTextImageStyle(element.Query) {
			rows = renderStyledBitmapTextMask(element, mask)
		} else {
			rows = maskToQuadrantsPadded(mask)
		}
		if span.Highlight {
			for index := range rows {
				rows[index] = highlightVisibleText(rows[index])
			}
		}
		chunks = append(chunks, rows)
		maxRows = max(maxRows, len(rows))
	}
	rows := make([]string, maxRows)
	for _, chunk := range chunks {
		chunkWidth := maxLineDisplayWidth(chunk)
		for row := 0; row < maxRows; row++ {
			if row < len(chunk) {
				rows[row] += chunk[row] + strings.Repeat(" ", max(0, chunkWidth-displayWidth(stripANSI(chunk[row]))))
			} else {
				rows[row] += strings.Repeat(" ", chunkWidth)
			}
		}
	}
	return trimBlank(rows)
}

func renderRotatedTextImageElement(element Element, width int, orientation string) []string {
	values, _ := url.ParseQuery(element.Query)
	scale := 1.0
	if parsed, err := strconv.ParseFloat(values.Get("scale"), 64); err == nil && parsed > 0 {
		scale = parsed
	} else {
		size := textSize(element)
		_, scale = textImageSourceAndScale(size)
	}
	if element.Kind == "bullet" {
		element.Text = "·  " + element.Text
	}
	mask := scaledC64TextMask(element.Text, scale)
	mask = rotateMask(mask, orientation)
	if len(mask) == 0 || len(mask[0]) == 0 {
		return nil
	}
	var rows []string
	if hasTextImageStyle(element.Query) {
		rows = renderStyledBitmapTextMask(element, mask)
	}
	if len(rows) == 0 {
		rows = maskToQuadrantsPadded(mask)
	}
	for i, row := range rows {
		if strings.Contains(row, "\033[") {
			rows[i] = cropANSIVisible(row, width)
		} else {
			rows[i] = crop(row, width)
		}
	}
	return rows
}

func hasTextImageStyle(query string) bool {
	values, err := url.ParseQuery(query)
	if err != nil {
		return false
	}
	for _, key := range []string{"glyph", "shape", "brightness", "contrast", "saturation", "sharpness", "alpha"} {
		if values.Get(key) != "" {
			return true
		}
	}
	return false
}

func renderStyledBitmapTextImage(element Element, factor float64) []string {
	mask := scaledC64TextMask(element.Text, factor)
	if len(mask) == 0 || len(mask[0]) == 0 {
		return nil
	}
	return renderStyledBitmapTextMask(element, mask)
}

func renderStyledBitmapTextMask(element Element, mask [][]bool) []string {
	if len(mask) == 0 || len(mask[0]) == 0 {
		return nil
	}
	mask = doubleMaskRows(mask)
	optsQuery := removeImageQueryKeys(element.Query, "scale")
	opts := parseImageASCIIOptions(optsQuery)
	opts.binaryTone = true
	pixelH := len(mask)
	pixelW := len(mask[0])
	pixels := make([]rgba8, pixelW*pixelH)
	r, g, b := uint8(255), uint8(255), uint8(255)
	if hex := currentElementHexForElement(element); hex != "" {
		if rr, gg, bb, ok := parseHexColour(hex); ok {
			r, g, b = uint8(rr), uint8(gg), uint8(bb)
		}
	}
	for y := 0; y < pixelH; y++ {
		for x := 0; x < pixelW; x++ {
			if mask[y][x] {
				pixels[y*pixelW+x] = rgba8{r: r, g: g, b: b, a: 255}
			}
		}
	}
	cellW := (pixelW + 1) / 2
	cellH := (pixelH + 3) / 4
	return renderUnicodeImageRows(pixels, pixelW, pixelH, cellW, cellH, opts)
}

func doubleMaskRows(mask [][]bool) [][]bool {
	out := make([][]bool, 0, len(mask)*2)
	for _, row := range mask {
		first := append([]bool(nil), row...)
		second := append([]bool(nil), row...)
		out = append(out, first, second)
	}
	return out
}

func renderBitmapTextImage(text string, factor float64) []string {
	mask := scaledC64TextMask(text, factor)
	if len(mask) == 0 {
		return nil
	}
	return maskToQuadrants(mask)
}

func scaledC64TextMask(text string, factor float64) [][]bool {
	return scaledTextMaskWithFont(text, factor, c64FullFont, 8)
}

func scaledTextMaskWithFont(text string, factor float64, font map[rune][]string, glyphWidth int) [][]bool {
	mask := textMaskWithFont(text, font, glyphWidth)
	if len(mask) == 0 || len(mask[0]) == 0 || factor <= 0 {
		return nil
	}
	srcH := len(mask)
	srcW := len(mask[0])
	dstH := max(1, int(math.Round(float64(srcH)*factor)))
	dstW := max(1, int(math.Round(float64(srcW)*factor)))
	scaled := make([][]bool, dstH)
	for y := 0; y < dstH; y++ {
		sy := min(srcH-1, int(float64(y)/factor))
		scaled[y] = make([]bool, dstW)
		for x := 0; x < dstW; x++ {
			sx := min(srcW-1, int(float64(x)/factor))
			scaled[y][x] = mask[sy][sx]
		}
	}
	return scaled
}

func rotateMask(mask [][]bool, orientation string) [][]bool {
	if len(mask) == 0 || len(mask[0]) == 0 {
		return mask
	}
	switch orientation {
	case "cw":
		return rotateMaskClockwise(mask)
	case "down":
		return rotateMaskDown(mask)
	case "ccw":
		return rotateMaskCounterClockwise(mask)
	default:
		return mask
	}
}

func rotateMaskClockwise(mask [][]bool) [][]bool {
	height := len(mask)
	width := len(mask[0])
	out := make([][]bool, width)
	for y := range out {
		out[y] = make([]bool, height)
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			out[x][height-1-y] = mask[y][x]
		}
	}
	return out
}

func rotateMaskDown(mask [][]bool) [][]bool {
	height := len(mask)
	width := len(mask[0])
	out := make([][]bool, height)
	for y := range out {
		out[y] = make([]bool, width)
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			out[height-1-y][width-1-x] = mask[y][x]
		}
	}
	return out
}

func rotateMaskCounterClockwise(mask [][]bool) [][]bool {
	height := len(mask)
	width := len(mask[0])
	out := make([][]bool, width)
	for y := range out {
		out[y] = make([]bool, height)
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			out[width-1-x][y] = mask[y][x]
		}
	}
	return out
}

func c64TextMask(text string) [][]bool {
	return textMaskWithFont(text, c64FullFont, 8)
}

func textMaskWithFont(text string, font map[rune][]string, glyphW int) [][]bool {
	rows := make([][]bool, 8)
	for i := range rows {
		rows[i] = make([]bool, 0, len([]rune(text))*glyphW)
	}
	for _, r := range text {
		glyph := font[r]
		if glyph == nil {
			glyph = font['?']
		}
		for y := 0; y < 8; y++ {
			line := []rune(padRunes(glyph[y], glyphW))
			for x := 0; x < glyphW; x++ {
				rows[y] = append(rows[y], line[x] != ' ')
			}
		}
	}
	return rows
}

func maskToQuadrants(mask [][]bool) []string {
	return trimBlank(maskToQuadrantsPadded(mask))
}

func maskToQuadrantsPadded(mask [][]bool) []string {
	if len(mask) == 0 || len(mask[0]) == 0 {
		return nil
	}
	height := len(mask)
	width := len(mask[0])
	outH := (height + 1) / 2
	outW := (width + 1) / 2
	rows := make([]string, outH)
	for y := 0; y < outH; y++ {
		var row strings.Builder
		for x := 0; x < outW; x++ {
			upperLeft := maskValue(mask, x*2, y*2)
			upperRight := maskValue(mask, x*2+1, y*2)
			lowerLeft := maskValue(mask, x*2, y*2+1)
			lowerRight := maskValue(mask, x*2+1, y*2+1)
			row.WriteRune(quadrantRune(upperLeft, upperRight, lowerLeft, lowerRight))
		}
		rows[y] = row.String()
	}
	return rows
}

func maskValue(mask [][]bool, x, y int) bool {
	return y >= 0 && y < len(mask) && x >= 0 && x < len(mask[y]) && mask[y][x]
}

func quadrantRune(upperLeft, upperRight, lowerLeft, lowerRight bool) rune {
	bits := 0
	if upperLeft {
		bits |= 1
	}
	if upperRight {
		bits |= 2
	}
	if lowerLeft {
		bits |= 4
	}
	if lowerRight {
		bits |= 8
	}
	switch bits {
	case 0:
		return ' '
	case 1:
		return '▘'
	case 2:
		return '▝'
	case 3:
		return '▀'
	case 4:
		return '▖'
	case 5:
		return '▌'
	case 6:
		return '▞'
	case 7:
		return '▛'
	case 8:
		return '▗'
	case 9:
		return '▚'
	case 10:
		return '▐'
	case 11:
		return '▜'
	case 12:
		return '▄'
	case 13:
		return '▙'
	case 14:
		return '▟'
	default:
		return '█'
	}
}

func scaleRows(rows []string, scale int) []string {
	var out []string
	for _, row := range rows {
		var expanded strings.Builder
		for _, r := range row {
			for i := 0; i < scale; i++ {
				expanded.WriteRune(r)
			}
		}
		for i := 0; i < scale; i++ {
			out = append(out, expanded.String())
		}
	}
	return out
}

type imageASCIIOptions struct {
	glyph      string
	shape      string
	fixScaling bool
	stretch    bool
	scaleX     float64
	scaleY     float64
	brightness float64
	contrast   float64
	saturation float64
	sharpness  float64
	colorful   bool
	colorMode  string
	colorBoost float64
	alphaGate  uint8
	binaryTone bool
}

type imageRenderStats struct {
	background rgba8
	hasAlpha   bool
}

type asciiImageFrame struct {
	image image.Image
	rows  []string
	delay time.Duration
}

type asciiImageAnimation struct {
	frames    []asciiImageFrame
	total     time.Duration
	opts      imageASCIIOptions
	width     int
	height    int
	cachePath string
	source    string
	warming   bool
}

type diskImageAnimation struct {
	Version int              `json:"version"`
	Source  string           `json:"source"`
	Frames  []diskImageFrame `json:"frames"`
}

type diskImageFrame struct {
	DelayMS int64    `json:"delay_ms"`
	Rows    []string `json:"rows"`
}

const diskImageAnimationVersion = 3

var asciiImageCache = map[string]*asciiImageAnimation{}
var fastASCIIImageCache = map[string][]string{}
var decodedStillImageCache = map[string]image.Image{}
var prewarmingImageCache bool
var fastImageRender bool

func renderASCIIImage(path, query string, maxWidth, maxHeight int) []string {
	if maxWidth <= 0 || maxHeight <= 0 {
		return nil
	}
	if fastImageRender {
		return renderFastASCIIImage(path, query, maxWidth, maxHeight)
	}
	renderQuery := imageRenderQuery(query)
	key := fmt.Sprintf("%s?%s@%dx%d", path, renderQuery, maxWidth, maxHeight)
	animation, ok := asciiImageCache[key]
	if !ok {
		animation = loadASCIIImageAnimation(path, renderQuery, maxWidth, maxHeight)
		asciiImageCache[key] = animation
	}
	if animation == nil || len(animation.frames) == 0 {
		return nil
	}
	if prewarmingImageCache {
		animation.warmCache()
		return animation.frameRows(0)
	}
	if len(animation.frames) == 1 || animation.total <= 0 {
		return animation.frameRows(0)
	}
	position := time.Duration(time.Now().UnixNano()) % animation.total
	if exportImageAnimationPosition != nil {
		position = *exportImageAnimationPosition % animation.total
	}
	var elapsed time.Duration
	for index, frame := range animation.frames {
		elapsed += frame.delay
		if position < elapsed {
			return animation.frameRows(index)
		}
	}
	return animation.frameRows(len(animation.frames) - 1)
}

func imageRenderQuery(query string) string {
	values, err := url.ParseQuery(query)
	if err != nil {
		return query
	}
	for _, key := range []string{
		"top", "bottom", "left", "right", "left_pct", "right_pct", "row_delta", "align", "width", "height", "layer", "outline", "link", "slide",
	} {
		values.Del(key)
	}
	return values.Encode()
}

func renderFastASCIIImage(path, query string, maxWidth, maxHeight int) []string {
	if maxWidth <= 0 || maxHeight <= 0 {
		return nil
	}
	renderQuery := imageRenderQuery(query)
	key := fmt.Sprintf("fast|%s?%s@%dx%d", path, renderQuery, maxWidth, maxHeight)
	if rows, ok := fastASCIIImageCache[key]; ok {
		return rows
	}
	img := loadDecodedStillImage(path)
	if img == nil {
		return nil
	}
	opts := parseImageASCIIOptions(renderQuery)
	rows := renderFastASCIIImageRows(img, opts, maxWidth, maxHeight)
	fastASCIIImageCache[key] = rows
	return rows
}

func loadDecodedStillImage(path string) image.Image {
	info, err := os.Stat(path)
	stamp := "missing"
	if err == nil {
		stamp = fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
	}
	key := path + "|" + stamp
	if img, ok := decodedStillImageCache[key]; ok {
		return img
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil
	}
	decodedStillImageCache[key] = img
	return img
}

func renderFastASCIIImageRows(img image.Image, opts imageASCIIOptions, maxWidth, maxHeight int) []string {
	b := imageContentBounds(img, hasTransparency(img))
	return renderUnicodeImage(img, b, opts, maxWidth, maxHeight, false)
}

func renderUnicodeImage(img image.Image, b image.Rectangle, opts imageASCIIOptions, maxWidth, maxHeight int, lanczos bool) []string {
	width, height := b.Dx(), b.Dy()
	if width <= 0 || height <= 0 {
		return nil
	}
	targetW := maxWidth
	targetH := maxHeight
	if !opts.stretch {
		aspectScale := 1.0
		if opts.fixScaling {
			aspectScale = 2.0
		}
		targetW = min(maxWidth, width)
		targetH = max(1, int(math.Round(float64(height)/float64(width)*float64(targetW)/aspectScale)))
		if targetH > maxHeight {
			targetH = maxHeight
			targetW = max(1, min(maxWidth, int(math.Round(float64(width)/float64(height)*float64(targetH)*aspectScale))))
		}
	}
	targetW = max(1, min(maxWidth, int(float64(targetW)*opts.scaleX)))
	targetH = max(1, min(maxHeight, int(float64(targetH)*opts.scaleY)))
	sampleW := targetW * 2
	sampleH := targetH * 4
	var pixels []rgba8
	if lanczos {
		pixels = resizeLanczosRGBA(img, b, sampleW, sampleH)
	} else {
		pixels = resizeNearestRGBA(img, b, sampleW, sampleH)
	}
	if opts.brightness != 1 || opts.contrast != 1 || opts.saturation != 1 {
		adjustImagePixels(pixels, opts)
	}
	if opts.sharpness != 1 {
		pixels = enhanceSharpnessRGBA(pixels, sampleW, sampleH, opts.sharpness)
	}
	return renderUnicodeImageRows(pixels, sampleW, sampleH, targetW, targetH, opts)
}

func loadASCIIImageAnimation(path, query string, maxWidth, maxHeight int) *asciiImageAnimation {
	opts := parseImageASCIIOptions(query)
	cachePath := asciiAnimationCachePath(path, query, maxWidth, maxHeight)
	if cached := loadDiskASCIIAnimation(cachePath); cached != nil {
		return cached
	}
	frames := decodeImageFrames(path)
	if len(frames) == 0 {
		return &asciiImageAnimation{}
	}
	asciiFrames := make([]asciiImageFrame, 0, len(frames))
	var total time.Duration
	for _, frame := range frames {
		delay := frame.delay
		if delay <= 0 {
			delay = 100 * time.Millisecond
		}
		asciiFrames = append(asciiFrames, asciiImageFrame{image: frame.image, delay: delay})
		total += delay
	}
	return &asciiImageAnimation{
		frames:    asciiFrames,
		total:     total,
		opts:      opts,
		width:     maxWidth,
		height:    maxHeight,
		cachePath: cachePath,
		source:    path,
	}
}

func (animation *asciiImageAnimation) frameRows(index int) []string {
	if index < 0 || index >= len(animation.frames) {
		return nil
	}
	if animation.frames[index].rows == nil {
		animation.frames[index].rows = renderASCIIImageRows(animation.frames[index].image, animation.opts, animation.width, animation.height)
		if animation.cachePath != "" && animation.allRowsRendered() {
			saveDiskASCIIAnimation(animation.cachePath, animation.source, animation)
		}
	}
	return animation.frames[index].rows
}

func (animation *asciiImageAnimation) allRowsRendered() bool {
	for _, frame := range animation.frames {
		if frame.rows == nil {
			return false
		}
	}
	return true
}

func (animation *asciiImageAnimation) warmCache() {
	if animation.allRowsRendered() {
		return
	}
	rendered := map[string][]string{}
	for index := range animation.frames {
		if animation.frames[index].rows == nil {
			hash := imageFrameHash(animation.frames[index].image)
			if rows, ok := rendered[hash]; ok {
				animation.frames[index].rows = rows
			} else {
				animation.frames[index].rows = renderASCIIImageRows(animation.frames[index].image, animation.opts, animation.width, animation.height)
				rendered[hash] = animation.frames[index].rows
			}
		} else {
			rendered[imageFrameHash(animation.frames[index].image)] = animation.frames[index].rows
		}
	}
	if animation.cachePath != "" && animation.allRowsRendered() {
		saveDiskASCIIAnimation(animation.cachePath, animation.source, animation)
	}
}

func imageFrameHash(img image.Image) string {
	bounds := img.Bounds()
	h := sha256.New()
	fmt.Fprintf(h, "%dx%d:", bounds.Dx(), bounds.Dy())
	var pixel [4]byte
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := pixelRGBA(img, x, y)
			pixel[0], pixel[1], pixel[2], pixel[3] = r, g, b, a
			_, _ = h.Write(pixel[:])
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func asciiAnimationCachePath(path, query string, maxWidth, maxHeight int) string {
	info, err := os.Stat(path)
	stamp := "missing"
	if err == nil {
		stamp = fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("v3|%s|%s|%s|%dx%d", path, stamp, query, maxWidth, maxHeight)))
	name := hex.EncodeToString(sum[:]) + ".json"
	return filepath.Join(".keynope", "cache", name)
}

func cacheSourcePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func cleanUnusedImageCache(slides []Slide) {
	used := map[string]bool{}
	for _, slide := range slides {
		for _, element := range slide.Elements {
			if element.Kind == "image" {
				used[cacheSourcePath(element.Path)] = true
			}
		}
	}
	entries, err := os.ReadDir(filepath.Join(".keynope", "cache"))
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(".keynope", "cache", entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var disk diskImageAnimation
		if json.Unmarshal(data, &disk) != nil || disk.Version != diskImageAnimationVersion || disk.Source == "" || !used[cacheSourcePath(disk.Source)] {
			_ = os.Remove(path)
		}
	}
}

func loadDiskASCIIAnimation(path string) *asciiImageAnimation {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var disk diskImageAnimation
	if json.Unmarshal(data, &disk) != nil || disk.Version != diskImageAnimationVersion || len(disk.Frames) == 0 {
		return nil
	}
	frames := make([]asciiImageFrame, 0, len(disk.Frames))
	var total time.Duration
	for _, frame := range disk.Frames {
		delay := time.Duration(frame.DelayMS) * time.Millisecond
		if delay <= 0 {
			delay = 100 * time.Millisecond
		}
		frames = append(frames, asciiImageFrame{rows: frame.Rows, delay: delay})
		total += delay
	}
	return &asciiImageAnimation{frames: frames, total: total}
}

func saveDiskASCIIAnimation(path, source string, animation *asciiImageAnimation) {
	if animation == nil || len(animation.frames) == 0 {
		return
	}
	disk := diskImageAnimation{Version: diskImageAnimationVersion, Source: cacheSourcePath(source)}
	for _, frame := range animation.frames {
		if frame.rows == nil {
			return
		}
		disk.Frames = append(disk.Frames, diskImageFrame{
			DelayMS: frame.delay.Milliseconds(),
			Rows:    frame.rows,
		})
	}
	data, err := json.Marshal(disk)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

type decodedImageFrame struct {
	image image.Image
	delay time.Duration
}

func decodeImageFrames(path string) []decodedImageFrame {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".gif" {
		return decodeGIFFrames(path)
	}
	if ext == ".webp" {
		if frames := decodeWebPFrames(path); len(frames) > 0 {
			return frames
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil
	}
	return []decodedImageFrame{{image: img, delay: 100 * time.Millisecond}}
}

func decodeGIFFrames(path string) []decodedImageFrame {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil || len(g.Image) == 0 {
		return nil
	}
	bounds := image.Rect(0, 0, g.Config.Width, g.Config.Height)
	canvas := image.NewRGBA(bounds)
	previous := image.NewRGBA(bounds)
	frames := make([]decodedImageFrame, 0, len(g.Image))
	for i, src := range g.Image {
		draw.Draw(previous, bounds, canvas, image.Point{}, draw.Src)
		draw.Draw(canvas, src.Bounds(), src, src.Bounds().Min, draw.Over)
		snapshot := image.NewRGBA(bounds)
		draw.Draw(snapshot, bounds, canvas, image.Point{}, draw.Src)
		delay := 100 * time.Millisecond
		if i < len(g.Delay) && g.Delay[i] > 0 {
			delay = time.Duration(g.Delay[i]) * 10 * time.Millisecond
		}
		frames = append(frames, decodedImageFrame{image: snapshot, delay: delay})
		if i < len(g.Disposal) {
			switch g.Disposal[i] {
			case gif.DisposalBackground:
				draw.Draw(canvas, src.Bounds(), image.Transparent, image.Point{}, draw.Src)
			case gif.DisposalPrevious:
				draw.Draw(canvas, bounds, previous, image.Point{}, draw.Src)
			}
		}
	}
	return frames
}

func decodeWebPFrames(path string) []decodedImageFrame {
	data, err := os.ReadFile(path)
	if err != nil || len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return nil
	}
	pos := 12
	canvasW, canvasH := 0, 0
	var frames []decodedImageFrame
	canvas := (*image.RGBA)(nil)
	for pos+8 <= len(data) {
		chunkType := string(data[pos : pos+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		chunkStart := pos + 8
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > len(data) {
			return nil
		}
		chunk := data[chunkStart:chunkEnd]
		switch chunkType {
		case "VP8X":
			if len(chunk) >= 10 {
				canvasW = 1 + int(readWebP24(chunk[4:7]))
				canvasH = 1 + int(readWebP24(chunk[7:10]))
				canvas = image.NewRGBA(image.Rect(0, 0, canvasW, canvasH))
			}
		case "ANMF":
			if len(chunk) >= 16 && canvas != nil {
				x := int(readWebP24(chunk[0:3])) * 2
				y := int(readWebP24(chunk[3:6])) * 2
				w := 1 + int(readWebP24(chunk[6:9]))
				h := 1 + int(readWebP24(chunk[9:12]))
				delay := time.Duration(readWebP24(chunk[12:15])) * time.Millisecond
				if delay <= 0 {
					delay = 100 * time.Millisecond
				}
				flags := chunk[15]
				frameImage, err := decodeWebPFramePayload(chunk[16:], w, h)
				if err == nil {
					if flags&2 != 0 {
						draw.Draw(canvas, image.Rect(x, y, x+w, y+h), image.Transparent, image.Point{}, draw.Src)
					}
					operator := draw.Over
					if flags&1 != 0 {
						operator = draw.Src
					}
					draw.Draw(canvas, image.Rect(x, y, x+w, y+h), frameImage, frameImage.Bounds().Min, operator)
					snapshot := image.NewRGBA(image.Rect(0, 0, canvasW, canvasH))
					draw.Draw(snapshot, snapshot.Bounds(), canvas, image.Point{}, draw.Src)
					frames = append(frames, decodedImageFrame{image: snapshot, delay: delay})
					if flags&2 != 0 {
						draw.Draw(canvas, image.Rect(x, y, x+w, y+h), image.Transparent, image.Point{}, draw.Src)
					}
				}
			}
		}
		pos = chunkEnd
		if chunkSize%2 == 1 {
			pos++
		}
	}
	if len(frames) > 0 {
		return frames
	}
	img, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return []decodedImageFrame{{image: img, delay: 100 * time.Millisecond}}
}

func decodeWebPFramePayload(payload []byte, width, height int) (image.Image, error) {
	var chunks []byte
	for pos := 0; pos+8 <= len(payload); {
		chunkSize := int(binary.LittleEndian.Uint32(payload[pos+4 : pos+8]))
		end := pos + 8 + chunkSize
		if end > len(payload) {
			break
		}
		chunks = append(chunks, payload[pos:end]...)
		if chunkSize%2 == 1 && end < len(payload) {
			chunks = append(chunks, payload[end])
			end++
		}
		pos = end
	}
	if len(chunks) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	var riff bytes.Buffer
	riff.WriteString("RIFF")
	size := uint32(4 + len(chunks))
	_ = binary.Write(&riff, binary.LittleEndian, size)
	riff.WriteString("WEBP")
	riff.Write(chunks)
	return webp.Decode(bytes.NewReader(riff.Bytes()))
}

func readWebP24(data []byte) uint32 {
	return uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16
}

func downscaleSourceImage(img image.Image, maxDimension int) image.Image {
	if maxDimension <= 0 {
		return img
	}
	b := img.Bounds()
	width, height := b.Dx(), b.Dy()
	longest := max(width, height)
	if longest <= maxDimension || width <= 0 || height <= 0 {
		return img
	}
	scale := float64(maxDimension) / float64(longest)
	targetW := max(1, int(math.Round(float64(width)*scale)))
	targetH := max(1, int(math.Round(float64(height)*scale)))
	out := image.NewNRGBA(image.Rect(0, 0, targetW, targetH))
	for y := 0; y < targetH; y++ {
		sy := b.Min.Y + min(height-1, int(float64(y)*float64(height)/float64(targetH)))
		for x := 0; x < targetW; x++ {
			sx := b.Min.X + min(width-1, int(float64(x)*float64(width)/float64(targetW)))
			r, g, bl, a := pixelRGBA(img, sx, sy)
			i := out.PixOffset(x, y)
			out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = r, g, bl, a
		}
	}
	return out
}

func renderASCIIImageRows(img image.Image, opts imageASCIIOptions, maxWidth, maxHeight int) []string {
	img = downscaleSourceImage(img, 512)
	transparent := hasTransparency(img)
	b := imageContentBounds(img, transparent)
	return renderUnicodeImage(img, b, opts, maxWidth, maxHeight, true)
}

func parseImageASCIIOptions(query string) imageASCIIOptions {
	opts := imageASCIIOptions{
		glyph:      "blocks",
		shape:      "subject",
		fixScaling: true,
		scaleX:     1,
		scaleY:     1,
		brightness: 1,
		contrast:   1,
		saturation: 1,
		sharpness:  1,
		colorful:   true,
		colorMode:  "fg",
		colorBoost: 1.0,
		alphaGate:  96,
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		return opts
	}
	switch strings.ToLower(values.Get("glyph")) {
	case "braille", "shade", "ascii", "dense":
		opts.glyph = strings.ToLower(values.Get("glyph"))
	case "blocks", "block", "":
		opts.glyph = "blocks"
	}
	switch strings.ToLower(values.Get("shape")) {
	case "luma", "alpha", "saturation", "contrast":
		opts.shape = strings.ToLower(values.Get("shape"))
	case "subject", "":
		opts.shape = "subject"
	}
	if fixScaling := values.Get("fix_scaling"); fixScaling != "" {
		opts.fixScaling = !(fixScaling == "0" || fixScaling == "false" || fixScaling == "no")
	}
	if stretch := values.Get("stretch"); stretch != "" {
		opts.stretch = stretch == "1" || stretch == "true" || stretch == "yes"
	}
	if scale := values.Get("scale"); scale != "" {
		left, right, hasPair := strings.Cut(scale, ",")
		if parsed, err := strconv.ParseFloat(left, 64); err == nil && parsed > 0 {
			opts.scaleX = clampFloat(parsed, 0.1, 1.0)
			opts.scaleY = opts.scaleX
		}
		if hasPair {
			if parsed, err := strconv.ParseFloat(right, 64); err == nil && parsed > 0 {
				opts.scaleY = clampFloat(parsed, 0.1, 1.0)
			}
		}
	}
	if brightness := values.Get("brightness"); brightness != "" {
		if parsed, err := strconv.ParseFloat(brightness, 64); err == nil && parsed > 0 {
			opts.brightness = clampFloat(parsed, 0.2, 2.0)
		}
	}
	if contrast := values.Get("contrast"); contrast != "" {
		if parsed, err := strconv.ParseFloat(contrast, 64); err == nil && parsed > 0 {
			opts.contrast = clampFloat(parsed, 0.2, 2.0)
		}
	}
	if saturation := values.Get("saturation"); saturation != "" {
		if parsed, err := strconv.ParseFloat(saturation, 64); err == nil && parsed >= 0 {
			opts.saturation = clampFloat(parsed, 0, 2.0)
		}
	}
	if sharpness := values.Get("sharpness"); sharpness != "" {
		if parsed, err := strconv.ParseFloat(sharpness, 64); err == nil && parsed > 0 {
			opts.sharpness = clampFloat(parsed, 0.2, 2.0)
		}
	}
	if alphaGate := values.Get("alpha"); alphaGate != "" {
		if parsed, err := strconv.Atoi(alphaGate); err == nil {
			opts.alphaGate = uint8(clampFloat(float64(parsed), 0, 255))
		}
	}
	return opts
}

func resizeNearestRGBA(img image.Image, box image.Rectangle, targetW, targetH int) []rgba8 {
	srcW, srcH := box.Dx(), box.Dy()
	out := make([]rgba8, targetW*targetH)
	for y := 0; y < targetH; y++ {
		sy := box.Min.Y + min(srcH-1, int(float64(y)*float64(srcH)/float64(targetH)))
		for x := 0; x < targetW; x++ {
			sx := box.Min.X + min(srcW-1, int(float64(x)*float64(srcW)/float64(targetW)))
			r, g, b, a := pixelRGBA(img, sx, sy)
			out[y*targetW+x] = rgba8{r: r, g: g, b: b, a: a}
		}
	}
	return out
}

func renderUnicodeImageRows(pixels []rgba8, pixelW, pixelH, cellW, cellH int, opts imageASCIIOptions) []string {
	stats := analyzeImagePixels(pixels)
	rows := make([]string, cellH)
	for cy := 0; cy < cellH; cy++ {
		var sb strings.Builder
		for cx := 0; cx < cellW; cx++ {
			block := sampleUnicodeCell(pixels, pixelW, pixelH, cx, cy)
			decisions := classifyImageBlock(block, opts, stats)
			char := unicodeImageCellRune(block, decisions, opts)
			if char == ' ' || char == '\u2800' {
				sb.WriteRune(' ')
				continue
			}
			colour := averageImageBlockColour(block, decisions)
			rr, gg, bb := boostRGB(colour.r, colour.g, colour.b, opts.colorBoost)
			writeRGBSequence(&sb, "38", rr, gg, bb, char)
		}
		sb.WriteString("\033[0m")
		rows[cy] = sb.String()
	}
	return rows
}

func sampleUnicodeCell(pixels []rgba8, pixelW, pixelH, cx, cy int) [8]rgba8 {
	var block [8]rgba8
	for py := 0; py < 4; py++ {
		y := min(pixelH-1, cy*4+py)
		for px := 0; px < 2; px++ {
			x := min(pixelW-1, cx*2+px)
			block[py*2+px] = pixels[y*pixelW+x]
		}
	}
	return block
}

func unicodeImageCellRune(block [8]rgba8, decisions [8]bool, opts imageASCIIOptions) rune {
	switch opts.glyph {
	case "braille":
		dotBits := []int{0, 3, 1, 4, 2, 5, 6, 7}
		mask := 0
		for i, bit := range dotBits {
			if decisions[i] {
				mask |= 1 << bit
			}
		}
		return rune(0x2800 + mask)
	case "shade":
		if opts.binaryTone {
			return binaryToneGlyph(decisions, []rune(" ░▒▓█"))
		}
		return toneGlyph(block, decisions, []rune(" ░▒▓█"))
	case "ascii":
		if opts.binaryTone {
			return binaryToneGlyph(decisions, []rune(" .:-=+*#%@"))
		}
		return toneGlyph(block, decisions, []rune(" .:-=+*#%@"))
	case "dense":
		if opts.binaryTone {
			return binaryToneGlyph(decisions, []rune(" .`',-:;_!i+*#%@"))
		}
		return toneGlyph(block, decisions, []rune(" .`',-:;_!i+*#%@"))
	}
	ul := decisions[0] || decisions[2]
	ur := decisions[1] || decisions[3]
	ll := decisions[4] || decisions[6]
	lr := decisions[5] || decisions[7]
	mask := 0
	if ul {
		mask |= 1
	}
	if ur {
		mask |= 2
	}
	if ll {
		mask |= 4
	}
	if lr {
		mask |= 8
	}
	switch mask {
	case 0:
		return ' '
	case 1:
		return '▘'
	case 2:
		return '▝'
	case 3:
		return '▀'
	case 4:
		return '▖'
	case 5:
		return '▌'
	case 6:
		return '▞'
	case 7:
		return '▛'
	case 8:
		return '▗'
	case 9:
		return '▚'
	case 10:
		return '▐'
	case 11:
		return '▜'
	case 12:
		return '▄'
	case 13:
		return '▙'
	case 14:
		return '▟'
	default:
		return '█'
	}
}

func binaryToneGlyph(decisions [8]bool, ramp []rune) rune {
	if len(ramp) == 0 {
		return ' '
	}
	count := 0
	for _, on := range decisions {
		if on {
			count++
		}
	}
	if count == 0 {
		return ' '
	}
	index := min(len(ramp)-1, int(math.Ceil(float64(count)*float64(len(ramp)-1)/8.0)))
	return ramp[index]
}

func toneGlyph(block [8]rgba8, decisions [8]bool, ramp []rune) rune {
	if len(ramp) == 0 {
		return ' '
	}
	luma, count := 0, 0
	for i, pixel := range block {
		if decisions[i] {
			luma += imagePixelLuma(pixel)
			count++
		}
	}
	if count == 0 {
		return ' '
	}
	value := luma / count
	index := min(len(ramp)-1, value*len(ramp)/256)
	return ramp[index]
}

func classifyImageBlock(block [8]rgba8, opts imageASCIIOptions, stats imageRenderStats) [8]bool {
	var out [8]bool
	localMin, localMax := 255, 0
	for _, p := range block {
		if p.a < opts.alphaGate {
			continue
		}
		localMin = min(localMin, imagePixelLuma(p))
		localMax = max(localMax, imagePixelLuma(p))
	}
	for i, p := range block {
		out[i] = imagePixelOn(p, opts, stats, localMin, localMax)
	}
	return out
}

func imagePixelOn(p rgba8, opts imageASCIIOptions, stats imageRenderStats, localMin, localMax int) bool {
	if p.a < opts.alphaGate {
		return false
	}
	switch opts.shape {
	case "alpha":
		return true
	case "luma":
		return imagePixelLuma(p) >= 132
	case "saturation":
		return imagePixelLuma(p) >= 132 || imagePixelSaturation(p) >= 45
	case "contrast":
		if localMax-localMin < 18 {
			return imagePixelLuma(p) >= 132 || imagePixelSaturation(p) >= 45
		}
		return imagePixelLuma(p) >= localMin+(localMax-localMin)/3
	default:
		if stats.hasAlpha {
			return true
		}
		return imageColourDistance(p, stats.background) >= 32 || imagePixelLuma(p) >= 132 || imagePixelSaturation(p) >= 58
	}
}

func analyzeImagePixels(pixels []rgba8) imageRenderStats {
	hasAlpha := false
	for _, p := range pixels {
		if p.a < 255 {
			hasAlpha = true
			break
		}
	}
	if len(pixels) == 0 {
		return imageRenderStats{hasAlpha: hasAlpha}
	}
	return imageRenderStats{background: pixels[0], hasAlpha: hasAlpha}
}

func averageImageBlockColour(block [8]rgba8, decisions [8]bool) rgba8 {
	var r, g, b, n int
	for i, p := range block {
		if decisions[i] {
			r += int(p.r)
			g += int(p.g)
			b += int(p.b)
			n++
		}
	}
	if n == 0 {
		for _, p := range block {
			r += int(p.r)
			g += int(p.g)
			b += int(p.b)
		}
		n = len(block)
	}
	return rgba8{r: uint8(r / n), g: uint8(g / n), b: uint8(b / n), a: 255}
}

func adjustImagePixels(pixels []rgba8, opts imageASCIIOptions) {
	for i, p := range pixels {
		r, g, b := adjustRGB(p.r, p.g, p.b, opts.brightness, opts.contrast, opts.saturation)
		pixels[i].r, pixels[i].g, pixels[i].b = r, g, b
	}
}

func adjustRGB(r, g, b uint8, brightness, contrast, saturation float64) (uint8, uint8, uint8) {
	rf := (float64(r)-128)*contrast + 128
	gf := (float64(g)-128)*contrast + 128
	bf := (float64(b)-128)*contrast + 128
	gray := 0.299*rf + 0.587*gf + 0.114*bf
	rf = (gray + (rf-gray)*saturation) * brightness
	gf = (gray + (gf-gray)*saturation) * brightness
	bf = (gray + (bf-gray)*saturation) * brightness
	return uint8(clampFloat(rf, 0, 255)), uint8(clampFloat(gf, 0, 255)), uint8(clampFloat(bf, 0, 255))
}

func imagePixelLuma(p rgba8) int {
	if p.a < 1 {
		return 0
	}
	return (299*int(p.r) + 587*int(p.g) + 114*int(p.b)) / 1000
}

func imagePixelSaturation(p rgba8) int {
	hi := max(int(p.r), max(int(p.g), int(p.b)))
	lo := min(int(p.r), min(int(p.g), int(p.b)))
	return hi - lo
}

func imageColourDistance(a, b rgba8) int {
	dr := int(a.r) - int(b.r)
	dg := int(a.g) - int(b.g)
	db := int(a.b) - int(b.b)
	return int(math.Sqrt(float64(dr*dr + dg*dg + db*db)))
}

func imageLuma(pixels []rgba8, brightness float64, useAlphaVisibility bool) []float64 {
	out := make([]float64, len(pixels))
	for index, pixel := range pixels {
		lumaByte := (299*int(pixel.r) + 587*int(pixel.g) + 114*int(pixel.b)) / 1000
		luma := float64(lumaByte) * brightness
		if useAlphaVisibility {
			alpha := int(pixel.a)
			if alpha <= 0 {
				out[index] = 0
				continue
			}
			luma = float64((255-lumaByte)*alpha/255) * brightness
		}
		out[index] = clampFloat(luma, 0, 255)
	}
	return out
}

func applyBrightness(pixels []rgba8, brightness float64) {
	for i := range pixels {
		pixels[i].r = uint8(clampFloat(float64(pixels[i].r)*brightness, 0, 255))
		pixels[i].g = uint8(clampFloat(float64(pixels[i].g)*brightness, 0, 255))
		pixels[i].b = uint8(clampFloat(float64(pixels[i].b)*brightness, 0, 255))
	}
}

func enhanceSharpnessRGBA(pixels []rgba8, width, height int, factor float64) []rgba8 {
	out := make([]rgba8, len(pixels))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var sumR, sumG, sumB float64
			var count float64
			for yy := -1; yy <= 1; yy++ {
				py := max(0, min(height-1, y+yy))
				for xx := -1; xx <= 1; xx++ {
					px := max(0, min(width-1, x+xx))
					p := pixels[py*width+px]
					sumR += float64(p.r)
					sumG += float64(p.g)
					sumB += float64(p.b)
					count++
				}
			}
			original := pixels[y*width+x]
			out[y*width+x] = rgba8{
				r: uint8(clampFloat((sumR/count)*(1-factor)+float64(original.r)*factor, 0, 255)),
				g: uint8(clampFloat((sumG/count)*(1-factor)+float64(original.g)*factor, 0, 255)),
				b: uint8(clampFloat((sumB/count)*(1-factor)+float64(original.b)*factor, 0, 255)),
				a: original.a,
			}
		}
	}
	return out
}

type rgba8 struct {
	r, g, b, a uint8
}

type resampleCoeffs struct {
	bounds []int
	coeffs []int
	ksize  int
}

const pillowPrecisionBits = 22

func resizeLanczosRGBA(img image.Image, box image.Rectangle, targetW, targetH int) []rgba8 {
	srcW, srcH := box.Dx(), box.Dy()
	src := make([]rgba8, srcW*srcH)
	for y := 0; y < srcH; y++ {
		for x := 0; x < srcW; x++ {
			r, g, b, a := pixelRGBA(img, box.Min.X+x, box.Min.Y+y)
			src[y*srcW+x] = rgba8{r: r, g: g, b: b, a: a}
		}
	}

	h := precomputePillowCoeffs(srcW, 0, float64(srcW), targetW)
	tmp := make([]rgba8, targetW*srcH)
	rounder := 1 << (pillowPrecisionBits - 1)
	for y := 0; y < srcH; y++ {
		for x := 0; x < targetW; x++ {
			xmin := h.bounds[x*2]
			xmax := h.bounds[x*2+1]
			k := h.coeffs[x*h.ksize:]
			ssR, ssG, ssB, ssA := rounder, rounder, rounder, rounder
			for i := 0; i < xmax; i++ {
				p := src[y*srcW+xmin+i]
				coef := k[i]
				ssR += int(p.r) * coef
				ssG += int(p.g) * coef
				ssB += int(p.b) * coef
				ssA += int(p.a) * coef
			}
			tmp[y*targetW+x] = rgba8{
				r: clipPillow8(ssR),
				g: clipPillow8(ssG),
				b: clipPillow8(ssB),
				a: clipPillow8(ssA),
			}
		}
	}

	v := precomputePillowCoeffs(srcH, 0, float64(srcH), targetH)
	out := make([]rgba8, targetW*targetH)
	for y := 0; y < targetH; y++ {
		ymin := v.bounds[y*2]
		ymax := v.bounds[y*2+1]
		k := v.coeffs[y*v.ksize:]
		for x := 0; x < targetW; x++ {
			ssR, ssG, ssB, ssA := rounder, rounder, rounder, rounder
			for i := 0; i < ymax; i++ {
				p := tmp[(ymin+i)*targetW+x]
				coef := k[i]
				ssR += int(p.r) * coef
				ssG += int(p.g) * coef
				ssB += int(p.b) * coef
				ssA += int(p.a) * coef
			}
			out[y*targetW+x] = rgba8{
				r: clipPillow8(ssR),
				g: clipPillow8(ssG),
				b: clipPillow8(ssB),
				a: clipPillow8(ssA),
			}
		}
	}
	return out
}

func precomputePillowCoeffs(inSize int, in0, in1 float64, outSize int) resampleCoeffs {
	scale := (in1 - in0) / float64(outSize)
	filterScale := scale
	if filterScale < 1.0 {
		filterScale = 1.0
	}
	support := 3.0 * filterScale
	ksize := int(math.Ceil(support))*2 + 1
	invFilterScale := 1.0 / filterScale
	bounds := make([]int, outSize*2)
	pre := make([]float64, outSize*ksize)

	for xx := 0; xx < outSize; xx++ {
		center := in0 + (float64(xx)+0.5)*scale
		xmin := int(center - support + 0.5)
		if xmin < 0 {
			xmin = 0
		}
		xmax := int(center + support + 0.5)
		if xmax > inSize {
			xmax = inSize
		}
		xmax -= xmin

		sum := 0.0
		row := pre[xx*ksize:]
		for x := 0; x < xmax; x++ {
			w := lanczosWeight((float64(x+xmin) - center + 0.5) * invFilterScale)
			row[x] = w
			sum += w
		}
		if sum != 0 {
			for x := 0; x < xmax; x++ {
				row[x] /= sum
			}
		}
		bounds[xx*2] = xmin
		bounds[xx*2+1] = xmax
	}

	coeffs := make([]int, len(pre))
	scaleInt := float64(1 << pillowPrecisionBits)
	for i, value := range pre {
		if value < 0 {
			coeffs[i] = int(-0.5 + value*scaleInt)
		} else {
			coeffs[i] = int(0.5 + value*scaleInt)
		}
	}
	return resampleCoeffs{bounds: bounds, coeffs: coeffs, ksize: ksize}
}

func clipPillow8(value int) uint8 {
	value >>= pillowPrecisionBits
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return uint8(value)
}

func imageContentBounds(img image.Image, useAlpha bool) image.Rectangle {
	b := img.Bounds()
	if !useAlpha {
		return b
	}
	found := false
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, b.Min.X, b.Min.Y
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := pixelRGBA(img, x, y)
			if a <= 0 {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x+1 > maxX {
				maxX = x + 1
			}
			if y+1 > maxY {
				maxY = y + 1
			}
			found = true
		}
	}
	if !found {
		return b
	}
	return image.Rect(minX, minY, maxX, maxY)
}

func sampleLanczosRGBA(img image.Image, offX, offY, width, height int, x, y float64) (float64, float64, float64, float64) {
	const radius = 3.0
	minX := int(math.Floor(x - radius + 1))
	maxX := int(math.Floor(x + radius))
	minY := int(math.Floor(y - radius + 1))
	maxY := int(math.Floor(y + radius))
	var r, g, b, a, total float64
	for yy := minY; yy <= maxY; yy++ {
		wy := lanczosWeight(y - float64(yy))
		if wy == 0 {
			continue
		}
		py := max(0, min(height-1, yy))
		for xx := minX; xx <= maxX; xx++ {
			wx := lanczosWeight(x - float64(xx))
			if wx == 0 {
				continue
			}
			weight := wx * wy
			px := max(0, min(width-1, xx))
			rr, gg, bb, aa := pixelRGBA(img, offX+px, offY+py)
			r += float64(rr) * weight
			g += float64(gg) * weight
			b += float64(bb) * weight
			a += float64(aa) * weight
			total += weight
		}
	}
	if total == 0 {
		rr, gg, bb, aa := pixelRGBA(img, offX+max(0, min(width-1, int(math.Round(x)))), offY+max(0, min(height-1, int(math.Round(y)))))
		return float64(rr), float64(gg), float64(bb), float64(aa)
	}
	return clampFloat(r/total, 0, 255), clampFloat(g/total, 0, 255), clampFloat(b/total, 0, 255), clampFloat(a/total, 0, 255)
}

func lanczosWeight(x float64) float64 {
	if x < 0 {
		x = -x
	}
	if x == 0 {
		return 1
	}
	if x >= 3 {
		return 0
	}
	return sinc(x) * sinc(x/3)
}

func sinc(x float64) float64 {
	if x == 0 {
		return 1
	}
	x *= math.Pi
	return math.Sin(x) / x
}

func pixelRGBA(img image.Image, x, y int) (uint8, uint8, uint8, uint8) {
	switch im := img.(type) {
	case *image.NRGBA:
		i := im.PixOffset(x, y)
		return im.Pix[i], im.Pix[i+1], im.Pix[i+2], im.Pix[i+3]
	case *image.RGBA:
		i := im.PixOffset(x, y)
		return im.Pix[i], im.Pix[i+1], im.Pix[i+2], im.Pix[i+3]
	case *image.Gray:
		i := im.PixOffset(x, y)
		v := im.Pix[i]
		return v, v, v, 255
	default:
		rr, gg, bb, aa := img.At(x, y).RGBA()
		return uint8(rr >> 8), uint8(gg >> 8), uint8(bb >> 8), uint8(aa >> 8)
	}
}

func hasTransparency(img image.Image) bool {
	switch img.(type) {
	case *image.YCbCr, *image.Gray:
		return false
	}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := pixelRGBA(img, x, y)
			if a < 255 {
				return true
			}
		}
	}
	return false
}

func cubicWeight(x float64) float64 {
	if x < 0 {
		x = -x
	}
	if x <= 1 {
		return 1.5*x*x*x - 2.5*x*x + 1
	}
	if x < 2 {
		return -0.5*x*x*x + 2.5*x*x - 4*x + 2
	}
	return 0
}

func enhanceSharpness(luma []float64, width, height int, factor float64) []float64 {
	out := make([]float64, len(luma))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var sum float64
			var count float64
			for yy := -1; yy <= 1; yy++ {
				py := max(0, min(height-1, y+yy))
				for xx := -1; xx <= 1; xx++ {
					px := max(0, min(width-1, x+xx))
					sum += luma[py*width+px]
					count++
				}
			}
			blurred := sum / count
			original := luma[y*width+x]
			out[y*width+x] = clampFloat(blurred*(1-factor)+original*factor, 0, 255)
		}
	}
	return out
}

func clampFloat(value, low, high float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func firstVisibleRune(charset []rune) rune {
	for _, char := range charset {
		if char != ' ' {
			return char
		}
	}
	return '.'
}

func boostRGB(r, g, b uint8, factor float64) (uint8, uint8, uint8) {
	if factor == 1 {
		return r, g, b
	}
	return uint8(clampFloat(float64(r)*factor, 0, 255)),
		uint8(clampFloat(float64(g)*factor, 0, 255)),
		uint8(clampFloat(float64(b)*factor, 0, 255))
}

func writeRGBSequence(sb *strings.Builder, mode string, r, g, b uint8, char rune) {
	sb.WriteString("\033[")
	sb.WriteString(mode)
	sb.WriteString(";2;")
	sb.WriteString(strconv.Itoa(int(r)))
	sb.WriteByte(';')
	sb.WriteString(strconv.Itoa(int(g)))
	sb.WriteByte(';')
	sb.WriteString(strconv.Itoa(int(b)))
	sb.WriteByte('m')
	sb.WriteRune(char)
}

func playEffect(slide Slide, width, height, page int, view ViewState) string {
	name := slide.Effect
	frame := 0
	overlay := displayLines(slide, width, height, page)
	hasAnimatedOverlay := slideHasAnimatedImage(slide)
	matrix := newMatrix(width, height)
	stars := newStars(width, height)
	fireworks := newBursts(name, width, height)
	cleared := false
	for {
		if action := pollKey(); action != "" {
			return action
		}
		flushTerminalFrame(func() {
			var backdropLines []exportLine
			if name == "matrix" || name == "plasma" {
				if !cleared {
					termPrintf("\033[%sm\033[2J\033[H%s", slideBG(slide), slideStyle(slide, false))
					backgroundFrame := captureTerminalOutput(func() {
						drawStaticBackground(slide.Background, width, height, slideBG(slide))
					})
					termPrint(backgroundFrame)
					backdropLines = append(backdropLines, ansiFrameToExportLines(backgroundFrame, width, height, ansiCSSColour(slideFG(slide)))...)
					cleared = true
				}
			} else {
				termPrintf("\033[%sm\033[2J\033[H%s", slideBG(slide), slideStyle(slide, false))
				backgroundFrame := captureTerminalOutput(func() {
					drawStaticBackground(slide.Background, width, height, slideBG(slide))
				})
				termPrint(backgroundFrame)
				backdropLines = append(backdropLines, ansiFrameToExportLines(backgroundFrame, width, height, ansiCSSColour(slideFG(slide)))...)
			}
			effectFrame := captureTerminalOutput(func() {
				drawEffectFrame(name, width, height, frame, matrix, stars, fireworks, slideBG(slide))
			})
			termPrint(effectFrame)
			if hasAnimatedOverlay {
				overlay = displayLines(slide, width, height, page)
			}
			backdropLines = append(backdropLines, ansiFrameToExportLines(effectFrame, width, height, ansiCSSColour(slideFG(slide)))...)
			drawTransparentShapeBackdrop(overlay, backdropLines, width, height, slide)
			drawOverlayLines(overlay, width, height, slide)
			drawLinkUnderlines(overlay, width, height, slide)
			drawViewChrome(width, height, view)
			drawViewOverlays(width, height, view)
		})
		time.Sleep(70 * time.Millisecond)
		frame++
	}
}

type matrixTrail struct {
	x     int
	y     int
	life  int
	rate  int
	clear bool
}

type matrixEffect struct {
	width  int
	height int
	trails []matrixTrail
}

func newMatrix(width, height int) *matrixEffect {
	m := &matrixEffect{width: width, height: height}
	for x := 0; x < width; x++ {
		t := matrixTrail{x: x, clear: true}
		m.reseed(&t)
		m.trails = append(m.trails, t)
	}
	return m
}

func (m *matrixEffect) reseed(t *matrixTrail) {
	t.y += t.rate
	t.life--
	if t.life > 0 {
		return
	}
	t.clear = !t.clear
	t.rate = 1 + rand.Intn(2)
	if t.clear {
		t.y = 0
		t.life = max(1, m.height/t.rate)
	} else {
		t.y = rand.Intn(max(1, m.height/2)) - m.height/4
		t.life = max(1, rand.Intn(max(1, m.height-t.y))/t.rate)
	}
}

func (m *matrixEffect) draw(frame int) {
	for i := range m.trails {
		t := &m.trails[i]
		if frame%2 != 0 {
			continue
		}
		if t.clear {
			for dy := 0; dy < 3; dy++ {
				y := t.y + dy
				if y >= 0 && y < m.height {
					termPrintf("\033[%d;%dH ", y+1, t.x+1)
				}
			}
		} else {
			for dy := 0; dy < 3; dy++ {
				y := t.y + dy
				if y >= 0 && y < m.height {
					termPrintf("\033[0;32m\033[%d;%dH%c", y+1, t.x+1, rune(32+rand.Intn(95)))
				}
			}
			for dy := 4; dy < 6; dy++ {
				y := t.y + dy
				if y >= 0 && y < m.height {
					termPrintf("\033[1;32m\033[%d;%dH%c", y+1, t.x+1, rune(32+rand.Intn(95)))
				}
			}
		}
		m.reseed(t)
	}
}

type star struct {
	x     int
	y     int
	cycle int
}

type starsEffect struct {
	width   int
	height  int
	pattern []rune
	stars   []star
}

func newStars(width, height int) *starsEffect {
	s := &starsEffect{
		width:   width,
		height:  height,
		pattern: []rune("..+..   ...x...  ...*...         "),
	}
	count := (width + height) / 2
	for i := 0; i < count; i++ {
		s.stars = append(s.stars, star{
			x:     rand.Intn(max(1, width)),
			y:     rand.Intn(max(1, height)),
			cycle: rand.Intn(len(s.pattern)),
		})
	}
	return s
}

func (s *starsEffect) draw() {
	for i := range s.stars {
		st := &s.stars[i]
		st.cycle = (st.cycle + 1) % len(s.pattern)
		ch := s.pattern[st.cycle]
		if ch == ' ' && rand.Intn(120) == 0 {
			st.x = rand.Intn(max(1, s.width))
			st.y = rand.Intn(max(1, s.height))
		}
		termPrintf("\033[0;37m\033[%d;%dH%s", st.y+1, st.x+1, string(ch))
	}
}

func drawPlasma(width, height, frame int) {
	greyscale := []rune(" .:;rsA23hHG#9&@")
	palette := []string{"\033[0;34m", "\033[0;34m", "\033[0;35m", "\033[0;35m", "\033[0;31m", "\033[1;31m"}
	t := float64(frame + 1)
	var out strings.Builder
	out.Grow(width * height * 16)
	for y := 0; y < max(0, height-1); y++ {
		for x := 0; x < max(0, width-1); x++ {
			f := func(x1, y1, xp, yp, n float64) float64 {
				return math.Sin(math.Sqrt(math.Pow(x1-float64(width)*xp, 2)+4*math.Pow(y1-float64(height)*yp, 2)) * math.Pi / n)
			}
			value := math.Abs(f(float64(x)+t/3, float64(y), 1.0/4, 1.0/3, 15)+
				f(float64(x), float64(y), 1.0/8, 1.0/5, 11)+
				f(float64(x), float64(y)+t/3, 1.0/2, 1.0/5, 13)+
				f(float64(x), float64(y), 3.0/4, 4.0/5, 13)) / 4
			p := int(math.Round(value * float64(len(palette)-1)))
			c := int(value * float64(len(greyscale)-1))
			fmt.Fprintf(&out, "%s\033[%d;%dH%s", palette[p], y+1, x+1, string(greyscale[c]))
		}
	}
	termPrint(out.String())
}

func drawGlitch(width, height, frame int) {
	palette := []string{"\033[0;31m", "\033[0;36m", "\033[1;37m", "\033[0;35m"}
	chars := []rune(" ░▒▓█!#$%&*+-=[]{}")
	for band := 0; band < max(3, height/5); band++ {
		y := rand.Intn(max(1, height))
		xOffset := rand.Intn(9) - 4
		color := palette[rand.Intn(len(palette))]
		for x := 0; x < width; x++ {
			if rand.Intn(100) < 45 {
				col := x + xOffset
				if col >= 0 && col < width {
					termPrintf("%s\033[%d;%dH%c", color, y+1, col+1, chars[rand.Intn(len(chars))])
				}
			}
		}
	}
	if frame%8 == 0 {
		y := rand.Intn(max(1, height))
		termPrintf("\033[1;37m\033[%d;1H%s", y+1, strings.Repeat("▀", max(0, width)))
	}
}

func drawDigitalSnow(name string, width, height, frame int) {
	chars := []rune("01abcdef{}[]()/\\|+-=<>")
	color := "\033[0;32m"
	for i := 0; i < max(1, width*height/18); i++ {
		x := rand.Intn(max(1, width))
		y := (rand.Intn(max(1, height)) + frame/2) % max(1, height)
		if rand.Intn(4) == 0 {
			termPrintf("\033[0;37m\033[%d;%dH.", y+1, x+1)
		} else {
			termPrintf("%s\033[%d;%dH%c", color, y+1, x+1, chars[rand.Intn(len(chars))])
		}
	}
}

func drawRadar(width, height, frame int) {
	cx, cy := width/2, height/2
	radius := min(width/2, height-2)
	angle := float64(frame%120) / 120 * 2 * math.Pi
	for r := 2; r <= radius; r += max(2, radius/4) {
		drawEllipse(cx, cy, r*2, r, "\033[0;32m", ".")
	}
	for r := 0; r < radius; r++ {
		x := cx + int(math.Cos(angle)*float64(r)*2)
		y := cy + int(math.Sin(angle)*float64(r))
		if x >= 0 && x < width && y >= 0 && y < height {
			termPrintf("\033[1;32m\033[%d;%dH%s", y+1, x+1, "█")
		}
	}
	for i := 0; i < 18; i++ {
		x := (i*17 + frame/2) % max(1, width)
		y := (i*i*7 + frame/3) % max(1, height)
		termPrintf("\033[0;32m\033[%d;%dH+", y+1, x+1)
	}
}

func drawEllipse(cx, cy, rx, ry int, color, char string) {
	if rx <= 0 || ry <= 0 {
		return
	}
	for deg := 0; deg < 360; deg += 6 {
		a := float64(deg) * math.Pi / 180
		x := cx + int(math.Cos(a)*float64(rx)/2)
		y := cy + int(math.Sin(a)*float64(ry))
		termPrintf("%s\033[%d;%dH%s", color, y+1, x+1, char)
	}
}

func drawNeural(width, height, frame int) {
	nodes := deterministicPoints(width, height, 18, 41)
	for i, a := range nodes {
		for j := i + 1; j < len(nodes); j++ {
			b := nodes[j]
			dx, dy := abs(a.x-b.x), abs(a.y-b.y)
			if dx+dy < width/3 {
				color := "\033[0;34m"
				if (i+j+frame/3)%9 == 0 {
					color = "\033[1;36m"
				}
				drawLine(a.x, a.y, b.x, b.y, color, ".")
			}
		}
	}
	for i, p := range nodes {
		color := "\033[0;36m"
		if (i+frame/2)%7 == 0 {
			color = "\033[1;37m"
		}
		termPrintf("%s\033[%d;%dH●", color, p.y+1, p.x+1)
	}
}

func drawCircuit(width, height, frame int) {
	for y := 2; y < height; y += 4 {
		color := "\033[0;36m"
		if (y+frame/2)%12 < 4 {
			color = "\033[1;36m"
		}
		for x := 0; x < width; x++ {
			if x%11 == 0 {
				termPrintf("%s\033[%d;%dH┬", color, y+1, x+1)
			} else if (x+frame/2)%23 < 14 {
				termPrintf("%s\033[%d;%dH─", color, y+1, x+1)
			}
		}
		for x := (y*7 + frame/3) % 11; x < width; x += 22 {
			for yy := y; yy < min(height, y+4); yy++ {
				termPrintf("%s\033[%d;%dH│", color, yy+1, x+1)
			}
		}
	}
}

func drawDataStorm(width, height, frame int) {
	if width <= 0 || height <= 0 {
		return
	}
	maxPile := max(1, height/4)
	streamRows := dataStormStreamRows(height)
	drops := max(32, width)
	streamMasks := dataStormStreamMasks(frame, width, height, maxPile, streamRows, drops)
	for x := 0; x < width; x++ {
		pile := dataStormPileHeight(x, frame, maxPile)
		for y := height - pile; y < height; y++ {
			if y < 0 {
				continue
			}
			glyph := dataStormRune(x*13 + y*7 + frame/9)
			color := "\033[0;32m"
			if y >= height-2 {
				color = "\033[0;36m"
			}
			termPrintf("%s\033[%d;%dH%c", color, y+1, x+1, glyph)
		}
	}
	for lane, y := range streamRows {
		snippet := dataStormSnippetForLane(lane, frame)
		runes := []rune(snippet)
		state := dataStormStreamState(lane, frame, width, len(runes))
		x := state.x
		color := "\033[1;32m"
		if lane%3 == 1 {
			color = "\033[1;37m"
		} else if lane%3 == 2 {
			color = "\033[0;32m"
		}
		drawClippedTextMasked(y, x, color, snippet, width, streamMasks[lane])
	}
	for i := 0; i < drops; i++ {
		lane := i % len(streamRows)
		currentSnippet := dataStormSnippetForLane(lane, frame)
		currentRunes := []rune(currentSnippet)
		state := dataStormStreamState(lane, frame, width, len(currentRunes))
		spawnFrame, charIndex, ok := dataStormScheduledDrop(lane, state.pass, i/len(streamRows), currentRunes, state.startFrame, state.duration)
		if !ok || frame < spawnFrame {
			continue
		}
		snippet := dataStormSnippetForLane(lane, spawnFrame)
		if snippet != currentSnippet {
			continue
		}
		runes := []rune(snippet)
		if len(runes) == 0 {
			continue
		}
		spawnState := dataStormStreamState(lane, spawnFrame, width, len(runes))
		spawnX := spawnState.x + charIndex
		age := frame - spawnFrame
		drift := dataStormDropDrift(i, lane)
		if spawnX < 0 || spawnX >= width {
			continue
		}
		speed := dataStormDropSpeed(i, spawnX)
		landX, floor, travel := dataStormDropLanding(spawnX, streamRows[lane], speed, drift, width, height, maxPile)
		if floor <= streamRows[lane] {
			continue
		}
		if age < 0 || age > travel+dataStormLandedLifetime(i, landX) {
			continue
		}
		trailCount := 5
		if age >= travel+4 {
			trailCount = 1
		}
		for trail := 0; trail < trailCount; trail++ {
			trailAge := min(age, travel) - trail*2
			if trailAge < 0 {
				continue
			}
			trailX, yy := dataStormDropPosition(spawnX, streamRows[lane], landX, floor, travel, trailAge)
			if yy < streamRows[lane] || yy > floor || trailX < 0 || trailX >= width {
				continue
			}
			glyph := runes[(charIndex+trail)%len(runes)]
			if glyph == ' ' || glyph == '\t' {
				glyph = dataStormRune(i*37 + trail*11 + frame)
			}
			color := "\033[0;32m"
			if trail == 0 {
				color = "\033[1;37m"
			} else if trail == 1 {
				color = "\033[1;32m"
			}
			termPrintf("%s\033[%d;%dH%c", color, yy+1, trailX+1, glyph)
		}
	}
}

func dataStormStreamRows(height int) []int {
	lanes := min(len(dataStormSnippets), max(3, height/5))
	rows := make([]int, 0, lanes)
	for i := 0; i < lanes; i++ {
		y := 2 + i*max(2, height/max(1, lanes+1))
		rows = append(rows, min(max(0, height-1), y))
	}
	return rows
}

func dataStormStreamSpeed(lane, spawn int) int {
	return 3 + dataStormHash(lane*101+spawn*29)%7
}

type dataStormStreamSnapshot struct {
	pass       int
	speed      int
	span       int
	x          int
	startFrame int
	duration   int
}

func dataStormStreamState(lane, frame, width, textLen int) dataStormStreamSnapshot {
	span := max(1, width+textLen+12)
	pass := frame / span
	speed := dataStormStreamSpeed(lane, pass)
	progress := frame*speed/2 + lane*17
	pass = progress / span
	speed = dataStormStreamSpeed(lane, pass)
	progress = frame*speed/2 + lane*17
	pass = progress / span
	position := progress % span
	startFrame := max(0, ((pass*span-lane*17)*2+speed-1)/speed)
	endFrame := max(startFrame+1, (((pass+1)*span-lane*17)*2+speed-1)/speed)
	return dataStormStreamSnapshot{
		pass:       pass,
		speed:      speed,
		span:       span,
		x:          width - position,
		startFrame: startFrame,
		duration:   max(1, endFrame-startFrame),
	}
}

func dataStormSnippetForLane(lane, frame int) string {
	if len(dataStormSnippets) == 0 {
		return ""
	}
	return dataStormSnippets[(lane+frame/240)%len(dataStormSnippets)]
}

func dataStormStreamMasks(frame, width, height, maxPile int, streamRows []int, drops int) map[int]map[int]bool {
	masks := map[int]map[int]bool{}
	if len(streamRows) == 0 {
		return masks
	}
	for i := 0; i < drops; i++ {
		lane := i % len(streamRows)
		snippet := dataStormSnippetForLane(lane, frame)
		runes := []rune(snippet)
		state := dataStormStreamState(lane, frame, width, len(runes))
		spawnFrame, charIndex, ok := dataStormScheduledDrop(lane, state.pass, i/len(streamRows), runes, state.startFrame, state.duration)
		if !ok || frame < spawnFrame {
			continue
		}
		if dataStormSnippetForLane(lane, spawnFrame) != snippet {
			continue
		}
		if masks[lane] == nil {
			masks[lane] = map[int]bool{}
		}
		masks[lane][charIndex] = true
	}
	return masks
}

func dataStormScheduledDrop(lane, pass, slot int, runes []rune, startFrame, duration int) (int, int, bool) {
	textLen := len(runes)
	if textLen <= 0 {
		return 0, 0, false
	}
	survivors := dataStormHash(lane*139+pass*47) % 5
	dropBudget := max(0, textLen-survivors)
	if slot < 0 || slot >= dropBudget {
		return 0, 0, false
	}
	charIndex := (dataStormHash(lane*23+pass*31) + slot*7) % textLen
	for offset := 0; offset < textLen; offset++ {
		candidate := (charIndex + offset) % textLen
		if runes[candidate] != ' ' && runes[candidate] != '\t' {
			charIndex = candidate
			break
		}
	}
	spawnOffset := (slot + 1) * max(1, duration) / max(1, dropBudget+1)
	return startFrame + spawnOffset, charIndex, true
}

var dataStormSnippets = []string{
	`{"event":"ingest","status":"ok"}`,
	`vector.search(top_k=32)`,
	`query.plan -> tool.call`,
	`chunk.window overlap=128`,
	`{"mime":"application/pdf"}`,
	`rerank: lexical + semantic`,
	`cache.hit ratio=0.87`,
	`trace_id=7f3a latency=42ms`,
	`index.flush segments=12`,
	`embedding.model dimensions=1536`,
	`worker.queue depth=0042`,
	`ACL filter applied`,
	`pipeline.stage normalize`,
	`ocr.page confidence=0.94`,
	`audio.transcript segment=18`,
	`image.caption generated`,
	`tool.result bytes=4096`,
	`guardrail.check pass`,
	`schema.validate ok`,
	`retry.backoff 250ms`,
	`stream.delta tokens=64`,
	`memory.write ephemeral`,
	`context.pack budget=8192`,
	`rank.fusion weight=0.62`,
	`metadata.extract fields=17`,
	`blob.read range=0..65535`,
	`session.state synced`,
	`eval.sample score=0.91`,
	`router.intent classify`,
	`response.draft revise`,
}

func dataStormRune(seed int) rune {
	if len(dataStormSnippets) == 0 {
		return '?'
	}
	snippet := []rune(dataStormSnippets[dataStormHash(seed)%len(dataStormSnippets)])
	if len(snippet) == 0 {
		return '?'
	}
	start := dataStormHash(seed/len(dataStormSnippets)+17) % len(snippet)
	for offset := 0; offset < len(snippet); offset++ {
		r := snippet[(start+offset)%len(snippet)]
		if r != ' ' && r != '\t' {
			return r
		}
	}
	return '?'
}

func dataStormDropSpeed(index, x int) int {
	return 2 + dataStormHash(index*97+x*31)%7
}

func dataStormDropDrift(index, lane int) int {
	return 1 + dataStormHash(index*43+lane*89)%4
}

func dataStormDropLanding(spawnX, startY, speed, drift, width, height, maxPile int) (int, int, int) {
	landX := min(max(0, spawnX), max(0, width-1))
	floor := 0
	travel := 1
	for iteration := 0; iteration < 4; iteration++ {
		floor = max(0, height-dataStormPileHeight(landX, 0, maxPile)-1)
		distance := max(0, floor-startY)
		travel = max(1, (distance*3+max(1, speed)-1)/max(1, speed))
		nextX := min(max(0, spawnX-travel*drift/7), max(0, width-1))
		if nextX == landX {
			break
		}
		landX = nextX
	}
	floor = max(0, height-dataStormPileHeight(landX, 0, maxPile)-1)
	distance := max(0, floor-startY)
	travel = max(1, (distance*3+max(1, speed)-1)/max(1, speed))
	return landX, floor, travel
}

func dataStormDropPosition(spawnX, startY, landX, floor, travel, age int) (int, int) {
	if travel <= 0 || age >= travel {
		return landX, floor
	}
	age = max(0, age)
	x := spawnX + (landX-spawnX)*age/travel
	y := startY + (floor-startY)*age/travel
	return x, y
}

func dataStormLandedLifetime(index, x int) int {
	return 36 + dataStormHash(index*59+x*71)%48
}

func dataStormPileHeight(x, frame, maxPile int) int {
	if maxPile <= 0 {
		return 0
	}
	_ = frame
	base := dataStormHash(x*17) % maxPile
	shape := dataStormHash(x*31+7) % max(1, maxPile/2+1)
	return 1 + (base+shape/2)%maxPile
}

func dataStormHash(value int) int {
	value ^= value << 13
	value ^= value >> 17
	value ^= value << 5
	if value < 0 {
		return -value
	}
	return value
}

func drawClippedText(y, x int, color, text string, width int) {
	if y < 0 || x >= width {
		return
	}
	if x < 0 {
		runes := []rune(text)
		if -x >= len(runes) {
			return
		}
		text = string(runes[-x:])
		x = 0
	}
	termPrintf("%s\033[%d;%dH%s", color, y+1, x+1, crop(text, max(0, width-x)))
}

func drawClippedTextMasked(y, x int, color, text string, width int, mask map[int]bool) {
	if len(mask) == 0 {
		drawClippedText(y, x, color, text, width)
		return
	}
	if y < 0 || x >= width {
		return
	}
	runes := []rune(text)
	startIndex := 0
	if x < 0 {
		startIndex = -x
		x = 0
	}
	var run []rune
	runX := x
	flush := func(col int) {
		if len(run) == 0 {
			return
		}
		termPrintf("%s\033[%d;%dH%s", color, y+1, runX+1, string(run))
		run = nil
		runX = col
	}
	for index := startIndex; index < len(runes) && x < width; index++ {
		if mask[index] {
			flush(x + 1)
			x++
			continue
		}
		if len(run) == 0 {
			runX = x
		}
		run = append(run, runes[index])
		x++
	}
	flush(x)
}

func drawFlame(width, height, frame int) {
	chars := []rune(" .:░▒▓█")
	for y := max(0, height/2); y < height; y++ {
		for x := 0; x < width; x++ {
			v := math.Sin(float64(x+frame)/3) + math.Sin(float64(x*2+frame)/7) + rand.Float64()*1.5
			falloff := float64(height-y) / float64(max(1, height/2))
			idx := int(clampFloat((v+2.0)*falloff*float64(len(chars)-1)/3.0, 0, float64(len(chars)-1)))
			color := "\033[0;31m"
			if idx > 4 {
				color = "\033[1;33m"
			} else if idx > 2 {
				color = "\033[1;31m"
			}
			if chars[idx] != ' ' {
				termPrintf("%s\033[%d;%dH%c", color, y+1, x+1, chars[idx])
			}
		}
	}
}

func drawWarp(width, height, frame int) {
	cx, cy := width/2, height/2
	for i := 0; i < 90; i++ {
		angle := float64(i*137) * math.Pi / 180
		speed := 1 + i%5
		r := float64((frame*speed + i*11) % max(1, width+height))
		x := cx + int(math.Cos(angle)*r)
		y := cy + int(math.Sin(angle)*r/2)
		if x >= 0 && x < width && y >= 0 && y < height {
			char := "."
			if r > float64(width/3) {
				char = "+"
			}
			termPrintf("\033[1;37m\033[%d;%dH%s", y+1, x+1, char)
		}
	}
}

func drawScanline(width, height, frame int) {
	y := frame % max(1, height)
	for row := 0; row < height; row++ {
		color := "\033[0;32m"
		if row == y {
			color = "\033[1;37m"
		} else if abs(row-y) < 3 {
			color = "\033[1;32m"
		}
		if row%2 == 0 || abs(row-y) < 3 {
			termPrintf("%s\033[%d;1H%s", color, row+1, strings.Repeat("─", max(0, width)))
		}
	}
}

type point struct {
	x, y int
}

func deterministicPoints(width, height, count, seed int) []point {
	points := make([]point, 0, count)
	for i := 0; i < count; i++ {
		points = append(points, point{
			x: (seed + i*23 + i*i*7) % max(1, width),
			y: (seed/2 + i*13 + i*i*5) % max(1, height),
		})
	}
	return points
}

func drawLine(x0, y0, x1, y1 int, color, char string) {
	dx := abs(x1 - x0)
	dy := -abs(y1 - y0)
	sx, sy := -1, -1
	if x0 < x1 {
		sx = 1
	}
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		termPrintf("%s\033[%d;%dH%s", color, y0+1, x0+1, char)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

type burst struct {
	x, y   int
	start  int
	life   int
	points int
	seed   int64
	color  string
}

type burstEffect struct {
	name          string
	width, height int
	bursts        []burst
}

func newBursts(name string, width, height int) *burstEffect {
	b := &burstEffect{name: name, width: width, height: height}
	colors := []string{"\033[1;31m", "\033[1;33m", "\033[1;37m", "\033[1;36m", "\033[1;35m"}
	for i := 0; i < 20; i++ {
		x := rand.Intn(max(1, width))
		if rand.Intn(3) == 0 {
			x = rand.Intn(max(1, width/3))
		} else if rand.Intn(3) == 0 {
			x = width*2/3 + rand.Intn(max(1, width-width*2/3))
		}
		y := rand.Intn(max(1, height))
		if rand.Intn(2) == 0 {
			y = height/2 + rand.Intn(max(1, height-height/2))
		}
		b.bursts = append(b.bursts, burst{
			x: x, y: y, start: rand.Intn(250), life: 20 + rand.Intn(11),
			points: 6 + rand.Intn(15), seed: rand.Int63(), color: colors[rand.Intn(len(colors))],
		})
	}
	return b
}

func (b *burstEffect) draw(frame int) {
	for _, burst := range b.bursts {
		age := (frame - burst.start) % 250
		if age < 0 || age > burst.life {
			continue
		}
		if b.name == "fireworks" {
			b.drawFirework(burst, age)
		} else {
			b.drawExplosion(burst, age)
		}
	}
}

func (b *burstEffect) drawFirework(burst burst, age int) {
	if age < 10 {
		y := b.height - 1 + (burst.y-(b.height-1))*age/10
		termPrintf("\033[1;33m\033[%d;%dH|", y+1, burst.x+1)
		return
	}

	explosionAge := age - 10
	explosionLife := max(1, burst.life-10)
	acceleration := 1.0 - 1.0/float64(explosionLife)
	for p := 0; p < burst.points; p++ {
		direction := float64(p) * 2 * math.Pi / float64(burst.points)
		x := float64(burst.x)
		y := float64(burst.y)
		dx := math.Sin(direction) * 3 * 8 / float64(explosionLife)
		dy := math.Cos(direction) * 1.5 * 8 / float64(explosionLife)
		for step := 0; step <= explosionAge; step++ {
			dy = dy*acceleration + 0.03
			dx *= acceleration
			x += dx
			y += dy
		}
		char := "+"
		if explosionAge > explosionLife*2/3 {
			char = "."
		}
		b.printParticle(int(x), int(y), burst.color, char)
		if explosionAge > 1 && explosionAge < explosionLife-1 {
			b.printParticle(int(x-dx*2), int(y-dy*2), "\033[0;37m", ".")
			if explosionAge%3 == 0 {
				b.printParticle(int(x-dx*4), int(y-dy*4), "\033[0;37m", ",")
			}
		}
	}
}

func (b *burstEffect) drawExplosion(burst burst, age int) {
	spawnFrames := min(age, max(0, burst.life-10))
	for spawn := 0; spawn <= spawnFrames; spawn++ {
		particleAge := age - spawn
		if particleAge >= 10 {
			continue
		}
		rng := rand.New(rand.NewSource(burst.seed + int64(spawn)*7919))
		for i := 0; i < 30; i++ {
			direction := rng.Float64() * 2 * math.Pi
			d := float64(max(1, burst.life-10))
			r := rng.Float64() * math.Sin(math.Pi*float64(d-float64(max(0, burst.life-10-spawn)))/(d*2)) * 3.0
			x := float64(burst.x) + math.Sin(direction)*r*2.0
			y := float64(burst.y) + math.Cos(direction)*r
			dx := math.Sin(direction) / 2.0
			dy := math.Cos(direction) / 4.0
			x += dx * float64(particleAge)
			y += dy * float64(particleAge)
			color := "\033[1;37m"
			if particleAge > 2 {
				color = "\033[1;33m"
			}
			if particleAge > 5 {
				color = "\033[1;31m"
			}
			if particleAge > 8 {
				color = "\033[0;31m"
			}
			b.printParticle(int(x), int(y), color, "#")
		}
	}
}

func (b *burstEffect) printParticle(x, y int, color, char string) {
	if x >= 0 && x < b.width && y >= 0 && y < b.height {
		termPrintf("%s\033[%d;%dH%s", color, y+1, x+1, char)
	}
}

func waitKey() string {
	for {
		if action := pollKey(); action != "" {
			return action
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func pollKey() string {
	event := readKeyEvent()
	if event.Action == "text" {
		queuedTimerText = event.Text
	}
	switch event.Action {
	case "quit", "prev", "next", "up", "down", "enter", "backspace", "text", "edit", "insert-image", "insert-text", "shape-picker", "insert-slide", "clone-slide", "delete-slide", "effect-picker", "background-picker", "search", "jump", "export", "present", "controls", "tab", "shift-tab", "undo", "redo", "copy", "cut", "paste", "mouse-click", "slide-list", "speaker-notes", "timer", "shortcuts", "master-view", "layout-picker", "page-number", "visual-properties":
		return event.Action
	default:
		return ""
	}
}

func readKeyEvent() KeyEvent {
	if event, ok := pollRemoteKeyEvent(); ok {
		return event
	}
	var buf [64]byte
	n, _ := inputRead(buf[:])
	if n == 0 {
		return KeyEvent{}
	}
	b := buf[:n]
	if event := parseMouseEvent(b); event.Action != "" {
		queuedMainMouseEvent = &event
		return event
	}
	if consumeBracketedPaste(b) {
		return KeyEvent{Action: "paste"}
	}
	if bytes.Contains(b, []byte{3}) {
		return KeyEvent{Action: "copy"}
	}
	if bytes.Contains(b, []byte{26}) {
		return KeyEvent{Action: "undo"}
	}
	if bytes.Contains(b, []byte{25}) {
		return KeyEvent{Action: "redo"}
	}
	if bytes.Contains(b, []byte{24}) {
		return KeyEvent{Action: "cut"}
	}
	if bytes.Contains(b, []byte{22}) {
		return KeyEvent{Action: "paste"}
	}
	if bytes.Equal(b, []byte{27}) {
		return KeyEvent{Action: "controls"}
	}
	if bytes.Contains(b, []byte{27, '[', 'A'}) {
		return KeyEvent{Action: "up"}
	}
	if bytes.Contains(b, []byte{27, '[', 'B'}) {
		return KeyEvent{Action: "down"}
	}
	if bytes.Contains(b, []byte{27, '[', 'Z'}) {
		return KeyEvent{Action: "shift-tab"}
	}
	if bytes.Contains(b, []byte{9}) {
		return KeyEvent{Action: "tab"}
	}
	if bytes.Contains(b, []byte{27, '[', 'D'}) {
		return KeyEvent{Action: "prev"}
	}
	if bytes.Contains(b, []byte{' '}) || bytes.Contains(b, []byte{27, '[', 'C'}) {
		return KeyEvent{Action: "next"}
	}
	if bytes.Contains(b, []byte{'n'}) {
		return KeyEvent{Action: "insert-slide"}
	}
	if bytes.Contains(b, []byte{'1'}) {
		return KeyEvent{Action: "slide-list"}
	}
	if bytes.Contains(b, []byte{'2'}) {
		return KeyEvent{Action: "speaker-notes"}
	}
	if bytes.Contains(b, []byte{'0'}) {
		return KeyEvent{Action: "timer"}
	}
	if bytes.Contains(b, []byte{'#'}) {
		return KeyEvent{Action: "page-number"}
	}
	if bytes.Contains(b, []byte{'v'}) {
		return KeyEvent{Action: "visual-properties"}
	}
	if bytes.Contains(b, []byte{'?'}) {
		return KeyEvent{Action: "shortcuts"}
	}
	if bytes.Contains(b, []byte{'M'}) {
		return KeyEvent{Action: "master-view"}
	}
	if bytes.Contains(b, []byte{'L'}) {
		return KeyEvent{Action: "layout-picker"}
	}
	if bytes.Contains(b, []byte{'c'}) {
		return KeyEvent{Action: "clone-slide"}
	}
	if bytes.Contains(b, []byte{'d'}) {
		return KeyEvent{Action: "delete-slide"}
	}
	if bytes.Contains(b, []byte{'e'}) {
		return KeyEvent{Action: "effect-picker"}
	}
	if bytes.Contains(b, []byte{'b'}) {
		return KeyEvent{Action: "background-picker"}
	}
	if bytes.Contains(b, []byte{'q'}) {
		return KeyEvent{Action: "quit"}
	}
	if bytes.Contains(b, []byte{'i'}) {
		return KeyEvent{Action: "insert-image"}
	}
	if bytes.Contains(b, []byte{'t'}) {
		return KeyEvent{Action: "insert-text"}
	}
	if bytes.Contains(b, []byte{'s'}) {
		return KeyEvent{Action: "shape-picker"}
	}
	if bytes.Contains(b, []byte{'p'}) {
		return KeyEvent{Action: "present"}
	}
	if bytes.Contains(b, []byte{'/'}) {
		return KeyEvent{Action: "search"}
	}
	if bytes.Contains(b, []byte{'j'}) {
		return KeyEvent{Action: "jump"}
	}
	if bytes.Contains(b, []byte{'x'}) {
		return KeyEvent{Action: "export"}
	}
	if bytes.Contains(b, []byte{127}) || bytes.Contains(b, []byte{8}) {
		return KeyEvent{Action: "backspace"}
	}
	if bytes.Contains(b, []byte{'\r'}) || bytes.Contains(b, []byte{'\n'}) {
		return KeyEvent{Action: "enter"}
	}
	text := string(b)
	if utf8.ValidString(text) {
		var out []rune
		for _, r := range text {
			if r >= 32 && r != 127 {
				out = append(out, r)
			}
		}
		if len(out) > 0 {
			return KeyEvent{Action: "text", Text: string(out)}
		}
	}
	return KeyEvent{}
}

func pollRemoteKeyEvent() (KeyEvent, bool) {
	select {
	case event := <-remoteKeyEvents:
		return event, true
	default:
		return KeyEvent{}, false
	}
}

func readEditKeyEvent() KeyEvent {
	return readEditKeyEventMode(false)
}

func readEditKeyEventForMode(mode string) KeyEvent {
	return readEditKeyEventMode(mode == "text")
}

func readEditKeyEventMode(textMode bool) KeyEvent {
	if event, ok := pollRemoteKeyEvent(); ok {
		return event
	}
	var buf [32]byte
	n, _ := inputRead(buf[:])
	if n == 0 {
		return KeyEvent{}
	}
	b := buf[:n]
	if event := parseMouseEvent(b); event.Action != "" {
		return event
	}
	if consumeBracketedPaste(b) {
		return KeyEvent{Action: "paste"}
	}
	if event := shiftArrowEvent(b); event.Action != "" {
		return event
	}
	if bytes.Contains(b, []byte{3}) {
		return KeyEvent{Action: "copy"}
	}
	if bytes.Contains(b, []byte{26}) {
		return KeyEvent{Action: "undo"}
	}
	if bytes.Contains(b, []byte{25}) {
		return KeyEvent{Action: "redo"}
	}
	if bytes.Contains(b, []byte{24}) {
		return KeyEvent{Action: "cut"}
	}
	if bytes.Contains(b, []byte{22}) {
		return KeyEvent{Action: "paste"}
	}
	if bytes.Contains(b, []byte{27, '[', 'Z'}) {
		return KeyEvent{Action: "shift-tab"}
	}
	if shiftEnterEvent(b) {
		return KeyEvent{Action: "insert-newline"}
	}
	if event := editEscapeEvent(b); event.Action != "" {
		return event
	}
	if bytes.Contains(b, []byte{9}) {
		return KeyEvent{Action: "tab"}
	}
	if bytes.Contains(b, []byte{127}) || bytes.Contains(b, []byte{8}) {
		return KeyEvent{Action: "backspace"}
	}
	if bytes.Contains(b, []byte{'\r'}) || bytes.Contains(b, []byte{'\n'}) {
		return KeyEvent{Action: "enter"}
	}
	if textMode {
		return printableKeyEvent(b)
	}
	if bytes.Contains(b, []byte{'#'}) {
		return KeyEvent{Action: "page-number"}
	}
	if bytes.Contains(b, []byte{'v'}) {
		return KeyEvent{Action: "visual-properties"}
	}
	if bytes.Contains(b, []byte{'0'}) {
		return KeyEvent{Action: "timer"}
	}
	if bytes.Contains(b, []byte{'='}) {
		return KeyEvent{Action: "align-center"}
	}
	if bytes.Contains(b, []byte{'>'}) {
		return KeyEvent{Action: "align-right"}
	}
	if bytes.Contains(b, []byte{'<'}) {
		return KeyEvent{Action: "align-left"}
	}
	if bytes.Contains(b, []byte{'['}) {
		return KeyEvent{Action: "layer-back"}
	}
	if bytes.Contains(b, []byte{']'}) {
		return KeyEvent{Action: "layer-front"}
	}
	if bytes.Contains(b, []byte{'+'}) {
		return KeyEvent{Action: "promote"}
	}
	if bytes.Contains(b, []byte{'-'}) || bytes.Contains(b, []byte{'_'}) {
		return KeyEvent{Action: "demote"}
	}
	if bytes.Contains(b, []byte{'c'}) {
		return KeyEvent{Action: "color"}
	}
	if bytes.Contains(b, []byte{'g'}) {
		return KeyEvent{Action: "shape-toggle"}
	}
	if bytes.Contains(b, []byte{'o'}) {
		return KeyEvent{Action: "outline"}
	}
	if bytes.Contains(b, []byte{'p'}) {
		return KeyEvent{Action: "placeholder-role"}
	}
	if bytes.Contains(b, []byte{'B'}) {
		return KeyEvent{Action: "toggle-bold"}
	}
	if bytes.Contains(b, []byte{'H'}) {
		return KeyEvent{Action: "toggle-highlight"}
	}
	if bytes.Contains(b, []byte{'R'}) {
		return KeyEvent{Action: "rotate-ccw"}
	}
	if bytes.Contains(b, []byte{'r'}) {
		return KeyEvent{Action: "rotate"}
	}
	if bytes.Contains(b, []byte{'/'}) {
		return KeyEvent{Action: "transparency"}
	}
	if bytes.Contains(b, []byte{'s'}) {
		return KeyEvent{Action: "style"}
	}
	if bytes.Contains(b, []byte{'1'}) {
		return KeyEvent{Action: "slide-list"}
	}
	if bytes.Contains(b, []byte{'2'}) {
		return KeyEvent{Action: "speaker-notes"}
	}
	if bytes.Contains(b, []byte{'l'}) {
		return KeyEvent{Action: "link"}
	}
	if bytes.Contains(b, []byte{'e'}) {
		return KeyEvent{Action: "edit-selected"}
	}
	if bytes.Contains(b, []byte{'t'}) {
		return KeyEvent{Action: "insert-text"}
	}
	if bytes.Contains(b, []byte{'m'}) {
		return KeyEvent{Action: "move"}
	}
	if bytes.Contains(b, []byte{' '}) {
		return KeyEvent{Action: "toggle-selection"}
	}
	text := string(b)
	if !utf8.ValidString(text) {
		return KeyEvent{}
	}
	var out []rune
	for _, r := range text {
		if r == 'q' {
			return KeyEvent{Action: "quit"}
		}
		if r == 'i' {
			return KeyEvent{Action: "insert-image"}
		}
		if r == 't' {
			return KeyEvent{Action: "insert-text"}
		}
		if r == 's' {
			return KeyEvent{Action: "style"}
		}
		if r == 'g' {
			return KeyEvent{Action: "shape-toggle"}
		}
		if r == 'o' {
			return KeyEvent{Action: "outline"}
		}
		if r == 'p' {
			return KeyEvent{Action: "placeholder-role"}
		}
		if r == 'R' {
			return KeyEvent{Action: "rotate-ccw"}
		}
		if r == 'r' {
			return KeyEvent{Action: "rotate"}
		}
		if r == '/' {
			return KeyEvent{Action: "transparency"}
		}
		if r >= 32 && r != 127 && r != 27 {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return KeyEvent{}
	}
	return KeyEvent{Action: "text", Text: string(out)}
}

func printableKeyEvent(b []byte) KeyEvent {
	text := string(b)
	if !utf8.ValidString(text) {
		return KeyEvent{}
	}
	var out []rune
	for _, r := range text {
		if r >= 32 && r != 127 && r != 27 {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return KeyEvent{}
	}
	return KeyEvent{Action: "text", Text: string(out)}
}

func readSlideNavigatorKeyEvent() KeyEvent {
	var buf [64]byte
	n, _ := inputRead(buf[:])
	if n == 0 {
		return KeyEvent{}
	}
	b := buf[:n]
	if event := parseMouseEvent(b); event.Action != "" {
		return event
	}
	if bytes.Equal(b, []byte{27}) {
		return KeyEvent{Action: "escape"}
	}
	if bytes.Contains(b, []byte{27, '[', 'A'}) {
		return KeyEvent{Action: "up"}
	}
	if bytes.Contains(b, []byte{27, '[', 'B'}) {
		return KeyEvent{Action: "down"}
	}
	if bytes.Contains(b, []byte{'\r'}) || bytes.Contains(b, []byte{'\n'}) {
		return KeyEvent{Action: "enter"}
	}
	if bytes.Contains(b, []byte{'1'}) {
		return KeyEvent{Action: "slide-list"}
	}
	return KeyEvent{}
}

func readSpeakerNotesViewKeyEvent() KeyEvent {
	var buf [64]byte
	n, _ := inputRead(buf[:])
	if n == 0 {
		return KeyEvent{}
	}
	b := buf[:n]
	if event := parseMouseEvent(b); event.Action != "" {
		return event
	}
	if bytes.Equal(b, []byte{27}) {
		return KeyEvent{Action: "escape"}
	}
	if bytes.Contains(b, []byte{9}) {
		return KeyEvent{Action: "tab"}
	}
	if bytes.Contains(b, []byte{'\r'}) || bytes.Contains(b, []byte{'\n'}) {
		return KeyEvent{Action: "enter"}
	}
	if bytes.Contains(b, []byte{27, '[', 'D'}) {
		return KeyEvent{Action: "prev"}
	}
	if bytes.Contains(b, []byte{' '}) || bytes.Contains(b, []byte{27, '[', 'C'}) {
		return KeyEvent{Action: "next"}
	}
	if bytes.Contains(b, []byte{'1'}) {
		return KeyEvent{Action: "slide-list"}
	}
	if bytes.Contains(b, []byte{'2'}) {
		return KeyEvent{Action: "speaker-notes"}
	}
	if bytes.Contains(b, []byte{'0'}) {
		return KeyEvent{Action: "timer"}
	}
	if bytes.Contains(b, []byte{'n'}) {
		return KeyEvent{Action: "insert-slide"}
	}
	if bytes.Contains(b, []byte{'c'}) {
		return KeyEvent{Action: "clone-slide"}
	}
	if bytes.Contains(b, []byte{'d'}) {
		return KeyEvent{Action: "delete-slide"}
	}
	if bytes.Contains(b, []byte{'e'}) {
		return KeyEvent{Action: "effect-picker"}
	}
	if bytes.Contains(b, []byte{'b'}) {
		return KeyEvent{Action: "background-picker"}
	}
	if bytes.Contains(b, []byte{'q'}) {
		return KeyEvent{Action: "quit"}
	}
	if bytes.Contains(b, []byte{'i'}) {
		return KeyEvent{Action: "insert-image"}
	}
	if bytes.Contains(b, []byte{'t'}) {
		return KeyEvent{Action: "insert-text"}
	}
	if bytes.Contains(b, []byte{'s'}) {
		return KeyEvent{Action: "shape-picker"}
	}
	if bytes.Contains(b, []byte{'p'}) {
		return KeyEvent{Action: "present"}
	}
	if bytes.Contains(b, []byte{'/'}) {
		return KeyEvent{Action: "search"}
	}
	if bytes.Contains(b, []byte{'j'}) {
		return KeyEvent{Action: "jump"}
	}
	if bytes.Contains(b, []byte{'x'}) {
		return KeyEvent{Action: "export"}
	}
	return KeyEvent{}
}

func readSpeakerNotesKeyEvent() KeyEvent {
	var buf [64]byte
	n, _ := inputRead(buf[:])
	if n == 0 {
		return KeyEvent{}
	}
	b := buf[:n]
	if event := parseMouseEvent(b); event.Action != "" {
		return event
	}
	if bytes.Equal(b, []byte{27}) {
		return KeyEvent{Action: "escape"}
	}
	if shiftEnterEvent(b) {
		return KeyEvent{Action: "insert-newline"}
	}
	if bytes.Contains(b, []byte{27, '[', 'A'}) {
		return KeyEvent{Action: "up"}
	}
	if bytes.Contains(b, []byte{27, '[', 'B'}) {
		return KeyEvent{Action: "down"}
	}
	if bytes.Contains(b, []byte{27, '[', 'D'}) {
		return KeyEvent{Action: "left"}
	}
	if bytes.Contains(b, []byte{27, '[', 'C'}) {
		return KeyEvent{Action: "right"}
	}
	if bytes.Contains(b, []byte{127}) || bytes.Contains(b, []byte{8}) {
		return KeyEvent{Action: "backspace"}
	}
	if bytes.Contains(b, []byte{'\r'}) || bytes.Contains(b, []byte{'\n'}) {
		return KeyEvent{Action: "enter"}
	}
	text := string(b)
	if utf8.ValidString(text) {
		var out []rune
		for _, r := range text {
			if r >= 32 && r != 127 {
				out = append(out, r)
			}
		}
		if len(out) > 0 {
			return KeyEvent{Action: "text", Text: string(out)}
		}
	}
	return KeyEvent{}
}

func readColorPickerKeyEvent() KeyEvent {
	var buf [32]byte
	n, _ := inputRead(buf[:])
	if n == 0 {
		return KeyEvent{}
	}
	b := buf[:n]
	if event := parseMouseEvent(b); event.Action != "" {
		return event
	}
	if event := editEscapeEvent(b); event.Action != "" {
		return event
	}
	if bytes.Contains(b, []byte{'\r'}) || bytes.Contains(b, []byte{'\n'}) {
		return KeyEvent{Action: "enter"}
	}
	if bytes.Contains(b, []byte{127}) || bytes.Contains(b, []byte{8}) {
		return KeyEvent{Action: "backspace"}
	}
	text := string(b)
	if !utf8.ValidString(text) {
		return KeyEvent{}
	}
	var out []rune
	for _, r := range text {
		if r == 'q' {
			return KeyEvent{Action: "quit"}
		}
		if r >= 32 && r != 127 && r != 27 {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return KeyEvent{}
	}
	return KeyEvent{Action: "text", Text: string(out)}
}

func readLinkInputKeyEvent() KeyEvent {
	var buf [256]byte
	n, _ := inputRead(buf[:])
	if n == 0 {
		return KeyEvent{}
	}
	b := buf[:n]
	if event := editEscapeEvent(b); event.Action != "" {
		return event
	}
	if bytes.Contains(b, []byte{'\r'}) || bytes.Contains(b, []byte{'\n'}) {
		return KeyEvent{Action: "enter"}
	}
	if bytes.Contains(b, []byte{127}) || bytes.Contains(b, []byte{8}) {
		return KeyEvent{Action: "backspace"}
	}
	text := string(b)
	if !utf8.ValidString(text) {
		return KeyEvent{}
	}
	var out []rune
	for _, r := range text {
		if r >= 32 && r != 127 && r != 27 {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return KeyEvent{}
	}
	return KeyEvent{Action: "text", Text: string(out)}
}

func readSearchKeyEvent() KeyEvent {
	var buf [32]byte
	n, _ := inputRead(buf[:])
	if n == 0 {
		return KeyEvent{}
	}
	b := buf[:n]
	if event := parseMouseEvent(b); event.Action != "" {
		return event
	}
	if consumeBracketedPaste(b) {
		return KeyEvent{Action: "control"}
	}
	if bytes.Equal(b, []byte{27}) {
		return KeyEvent{Action: "escape"}
	}
	if bytes.Contains(b, []byte{127}) || bytes.Contains(b, []byte{8}) {
		return KeyEvent{Action: "backspace"}
	}
	if bytes.Contains(b, []byte{'\r'}) || bytes.Contains(b, []byte{'\n'}) {
		return KeyEvent{Action: "enter"}
	}
	if len(b) > 0 && b[0] == 27 {
		return KeyEvent{Action: "control"}
	}
	text := string(b)
	if !utf8.ValidString(text) {
		return KeyEvent{}
	}
	var out []rune
	for _, r := range text {
		if r >= 32 && r != 127 && r != 27 {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return KeyEvent{}
	}
	return KeyEvent{Action: "text", Text: string(out)}
}

func readJumpKeyEvent() KeyEvent {
	var buf [32]byte
	n, _ := inputRead(buf[:])
	if n == 0 {
		return KeyEvent{}
	}
	b := buf[:n]
	if event := parseMouseEvent(b); event.Action != "" {
		return event
	}
	if consumeBracketedPaste(b) {
		return KeyEvent{Action: "control"}
	}
	if bytes.Equal(b, []byte{27}) {
		return KeyEvent{Action: "escape"}
	}
	if bytes.Contains(b, []byte{127}) || bytes.Contains(b, []byte{8}) {
		return KeyEvent{Action: "backspace"}
	}
	if bytes.Contains(b, []byte{'\r'}) || bytes.Contains(b, []byte{'\n'}) {
		return KeyEvent{Action: "enter"}
	}
	if len(b) > 0 && b[0] == 27 {
		return KeyEvent{Action: "control"}
	}
	text := string(b)
	if !utf8.ValidString(text) {
		return KeyEvent{}
	}
	var out []rune
	for _, r := range text {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return KeyEvent{}
	}
	return KeyEvent{Action: "text", Text: string(out)}
}

func readImagePlacementKeyEvent() KeyEvent {
	var buf [32]byte
	n, _ := inputRead(buf[:])
	if n == 0 {
		return KeyEvent{}
	}
	b := buf[:n]
	if event := parseMouseEvent(b); event.Action != "" {
		return event
	}
	if consumeBracketedPaste(b) {
		return KeyEvent{Action: "paste"}
	}
	if event := shiftArrowEvent(b); event.Action != "" {
		return event
	}
	if event := editEscapeEvent(b); event.Action != "" {
		return event
	}
	if bytes.Contains(b, []byte{3}) {
		return KeyEvent{Action: "copy"}
	}
	if bytes.Contains(b, []byte{26}) {
		return KeyEvent{Action: "undo"}
	}
	if bytes.Contains(b, []byte{25}) {
		return KeyEvent{Action: "redo"}
	}
	if bytes.Contains(b, []byte{24}) {
		return KeyEvent{Action: "cut"}
	}
	if bytes.Contains(b, []byte{22}) {
		return KeyEvent{Action: "paste"}
	}
	if bytes.Contains(b, []byte{'\r'}) || bytes.Contains(b, []byte{'\n'}) {
		return KeyEvent{Action: "save"}
	}
	if bytes.Contains(b, []byte{127}) || bytes.Contains(b, []byte{8}) {
		return KeyEvent{Action: "backspace"}
	}
	if bytes.Contains(b, []byte{'q'}) {
		return KeyEvent{Action: "quit"}
	}
	if bytes.Contains(b, []byte{'s'}) {
		return KeyEvent{Action: "settings"}
	}
	if bytes.Contains(b, []byte{'='}) {
		return KeyEvent{Action: "align-center"}
	}
	if bytes.Contains(b, []byte{'>'}) {
		return KeyEvent{Action: "align-right"}
	}
	if bytes.Contains(b, []byte{'<'}) {
		return KeyEvent{Action: "align-left"}
	}
	if bytes.Contains(b, []byte{'+'}) {
		return KeyEvent{Action: "scale-up"}
	}
	if bytes.Contains(b, []byte{'-'}) || bytes.Contains(b, []byte{'_'}) {
		return KeyEvent{Action: "scale-down"}
	}
	return KeyEvent{}
}

var (
	bracketedPasteStart = []byte{27, '[', '2', '0', '0', '~'}
	bracketedPasteEnd   = []byte{27, '[', '2', '0', '1', '~'}
)

func consumeBracketedPaste(initial []byte) bool {
	if !bytes.Contains(initial, bracketedPasteStart) {
		return false
	}
	data := append([]byte(nil), initial...)
	var buf [512]byte
	deadline := time.Now().Add(750 * time.Millisecond)
	for !bytes.Contains(data, bracketedPasteEnd) && time.Now().Before(deadline) {
		n, _ := inputRead(buf[:])
		if n == 0 {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		data = append(data, buf[:n]...)
	}
	return true
}

func parseMouseEvent(b []byte) KeyEvent {
	text := string(b)
	start := strings.Index(text, "\x1b[<")
	if start < 0 {
		return KeyEvent{}
	}
	end := start + 3
	for end < len(text) && text[end] != 'M' && text[end] != 'm' {
		end++
	}
	if end >= len(text) {
		return KeyEvent{Action: "mouse"}
	}
	payload := text[start+3 : end]
	parts := strings.Split(payload, ";")
	if len(parts) != 3 {
		return KeyEvent{Action: "mouse"}
	}
	button, err1 := strconv.Atoi(parts[0])
	x, err2 := strconv.Atoi(parts[1])
	y, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return KeyEvent{Action: "mouse"}
	}
	if button >= 64 && button <= 67 {
		return KeyEvent{Action: "mouse-scroll", X: x - 1, Y: y - 1, Button: button}
	}
	if text[end] == 'm' {
		return KeyEvent{Action: "mouse-release", X: x - 1, Y: y - 1, Button: button}
	}
	if button&4 != 0 {
		return KeyEvent{Action: "shift-mouse-click", X: x - 1, Y: y - 1, Button: button}
	}
	return KeyEvent{Action: "mouse-click", X: x - 1, Y: y - 1, Button: button}
}

func editEscapeEvent(b []byte) KeyEvent {
	if len(b) == 0 || b[0] != 27 {
		return KeyEvent{}
	}
	if len(b) == 1 {
		return KeyEvent{Action: "escape"}
	}
	switch {
	case bytes.Contains(b, []byte{27, '[', 'A'}) || bytes.Contains(b, []byte{27, 'O', 'A'}):
		return KeyEvent{Action: "up"}
	case bytes.Contains(b, []byte{27, '[', 'B'}) || bytes.Contains(b, []byte{27, 'O', 'B'}):
		return KeyEvent{Action: "down"}
	case bytes.Contains(b, []byte{27, '[', 'C'}) || bytes.Contains(b, []byte{27, 'O', 'C'}):
		return KeyEvent{Action: "right"}
	case bytes.Contains(b, []byte{27, '[', 'D'}) || bytes.Contains(b, []byte{27, 'O', 'D'}):
		return KeyEvent{Action: "left"}
	default:
		return KeyEvent{Action: "control"}
	}
}

func shiftEnterEvent(b []byte) bool {
	sequences := [][]byte{
		{27, '[', '1', '3', ';', '2', 'u'},
		{27, '[', '1', '3', ';', '2', '~'},
		{27, '[', '2', '7', ';', '2', ';', '1', '3', '~'},
		{27, '[', '1', ';', '2', '\r'},
		{27, '[', '1', ';', '2', '\n'},
	}
	for _, sequence := range sequences {
		if bytes.Contains(b, sequence) {
			return true
		}
	}
	return false
}

func shiftArrowEvent(b []byte) KeyEvent {
	switch {
	case bytes.Contains(b, []byte{27, '[', '1', ';', '2', 'A'}) || bytes.Contains(b, []byte{27, '[', '2', 'A'}):
		return KeyEvent{Action: "shift-up"}
	case bytes.Contains(b, []byte{27, '[', '1', ';', '2', 'B'}) || bytes.Contains(b, []byte{27, '[', '2', 'B'}):
		return KeyEvent{Action: "shift-down"}
	case bytes.Contains(b, []byte{27, '[', '1', ';', '2', 'C'}) || bytes.Contains(b, []byte{27, '[', '2', 'C'}):
		return KeyEvent{Action: "shift-right"}
	case bytes.Contains(b, []byte{27, '[', '1', ';', '2', 'D'}) || bytes.Contains(b, []byte{27, '[', '2', 'D'}):
		return KeyEvent{Action: "shift-left"}
	default:
		return KeyEvent{}
	}
}

func rawTerminal() (func(), error) {
	oldCmd := exec.Command("stty", "-g")
	oldCmd.Stdin = os.Stdin
	old, _ := oldCmd.Output()

	rawCmd := exec.Command("stty", "raw", "-echo", "min", "0", "time", "0")
	rawCmd.Stdin = os.Stdin
	if err := rawCmd.Run(); err != nil {
		return nil, err
	}
	return func() {
		restoreCmd := exec.Command("stty", string(bytes.TrimSpace(old)))
		restoreCmd.Stdin = os.Stdin
		_ = restoreCmd.Run()
	}, nil
}

func terminalSize() (int, int) {
	if width, height, ok := terminalSizeOK(); ok {
		return width, height
	}
	return 80, 25
}

func terminalAuthoredSize() (int, int) {
	width, height := terminalSize()
	return authoredRenderSize(width, height)
}

func terminalSizeOK() (int, int, bool) {
	if columns, errW := strconv.Atoi(os.Getenv("COLUMNS")); errW == nil && columns > 0 {
		if lines, errH := strconv.Atoi(os.Getenv("LINES")); errH == nil && lines > 0 {
			return columns, lines, true
		}
	}
	cmd := exec.Command("stty", "size")
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	if err == nil {
		parts := strings.Fields(string(out))
		if len(parts) == 2 {
			h, _ := strconv.Atoi(parts[0])
			w, _ := strconv.Atoi(parts[1])
			if w > 0 && h > 0 {
				return w, h, true
			}
		}
	}
	return 0, 0, false
}

func c64Prefix(text string) string {
	var sb strings.Builder
	for _, r := range text {
		if r == '█' {
			sb.WriteString("█   ")
		} else {
			sb.WriteString("    ")
		}
	}
	return sb.String()
}

func wrapWords(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	cur := ""
	for _, word := range words {
		if len([]rune(word)) > width {
			if cur != "" {
				lines = append(lines, cur)
				cur = ""
			}
			rs := []rune(word)
			for len(rs) > width {
				lines = append(lines, string(rs[:width]))
				rs = rs[width:]
			}
			if len(rs) > 0 {
				cur = string(rs)
			}
			continue
		}
		candidate := word
		if cur != "" {
			candidate = cur + " " + word
		}
		if len([]rune(candidate)) <= width {
			cur = candidate
		} else {
			lines = append(lines, cur)
			cur = word
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func trimBlank(rows []string) []string {
	out := rows[:0]
	for _, row := range rows {
		out = append(out, strings.TrimRight(row, " "))
	}
	return out
}

func crop(s string, width int) string {
	rs := []rune(s)
	if len(rs) <= width {
		return s
	}
	return string(rs[:width])
}

func cropANSIVisible(text string, width int) string {
	if width <= 0 {
		return ""
	}
	var out strings.Builder
	visible := 0
	for i := 0; i < len(text) && visible < width; {
		if text[i] == 0x1b {
			end := i + 1
			for end < len(text) && text[end] != 'm' {
				end++
			}
			if end < len(text) {
				out.WriteString(text[i : end+1])
				i = end + 1
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		out.WriteRune(r)
		visible++
		i += size
	}
	out.WriteString("\033[0m")
	return out.String()
}

func displayWidth(s string) int { return len([]rune(s)) }

func padRunes(s string, width int) string {
	rs := []rune(s)
	if len(rs) >= width {
		return string(rs[:width])
	}
	return s + strings.Repeat(" ", width-len(rs))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
