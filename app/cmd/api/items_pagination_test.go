package main

import (
	"encoding/json"
	"math"
	"net/http"
	"slices"
	"strconv"
	"testing"

	"gkfeed/api/internal/config"
)

func TestItemsPaginationHTTPContract(t *testing.T) {
	resourceTestDatabase(t)
	handler := newHandler(config.Config{})
	for _, path := range []string{"/api/v1/get_items", "/api/v2/items"} {
		for _, test := range []struct {
			query      string
			wantIDs    []int
			wantCursor int
		}{
			{query: "", wantIDs: []int{23, 22}},
			{query: "?limit=", wantIDs: []int{23, 22}},
			{query: "?limit=500", wantIDs: []int{23, 22}},
			{query: "?limit=1", wantIDs: []int{23}, wantCursor: 23},
			{query: "?limit=1&cursor=23", wantIDs: []int{22}},
			{query: "?cursor=0", wantIDs: []int{}},
		} {
			t.Run(path+test.query, func(t *testing.T) {
				response := resourceRequest(handler, http.MethodGet, path+test.query, "owner")
				var page struct {
					Items []struct {
						ID int `json:"id"`
					} `json:"items"`
					NextCursor *int `json:"next_cursor"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &page); response.Code != 200 || err != nil || response.Header().Get("Content-Type") != "application/json" || page.Items == nil {
					t.Fatalf("item page = %d %s, %v", response.Code, response.Body.String(), err)
				}
				ids := make([]int, len(page.Items))
				for i, item := range page.Items {
					ids[i] = item.ID
				}
				if !slices.Equal(ids, test.wantIDs) || (page.NextCursor == nil) != (test.wantCursor == 0) || (page.NextCursor != nil && *page.NextCursor != test.wantCursor) {
					t.Fatalf("item page = %s, want IDs %v and cursor %d", response.Body.String(), test.wantIDs, test.wantCursor)
				}
			})
		}
		for _, query := range []string{"?limit=0", "?limit=-1", "?limit=501", "?limit=" + strconv.Itoa(math.MaxInt), "?limit=" + strconv.Itoa(math.MaxInt) + "0", "?limit=many", "?cursor=-1"} {
			t.Run(path+query, func(t *testing.T) {
				response := resourceRequest(handler, http.MethodGet, path+query, "owner")
				wantBody := "Invalid limit\n"
				if query == "?cursor=-1" {
					wantBody = "Invalid cursor\n"
				}
				if response.Code != 400 || response.Body.String() != wantBody || response.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
					t.Fatalf("invalid pagination = %d %v %q", response.Code, response.Header(), response.Body.String())
				}
			})
		}
		response := resourceRequest(handler, http.MethodGet, path+"?limit=501", "")
		if response.Code != 401 || response.Body.String() != "Unauthorized\n" || response.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
			t.Fatalf("unauthenticated item list = %d %v %q", response.Code, response.Header(), response.Body.String())
		}
	}
}
