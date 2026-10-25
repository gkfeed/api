package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"gkfeed/api/internal/config"
)

func TestSwaggerResourceErrorScope(t *testing.T) {
	response := resourceRequest(newHandler(config.Config{}), http.MethodGet, "/api/swagger/doc.json", "")
	var document struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name    string  `json:"name"`
				Minimum float64 `json:"minimum"`
			} `json:"parameters"`
			Responses map[string]struct {
				Schema struct {
					Ref   string `json:"$ref"`
					Type  string `json:"type"`
					Items struct {
						Ref string `json:"$ref"`
					} `json:"items"`
				} `json:"schema"`
			} `json:"responses"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &document); response.Code != 200 || err != nil {
		t.Fatalf("Swagger response = %d %s, %v", response.Code, response.Body.String(), err)
	}
	for _, operation := range resourceOperations {
		path := operation.path
		if path != "/api/v2/feeds" {
			path = path[:strings.LastIndex(path, "/")] + "/{id}"
		}
		doc := document.Paths[path][strings.ToLower(operation.method)]
		statuses := []string{"401", "405", "500"}
		if path != "/api/v2/feeds" {
			statuses = append(statuses, "400", "404")
			if len(doc.Parameters) != 1 || doc.Parameters[0].Name != "id" || doc.Parameters[0].Minimum != 1 {
				t.Fatalf("%s %s ID parameter = %#v", operation.method, path, doc.Parameters)
			}
		}
		for _, status := range statuses {
			if doc.Responses[status].Schema.Ref != "#/definitions/handlers.ResourceError" {
				t.Errorf("%s %s %s error schema = %#v", operation.method, path, status, doc.Responses[status].Schema)
			}
		}
	}
	list := document.Paths["/api/v2/feeds"]["get"].Responses["200"].Schema
	if list.Type != "array" || list.Items.Ref != "#/definitions/handlers.feedDTO" {
		t.Fatalf("v2 feed list schema = %#v", list)
	}
	for _, operation := range []struct{ path, method string }{
		{"/api/v1/list", "get"},
		{"/api/v1/items/{id}", "delete"},
		{"/api/v2/feeds", "post"},
		{"/api/v2/items", "get"},
		{"/api/v2/items/sync", "get"},
		{"/api/v2/items/changes", "get"},
	} {
		doc, ok := document.Paths[operation.path][operation.method]
		if !ok {
			t.Fatalf("missing documented operation: %s %s", operation.method, operation.path)
		}
		for _, status := range []string{"400", "401", "404", "405", "500"} {
			if doc.Responses[status].Schema.Ref == "#/definitions/handlers.ResourceError" {
				t.Errorf("%s %s unexpectedly documents v2 resource errors", operation.method, operation.path)
			}
		}
	}
}
