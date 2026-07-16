package cursor

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

var ErrInvalid = errors.New("invalid cursor")
var ErrExpired = errors.New("cursor expired")

type State struct {
	Version            int       `json:"v"`
	QueryHash          string    `json:"q,omitempty"`
	RequestFingerprint string    `json:"f,omitempty"`
	Provider           string    `json:"p"`
	Providers          []string  `json:"ps"`
	ProviderPage       int       `json:"n"`
	ProviderPageToken  string    `json:"pt,omitempty"`
	Limit              int       `json:"l"`
	ExpiresAt          time.Time `json:"e"`
}

type Codec struct {
	aead cipher.AEAD
	ttl  time.Duration
	now  func() time.Time
}

func New(secret string, ttl time.Duration, now func() time.Time) (*Codec, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("cursor secret must not be empty")
	}
	if ttl <= 0 {
		return nil, errors.New("cursor TTL must be positive")
	}
	digest := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(digest[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	return &Codec{aead: aead, ttl: ttl, now: now}, nil
}

func (codec *Codec) Encode(state State) (string, error) {
	version, prefix, additionalData := 2, "cur_v2_", []byte("search-cursor-v2")
	if state.RequestFingerprint == "" && state.QueryHash != "" {
		version, prefix, additionalData = 1, "cur_v1_", []byte("search-cursor-v1")
	}
	state.Version = version
	if state.ExpiresAt.IsZero() {
		state.ExpiresAt = codec.now().Add(codec.ttl).UTC()
	}
	plain, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, codec.aead.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := codec.aead.Seal(nonce, nonce, plain, additionalData)
	return prefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (codec *Codec) Decode(token string) (State, error) {
	prefix, additionalData := "cur_v2_", []byte("search-cursor-v2")
	if strings.HasPrefix(token, "cur_v1_") {
		prefix, additionalData = "cur_v1_", []byte("search-cursor-v1")
	} else if !strings.HasPrefix(token, prefix) {
		return State{}, ErrInvalid
	}
	sealed, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, prefix))
	if err != nil || len(sealed) <= codec.aead.NonceSize() {
		return State{}, ErrInvalid
	}
	nonce, body := sealed[:codec.aead.NonceSize()], sealed[codec.aead.NonceSize():]
	plain, err := codec.aead.Open(nil, nonce, body, additionalData)
	if err != nil {
		return State{}, ErrInvalid
	}
	var state State
	if err := json.Unmarshal(plain, &state); err != nil || (state.Version != 1 && state.Version != 2) || state.ProviderPage < 1 || state.Limit < 1 || len(state.Providers) == 0 {
		return State{}, ErrInvalid
	}
	if state.Version == 2 && state.RequestFingerprint == "" {
		return State{}, ErrInvalid
	}
	if !state.ExpiresAt.After(codec.now()) {
		return State{}, ErrExpired
	}
	return state, nil
}

func QueryHash(query string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(query)))
	return fmt.Sprintf("%x", digest[:])
}
