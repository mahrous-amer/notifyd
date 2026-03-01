package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// parsePagination is unexported, so tests live in the same package (white-box).

func TestParsePagination(t *testing.T) {
	t.Run("no query params returns defaults", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		limit, offset := parsePagination(req)
		assert.Equal(t, 20, limit)
		assert.Equal(t, 0, offset)
	})

	t.Run("custom valid limit and offset", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?limit=50&offset=10", nil)
		limit, offset := parsePagination(req)
		assert.Equal(t, 50, limit)
		assert.Equal(t, 10, offset)
	})

	t.Run("limit of 1 is accepted", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?limit=1", nil)
		limit, _ := parsePagination(req)
		assert.Equal(t, 1, limit)
	})

	t.Run("limit of 100 is accepted", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?limit=100", nil)
		limit, _ := parsePagination(req)
		assert.Equal(t, 100, limit)
	})

	t.Run("negative limit falls back to default 20", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?limit=-5", nil)
		limit, _ := parsePagination(req)
		assert.Equal(t, 20, limit)
	})

	t.Run("zero limit falls back to default 20", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?limit=0", nil)
		limit, _ := parsePagination(req)
		assert.Equal(t, 20, limit)
	})

	t.Run("limit above 100 falls back to default 20", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?limit=101", nil)
		limit, _ := parsePagination(req)
		assert.Equal(t, 20, limit)
	})

	t.Run("non-numeric limit falls back to default 20", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?limit=abc", nil)
		limit, _ := parsePagination(req)
		assert.Equal(t, 20, limit)
	})

	t.Run("negative offset falls back to 0", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?offset=-1", nil)
		_, offset := parsePagination(req)
		assert.Equal(t, 0, offset)
	})

	t.Run("zero offset is accepted", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?offset=0", nil)
		_, offset := parsePagination(req)
		assert.Equal(t, 0, offset)
	})

	t.Run("non-numeric offset falls back to 0", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?offset=xyz", nil)
		_, offset := parsePagination(req)
		assert.Equal(t, 0, offset)
	})
}
