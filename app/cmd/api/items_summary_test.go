package main

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"gkfeed/api/internal/config"
)

func TestItemListViews(t *testing.T) {
	resourceTestDatabase(t)
	handler := newHandler(config.Config{})
	for _, path := range []string{"/api/v1/get_items", "/api/v2/items"} {
		t.Run(path, func(t *testing.T) {
			readPage := func(query, user string) (map[string]any, []map[string]any) {
				t.Helper()
				response := resourceRequest(handler, http.MethodGet, path+query, user)
				var page map[string]any
				if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &page) != nil {
					t.Fatalf("page = %d %s", response.Code, response.Body.String())
				}
				var body struct {
					Items []map[string]any `json:"items"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Items == nil {
					t.Fatalf("items = %#v, error = %v", body.Items, err)
				}
				return page, body.Items
			}

			full, fullItems := readPage("?limit=1", "owner")
			explicit, _ := readPage("?limit=1&view=full", "owner")
			if !reflect.DeepEqual(full, explicit) || len(fullItems) != 1 || fullItems[0]["text"] != "Text" {
				t.Fatalf("full view changed: %#v / %#v", full, explicit)
			}
			summary, summaries := readPage("?limit=1&view=summary", "owner")
			if len(summaries) != 1 || summaries[0]["id"] != float64(23) || summary["next_cursor"] != float64(23) {
				t.Fatalf("summary = %#v", summary)
			}
			delete(fullItems[0], "text")
			if !reflect.DeepEqual(fullItems[0], summaries[0]) || len(summaries[0]) != 5 {
				t.Fatalf("summary fields = %#v, want %#v", summaries[0], fullItems[0])
			}
			last, lastItems := readPage("?limit=1&view=summary&cursor=23", "owner")
			if len(lastItems) != 1 || lastItems[0]["id"] != float64(22) || last["next_cursor"] != nil {
				t.Fatalf("last page = %#v", last)
			}
			for _, query := range []string{"?view=summary&cursor=22", "?view=summary&cursor=0"} {
				user := "owner"
				if query == "?view=summary&cursor=0" {
					user = "other"
				}
				empty, items := readPage(query, user)
				if len(items) != 0 || empty["next_cursor"] != nil {
					t.Fatalf("empty/foreign page = %#v", empty)
				}
			}
			for _, query := range []string{"?view=invalid", "?view=", "?view=summary&view=full"} {
				if response := resourceRequest(handler, http.MethodGet, path+query, "owner"); response.Code != http.StatusBadRequest {
					t.Fatalf("invalid view %q = %d %s", query, response.Code, response.Body.String())
				}
			}
			if response := resourceRequest(handler, http.MethodGet, path+"?view=summary", ""); response.Code != http.StatusUnauthorized {
				t.Fatalf("unauthenticated summary = %d", response.Code)
			}
		})
	}
	// Summary mode must not remove text from the existing detail representation.
	response := resourceRequest(handler, http.MethodGet, "/api/v2/items/23", "owner")
	var detail struct {
		Item struct {
			Text string `json:"text"`
		} `json:"item"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &detail) != nil || detail.Item.Text != "Text" {
		t.Fatalf("detail = %d %s", response.Code, response.Body.String())
	}
}

func TestSwaggerItemListViews(t *testing.T) {
	response := resourceRequest(newHandler(config.Config{}), http.MethodGet, "/api/swagger/doc.json", "")
	var document struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name    string   `json:"name"`
				Default string   `json:"default"`
				Enum    []string `json:"enum"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &document); response.Code != 200 || err != nil {
		t.Fatalf("Swagger = %d %s, %v", response.Code, response.Body.String(), err)
	}
	for _, path := range []string{"/api/v1/get_items", "/api/v2/items"} {
		found := false
		for _, parameter := range document.Paths[path]["get"].Parameters {
			if parameter.Name == "view" {
				found = true
				if parameter.Default != "full" || !reflect.DeepEqual(parameter.Enum, []string{"full", "summary"}) {
					t.Errorf("%s view parameter = %#v", path, parameter)
				}
			}
		}
		if !found {
			t.Errorf("%s has no documented view parameter", path)
		}
	}
}
