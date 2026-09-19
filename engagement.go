package main

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const engagementVersion = 1

var engagementMetaRE = regexp.MustCompile(`<!--\s*keynope-engagement\s+version=1\s+base64:([A-Za-z0-9+/=]+)\s*-->`)

// Eight symbols from a 62-character alphabet carry about 47.6 bits of entropy. The
// channel code is shared by trusted workshop participants.
const activityCodeAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

var defaultPulseOptions = []string{"Not at all", "Hardly", "Meh", "Somewhat", "Quite", "Very"}
var legacyPulseOptions = []string{"0", "1", "2", "3", "4", "5"}

var activityCodeRE = regexp.MustCompile(`^[A-Za-z0-9]{8}$`)

// EngagementDefinition is authored activity configuration. Completed results are
// stored separately on the slide, not in definitions sent to participants.
type EngagementDefinition struct {
	ID            string               `json:"id,omitempty"`
	Code          string               `json:"code,omitempty"`
	Kind          string               `json:"kind"`
	Prompt        string               `json:"prompt,omitempty"`
	Options       []string             `json:"options,omitempty"`
	Zones         []string             `json:"zones,omitempty"`
	Cards         []string             `json:"cards,omitempty"`
	Questions     []EngagementQuestion `json:"questions,omitempty"`
	Prerequisites []PrerequisiteItem   `json:"prerequisites,omitempty"`
	// Omitted in older decks: preserve their completion-based grouping.
	DisableGrouping   bool `json:"disableGrouping,omitempty"`
	Correct           int  `json:"correct,omitempty"`
	GroupSize         int  `json:"groupSize,omitempty"`
	GroupCount        int  `json:"groupCount,omitempty"`
	ImpostorCount     int  `json:"impostorCount,omitempty"`
	ChosenCount       int  `json:"chosenCount,omitempty"`
	MaxEntries        int  `json:"maxEntries,omitempty"`
	TimerSeconds      int  `json:"timerSeconds,omitempty"`
	JoinSeconds       int  `json:"joinSeconds,omitempty"`
	DiscussionSeconds int  `json:"discussionSeconds,omitempty"`
	// Named is opt-in. Its zero value keeps activities anonymous by default.
	Named          bool            `json:"named,omitempty"`
	DotBudget      int             `json:"dotBudget,omitempty"`
	StackDots      bool            `json:"stackDots,omitempty"`
	ImportPrevious bool            `json:"importPrevious,omitempty"`
	Extra          *jsonExtensions `json:"-"`
}

type EngagementQuestion struct {
	Prompt  string          `json:"prompt"`
	Options []string        `json:"options"`
	Correct int             `json:"correct"`
	Extra   *jsonExtensions `json:"-"`
}

type PrerequisiteItem struct {
	Title        string          `json:"title"`
	Instructions string          `json:"instructions"`
	Extra        *jsonExtensions `json:"-"`
}

func (d EngagementDefinition) MarshalJSON() ([]byte, error) {
	type stored EngagementDefinition
	return encodeJSONExtensions(stored(d), d.Extra)
}

func (d *EngagementDefinition) UnmarshalJSON(data []byte) error {
	type stored EngagementDefinition
	var value stored
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "id", "code", "kind", "prompt", "options", "zones", "cards", "questions", "prerequisites", "disableGrouping", "correct", "groupSize", "groupCount", "impostorCount", "chosenCount", "maxEntries", "timerSeconds", "joinSeconds", "discussionSeconds", "named", "dotBudget", "stackDots", "importPrevious")
	if err != nil {
		return err
	}
	// Never retain retired connection credentials as forward-compatible
	// authored metadata. Session authentication remains ephemeral.
	if extra != nil {
		for _, key := range []string{"stateKey", "presenterKey", "signingKey", "privateKey", "webCryptoKey"} {
			delete(*extra, key)
		}
		if len(*extra) == 0 {
			extra = nil
		}
	}
	*d = EngagementDefinition(value)
	d.Extra = extra
	return nil
}

