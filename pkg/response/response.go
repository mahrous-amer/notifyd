package response

import (
	"encoding/json"
	"net/http"
)

type ErrorResponse struct {
	Error string `json:"error"`
}

type ListResponse struct {
	Data       interface{} `json:"data"`
	Total      int         `json:"total"`
	Limit      int         `json:"limit"`
	Offset     int         `json:"offset"`
}

func JSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, ErrorResponse{Error: msg})
}
