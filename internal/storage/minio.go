package storage

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// FileInfo contains metadata about an uploaded file
type FileInfo struct {
	URL      string
	FileName string
	FileSize int64
	MimeType string
}

// MinioStorage handles file uploads to MinIO
type MinioStorage struct {
	client     *minio.Client
	bucketName string
	publicURL  string
	log        *log.Helper
}

// MinioConfig contains MinIO connection settings
type MinioConfig struct {
	Endpoint   string
	AccessKey  string
	SecretKey  string
	BucketName string
	UseSSL     bool
	PublicURL  string // External URL for accessing files
}

// NewMinioStorage creates a new MinIO storage client
func NewMinioStorage(cfg *MinioConfig, logger log.Logger) (*MinioStorage, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO client: %w", err)
	}

	// Ensure bucket exists
	ctx := context.Background()
	exists, err := client.BucketExists(ctx, cfg.BucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to check bucket: %w", err)
	}

	if !exists {
		err = client.MakeBucket(ctx, cfg.BucketName, minio.MakeBucketOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to create bucket: %w", err)
		}

		// Set bucket policy to public read
		policy := fmt.Sprintf(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Effect": "Allow",
				"Principal": {"AWS": ["*"]},
				"Action": ["s3:GetObject"],
				"Resource": ["arn:aws:s3:::%s/*"]
			}]
		}`, cfg.BucketName)
		err = client.SetBucketPolicy(ctx, cfg.BucketName, policy)
		if err != nil {
			return nil, fmt.Errorf("failed to set bucket policy: %w", err)
		}
	}

	return &MinioStorage{
		client:     client,
		bucketName: cfg.BucketName,
		publicURL:  cfg.PublicURL,
		log:        log.NewHelper(log.With(logger, "module", "storage/minio")),
	}, nil
}

// UploadFile uploads a file to MinIO and returns file info
func (s *MinioStorage) UploadFile(ctx context.Context, reader io.Reader, fileName string, fileSize int64, mimeType string) (*FileInfo, error) {
	// Generate unique file path
	ext := filepath.Ext(fileName)
	objectName := fmt.Sprintf("%s/%s%s",
		time.Now().Format("2006/01/02"),
		uuid.New().String(),
		ext,
	)

	// Determine content type
	contentType := mimeType
	if contentType == "" {
		contentType = getMimeType(ext)
	}

	// Upload file
	_, err := s.client.PutObject(ctx, s.bucketName, objectName, reader, fileSize, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		s.log.Errorf("Failed to upload file: %v", err)
		return nil, fmt.Errorf("failed to upload file: %w", err)
	}

	// Build public URL
	fileURL := fmt.Sprintf("%s/%s/%s", s.publicURL, s.bucketName, objectName)

	s.log.Infof("File uploaded successfully: %s (size: %d, type: %s)", objectName, fileSize, contentType)

	return &FileInfo{
		URL:      fileURL,
		FileName: fileName,
		FileSize: fileSize,
		MimeType: contentType,
	}, nil
}

// DeleteFile removes a file from MinIO
func (s *MinioStorage) DeleteFile(ctx context.Context, fileURL string) error {
	// Extract object name from URL
	objectName := extractObjectName(fileURL, s.publicURL, s.bucketName)
	if objectName == "" {
		return fmt.Errorf("invalid file URL")
	}

	err := s.client.RemoveObject(ctx, s.bucketName, objectName, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	s.log.Infof("File deleted: %s", objectName)
	return nil
}

// IsImage checks if the mime type is an image
func IsImage(mimeType string) bool {
	return strings.HasPrefix(mimeType, "image/")
}

// GetMessageType returns "image" or "file" based on mime type
func GetMessageType(mimeType string) string {
	if IsImage(mimeType) {
		return "image"
	}
	return "file"
}

// getMimeType returns mime type based on file extension
func getMimeType(ext string) string {
	mimeTypes := map[string]string{
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".png":  "image/png",
		".gif":  "image/gif",
		".webp": "image/webp",
		".svg":  "image/svg+xml",
		".pdf":  "application/pdf",
		".doc":  "application/msword",
		".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		".xls":  "application/vnd.ms-excel",
		".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		".ppt":  "application/vnd.ms-powerpoint",
		".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		".zip":  "application/zip",
		".rar":  "application/x-rar-compressed",
		".txt":  "text/plain",
		".json": "application/json",
		".xml":  "application/xml",
		".mp3":  "audio/mpeg",
		".mp4":  "video/mp4",
		".mov":  "video/quicktime",
		".avi":  "video/x-msvideo",
	}

	if mime, ok := mimeTypes[strings.ToLower(ext)]; ok {
		return mime
	}
	return "application/octet-stream"
}

// extractObjectName extracts the object name from a full URL
func extractObjectName(fileURL, publicURL, bucketName string) string {
	prefix := fmt.Sprintf("%s/%s/", publicURL, bucketName)
	if strings.HasPrefix(fileURL, prefix) {
		return strings.TrimPrefix(fileURL, prefix)
	}
	return ""
}

// MaxFileSize is the maximum allowed file size (10MB)
const MaxFileSize = 10 * 1024 * 1024

// AllowedImageTypes for image uploads
var AllowedImageTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// AllowedFileTypes for general file uploads
var AllowedFileTypes = map[string]bool{
	"image/jpeg":       true,
	"image/png":        true,
	"image/gif":        true,
	"image/webp":       true,
	"application/pdf":  true,
	"application/msword": true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
	"application/vnd.ms-excel": true,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": true,
	"application/zip":  true,
	"text/plain":       true,
	"application/json": true,
}