func (q EngagementQuestion) MarshalJSON() ([]byte, error) {
	type stored EngagementQuestion
	return encodeJSONExtensions(stored(q), q.Extra)
}

func (q *EngagementQuestion) UnmarshalJSON(data []byte) error {
	type stored EngagementQuestion
	var value stored
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "prompt", "options", "correct")
	if err != nil {
		return err
	}
	*q = EngagementQuestion(value)
	q.Extra = extra
	return nil
}

func (p PrerequisiteItem) MarshalJSON() ([]byte, error) {
	type stored PrerequisiteItem
	return encodeJSONExtensions(stored(p), p.Extra)
}

func (p *PrerequisiteItem) UnmarshalJSON(data []byte) error {
	type stored PrerequisiteItem
	var value stored
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "title", "instructions")
	if err != nil {
		return err
	}
	*p = PrerequisiteItem(value)
	p.Extra = extra
	return nil
}

type EngagementAttribution struct {
	ID               string              `json:"id,omitempty"`
	DisplayName      string              `json:"displayName"`
	Choice           *int                `json:"choice,omitempty"`
	Dots             []int               `json:"dots,omitempty"`
	Idea             string              `json:"idea,omitempty"`
	Assignments      []int               `json:"assignments,omitempty"`
	Answers          []string            `json:"answers,omitempty"`
	Choices          []int               `json:"choices,omitempty"`
	MultiAssignments [][]int             `json:"multiAssignments,omitempty"`
	Drawing          []string            `json:"drawing,omitempty"`
	Avatar           *IntroductionAvatar `json:"avatar,omitempty"`
	Tags             []string            `json:"tags,omitempty"`
	Target           string              `json:"target,omitempty"`
	Question         string              `json:"question,omitempty"`
	Votes            int                 `json:"votes,omitempty"`
	Voters           []string            `json:"voters,omitempty"`
	Group            string              `json:"group,omitempty"`
}

type IntroductionAvatar struct {
	Selections map[string]int    `json:"selections"`
	Colors     map[string]string `json:"colors"`
}

type EngagementGroup struct {
	Name    string   `json:"name"`
	Label   string   `json:"label,omitempty"`
	Members []string `json:"members"`
}

func randomActivityString(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("activity identifier length must be positive")
	}
	result := make([]byte, 0, length)
	limit := 256 - 256%len(activityCodeAlphabet)
	for len(result) < length {
		var raw [1]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return "", fmt.Errorf("generate activity identifier: %w", err)
		}
		if int(raw[0]) >= limit {
			continue
		}
		result = append(result, activityCodeAlphabet[int(raw[0])%len(activityCodeAlphabet)])
	}
	return string(result), nil
}

func ensureActivityIdentity(definition *EngagementDefinition) error {
	if definition == nil {
		return nil
	}
	if err := ensureActivityID(definition); err != nil {
		return err
	}
	if !activityCodeRE.MatchString(definition.Code) {
		code, err := randomActivityString(8)
		if err != nil {
			return err
		}
		definition.Code = code
	}
	return nil
}

func ensureActivityID(definition *EngagementDefinition) error {
	if definition == nil || definition.ID != "" {
		return nil
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("generate activity id: %w", err)
	}
	definition.ID = strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
	return nil
}

func activityJoinURL(definition *EngagementDefinition) string {
	if definition == nil || definition.Code == "" {
		return ""
	}
	return "https://keynope.sh/join/" + definition.Code
}

