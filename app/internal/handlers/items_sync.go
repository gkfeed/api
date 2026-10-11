package handlers

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"gkfeed/api/internal/library"
)

const maxSyncLimit = 500

type syncToken struct {
	Version  int    `json:"v"`
	Kind     string `json:"k"`
	UserID   int    `json:"u"`
	AfterID  int    `json:"a,omitempty"`
	MaxID    int    `json:"m,omitempty"`
	Sequence int64  `json:"s"`
}

type ItemsSyncService interface {
	SyncItemsPage(context.Context, int, int, int, int) (library.SyncPage, error)
	ItemChanges(context.Context, int, int64, int) (library.ChangesPage, error)
}

type ItemsSyncHandler struct {
	service ItemsSyncService
	aead    cipher.AEAD
}

func NewItemsSyncHandler(service ItemsSyncService, secret string) *ItemsSyncHandler {
	key := sha256.Sum256([]byte("gkfeed-items-sync-v1:" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		panic(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return &ItemsSyncHandler{service: service, aead: aead}
}

func (h *LibraryHandler) SyncHandler(secret string) *ItemsSyncHandler {
	return NewItemsSyncHandler(h.service, secret)
}

func (h *ItemsSyncHandler) encode(token syncToken) (string, error) {
	token.Version = 1
	data, err := json.Marshal(token)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, h.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := h.aead.Seal(nonce, nonce, data, nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (h *ItemsSyncHandler) decode(value, kind string, userID int) (syncToken, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) < h.aead.NonceSize() {
		return syncToken{}, errors.New("invalid cursor")
	}
	data, err := h.aead.Open(nil, raw[:h.aead.NonceSize()], raw[h.aead.NonceSize():], nil)
	if err != nil {
		return syncToken{}, errors.New("invalid cursor")
	}
	var token syncToken
	if err := json.Unmarshal(data, &token); err != nil || token.Version != 1 || token.Kind != kind || token.UserID != userID || token.AfterID < 0 || token.MaxID < 0 || token.Sequence < 0 {
		return syncToken{}, errors.New("invalid cursor")
	}
	return token, nil
}

func syncLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	limit, ok := positiveQueryInt(w, r, "limit", defaultItemsLimit, false)
	if !ok {
		return 0, false
	}
	if limit > maxSyncLimit {
		http.Error(w, "Invalid limit", http.StatusBadRequest)
		return 0, false
	}
	return limit, true
}

type syncItemsResponse struct {
	Items      []itemDTO `json:"items"`
	NextCursor string    `json:"next_cursor"`
	HasMore    bool      `json:"has_more"`
	SyncCursor string    `json:"sync_cursor"`
}

// HandleItems lists items by descending ID inside an initial ID ceiling.
// @Summary List items for synchronization
// @Description Returns a bounded item page and a sync cursor for later changes.
// @Tags items
// @Produce json
// @Param limit query int false "Items per page (default 100, maximum 500)"
// @Param cursor query string false "Opaque page cursor"
// @Security BasicAuth
// @Security BearerAuth
// @Success 200 {object} syncItemsResponse
// @Failure 400
// @Failure 401
// @Failure 500
// @Router /api/v2/items/sync [get]
func (h *ItemsSyncHandler) HandleItems(w http.ResponseWriter, r *http.Request) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}
	limit, ok := syncLimit(w, r)
	if !ok {
		return
	}
	token := syncToken{Kind: "page", UserID: user.ID}
	if values, exists := r.URL.Query()["cursor"]; exists {
		if len(values) != 1 || values[0] == "" {
			http.Error(w, "Invalid cursor", http.StatusBadRequest)
			return
		}
		var err error
		token, err = h.decode(values[0], "page", user.ID)
		if err != nil || token.AfterID == 0 || token.MaxID == 0 || token.AfterID > token.MaxID {
			http.Error(w, "Invalid cursor", http.StatusBadRequest)
			return
		}
	}
	page, err := h.service.SyncItemsPage(r.Context(), user.ID, token.AfterID, token.MaxID, limit)
	if err != nil {
		writeInternalServerError(w, err)
		return
	}
	if token.MaxID == 0 {
		token.MaxID, token.Sequence = page.MaxID, page.Sequence
	}
	syncCursor, err := h.encode(syncToken{Kind: "sync", UserID: user.ID, Sequence: token.Sequence})
	if err != nil {
		writeInternalServerError(w, err)
		return
	}
	response := syncItemsResponse{Items: itemDTOs(page.Items), HasMore: page.HasMore, SyncCursor: syncCursor}
	if page.HasMore {
		token.AfterID = page.LastID
		response.NextCursor, err = h.encode(token)
		if err != nil {
			writeInternalServerError(w, err)
			return
		}
	}
	writeJSON(w, response)
}

type itemChangesResponse struct {
	Upserted   []itemDTO `json:"upserted"`
	DeletedIDs []int     `json:"deleted_ids"`
	NextCursor string    `json:"next_cursor"`
	HasMore    bool      `json:"has_more"`
}

// HandleChanges returns a bounded sequence of committed item mutations.
// @Summary Get item changes
// @Description Returns item mutations after a sync cursor. Apply each response before using its next cursor.
// @Tags items
// @Produce json
// @Param limit query int false "Changes per page (default 100, maximum 500)"
// @Param cursor query string true "Opaque sync cursor"
// @Security BasicAuth
// @Security BearerAuth
// @Success 200 {object} itemChangesResponse
// @Failure 400
// @Failure 401
// @Failure 500
// @Router /api/v2/items/changes [get]
func (h *ItemsSyncHandler) HandleChanges(w http.ResponseWriter, r *http.Request) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}
	limit, ok := syncLimit(w, r)
	if !ok {
		return
	}
	values := r.URL.Query()["cursor"]
	if len(values) != 1 || values[0] == "" {
		http.Error(w, "Invalid cursor", http.StatusBadRequest)
		return
	}
	token, err := h.decode(values[0], "sync", user.ID)
	if err != nil || token.AfterID != 0 || token.MaxID != 0 {
		http.Error(w, "Invalid cursor", http.StatusBadRequest)
		return
	}
	page, err := h.service.ItemChanges(r.Context(), user.ID, token.Sequence, limit)
	if err != nil {
		writeInternalServerError(w, err)
		return
	}
	response := itemChangesResponse{Upserted: []itemDTO{}, DeletedIDs: []int{}, HasMore: page.HasMore}
	latest := make(map[int]*library.Item, len(page.Changes))
	order := make([]int, 0, len(page.Changes))
	for _, change := range page.Changes {
		if _, seen := latest[change.ItemID]; !seen {
			order = append(order, change.ItemID)
		}
		latest[change.ItemID] = change.Item
	}
	for _, id := range order {
		if latest[id] == nil {
			response.DeletedIDs = append(response.DeletedIDs, id)
		} else {
			response.Upserted = append(response.Upserted, toItemDTO(*latest[id]))
		}
	}
	token.Sequence = page.Sequence
	response.NextCursor, err = h.encode(token)
	if err != nil {
		writeInternalServerError(w, err)
		return
	}
	writeJSON(w, response)
}
