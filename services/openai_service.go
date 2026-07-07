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
	"regexp"
	"strings"
	"time"
)

const (
	openAIResponsesURL = "https://api.openai.com/v1/responses"
	objectIDModel      = "gpt-4.1-mini"
	objectIDPrompt     = `You identify the main physical object in an image and translate it into the user's target language.
Return only valid JSON with this exact shape:
{
  "object_name_en": "apple",
  "target_language": "Spanish",
  "translated_word": "manzana",
  "article": "la",
  "display_word": "la manzana",
  "confidence": 0.94
}
Rules:
- object_name_en must be one common English singular noun in lowercase.
- target_language must exactly match the target language provided by the user.
- translated_word must be the object translated into the target language.
- article should be the natural article for the translated word when the target language commonly uses articles. Use an empty string if no article is natural.
- display_word should be article + translated_word when article is present, otherwise just translated_word.
- confidence must be a number from 0 to 1.
- Do not include markdown, explanations, or extra keys.`
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

type ObjectTranslationResult struct {
	ObjectNameEN   string  `json:"object_name_en"`
	TargetLanguage string  `json:"target_language"`
	TranslatedWord string  `json:"translated_word"`
	Article        string  `json:"article"`
	DisplayWord    string  `json:"display_word"`
	Confidence     float64 `json:"confidence"`
}

func IdentifyObject(ctx context.Context, imageBytes []byte, mimeType string, targetLanguage string) (ObjectTranslationResult, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return ObjectTranslationResult{}, ErrOpenAIAPIKeyMissing
	}

	targetLanguage = strings.TrimSpace(targetLanguage)
	if targetLanguage == "" {
		return ObjectTranslationResult{}, ErrOpenAIInvalidOutput
	}

	log.Printf(
		"openai object identification request: model=%s target_language=%s mime_type=%s image_bytes=%d",
		objectIDModel,
		targetLanguage,
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
						Text: fmt.Sprintf(
							"Identify the main object in this image and translate it into %s.",
							targetLanguage,
						),
					},
					{
						Type:     "input_image",
						ImageURL: imageDataURL,
					},
				},
			},
		},
		MaxOutputTokens: 180,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return ObjectTranslationResult{}, fmt.Errorf("%w: %v", ErrOpenAIRequestFailed, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIResponsesURL, bytes.NewReader(body))
	if err != nil {
		return ObjectTranslationResult{}, fmt.Errorf("%w: %v", ErrOpenAIRequestFailed, err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := openAIHTTPClient.Do(req)
	if err != nil {
		return ObjectTranslationResult{}, fmt.Errorf("%w: %v", ErrOpenAIRequestFailed, err)
	}
	defer resp.Body.Close()

	log.Printf("openai object identification response: status=%d", resp.StatusCode)

	var output responsesResponse
	if err := json.NewDecoder(resp.Body).Decode(&output); err != nil {
		return ObjectTranslationResult{}, fmt.Errorf("%w: %v", ErrOpenAIInvalidOutput, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if output.Error != nil && output.Error.Message != "" {
			log.Printf(
				"openai object identification error: type=%s message=%s",
				output.Error.Type,
				output.Error.Message,
			)

			return ObjectTranslationResult{}, fmt.Errorf("%w: %s", ErrOpenAIRequestFailed, output.Error.Message)
		}
		return ObjectTranslationResult{}, fmt.Errorf("%w: status %d", ErrOpenAIRequestFailed, resp.StatusCode)
	}

	result, err := parseObjectTranslationResult(extractResponseText(output))
	if err != nil {
		log.Printf("openai object identification invalid output: output_text=%q", output.OutputText)
		return ObjectTranslationResult{}, err
	}

	log.Printf(
		"openai object identification success: object_name_en=%s target_language=%s display_word=%s confidence=%.2f",
		result.ObjectNameEN,
		result.TargetLanguage,
		result.DisplayWord,
		result.Confidence,
	)

	return result, nil
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

func parseObjectTranslationResult(value string) (ObjectTranslationResult, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return ObjectTranslationResult{}, ErrOpenAIInvalidOutput
	}

	value = stripJSONCodeFence(value)

	var result ObjectTranslationResult
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return ObjectTranslationResult{}, fmt.Errorf("%w: %v", ErrOpenAIInvalidOutput, err)
	}

	result.ObjectNameEN = strings.ToLower(strings.TrimSpace(result.ObjectNameEN))
	result.TargetLanguage = strings.TrimSpace(result.TargetLanguage)
	result.TranslatedWord = strings.TrimSpace(result.TranslatedWord)
	result.Article = strings.TrimSpace(result.Article)
	result.DisplayWord = strings.TrimSpace(result.DisplayWord)

	if result.DisplayWord == "" && result.TranslatedWord != "" {
		result.DisplayWord = result.TranslatedWord
		if result.Article != "" {
			result.DisplayWord = result.Article + " " + result.TranslatedWord
		}
	}

	if result.ObjectNameEN == "" ||
		result.TargetLanguage == "" ||
		result.TranslatedWord == "" ||
		result.DisplayWord == "" ||
		result.Confidence < 0 ||
		result.Confidence > 1 {
		return ObjectTranslationResult{}, ErrOpenAIInvalidOutput
	}

	return result, nil
}

func stripJSONCodeFence(value string) string {
	codeFencePattern := regexp.MustCompile("(?s)^```(?:json)?\\s*(.*?)\\s*```$")
	matches := codeFencePattern.FindStringSubmatch(strings.TrimSpace(value))
	if len(matches) == 2 {
		return strings.TrimSpace(matches[1])
	}
	return value
}
