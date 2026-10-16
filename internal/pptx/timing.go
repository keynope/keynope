package pptx

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const presentationNamespace = "http://schemas.openxmlformats.org/presentationml/2006/main"
const powerpoint2010Namespace = "http://schemas.microsoft.com/office/powerpoint/2010/main"

// Timeline is the executable entrance subset, not an opaque Office timing tree.
// IDs are Object.ID values on write and own-slide cNvPr IDs on read.
type Timeline struct {
	Transition Transition
	Steps      []BuildStep
}
type Transition struct {
	Effect, Direction string
	DurationMS        int
}
type BuildStep struct {
	TargetIDs         []string
	Effect, Direction string
	DurationMS        int
	Advance           string
	DelayMS           int
}

func timingDirection(value string) string {
	switch value {
	case "right", "r":
		return "right"
	case "up", "u":
		return "up"
	case "down", "d":
		return "down"
	default:
		return "left"
	}
}
func timingSide(value string) string { return timingDirection(value)[:1] }

func timelineXML(timeline *Timeline, ids map[string]int, objects []Object) (string, []Warning) {
	if timeline == nil {
		return "", nil
	}
	var out strings.Builder
	var warnings []Warning
	warn := func(code, message, object string) {
		warnings = append(warnings, Warning{Code: code, Message: message, Object: object})
	}
	t := timeline.Transition
	if t.Effect != "" && t.Effect != "cut" && t.DurationMS > 0 {
		effect := t.Effect
		if effect != "fade" && effect != "push" && effect != "wipe" {
			effect = "cut"
			warn("transition-fallback", "Unsupported slide transition was exported as a cut.", "")
		}
		if effect != "cut" {
			duration := max(1, min(60000, t.DurationMS))
			speed := "slow"
			if duration <= 500 {
				speed = "fast"
			} else if duration <= 1000 {
				speed = "med"
			}
			fmt.Fprintf(&out, `<p:transition spd="%s" advClick="1" p14:dur="%d"><p:%s`, speed, duration, effect)
			if effect != "fade" {
				fmt.Fprintf(&out, ` dir="%s"`, timingSide(t.Direction))
			}
			out.WriteString(`/></p:transition>`)
		}
	}
	if len(timeline.Steps) > 1000 {
		warn("timing-too-large", "The timeline exceeds the supported 1000-step limit and was exported in its final visible state.", "")
		return out.String(), warnings
	}
	steps := []BuildStep{}
	seen := map[string]bool{}
	for _, source := range timeline.Steps {
		if len(source.TargetIDs) > 1000 {
			warn("timing-too-large", "An entrance step exceeds the supported 1000-target limit and the timeline was exported in its final visible state.", "")
			return out.String(), warnings
		}
		step := source
		step.TargetIDs = nil
		for _, target := range source.TargetIDs {
			if ids[target] <= 0 || seen[target] {
				warn("build-target-omitted", "Build target is missing, ambiguous or repeated; it has no entrance animation.", target)
				continue
			}
			seen[target] = true
			step.TargetIDs = append(step.TargetIDs, target)
		}
		if len(step.TargetIDs) == 0 {
			continue
		}
		if step.Effect != "appear" && step.Effect != "fade" && step.Effect != "wipe" && step.Effect != "zoom" {
			warn("build-effect-fallback", "Unsupported entrance effect was exported as Appear.", step.TargetIDs[0])
			step.Effect = "appear"
		}
		if step.DurationMS < 1 {
			warn("build-duration-rounded", "PowerPoint's entrance duration was rounded to 1 ms.", step.TargetIDs[0])
		}
		step.DurationMS = max(1, min(60000, step.DurationMS))
		if source.DurationMS > 60000 || source.DelayMS < 0 || source.DelayMS > 3600000 {
			warn("build-timing-clamped", "Entrance duration or delay was limited to the supported range.", step.TargetIDs[0])
		}
		step.DelayMS = max(0, min(3600000, step.DelayMS))
		step.Direction = timingDirection(step.Direction)
		if step.Advance != "after" {
			step.Advance = "click"
			step.DelayMS = 0
		}
		steps = append(steps, step)
	}
	if len(steps) == 0 {
		return out.String(), warnings
	}
	var b strings.Builder
	b.WriteString(`<p:timing><p:tnLst><p:par><p:cTn id="1" dur="indefinite" restart="never" nodeType="tmRoot"><p:childTnLst><p:seq concurrent="1" nextAc="seek"><p:cTn id="2" dur="indefinite" nodeType="mainSeq"><p:childTnLst>`)
	next := 2
	id := func() int { next++; return next }
	groups := [][]BuildStep{}
	for _, step := range steps {
		if len(groups) == 0 || step.Advance == "click" {
			groups = append(groups, []BuildStep{})
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], step)
	}
	groupIndex := 0
	for _, group := range groups {
		fmt.Fprintf(&b, `<p:par><p:cTn id="%d" fill="hold"><p:stCondLst>`, id())
		if group[0].Advance == "after" {
			b.WriteString(`<p:cond delay="indefinite"/><p:cond evt="onBegin" delay="0"><p:tn val="2"/></p:cond>`)
		} else {
			b.WriteString(`<p:cond delay="indefinite"/>`)
		}
		b.WriteString(`</p:stCondLst><p:childTnLst>`)
		offset := int64(0)
		for _, step := range group {
			offset += int64(step.DelayMS)
			fmt.Fprintf(&b, `<p:par><p:cTn id="%d" fill="hold"><p:stCondLst><p:cond delay="%d"/></p:stCondLst><p:childTnLst>`, id(), offset)
			for targetIndex, target := range step.TargetIDs {
				nodeType := "withEffect"
				if targetIndex == 0 {
					nodeType = "clickEffect"
					if step.Advance == "after" {
						nodeType = "afterEffect"
					}
				}
				preset := 1
				subtype := 0
				switch step.Effect {
				case "fade":
					preset = 10
				case "wipe":
					preset = 22
					subtype = map[string]int{"up": 1, "right": 2, "down": 4, "left": 8}[step.Direction]
				case "zoom":
					preset = 23
					subtype = 16
				}
				fmt.Fprintf(&b, `<p:par><p:cTn id="%d" presetID="%d" presetClass="entr" presetSubtype="%d" dur="%d" fill="hold" grpId="%d" nodeType="%s"><p:stCondLst><p:cond delay="0"/></p:stCondLst><p:childTnLst>`, id(), preset, subtype, step.DurationMS, groupIndex, nodeType)
				fmt.Fprintf(&b, `<p:set><p:cBhvr><p:cTn id="%d" dur="1" fill="hold"><p:stCondLst><p:cond delay="0"/></p:stCondLst></p:cTn><p:tgtEl><p:spTgt spid="%d"/></p:tgtEl><p:attrNameLst><p:attrName>style.visibility</p:attrName></p:attrNameLst></p:cBhvr><p:to><p:strVal val="visible"/></p:to></p:set>`, id(), ids[target])
				if step.Effect != "appear" {
					filter := "fade"
					if step.Effect == "wipe" {
						filter = "wipe(" + step.Direction + ")"
					}
					fmt.Fprintf(&b, `<p:animEffect transition="in" filter="%s"><p:cBhvr><p:cTn id="%d" dur="%d"/><p:tgtEl><p:spTgt spid="%d"/></p:tgtEl></p:cBhvr></p:animEffect>`, filter, id(), step.DurationMS, ids[target])
					if step.Effect == "zoom" {
						fmt.Fprintf(&b, `<p:animScale zoomContents="1"><p:cBhvr><p:cTn id="%d" dur="%d"/><p:tgtEl><p:spTgt spid="%d"/></p:tgtEl></p:cBhvr><p:from x="86000" y="86000"/><p:to x="100000" y="100000"/></p:animScale>`, id(), step.DurationMS, ids[target])
					}
				}
				b.WriteString(`</p:childTnLst></p:cTn></p:par>`)
			}
			b.WriteString(`</p:childTnLst></p:cTn></p:par>`)
			offset += int64(step.DurationMS)
			groupIndex++
			if b.Len() > 4<<20 {
				warn("timing-too-large", "The timing XML exceeds 4 MiB and was exported in its final visible state.", "")
				return out.String(), warnings
			}
		}
		b.WriteString(`</p:childTnLst></p:cTn></p:par>`)
	}
	b.WriteString(`</p:childTnLst></p:cTn><p:prevCondLst><p:cond evt="onPrev" delay="0"><p:tgtEl><p:sldTgt/></p:tgtEl></p:cond></p:prevCondLst><p:nextCondLst><p:cond evt="onNext" delay="0"><p:tgtEl><p:sldTgt/></p:tgtEl></p:cond></p:nextCondLst></p:seq></p:childTnLst></p:cTn></p:par></p:tnLst><p:bldLst>`)
	kinds := map[string]string{}
	for _, object := range objects {
		kinds[object.ID] = object.Kind
	}
	for index, step := range steps {
		for _, target := range step.TargetIDs {
			if kinds[target] == "table" || kinds[target] == "chart" {
				fmt.Fprintf(&b, `<p:bldGraphic spid="%d" grpId="%d"><p:bldAsOne/></p:bldGraphic>`, ids[target], index)
			} else {
				fmt.Fprintf(&b, `<p:bldP spid="%d" grpId="%d" build="allAtOnce" animBg="1"/>`, ids[target], index)
			}
		}
	}
	b.WriteString(`</p:bldLst></p:timing>`)
	if b.Len() > 4<<20 {
		warn("timing-too-large", "The timing XML exceeds 4 MiB and was exported in its final visible state.", "")
		return out.String(), warnings
	}
	out.WriteString(b.String())
	return out.String(), warnings
}

