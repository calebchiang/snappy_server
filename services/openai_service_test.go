package services

import (
	"errors"
	"testing"
)

func TestParseObjectTranslationResult(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  ObjectTranslationResult
	}{
		{
			name: "valid json",
			input: `{
				"object_name_en": "Apple",
				"target_language": "Spanish",
				"translated_word": "manzana",
				"article": "la",
				"display_word": "la manzana",
				"confidence": 0.94
			}`,
			want: ObjectTranslationResult{
				ObjectNameEN:   "apple",
				TargetLanguage: "Spanish",
				TranslatedWord: "manzana",
				Article:        "la",
				DisplayWord:    "la manzana",
				Confidence:     0.94,
			},
		},
		{
			name: "json code fence",
			input: "```json\n" +
				`{
					"object_name_en": "cup",
					"target_language": "French",
					"translated_word": "tasse",
					"article": "la",
					"display_word": "la tasse",
					"confidence": 0.88
				}` +
				"\n```",
			want: ObjectTranslationResult{
				ObjectNameEN:   "cup",
				TargetLanguage: "French",
				TranslatedWord: "tasse",
				Article:        "la",
				DisplayWord:    "la tasse",
				Confidence:     0.88,
			},
		},
		{
			name: "fills display word",
			input: `{
				"object_name_en": "book",
				"target_language": "Japanese",
				"translated_word": "本",
				"article": "",
				"display_word": "",
				"confidence": 0.91
			}`,
			want: ObjectTranslationResult{
				ObjectNameEN:   "book",
				TargetLanguage: "Japanese",
				TranslatedWord: "本",
				Article:        "",
				DisplayWord:    "本",
				Confidence:     0.91,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseObjectTranslationResult(tt.input)
			if err != nil {
				t.Fatalf("parseObjectTranslationResult returned error: %v", err)
			}

			if got != tt.want {
				t.Fatalf("parseObjectTranslationResult() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseObjectTranslationResultInvalidOutput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "invalid json",
			input: "apple",
		},
		{
			name: "missing translated word",
			input: `{
				"object_name_en": "apple",
				"target_language": "Spanish",
				"translated_word": "",
				"article": "la",
				"display_word": "la manzana",
				"confidence": 0.94
			}`,
		},
		{
			name: "confidence too high",
			input: `{
				"object_name_en": "apple",
				"target_language": "Spanish",
				"translated_word": "manzana",
				"article": "la",
				"display_word": "la manzana",
				"confidence": 1.2
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseObjectTranslationResult(tt.input)
			if !errors.Is(err, ErrOpenAIInvalidOutput) {
				t.Fatalf("parseObjectTranslationResult error = %v, want ErrOpenAIInvalidOutput", err)
			}
		})
	}
}