// EngagementRuntimeState is transient presenter state shared between Keynope's
// controller and presentation surfaces. It is never serialized into Markdown.
type EngagementRuntimeState struct {
	AppearanceMode    string                  `json:"appearanceMode,omitempty"`
	Definition        EngagementDefinition    `json:"definition"`
	Slide             int                     `json:"slide"`
	Phase             int                     `json:"phase"`
	Counts            []int                   `json:"counts,omitempty"`
	Ideas             []string                `json:"ideas,omitempty"`
	Assignments       []int                   `json:"assignments,omitempty"`
	Respondents       []string                `json:"respondents,omitempty"`
	Attributions      []EngagementAttribution `json:"attributions,omitempty"`
	Groups            []EngagementGroup       `json:"groups,omitempty"`
	SessionCode       string                  `json:"sessionCode,omitempty"`
	JoinURL           string                  `json:"joinUrl,omitempty"`
	QRCode            string                  `json:"qrCode,omitempty"`
	HideActivityQR    bool                    `json:"hideActivityQR,omitempty"`
	RoomReady         bool                    `json:"roomReady,omitempty"`
	Game              json.RawMessage         `json:"game,omitempty"`
	DeadlineMS        int64                   `json:"deadlineMs,omitempty"`
	PausedRemainingMS int64                   `json:"pausedRemainingMs,omitempty"`
	Participants      int                     `json:"participants,omitempty"`
	QuestionIndex     int                     `json:"questionIndex,omitempty"`
	QuestionRevealed  bool                    `json:"questionRevealed,omitempty"`
}

func cloneEngagementRuntime(runtime *EngagementRuntimeState) *EngagementRuntimeState {
	if runtime == nil {
		return nil
	}
	copyRuntime := *runtime
	copyRuntime.Game = append(json.RawMessage(nil), runtime.Game...)
	copyRuntime.Definition = *cloneEngagement(&runtime.Definition)
	copyRuntime.Counts = append([]int(nil), runtime.Counts...)
	copyRuntime.Ideas = append([]string(nil), runtime.Ideas...)
	copyRuntime.Assignments = append([]int(nil), runtime.Assignments...)
	copyRuntime.Respondents = append([]string(nil), runtime.Respondents...)
	copyRuntime.Attributions = append([]EngagementAttribution(nil), runtime.Attributions...)
	for index := range copyRuntime.Attributions {
		copyRuntime.Attributions[index].Dots = append([]int(nil), runtime.Attributions[index].Dots...)
		if runtime.Attributions[index].Choice != nil {
			choice := *runtime.Attributions[index].Choice
			copyRuntime.Attributions[index].Choice = &choice
		}
		copyRuntime.Attributions[index].Assignments = append([]int(nil), runtime.Attributions[index].Assignments...)
		copyRuntime.Attributions[index].Answers = append([]string(nil), runtime.Attributions[index].Answers...)
		copyRuntime.Attributions[index].Choices = append([]int(nil), runtime.Attributions[index].Choices...)
		copyRuntime.Attributions[index].Drawing = append([]string(nil), runtime.Attributions[index].Drawing...)
		if runtime.Attributions[index].Avatar != nil {
			copyRuntime.Attributions[index].Avatar = &IntroductionAvatar{
				Selections: cloneStringIntMap(runtime.Attributions[index].Avatar.Selections),
				Colors:     cloneStringStringMap(runtime.Attributions[index].Avatar.Colors),
			}
		}
		copyRuntime.Attributions[index].Tags = append([]string(nil), runtime.Attributions[index].Tags...)
		copyRuntime.Attributions[index].Voters = append([]string(nil), runtime.Attributions[index].Voters...)
		copyRuntime.Attributions[index].MultiAssignments = make([][]int, len(runtime.Attributions[index].MultiAssignments))
		for assignmentIndex := range runtime.Attributions[index].MultiAssignments {
			copyRuntime.Attributions[index].MultiAssignments[assignmentIndex] = append([]int(nil), runtime.Attributions[index].MultiAssignments[assignmentIndex]...)
		}
	}
	copyRuntime.Groups = append([]EngagementGroup(nil), runtime.Groups...)
	for index := range copyRuntime.Groups {
		copyRuntime.Groups[index].Members = append([]string(nil), runtime.Groups[index].Members...)
	}
	return &copyRuntime
}

