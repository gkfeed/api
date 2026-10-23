package handlers

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"gkfeed/api/internal/library"
	"gkfeed/api/internal/services"
)

type createFeedsDTO struct {
	URL   string             `json:"url" binding:"required"`
	Title string             `json:"title,omitempty"`
	Type  library.FeedType   `json:"type,omitempty"`
	Types []library.FeedType `json:"types,omitempty"`
}

type feedCreationResponse struct {
	Results []feedMutationResponse `json:"results"`
}

type feedCreationError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// @Summary Create subscriptions from a source URL
// @Description Resolves missing metadata and creates all selected subscriptions atomically. Explicit type or types overrides automatic selection. Repeats return existing feeds without changing them.
// @Tags feeds
// @Accept json
// @Produce json
// @Param feed body createFeedsDTO true "One source URL, optional title and either type or types"
// @Security BasicAuth
// @Security BearerAuth
// @Security BrowserSession
// @Success 200 {object} feedCreationResponse
// @Failure 400
// @Failure 401
// @Failure 422 {object} feedCreationError
// @Failure 500
// @Failure 502 {object} feedCreationError
// @Failure 503 {object} feedCreationError
// @Failure 504 {object} feedCreationError
// @Router /api/v2/feeds [post]
func (h *LibraryHandler) HandleCreateFeed(w http.ResponseWriter, r *http.Request) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}
	var input createFeedsDTO
	if !decodeJSON(w, r, &input) {
		return
	}
	if h.creator == nil {
		writeFeedCreationError(w, http.StatusServiceUnavailable, "creation_unavailable", "Feed creation is unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	feeds, err := h.creator.Prepare(ctx, library.CreateFeedsInput{
		URL: input.URL, Title: input.Title, Type: input.Type, Types: input.Types,
	})
	if err != nil {
		var timeout net.Error
		switch {
		case errors.Is(err, services.ErrInvalidCreation):
			writeFeedCreationError(w, 400, "invalid_creation", err.Error())
		case errors.Is(err, services.ErrTypeRequired):
			writeFeedCreationError(w, 422, "type_required", "Specify type or types explicitly")
		case errors.Is(err, services.ErrDetectionUnavailable):
			writeFeedCreationError(w, 503, "detection_unavailable", "Feed type detection is not configured; specify type or types")
		case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()):
			writeFeedCreationError(w, 504, "resolution_timeout", "Feed resolution timed out")
		case errors.Is(err, services.ErrChannelUnresolved):
			writeFeedCreationError(w, 502, "youtube_channel_unresolved", "YouTube channel could not be resolved")
		case errors.Is(err, services.ErrDetectionFailed):
			writeFeedCreationError(w, 502, "detection_failed", "Feed type detection failed")
		default:
			writeInternalServerError(w, err)
		}
		return
	}
	results, err := h.feeds.AddBatch(ctx, user.ID, feeds)
	if err != nil {
		writeInternalServerError(w, err)
		return
	}
	response := feedCreationResponse{Results: make([]feedMutationResponse, len(results))}
	for i, result := range results {
		response.Results[i] = feedMutationResponse{Created: result.Created, Item: toFeedDTO(result.Feed)}
	}
	writeJSON(w, response)
}

func writeFeedCreationError(w http.ResponseWriter, status int, code, message string) {
	var response feedCreationError
	response.Error.Code, response.Error.Message = code, message
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	writeJSON(w, response)
}