// Decode only timing/transition trees with namespace-aware, bounded XML.
// Existing slide parsing stays fast for the common no-timeline case.
type timingNode struct {
	name     xml.Name
	attrs    []xml.Attr
	text     string
	children []*timingNode
}

func (n *timingNode) attr(name string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
func (n *timingNode) attrNS(space, name string) string {
	if n != nil {
		for _, a := range n.attrs {
			if a.Name.Space == space && a.Name.Local == name {
				return a.Value
			}
		}
	}
	return ""
}
func (n *timingNode) child(name string) *timingNode {
	if n != nil {
		for _, c := range n.children {
			if c.name.Space == presentationNamespace && c.name.Local == name {
				return c
			}
		}
	}
	return nil
}
func timingTree(ctx context.Context, d *xml.Decoder, start xml.StartElement, depth int, count *int) (*timingNode, error) {
	*count++
	if depth > 32 || *count > 100000 {
		return nil, fmt.Errorf("timing tree exceeds bounds")
	}
	n := &timingNode{name: start.Name, attrs: start.Attr}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		token, err := d.Token()
		if err != nil {
			return nil, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			child, err := timingTree(ctx, d, t, depth+1, count)
			if err != nil {
				return nil, err
			}
			n.children = append(n.children, child)
		case xml.CharData:
			n.text += string(t)
		case xml.EndElement:
			return n, nil
		}
	}
}
func readTimeline(ctx context.Context, data []byte, root *node, objects []Object) (*Timeline, []Warning) {
	if findChild(root, "timing") == nil && findChild(root, "transition") == nil {
		return nil, nil
	}
	var transition, timing *timingNode
	d := xml.NewDecoder(bytes.NewReader(data))
	depth, count := 0, 0
	unsupported := func(message string) (*Timeline, []Warning) {
		return nil, []Warning{{Code: "timing-unsupported", Message: message + "; all objects remain visible."}}
	}
	for {
		if ctx.Err() != nil {
			return unsupported("Animation import was cancelled")
		}
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return unsupported("Malformed animation XML")
		}
		switch t := token.(type) {
		case xml.StartElement:
			depth++
			if depth == 2 && t.Name.Space == presentationNamespace && (t.Name.Local == "timing" || t.Name.Local == "transition") {
				if t.Name.Local == "timing" && timing != nil || t.Name.Local == "transition" && transition != nil {
					return unsupported("Repeated animation or transition root")
				}
				n, err := timingTree(ctx, d, t, 0, &count)
				if err != nil {
					return unsupported("Animation XML exceeds supported bounds")
				}
				if t.Name.Local == "timing" {
					timing = n
				} else {
					transition = n
				}
				depth--
			}
		case xml.EndElement:
			depth--
		}
	}
	result := &Timeline{Transition: Transition{Effect: "cut"}}
	warnings := []Warning{}
	if transition != nil {
		if transition.attr("advTm") != "" {
			warnings = append(warnings, Warning{Code: "automatic-slide-advance-omitted", Message: "Automatic slide advance is not imported; slides advance manually."})
		}
		if len(transition.children) == 1 && transition.children[0].name.Space == presentationNamespace {
			effect := transition.children[0]
			switch effect.name.Local {
			case "cut":
			case "fade", "push", "wipe":
				result.Transition.Effect = effect.name.Local
				result.Transition.Direction = timingDirection(effect.attr("dir"))
				duration, err := strconv.Atoi(transition.attrNS(powerpoint2010Namespace, "dur"))
				if err != nil {
					duration = 500
					switch transition.attr("spd") {
					case "med":
						duration = 1000
					case "slow":
						duration = 2000
					}
					warnings = append(warnings, Warning{Code: "transition-duration-estimated", Message: "Legacy transition speed was mapped to an estimated duration."})
				}
				result.Transition.DurationMS = max(0, min(60000, duration))
			default:
				warnings = append(warnings, Warning{Code: "transition-fallback", Message: "Unsupported slide transition was imported as a cut."})
			}
		} else {
			warnings = append(warnings, Warning{Code: "transition-fallback", Message: "Complex slide transition was imported as a cut."})
		}
	}
	if timing == nil {
		if findChild(root, "timing") != nil {
			warnings = append(warnings, Warning{Code: "timing-unsupported", Message: "Unknown timing namespace was not imported; all objects remain visible."})
		}
		return result, warnings
	}
	steps, err := readEntranceSteps(timing, objects)
	if err != nil {
		warnings = append(warnings, Warning{Code: "timing-unsupported", Message: err.Error() + "; object builds were not imported and all objects remain visible."})
	} else {
		result.Steps = steps
	}
	return result, warnings
}

