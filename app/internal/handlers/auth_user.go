package handlers

import "net/http"

// @Summary      Current user
// @Description  Returns the authenticated user's ID and name.
// @Tags         auth
// @Produce      json
// @Security     BasicAuth
// @Security     BearerAuth
// @Security     BrowserSession
// @Success      200  {object}  object{id=int,name=string}
// @Failure      401
// @Router       /api/v1/auth/me [get]
func HandleMe(w http.ResponseWriter, r *http.Request) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}

	writeJSON(w, struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}{
		ID:   user.ID,
		Name: user.Name,
	})
}
