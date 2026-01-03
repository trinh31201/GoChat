package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/yourusername/chat-app/internal/client"
	"github.com/yourusername/chat-app/internal/storage"
)

// UploadResponse is the response for file upload
type UploadResponse struct {
	FileURL     string `json:"file_url"`
	FileName    string `json:"file_name"`
	FileSize    int64  `json:"file_size"`
	MimeType    string `json:"mime_type"`
	MessageType string `json:"message_type"` // "image" or "file"
}

// ErrorResponse is the error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// HandleUpload creates a file upload handler
func HandleUpload(store *storage.MinioStorage, userClient *client.UserClient, logger log.Logger) http.HandlerFunc {
	log := log.NewHelper(log.With(logger, "module", "server/upload"))

	return func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if r.Method != "POST" {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Authenticate via User Service (like WebSocket)
		userID, err := authenticateUpload(r, userClient)
		if err != nil {
			log.Warnf("Upload authentication failed: %v", err)
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		// Parse multipart form (max 10MB)
		if err := r.ParseMultipartForm(storage.MaxFileSize); err != nil {
			log.Warnf("Failed to parse multipart form: %v", err)
			writeError(w, http.StatusBadRequest, "file too large or invalid form")
			return
		}

		// Get file from form
		file, header, err := r.FormFile("file")
		if err != nil {
			log.Warnf("Failed to get file from form: %v", err)
			writeError(w, http.StatusBadRequest, "no file provided")
			return
		}
		defer file.Close()

		// Validate file size
		if header.Size > storage.MaxFileSize {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("file too large (max %d MB)", storage.MaxFileSize/(1024*1024)))
			return
		}

		// Get mime type from content type header
		mimeType := header.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}

		// Validate file type
		if !storage.AllowedFileTypes[mimeType] {
			writeError(w, http.StatusBadRequest, "file type not allowed")
			return
		}

		// Upload to MinIO
		fileInfo, err := store.UploadFile(r.Context(), file, header.Filename, header.Size, mimeType)
		if err != nil {
			log.Errorf("Failed to upload file: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to upload file")
			return
		}

		log.Infof("File uploaded by user %d: %s (%d bytes)", userID, header.Filename, header.Size)

		// Return response
		resp := UploadResponse{
			FileURL:     fileInfo.URL,
			FileName:    fileInfo.FileName,
			FileSize:    fileInfo.FileSize,
			MimeType:    fileInfo.MimeType,
			MessageType: storage.GetMessageType(fileInfo.MimeType),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}
}

// authenticateUpload extracts token and validates via User Service
func authenticateUpload(r *http.Request, userClient *client.UserClient) (int64, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return 0, fmt.Errorf("missing authorization header")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return 0, fmt.Errorf("invalid authorization header format")
	}

	token := parts[1]

	// Validate via User Service (like WebSocket does)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	userID, _, err := userClient.ValidateToken(ctx, token)
	if err != nil {
		return 0, fmt.Errorf("invalid token: %w", err)
	}

	return userID, nil
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{Error: message})
}