func numericDelay(n *timingNode) (int64, error) {
	list := n.child("stCondLst")
	if list == nil || len(list.children) != 1 || list.children[0].attr("evt") != "" {
		return 0, fmt.Errorf("unsupported timing trigger")
	}
	v, err := strconv.ParseInt(list.children[0].attr("delay"), 10, 64)
	if err != nil || v < 0 || v > 3660000000 {
		return 0, fmt.Errorf("invalid timing delay")
	}
	return v, nil
}
func readEntranceSteps(timing *timingNode, objects []Object) ([]BuildStep, error) {
	valid := map[string]bool{}
	for _, object := range objects {
		if !object.Inherited {
			if _, exists := valid[object.ID]; exists {
				valid[object.ID] = false
			} else {
				valid[object.ID] = true
			}
		}
	}
	ids := map[uint64]bool{}
	var validate func(*timingNode) error
	validate = func(n *timingNode) error {
		if n.name.Space != presentationNamespace {
			return fmt.Errorf("unsupported timing namespace")
		}
		switch n.name.Local {
		case "timing", "tnLst", "par", "cTn", "childTnLst", "seq", "stCondLst", "cond", "tn", "tgtEl", "sldTgt", "prevCondLst", "nextCondLst", "set", "cBhvr", "spTgt", "attrNameLst", "attrName", "to", "strVal", "animEffect", "animScale", "from", "bldLst", "bldP", "bldGraphic", "bldAsOne":
		default:
			return fmt.Errorf("unsupported timing element %s", n.name.Local)
		}
		if n.name.Local == "cTn" {
			id, err := strconv.ParseUint(n.attr("id"), 10, 32)
			if err != nil || id == 0 || ids[id] {
				return fmt.Errorf("invalid or repeated timing ID")
			}
			ids[id] = true
			if n.attr("repeatCount") != "" && n.attr("repeatCount") != "1000" || n.attr("repeatDur") != "" || n.attr("autoRev") == "1" || n.attr("autoRev") == "true" {
				return fmt.Errorf("repeating/reversing animations are unsupported")
			}
			if n.attr("spd") != "" && n.attr("spd") != "100000" || n.attr("accel") != "" && n.attr("accel") != "0" || n.attr("decel") != "" && n.attr("decel") != "0" || n.attr("tmFilter") != "" || n.attr("evtFilter") != "" {
				return fmt.Errorf("nonlinear or filtered animation timing is unsupported")
			}
		}
		for _, c := range n.children {
			if err := validate(c); err != nil {
				return err
			}
		}
		return nil
	}
	if err := validate(timing); err != nil {
		return nil, err
	}
	timeNodes := timing.child("tnLst")
	if timeNodes == nil || len(timeNodes.children) != 1 || timeNodes.children[0].name.Local != "par" || len(timeNodes.children[0].children) != 1 {
		return nil, fmt.Errorf("multiple or interactive timing roots are unsupported")
	}
	root := timeNodes.child("par").child("cTn")
	if root == nil || root.attr("nodeType") != "tmRoot" {
		return nil, fmt.Errorf("unsupported timing root")
	}
	list := root.child("childTnLst")
	if list == nil || len(list.children) != 1 || list.children[0].name.Local != "seq" {
		return nil, fmt.Errorf("interactive or multiple sequences are unsupported")
	}
	seq := list.child("seq")
	if seq.attr("concurrent") != "1" || seq.attr("nextAc") != "seek" {
		return nil, fmt.Errorf("unsupported sequence navigation policy")
	}
	for _, event := range []struct{ name, evt string }{{"prevCondLst", "onPrev"}, {"nextCondLst", "onNext"}} {
		conditions := seq.child(event.name)
		if conditions == nil || len(conditions.children) != 1 {
			return nil, fmt.Errorf("unsupported slide navigation conditions")
		}
		condition := conditions.children[0]
		if condition.attr("evt") != event.evt || condition.attr("delay") != "0" || condition.child("tgtEl").child("sldTgt") == nil {
			return nil, fmt.Errorf("unsupported slide navigation trigger")
		}
	}
	sequence := seq.child("cTn")
	if sequence == nil || sequence.attr("nodeType") != "mainSeq" {
		return nil, fmt.Errorf("unsupported animation sequence")
	}
	steps := []BuildStep{}
	seen := map[string]bool{}
	groups := sequence.child("childTnLst")
	if groups == nil {
		return nil, fmt.Errorf("missing animation groups")
	}
	for groupIndex, group := range groups.children {
		if group.name.Local != "par" {
			return nil, fmt.Errorf("unsupported animation group")
		}
		c := group.child("cTn")
		conditions := c.child("stCondLst")
		if conditions == nil || len(conditions.children) < 1 || len(conditions.children) > 2 {
			return nil, fmt.Errorf("missing click conditions")
		}
		automatic := false
		for _, condition := range conditions.children {
			if condition.name.Local != "cond" {
				return nil, fmt.Errorf("unsupported click condition")
			}
			if groupIndex == 0 && !automatic && condition.attr("evt") == "onBegin" && condition.attr("delay") == "0" && len(condition.children) == 1 && condition.child("tn").attr("val") == sequence.attr("id") {
				automatic = true
			} else if condition.attr("evt") != "" || condition.attr("delay") != "indefinite" || len(condition.children) != 0 {
				return nil, fmt.Errorf("unsupported click trigger")
			}
		}
		wrappers := c.child("childTnLst")
		if wrappers == nil {
			return nil, fmt.Errorf("missing entrance steps")
		}
		previousEnd := int64(0)
		for stepIndex, wrapper := range wrappers.children {
			if len(steps) >= 1000 {
				return nil, fmt.Errorf("too many entrance steps")
			}
			container := wrapper.child("cTn")
			offset, err := numericDelay(container)
			if err != nil {
				return nil, err
			}
			delay := offset - previousEnd
			if delay < 0 || delay > 3600000 {
				return nil, fmt.Errorf("overlapping or excessive entrance delay")
			}
			step := BuildStep{Advance: "after", DelayMS: int(delay)}
			if stepIndex == 0 && !automatic {
				step.Advance = "click"
				if offset != 0 {
					return nil, fmt.Errorf("delayed click effects are unsupported")
				}
			}
			effects := container.child("childTnLst")
			if effects == nil || len(effects.children) > 1000 {
				return nil, fmt.Errorf("invalid entrance target count")
			}
			for effectIndex, effect := range effects.children {
				if effect.name.Local != "par" || len(effect.children) != 1 {
					return nil, fmt.Errorf("unsupported entrance wrapper")
				}
				expectedTrigger := "withEffect"
				if effectIndex == 0 {
					expectedTrigger = "afterEffect"
					if step.Advance == "click" {
						expectedTrigger = "clickEffect"
					}
				}
				if effect.child("cTn").attr("nodeType") != expectedTrigger {
					return nil, fmt.Errorf("inconsistent entrance trigger")
				}
				parsed, target, err := readEntrance(effect.child("cTn"))
				if err != nil {
					return nil, err
				}
				if !valid[target] || seen[target] {
					return nil, fmt.Errorf("missing, inherited, ambiguous or repeated entrance target")
				}
				if effectIndex == 0 {
					step.Effect, step.Direction, step.DurationMS = parsed.Effect, parsed.Direction, parsed.DurationMS
				} else if step.Effect != parsed.Effect || step.Direction != parsed.Direction || step.DurationMS != parsed.DurationMS {
					return nil, fmt.Errorf("mixed simultaneous entrance effects are unsupported")
				}
				seen[target] = true
				step.TargetIDs = append(step.TargetIDs, target)
			}
			if len(step.TargetIDs) == 0 {
				return nil, fmt.Errorf("empty entrance step")
			}
			steps = append(steps, step)
			previousEnd = offset + int64(step.DurationMS)
		}
	}
	return steps, nil
}
func readEntrance(n *timingNode) (BuildStep, string, error) {
	invalid := func() (BuildStep, string, error) { return BuildStep{}, "", fmt.Errorf("unsupported entrance behavior") }
	if n == nil || n.attr("presetClass") != "entr" {
		return invalid()
	}
	if n.attr("fill") != "hold" {
		return invalid()
	}
	switch n.attr("nodeType") {
	case "clickEffect", "afterEffect", "withEffect":
	default:
		return invalid()
	}
	if delay, err := numericDelay(n); err != nil || delay != 0 {
		return invalid()
	}
	duration, err := strconv.Atoi(n.attr("dur"))
	if n.attr("dur") == "" {
		duration = 0
		err = nil
	}
	if err != nil || duration < 0 || duration > 60000 {
		return invalid()
	}
	step := BuildStep{Effect: "appear", Direction: "left", DurationMS: duration}
	target := ""
	visible, fade, scaled := false, false, false
	behaviors := n.child("childTnLst")
	if behaviors == nil {
		return invalid()
	}
	for _, behavior := range behaviors.children {
		common := behavior.child("cBhvr")
		if common.attr("additive") != "" && common.attr("additive") != "base" || common.attr("accumulate") != "" && common.attr("accumulate") != "none" {
			return invalid()
		}
		behaviorDuration, err := strconv.Atoi(common.child("cTn").attr("dur"))
		if err != nil || behaviorDuration < 1 || behaviorDuration > 60000 {
			return invalid()
		}
		if n.attr("dur") == "" {
			step.DurationMS = max(step.DurationMS, behaviorDuration)
		} else if behaviorDuration > duration {
			return invalid()
		}
		tgt := common.child("tgtEl").child("spTgt")
		if tgt == nil || len(tgt.children) != 0 {
			return invalid()
		}
		id := tgt.attr("spid")
		spid, idErr := strconv.ParseUint(id, 10, 32)
		if idErr != nil || spid == 0 || target != "" && target != id {
			return invalid()
		}
		target = id
		switch behavior.name.Local {
		case "set":
			if visible || common.child("cTn").attr("fill") != "hold" || common.child("attrNameLst").child("attrName") == nil || common.child("attrNameLst").child("attrName").text != "style.visibility" || behavior.child("to").child("strVal").attr("val") != "visible" {
				return invalid()
			}
			visible = true
		case "animEffect":
			if fade || behavior.attr("transition") != "in" {
				return invalid()
			}
			fade = true
			filter := behavior.attr("filter")
			if filter == "fade" {
				step.Effect = "fade"
			} else if strings.HasPrefix(filter, "wipe(") && strings.HasSuffix(filter, ")") {
				direction := strings.TrimSuffix(strings.TrimPrefix(filter, "wipe("), ")")
				if direction != "left" && direction != "right" && direction != "up" && direction != "down" {
					return invalid()
				}
				step.Effect, step.Direction = "wipe", direction
			} else {
				return invalid()
			}
		case "animScale":
			if scaled || behavior.attr("zoomContents") != "1" && behavior.attr("zoomContents") != "true" || behavior.child("from").attr("x") != "86000" || behavior.child("from").attr("y") != "86000" || behavior.child("to").attr("x") != "100000" || behavior.child("to").attr("y") != "100000" {
				return invalid()
			}
			scaled = true
		default:
			return invalid()
		}
	}
	if !visible || target == "" {
		return invalid()
	}
	if step.DurationMS < 1 {
		return invalid()
	}
	if scaled {
		if step.Effect != "fade" {
			return invalid()
		}
		step.Effect = "zoom"
	}
	return step, target, nil
}
