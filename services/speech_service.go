package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	openAISpeechURL       = "https://api.openai.com/v1/audio/speech"
	defaultTTSModel       = "tts-1-hd"
	defaultTTSVoice       = "nova"
	defaultTTSSpeed       = 0.9
	maxSpeechResponseSize = 5 << 20
)

var (
	ErrSpeechInvalidInput = errors.New("invalid speech input")
	ErrSpeechTooLarge     = errors.New("speech response too large")
)

type SpeechSettings struct {
	Model string
	Voice string
	Speed float64
}

type speechRequest struct {
	Model          string  `json:"model"`
	Input          string  `json:"input"`
	Voice          string  `json:"voice"`
	ResponseFormat string  `json:"response_format"`
	Speed          float64 `json:"speed"`
}

func CurrentSpeechSettings() SpeechSettings {
	model := strings.TrimSpace(os.Getenv("OPENAI_TTS_MODEL"))
	if model == "" {
		model = defaultTTSModel
	}
	voice := strings.TrimSpace(os.Getenv("OPENAI_TTS_VOICE"))
	if voice == "" {
		voice = defaultTTSVoice
	}
	return SpeechSettings{Model: model, Voice: voice, Speed: defaultTTSSpeed}
}

func SpeechCacheSignature(text string, targetLanguage string, settings SpeechSettings) string {
	value := strings.Join([]string{
		strings.TrimSpace(text),
		strings.TrimSpace(targetLanguage),
		settings.Model,
		settings.Voice,
		fmt.Sprintf("%.2f", settings.Speed),
		"aac",
	}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

func GenerateSpeech(
	ctx context.Context,
	text string,
	targetLanguage string,
	settings SpeechSettings,
) ([]byte, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return nil, ErrOpenAIAPIKeyMissing
	}

	text = strings.TrimSpace(text)
	targetLanguage = strings.TrimSpace(targetLanguage)
	if text == "" || targetLanguage == "" || len([]rune(text)) > 4096 {
		return nil, ErrSpeechInvalidInput
	}

	payload, err := json.Marshal(speechRequest{
		Model: settings.Model, Input: text, Voice: settings.Voice,
		ResponseFormat: "aac", Speed: settings.Speed,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOpenAIRequestFailed, err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, openAISpeechURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOpenAIRequestFailed, err)
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")

	startedAt := time.Now()
	response, err := openAIHTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOpenAIRequestFailed, err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		log.Printf("openai speech error: status=%d body=%s", response.StatusCode, strings.TrimSpace(string(body)))
		return nil, fmt.Errorf("%w: status %d", ErrOpenAIRequestFailed, response.StatusCode)
	}

	limited := io.LimitReader(response.Body, maxSpeechResponseSize+1)
	audioBytes, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOpenAIRequestFailed, err)
	}
	if len(audioBytes) == 0 {
		return nil, fmt.Errorf("%w: empty audio", ErrOpenAIInvalidOutput)
	}
	if len(audioBytes) > maxSpeechResponseSize {
		return nil, ErrSpeechTooLarge
	}

	log.Printf(
		"openai speech success: model=%s voice=%s target_language=%s bytes=%d latency_ms=%d",
		settings.Model, settings.Voice, targetLanguage, len(audioBytes), time.Since(startedAt).Milliseconds(),
	)
	return audioBytes, nil
}
