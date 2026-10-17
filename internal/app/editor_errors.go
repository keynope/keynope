package app

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

// Error metadata is transport/runtime state, never authored deck metadata.
type editorCommandError struct {
	code, field, objectID string
	cause                 error
}

func (failure *editorCommandError) Error() string { return failure.cause.Error() }
func (failure *editorCommandError) Unwrap() error { return failure.cause }

func editorFailure(code, field, objectID string, cause error) error {
	if cause == nil {
		return nil
	}
	var existing *editorCommandError
	if errors.As(cause, &existing) {
		return cause
	}
	return &editorCommandError{code: code, field: field, objectID: objectID, cause: cause}
}

type editorProblem struct {
	Version  int    `json:"version"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Status   int    `json:"status"`
	Field    string `json:"field,omitempty"`
	ObjectID string `json:"objectId,omitempty"`
}

func editorProblemFor(action nativeEditorAction, err error, status int) editorProblem {
	problem := editorProblem{Version: 1, Code: "command-failed", Message: err.Error(), Status: status}
	var failure *editorCommandError
	if errors.As(err, &failure) {
		problem.Code, problem.Field, problem.ObjectID = failure.code, failure.field, failure.objectID
	} else if errors.Is(err, errClipboardPasteStale) {
		problem.Code = "stale-target"
	} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		problem.Code = "cancelled"
	} else if errors.Is(err, errInvalidEditorAction) {
		problem.Code = "invalid-command"
	} else {
		switch action.Action {
		case "insert-table", "set-table":
			problem.Code, problem.Field = "invalid-table", "tableData"
		case "insert-chart", "set-chart-data":
			problem.Code, problem.Field = "invalid-chart-data", "chartData"
		case "set-chart-options":
			problem.Code, problem.Field = "invalid-chart-options", "chartOptions"
		case "set-chart-series-style":
			problem.Code, problem.Field = "invalid-series", "chartSeriesIndex"
		case "set-selection-property":
			problem.Code, problem.Field = "invalid-value", action.Property
		case "set-selection-geometry":
			problem.Code, problem.Field = "invalid-geometry", "geometry"
		case "paste-objects":
			problem.Code = "invalid-clipboard"
		}
	}
	if problem.ObjectID == "" {
		problem.ObjectID = action.ObjectID
		if problem.ObjectID == "" && len(action.ObjectIDs) == 1 {
			problem.ObjectID = action.ObjectIDs[0]
		}
	}
	if !validObjectID(problem.ObjectID) {
		problem.ObjectID = ""
	}
	if len(problem.Field) > 128 || strings.IndexFunc(problem.Field, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_')
	}) >= 0 {
		problem.Field = ""
	}
	return problem
}

func acceptsEditorProblem(accept string) bool {
	if len(accept) > 512 {
		return false
	}
	for _, part := range strings.Split(accept, ",") {
		kind, params, err := mime.ParseMediaType(strings.TrimSpace(part))
		if err != nil || kind != "application/problem+json" {
			continue
		}
		if quality, exists := params["q"]; exists {
			value, err := strconv.ParseFloat(quality, 64)
			if err != nil || !(value > 0 && value <= 1) {
				continue
			}
		}
		return true
	}
	return false
}

func writeEditorFailure(w http.ResponseWriter, r *http.Request, action nativeEditorAction, err error, status int) {
	problem := editorProblemFor(action, err, status)
	w.Header().Set("X-Keynope-Error-Code", problem.Code)
	w.Header().Set("Cache-Control", "no-store")
	if !acceptsEditorProblem(r.Header.Get("Accept")) {
		http.Error(w, problem.Message, status)
		return
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem)
}
