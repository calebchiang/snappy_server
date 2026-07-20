package services

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestGenerateSpeechRequest(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	originalClient := openAIHTTPClient
	t.Cleanup(func() { openAIHTTPClient = originalClient })

	wantAudio := []byte("aac-audio")
	openAIHTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != openAISpeechURL {
			t.Fatalf("URL = %s, want %s", request.URL.String(), openAISpeechURL)
		}
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected authorization header")
		}

		var payload speechRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Input != "la botella" || payload.Model != "tts-1-hd" ||
			payload.Voice != "nova" || payload.ResponseFormat != "aac" || payload.Speed != 0.9 {
			t.Fatalf("unexpected payload: %#v", payload)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(wantAudio)),
			Header:     make(http.Header),
		}, nil
	})}

	got, err := GenerateSpeech(
		context.Background(),
		" la botella ",
		"Spanish",
		SpeechSettings{Model: "tts-1-hd", Voice: "nova", Speed: 0.9},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, wantAudio) {
		t.Fatalf("audio = %q, want %q", got, wantAudio)
	}
}

func TestSpeechCacheSignatureChangesWithSpokenTextAndConfiguration(t *testing.T) {
	settings := SpeechSettings{Model: "tts-1-hd", Voice: "nova", Speed: 0.9}
	base := SpeechCacheSignature("la botella", "Spanish", settings)

	if base == SpeechCacheSignature("botella", "Spanish", settings) {
		t.Fatal("signature did not change with spoken text")
	}
	settings.Voice = "alloy"
	if base == SpeechCacheSignature("la botella", "Spanish", settings) {
		t.Fatal("signature did not change with voice")
	}
}