func cloneEngagement(definition *EngagementDefinition) *EngagementDefinition {
	if definition == nil {
		return nil
	}
	copyDefinition := *definition
	copyDefinition.Extra = cloneJSONExtensions(definition.Extra)
	copyDefinition.Options = append([]string(nil), definition.Options...)
	copyDefinition.Zones = append([]string(nil), definition.Zones...)
	copyDefinition.Cards = append([]string(nil), definition.Cards...)
	copyDefinition.Questions = append([]EngagementQuestion(nil), definition.Questions...)
	copyDefinition.Prerequisites = append([]PrerequisiteItem(nil), definition.Prerequisites...)
	for index := range copyDefinition.Questions {
		copyDefinition.Questions[index].Options = append([]string(nil), definition.Questions[index].Options...)
		copyDefinition.Questions[index].Extra = cloneJSONExtensions(definition.Questions[index].Extra)
	}
	for index := range copyDefinition.Prerequisites {
		copyDefinition.Prerequisites[index].Extra = cloneJSONExtensions(definition.Prerequisites[index].Extra)
	}
	return &copyDefinition
}

// Older slide clones kept the source activity ID. Every activity needs its own
// identity for both live sessions and result storage, even when kinds match.
func ensureUniqueEngagementIDs(deck *Deck) bool {
	reserved := make(map[string]bool)
	for _, slide := range deck.Slides {
		if slide.Engagement != nil {
			reserved[slide.Engagement.ID] = true
		}
	}
	seen := make(map[string]bool)
	changed := false
	for i := range deck.Slides {
		slide := &deck.Slides[i]
		if slide.Engagement == nil {
			continue
		}
		oldID := slide.Engagement.ID
		if oldID == "" || seen[oldID] {
			slide.Engagement = cloneEngagement(slide.Engagement)
			for {
				slide.Engagement.ID = newStableID("activity")
				if !reserved[slide.Engagement.ID] {
					break
				}
			}
			reserved[slide.Engagement.ID] = true
			// Keep results already attached to this specific slide.
			if result := slide.EngagementResult; result != nil && result.ActivityID == oldID && result.Kind == slide.Engagement.Kind {
				slide.EngagementResult = cloneEngagementResult(result)
				slide.EngagementResult.ActivityID = slide.Engagement.ID
				if slide.EngagementResult.Definition != nil {
					slide.EngagementResult.Definition.ID = slide.Engagement.ID
				}
			}
			changed = true
		}
		seen[slide.Engagement.ID] = true
	}
	return changed
}

