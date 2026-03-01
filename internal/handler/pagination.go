package handler

import (
	"net/http"
	"strconv"
)

// parsePagination extracts limit and offset query parameters from the request.
// Limit is clamped to [1, 100] with a default of 20. Offset is non-negative
// with a default of 0.
func parsePagination(r *http.Request) (limit int, offset int) {
	limit = 20
	offset = 0

	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}
	return limit, offset
}
