package debugartifact

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
)

var safeSegment = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type Store struct {
	baseDir      string
	previewBytes int
}

func New(baseDir string, previewBytes int) (*Store, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("debug artifact base directory is empty")
	}
	if previewBytes <= 0 {
		return nil, fmt.Errorf("debug preview bytes must be positive")
	}
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return nil, fmt.Errorf("create debug artifact directory: %w", err)
	}
	return &Store{baseDir: baseDir, previewBytes: previewBytes}, nil
}

func (s *Store) Preview(body []byte) (string, string) {
	return PreviewAndHash(body, s.previewBytes)
}

func (s *Store) RedactHeaders(headers http.Header) http.Header {
	return RedactHeaders(headers)
}

func (s *Store) SaveHTML(requestID, transport string, body []byte) (string, error) {
	return s.save(requestID, transport, ".html", body)
}

func (s *Store) SaveScreenshot(requestID, transport string, body []byte) (string, error) {
	return s.save(requestID, transport, ".png", body)
}

func (s *Store) save(requestID, transport, extension string, body []byte) (string, error) {
	if !safeSegment.MatchString(requestID) {
		return "", fmt.Errorf("unsafe request ID %q", requestID)
	}
	if !safeSegment.MatchString(transport) {
		return "", fmt.Errorf("unsafe transport name %q", transport)
	}
	dir := filepath.Join(s.baseDir, requestID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create request artifact directory: %w", err)
	}
	path := filepath.Join(dir, transport+extension)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", fmt.Errorf("write artifact %s: %w", path, err)
	}
	return path, nil
}

func RedactHeaders(headers http.Header) http.Header {
	redacted := headers.Clone()
	for _, key := range []string{"Cookie", "Set-Cookie", "Authorization", "Proxy-Authorization"} {
		if _, exists := redacted[http.CanonicalHeaderKey(key)]; exists {
			redacted.Set(key, "[REDACTED]")
		}
	}
	return redacted
}

func PreviewAndHash(body []byte, maxBytes int) (string, string) {
	digest := sha256.Sum256(body)
	preview := body
	if maxBytes >= 0 && len(preview) > maxBytes {
		preview = preview[:maxBytes]
	}
	return string(preview), hex.EncodeToString(digest[:])
}