func normalizeEngagement(definition EngagementDefinition) (EngagementDefinition, error) {
	if err := ensureActivityID(&definition); err != nil {
		return EngagementDefinition{}, err
	}
	definition.Kind = strings.ToLower(strings.TrimSpace(definition.Kind))
	// Onboarding owns the deck's durable participant session. Other activity
	// codes remain transient and are deliberately stripped from authored data.
	if definition.Kind == "onboarding" {
		if err := ensureActivityIdentity(&definition); err != nil {
			return EngagementDefinition{}, err
		}
	} else {
		definition.Code = ""
	}
	definition.Prompt = strings.TrimSpace(definition.Prompt)
	if definition.Kind == "dots" && definition.ImportPrevious {
		// Imported workshop contributions must survive result/save round trips
		// without the 160-byte truncation used by manually authored options.
		items := make([]string, 0, len(definition.Options))
		for _, item := range definition.Options {
			item = strings.TrimSpace(item)
			if len(item) > 4000 {
				return EngagementDefinition{}, fmt.Errorf("imported voting item is too long")
			}
			if item != "" {
				items = append(items, item)
			}
		}
		definition.Options = items
	} else {
		definition.Options = cleanEngagementItems(definition.Options)
	}
	definition.Zones = cleanEngagementItems(definition.Zones)
	definition.Cards = cleanEngagementItems(definition.Cards)
	definition.Questions = cleanEngagementQuestions(definition.Questions)
	if definition.TimerSeconds < 0 || definition.TimerSeconds > 99*60+59 {
		return EngagementDefinition{}, fmt.Errorf("activity timer must be between 00:00 and 99:59")
	}
	if len(definition.Prompt) > 500 {
		return EngagementDefinition{}, fmt.Errorf("engagement prompt is too long")
	}
	switch definition.Kind {
	case "nominate":
		definition.Options, definition.Zones, definition.Cards, definition.Questions = nil, nil, nil, nil
	case "shuffle":
		definition.Named = true
		definition.TimerSeconds = 0
		definition.Options, definition.Zones, definition.Cards, definition.Questions = nil, nil, nil, nil
	case "finalanswer":
		definition.MaxEntries = 1
		definition.Named = true
		if definition.TimerSeconds == 0 {
			definition.TimerSeconds = 5 * 60
		}
		if definition.TimerSeconds < 60 || definition.TimerSeconds > 600 {
			return EngagementDefinition{}, fmt.Errorf("Final Answer timer must be between 1 and 10 minutes")
		}
		definition.Options, definition.Zones, definition.Cards, definition.Questions = nil, nil, nil, nil
	case "deducer":
		if definition.GroupCount == 0 {
			definition.GroupCount = 4
		}
		if definition.MaxEntries == 0 {
			definition.MaxEntries = 5
		}
		if definition.TimerSeconds == 0 {
			definition.TimerSeconds = 5 * 60
		}
		if definition.GroupCount < 1 || definition.GroupCount > 10 {
			return EngagementDefinition{}, fmt.Errorf("Deducer needs 1 to 10 groups")
		}
		if definition.MaxEntries < 1 || definition.MaxEntries > 100 {
			return EngagementDefinition{}, fmt.Errorf("Deducer needs 1 to 100 entries per group")
		}
		if definition.TimerSeconds < 60 || definition.TimerSeconds > 600 {
			return EngagementDefinition{}, fmt.Errorf("Deducer timer must be between 1 and 10 minutes")
		}
		definition.Named = true
		definition.Options, definition.Zones, definition.Cards, definition.Questions = nil, nil, nil, nil
	case "pressure":
		if definition.TimerSeconds == 0 {
			definition.TimerSeconds = 5 * 60
		}
		definition.Options, definition.Zones, definition.Cards, definition.Questions = nil, nil, nil, nil
	case "chosen":
		if definition.ChosenCount == 0 {
			definition.ChosenCount = 1
		}
		if definition.ChosenCount < 1 || definition.ChosenCount > 1000 {
			return EngagementDefinition{}, fmt.Errorf("The Chosen needs between 1 and 1000 people")
		}
		definition.Named = true
		definition.Options, definition.Zones, definition.Cards, definition.Questions = nil, nil, nil, nil
	case "ball", "gallery", "hunt", "teach", "fame", "agreements", "three":
		if len(definition.Options) > 40 {
			return EngagementDefinition{}, fmt.Errorf("activity supports at most 40 items")
		}
		if (definition.Kind == "hunt" || definition.Kind == "gallery") && len(definition.Options) == 0 {
			return EngagementDefinition{}, fmt.Errorf("activity needs at least one item")
		}
		if definition.Kind == "hunt" {
			correct := false
			for _, option := range definition.Options {
				correct = correct || strings.HasPrefix(option, "*")
			}
			if !correct {
				return EngagementDefinition{}, fmt.Errorf("mark at least one expected Data Hunt item with *")
			}
		}
		if definition.Kind == "ball" || definition.Kind == "teach" {
			definition.Named = true
		}
		if definition.Kind == "teach" {
			if definition.JoinSeconds == 0 {
				definition.JoinSeconds = 180
			}
			if definition.DiscussionSeconds == 0 {
				definition.DiscussionSeconds = 60
			}
			if definition.JoinSeconds < 1 || definition.JoinSeconds > 5999 || definition.DiscussionSeconds < 1 || definition.DiscussionSeconds > 5999 {
				return EngagementDefinition{}, fmt.Errorf("teach-back timers must be 1 to 5999 seconds")
			}
		}
		definition.Zones, definition.Cards, definition.Questions = nil, nil, nil
	case "dots":
		if definition.ImportPrevious {
			if len(definition.Options) > 500 {
				return EngagementDefinition{}, fmt.Errorf("automatic dot voting supports up to 500 submitted items")
			}
		} else if len(definition.Options) < 2 || len(definition.Options) > 40 {
			return EngagementDefinition{}, fmt.Errorf("dot voting needs 2 to 40 options")
		}
		if definition.DotBudget == 0 {
			definition.DotBudget = 3
		}
		if definition.DotBudget < 1 || definition.DotBudget > 20 {
			return EngagementDefinition{}, fmt.Errorf("dot budget must be 1 to 20")
		}
		definition.Zones, definition.Cards, definition.Questions = nil, nil, nil
	case "prerequisites":
		if len(definition.Prerequisites) < 1 || len(definition.Prerequisites) > 40 {
			return EngagementDefinition{}, fmt.Errorf("prerequisites needs 1 to 40 items")
		}
		for i := range definition.Prerequisites {
			item := &definition.Prerequisites[i]
			item.Title = strings.TrimSpace(item.Title)
			item.Instructions = strings.TrimSpace(item.Instructions)
			if item.Title == "" || len(item.Title) > 160 || len(item.Instructions) > 4000 {
				return EngagementDefinition{}, fmt.Errorf("each prerequisite needs a title (up to 160 characters) and instructions of at most 4000 characters")
			}
		}
	case "finishpair":
		definition.Named = true
		definition.Options, definition.Zones, definition.Cards, definition.Questions = nil, nil, nil, nil
	case "pulse":
		if len(definition.Options) == 0 {
			definition.Options = append([]string(nil), defaultPulseOptions...)
		} else if engagementItemsEqual(definition.Options, legacyPulseOptions) {
			definition.Options = append([]string(nil), defaultPulseOptions...)
		}
		if len(definition.Options) < 2 || len(definition.Options) > 12 {
			return EngagementDefinition{}, fmt.Errorf("pulse needs between 2 and 12 choices")
		}
		definition.Zones, definition.Cards = nil, nil
		definition.Questions = nil
	case "storm":
		definition.Options, definition.Zones, definition.Cards, definition.Questions = nil, nil, nil, nil
	case "sort":
		if len(definition.Zones) < 2 || len(definition.Zones) > 8 {
			return EngagementDefinition{}, fmt.Errorf("sort needs between 2 and 8 destinations")
		}
		if len(definition.Cards) == 0 || len(definition.Cards) > 40 {
			return EngagementDefinition{}, fmt.Errorf("sort needs between 1 and 40 cards")
		}
		definition.Options = nil
		definition.Questions = nil
	case "dual":
		if len(definition.Options) == 0 {
			definition.Options = []string{"What worked?", "What could improve?"}
		}
		if len(definition.Options) != 2 {
			return EngagementDefinition{}, fmt.Errorf("dual response needs exactly 2 prompts")
		}
		definition.Zones, definition.Cards, definition.Questions = nil, nil, nil
	case "quiz":
		if len(definition.Questions) == 0 || len(definition.Questions) > 20 {
			return EngagementDefinition{}, fmt.Errorf("quiz needs between 1 and 20 questions")
		}
		definition.Options, definition.Zones, definition.Cards = nil, nil, nil
	case "truefalse":
		// Migrate the original single-question representation without breaking
		// existing decks. New activities keep up to five questions.
		if len(definition.Questions) == 0 {
			prompt := definition.Prompt
			if prompt == "" {
				prompt = "Statement"
			}
			definition.Questions = []EngagementQuestion{{
				Prompt: prompt, Options: []string{"Fact", "Fiction"}, Correct: definition.Correct,
			}}
		}
		if len(definition.Questions) == 0 || len(definition.Questions) > 5 {
			return EngagementDefinition{}, fmt.Errorf("fact or fiction needs between 1 and 5 questions")
		}
		for index := range definition.Questions {
			definition.Questions[index].Options = []string{"Fact", "Fiction"}
			if definition.Questions[index].Correct < 0 || definition.Questions[index].Correct > 1 {
				return EngagementDefinition{}, fmt.Errorf("fact or fiction answer must be Fact or Fiction")
			}
		}
		definition.Options = []string{"Fact", "Fiction"}
		definition.Zones, definition.Cards = nil, nil
	case "onboarding":
		definition.Named = true
		definition.TimerSeconds = 0
		definition.Options, definition.Zones, definition.Cards, definition.Questions = nil, nil, nil, nil
	case "match":
		if len(definition.Zones) < 2 || len(definition.Zones) > 8 {
			return EngagementDefinition{}, fmt.Errorf("mix and match needs between 2 and 8 destinations")
		}
		if len(definition.Cards) == 0 || len(definition.Cards) > 40 {
			return EngagementDefinition{}, fmt.Errorf("mix and match needs between 1 and 40 cards")
		}
		definition.Options, definition.Questions = nil, nil
	case "questions", "draw", "introduction", "expertise":
		if definition.Kind == "draw" || definition.Kind == "introduction" {
			definition.Named = true
		}
		definition.Options, definition.Zones, definition.Cards, definition.Questions = nil, nil, nil, nil
	case "wall":
		if len(definition.Options) == 0 {
			definition.Options = []string{"What went well", "What could be better", "What is your key takeaway"}
		}
		if len(definition.Options) < 1 || len(definition.Options) > 4 {
			return EngagementDefinition{}, fmt.Errorf("feedback wall needs between 1 and 4 columns")
		}
		definition.Zones, definition.Cards, definition.Questions = nil, nil, nil
	case "pair":
		if definition.GroupSize == 0 {
			definition.GroupSize = 2
		}
		if definition.GroupSize < 2 || definition.GroupSize > 3 {
			return EngagementDefinition{}, fmt.Errorf("pair share group size must be 2 or 3")
		}
		if definition.JoinSeconds == 0 {
			definition.JoinSeconds = definition.TimerSeconds
		}
		if definition.JoinSeconds == 0 {
			definition.JoinSeconds = 2 * 60
		}
		if definition.DiscussionSeconds == 0 {
			definition.DiscussionSeconds = 5 * 60
		}
		if definition.JoinSeconds < 1 || definition.JoinSeconds > 99*60+59 {
			return EngagementDefinition{}, fmt.Errorf("pair share join timer must be between 00:01 and 99:59")
		}
		if definition.DiscussionSeconds < 1 || definition.DiscussionSeconds > 99*60+59 {
			return EngagementDefinition{}, fmt.Errorf("pair share discussion timer must be between 00:01 and 99:59")
		}
		definition.TimerSeconds = 0
		definition.Named = true
		definition.Options, definition.Zones, definition.Cards, definition.Questions = nil, nil, nil, nil
	case "cards":
		if definition.GroupCount == 0 {
			definition.GroupCount = 4
		}
		if definition.GroupCount < 2 || definition.GroupCount > 13 {
			return EngagementDefinition{}, fmt.Errorf("playing card group count must be between 2 and 13")
		}
		definition.GroupSize = 0
		if definition.JoinSeconds == 0 {
			definition.JoinSeconds = definition.TimerSeconds
		}
		if definition.JoinSeconds == 0 {
			definition.JoinSeconds = 2 * 60
		}
		if definition.JoinSeconds < 1 || definition.JoinSeconds > 99*60+59 {
			return EngagementDefinition{}, fmt.Errorf("playing card join timer must be between 00:01 and 99:59")
		}
		definition.TimerSeconds = 0
		// Playing Cards is a social grouping exercise: participants must be
		// identifiable when the presenter reveals the groups.
		definition.Named = true
		definition.Options, definition.Zones, definition.Cards, definition.Questions = nil, nil, nil, nil
	case "impostor":
		if definition.ImpostorCount == 0 {
			definition.ImpostorCount = 1
		}
		if definition.ImpostorCount < 1 || definition.ImpostorCount > 20 {
			return EngagementDefinition{}, fmt.Errorf("impostor count must be between 1 and 20")
		}
		if definition.JoinSeconds == 0 {
			definition.JoinSeconds = definition.TimerSeconds
		}
		if definition.JoinSeconds == 0 {
			definition.JoinSeconds = 2 * 60
		}
		if definition.JoinSeconds < 1 || definition.JoinSeconds > 99*60+59 {
			return EngagementDefinition{}, fmt.Errorf("impostor join timer must be between 00:01 and 99:59")
		}
		definition.TimerSeconds = 0
		definition.Named = false
		definition.Options, definition.Zones, definition.Cards, definition.Questions = nil, nil, nil, nil
	default:
		return EngagementDefinition{}, fmt.Errorf("unknown engagement type %q", definition.Kind)
	}
	if definition.Kind != "pair" {
		definition.GroupSize = 0
	}
	if definition.Kind != "cards" && definition.Kind != "deducer" {
		definition.GroupCount = 0
	}
	if definition.Kind != "impostor" {
		definition.ImpostorCount = 0
	}
	if definition.Kind != "chosen" {
		definition.ChosenCount = 0
	}
	if definition.Kind != "deducer" && definition.Kind != "finalanswer" {
		definition.MaxEntries = 0
	}
	if definition.Kind != "pair" && definition.Kind != "cards" && definition.Kind != "impostor" && definition.Kind != "teach" {
		definition.JoinSeconds = 0
	}
	if definition.Kind != "pair" && definition.Kind != "teach" {
		definition.DiscussionSeconds = 0
	}
	if definition.Kind != "truefalse" {
		definition.Correct = 0
	}
	return definition, nil
}

