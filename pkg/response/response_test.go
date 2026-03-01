package response_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bse/notifyd/pkg/response"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSON(t *testing.T) {
	t.Run("sets Content-Type to application/json", func(t *testing.T) {
		rec := httptest.NewRecorder()
		response.JSON(rec, http.StatusOK, map[string]string{"key": "value"})
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	})

	t.Run("writes the given status code", func(t *testing.T) {
		rec := httptest.NewRecorder()
		response.JSON(rec, http.StatusCreated, map[string]string{})
		assert.Equal(t, http.StatusCreated, rec.Code)
	})

	t.Run("body marshals to JSON correctly", func(t *testing.T) {
		type payload struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		}

		rec := httptest.NewRecorder()
		response.JSON(rec, http.StatusOK, payload{Name: "test", Count: 42})

		var decoded payload
		err := json.NewDecoder(rec.Body).Decode(&decoded)
		require.NoError(t, err)
		assert.Equal(t, "test", decoded.Name)
		assert.Equal(t, 42, decoded.Count)
	})
}

func TestError(t *testing.T) {
	t.Run("returns ErrorResponse JSON with correct message", func(t *testing.T) {
		rec := httptest.NewRecorder()
		response.Error(rec, http.StatusBadRequest, "invalid input")

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		var errResp struct {
			Error string `json:"error"`
		}
		err := json.NewDecoder(rec.Body).Decode(&errResp)
		require.NoError(t, err)
		assert.Equal(t, "invalid input", errResp.Error)
	})

	t.Run("returns 404 with not found message", func(t *testing.T) {
		rec := httptest.NewRecorder()
		response.Error(rec, http.StatusNotFound, "resource not found")

		assert.Equal(t, http.StatusNotFound, rec.Code)

		var errResp struct {
			Error string `json:"error"`
		}
		err := json.NewDecoder(rec.Body).Decode(&errResp)
		require.NoError(t, err)
		assert.Equal(t, "resource not found", errResp.Error)
	})
}

func TestListResponse(t *testing.T) {
	t.Run("marshals data, total, limit, and offset fields", func(t *testing.T) {
		items := []string{"alpha", "beta", "gamma"}
		rec := httptest.NewRecorder()

		response.JSON(rec, http.StatusOK, response.ListResponse{
			Data:   items,
			Total:  100,
			Limit:  20,
			Offset: 40,
		})

		require.Equal(t, http.StatusOK, rec.Code)

		var decoded struct {
			Data   []string `json:"data"`
			Total  int      `json:"total"`
			Limit  int      `json:"limit"`
			Offset int      `json:"offset"`
		}
		err := json.NewDecoder(rec.Body).Decode(&decoded)
		require.NoError(t, err)

		assert.Equal(t, items, decoded.Data)
		assert.Equal(t, 100, decoded.Total)
		assert.Equal(t, 20, decoded.Limit)
		assert.Equal(t, 40, decoded.Offset)
	})

	t.Run("body is valid JSON with expected top-level keys", func(t *testing.T) {
		rec := httptest.NewRecorder()
		response.JSON(rec, http.StatusOK, response.ListResponse{
			Data:   []int{1, 2, 3},
			Total:  3,
			Limit:  10,
			Offset: 0,
		})

		body := strings.TrimSpace(rec.Body.String())
		assert.True(t, json.Valid([]byte(body)), "response body should be valid JSON")

		var raw map[string]json.RawMessage
		err := json.Unmarshal([]byte(body), &raw)
		require.NoError(t, err)

		assert.Contains(t, raw, "data")
		assert.Contains(t, raw, "total")
		assert.Contains(t, raw, "limit")
		assert.Contains(t, raw, "offset")
	})
}
