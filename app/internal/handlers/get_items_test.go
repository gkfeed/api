package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestItemsPageParameters(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantLimit  int
		wantCursor *int
		wantOK     bool
	}{
		{name: "defaults", wantLimit: defaultItemsLimit, wantOK: true},
		{name: "values", query: "?limit=25&cursor=10", wantLimit: 25, wantCursor: intPointer(10), wantOK: true},
		{name: "zero cursor", query: "?cursor=0", wantLimit: defaultItemsLimit, wantCursor: intPointer(0), wantOK: true},
		{name: "zero limit", query: "?limit=0", wantOK: false},
		{name: "negative cursor", query: "?cursor=-1", wantOK: false},
		{name: "invalid limit", query: "?limit=many", wantOK: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/get_items"+test.query, nil)
			response := httptest.NewRecorder()

			limit, cursor, ok := itemsPageParameters(response, request)

			if limit != test.wantLimit || ok != test.wantOK || !equalIntPointers(cursor, test.wantCursor) {
				t.Fatalf("itemsPageParameters() = (%d, %v, %t), want (%d, %v, %t)", limit, cursor, ok, test.wantLimit, test.wantCursor, test.wantOK)
			}
		})
	}
}

func intPointer(value int) *int {
	return &value
}

func equalIntPointers(left, right *int) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