func cloneStringIntMap(input map[string]int) map[string]int {
	if input == nil {
		return nil
	}
	output := make(map[string]int, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func cloneStringStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func cleanEngagementQuestions(questions []EngagementQuestion) []EngagementQuestion {
	out := make([]EngagementQuestion, 0, len(questions))
	for _, question := range questions {
		question.Prompt = strings.TrimSpace(question.Prompt)
		question.Options = cleanEngagementItems(question.Options)
		if question.Prompt == "" {
			continue
		}
		if len(question.Prompt) > 300 {
			question.Prompt = question.Prompt[:300]
		}
		if len(question.Options) < 2 || len(question.Options) > 8 || question.Correct < 0 || question.Correct >= len(question.Options) {
			continue
		}
		out = append(out, question)
	}
	return out
}

func engagementItemsEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func cleanEngagementItems(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if len(item) > 160 {
			item = item[:160]
		}
		out = append(out, item)
	}
	return out
}

func encodeEngagementMetadata(definition *EngagementDefinition) (string, error) {
	if definition == nil {
		return "", nil
	}
	normalized, err := normalizeEngagement(*definition)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("<!-- keynope-engagement version=%d base64:%s -->", engagementVersion, base64.StdEncoding.EncodeToString(payload)), nil
}

func decodeEngagementMetadata(comment string) (*EngagementDefinition, error) {
	match := engagementMetaRE.FindStringSubmatch(strings.TrimSpace(comment))
	if match == nil {
		return nil, nil
	}
	payload, err := base64.StdEncoding.DecodeString(match[1])
	if err != nil {
		return nil, fmt.Errorf("decode engagement: %w", err)
	}
	var definition EngagementDefinition
	if err := json.Unmarshal(payload, &definition); err != nil {
		return nil, fmt.Errorf("decode engagement: %w", err)
	}
	normalized, err := normalizeEngagement(definition)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}
