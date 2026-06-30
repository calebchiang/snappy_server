package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"
)

const (
	openAIResponsesURL = "https://api.openai.com/v1/responses"
	objectIDModel      = "gpt-4.1-mini"
	objectIDPrompt     = "You identify the main physical object in an image. Return exactly one common English singular noun. Use lowercase only. Do not include adjectives, explanations, punctuation, or multiple words. If uncertain, return your best guess as one word."
)

var (
	ErrOpenAIAPIKeyMissing = errors.New("openai api key missing")
	ErrOpenAIRequestFailed = errors.New("openai request failed")
	ErrOpenAIInvalidOutput = errors.New("openai returned invalid output")
)

var openAIHTTPClient = &http.Client{
	Timeout: 20 * time.Second,
}

type responsesRequest struct {
	Model           string                  `json:"model"`
	Instructions    string                  `json:"instructions"`
	Input           []responsesInputMessage `json:"input"`
	MaxOutputTokens int                     `json:"max_output_tokens"`
}

type responsesInputMessage struct {
	Role    string                  `json:"role"`
	Content []responsesInputContent `json:"content"`
}

type responsesInputContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

type responsesResponse struct {
	OutputText string `json:"output_text"`
	Output     []struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func IdentifyObject(ctx context.Context, imageBytes []byte, mimeType string) (string, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return "", ErrOpenAIAPIKeyMissing
	}

	log.Printf(
		"openai object identification request: model=%s mime_type=%s image_bytes=%d",
		objectIDModel,
		mimeType,
		len(imageBytes),
	)

	imageDataURL := fmt.Sprintf(
		"data:%s;base64,%s",
		mimeType,
		base64.StdEncoding.EncodeToString(imageBytes),
	)

	payload := responsesRequest{
		Model:        objectIDModel,
		Instructions: objectIDPrompt,
		Input: []responsesInputMessage{
			{
				Role: "user",
				Content: []responsesInputContent{
					{
						Type: "input_text",
						Text: "Identify the main object in this image.",
					},
					{
						Type:     "input_image",
						ImageURL: imageDataURL,
					},
				},
			},
		},
		MaxOutputTokens: 12,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrOpenAIRequestFailed, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIResponsesURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrOpenAIRequestFailed, err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := openAIHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrOpenAIRequestFailed, err)
	}
	defer resp.Body.Close()

	log.Printf("openai object identification response: status=%d", resp.StatusCode)

	var output responsesResponse
	if err := json.NewDecoder(resp.Body).Decode(&output); err != nil {
		return "", fmt.Errorf("%w: %v", ErrOpenAIInvalidOutput, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if output.Error != nil && output.Error.Message != "" {
			log.Printf(
				"openai object identification error: type=%s message=%s",
				output.Error.Type,
				output.Error.Message,
			)

			return "", fmt.Errorf("%w: %s", ErrOpenAIRequestFailed, output.Error.Message)
		}
		return "", fmt.Errorf("%w: status %d", ErrOpenAIRequestFailed, resp.StatusCode)
	}

	word := normalizeObjectWord(extractResponseText(output))
	if word == "" {
		log.Printf("openai object identification invalid output: output_text=%q", output.OutputText)
		return "", ErrOpenAIInvalidOutput
	}

	log.Printf("openai object identification success: word=%s", word)

	return word, nil
}

func extractResponseText(output responsesResponse) string {
	if strings.TrimSpace(output.OutputText) != "" {
		return output.OutputText
	}

	for _, item := range output.Output {
		for _, content := range item.Content {
			if content.Text != "" {
				return content.Text
			}
		}
	}

	return ""
}

func normalizeObjectWord(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}

	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}

	word := strings.TrimFunc(fields[0], func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})

	if word == "a" || word == "an" || word == "the" {
		if len(fields) < 2 {
			return ""
		}
		word = strings.TrimFunc(fields[1], func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		})
	}

	return word
}
