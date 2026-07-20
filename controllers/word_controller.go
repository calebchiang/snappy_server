package controllers

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/calebchiang/thirdparty_server/database"
	"github.com/calebchiang/thirdparty_server/models"
	"github.com/calebchiang/thirdparty_server/services"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

var pronunciationRequests singleflight.Group

type updateWordFavoriteRequest struct {
	IsFavorite bool `json:"is_favorite"`
}

func SaveWord(c *gin.Context) {
	userIDValue, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	userID, ok := userIDValue.(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid user ID"})
		return
	}

	text := strings.TrimSpace(c.PostForm("word"))
	if text == "" {
		text = strings.TrimSpace(c.PostForm("text"))
	}
	objectNameEN := strings.TrimSpace(c.PostForm("object_name_en"))
	targetLanguage := strings.TrimSpace(c.PostForm("target_language"))
	translatedWord := strings.TrimSpace(c.PostForm("translated_word"))
	article := strings.TrimSpace(c.PostForm("article"))
	displayWord := strings.TrimSpace(c.PostForm("display_word"))
	pronunciationGuide := strings.TrimSpace(c.PostForm("pronunciation_guide"))
	confidence := parseOptionalFloat(c.PostForm("confidence"))

	if displayWord == "" {
		displayWord = text
	}
	if text == "" {
		text = displayWord
	}
	if translatedWord == "" {
		translatedWord = displayWord
	}
	if text == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Word is required"})
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		fileHeader, err = c.FormFile("image")
	}
	if err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Image file is too large"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "Image file is required"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unable to read image file"})
		return
	}
	defer file.Close()

	imageBytes, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unable to read image file"})
		return
	}

	mimeType := http.DetectContentType(imageBytes)
	extension, ok := supportedWordImageExtension(mimeType)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported image type"})
		return
	}

	uploadResult, err := services.UploadWordImage(
		c.Request.Context(),
		userID,
		imageBytes,
		mimeType,
		extension,
	)
	if err != nil {
		log.Printf(
			"word image upload failed: user_id=%d mime_type=%s image_bytes=%d error=%v",
			userID,
			mimeType,
			len(imageBytes),
			err,
		)

		if errors.Is(err, services.ErrR2ConfigMissing) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "R2 storage not configured"})
			return
		}

		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to upload image"})
		return
	}

	word := models.Word{
		UserID:             userID,
		Text:               text,
		ObjectNameEN:       objectNameEN,
		TargetLanguage:     targetLanguage,
		TranslatedWord:     translatedWord,
		Article:            article,
		DisplayWord:        displayWord,
		PronunciationGuide: pronunciationGuide,
		Confidence:         confidence,
		ImageKey:           uploadResult.Key,
		ImageURL:           uploadResult.URL,
	}

	if err := database.DB.Create(&word).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save word"})
		return
	}

	c.JSON(http.StatusCreated, wordResponse(word))
}

func GetWords(c *gin.Context) {
	userIDValue, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	userID, ok := userIDValue.(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid user ID"})
		return
	}

	var words []models.Word
	query := database.DB.Where("user_id = ?", userID)

	if favoriteFilter := strings.TrimSpace(c.Query("favorite")); favoriteFilter != "" {
		isFavorite, err := strconv.ParseBool(favoriteFilter)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid favorite filter"})
			return
		}

		query = query.Where("is_favorite = ?", isFavorite)
	}

	if err := query.Order("created_at DESC").Find(&words).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch words"})
		return
	}

	response := make([]gin.H, 0, len(words))
	for _, word := range words {
		response = append(response, wordResponse(word))
	}

	c.JSON(http.StatusOK, gin.H{
		"words": response,
	})
}

func GetWord(c *gin.Context) {
	userIDValue, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	userID, ok := userIDValue.(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid user ID"})
		return
	}

	wordID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || wordID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid word ID"})
		return
	}

	var word models.Word
	if err := database.DB.
		Where("id = ? AND user_id = ?", wordID, userID).
		First(&word).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Word not found"})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch word"})
		return
	}

	c.JSON(http.StatusOK, wordResponse(word))
}

type pronunciationResponse struct {
	WordID      uint   `json:"word_id"`
	AudioURL    string `json:"audio_url"`
	ContentType string `json:"content_type"`
	CacheKey    string `json:"cache_key"`
	Cached      bool   `json:"cached"`
}

func GetWordPronunciation(c *gin.Context) {
	userIDValue, exists := c.Get("user_id")
	userID, ok := userIDValue.(uint)
	if !exists || !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	wordIDValue, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || wordIDValue == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid word ID"})
		return
	}
	wordID := uint(wordIDValue)

	var word models.Word
	if err := database.DB.Where("id = ? AND user_id = ?", wordID, userID).First(&word).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Word not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch word"})
		return
	}

	spokenText := strings.TrimSpace(word.DisplayWord)
	if spokenText == "" {
		spokenText = strings.TrimSpace(word.Text)
	}
	if spokenText == "" || strings.TrimSpace(word.TargetLanguage) == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "Pronunciation is unavailable for this word"})
		return
	}

	settings := services.CurrentSpeechSettings()
	signature := services.SpeechCacheSignature(spokenText, word.TargetLanguage, settings)
	requestKey := fmt.Sprintf("%d:%d:%s", userID, wordID, signature)

	value, err, _ := pronunciationRequests.Do(requestKey, func() (interface{}, error) {
		var currentWord models.Word
		if err := database.DB.Where("id = ? AND user_id = ?", wordID, userID).First(&currentWord).Error; err != nil {
			return nil, err
		}

		if currentWord.PronunciationAudioSignature == signature &&
			strings.TrimSpace(currentWord.PronunciationAudioURL) != "" {
			return pronunciationResponse{
				WordID: wordID, AudioURL: currentWord.PronunciationAudioURL,
				ContentType: "audio/aac", CacheKey: signature, Cached: true,
			}, nil
		}

		startedAt := time.Now()
		audioBytes, err := services.GenerateSpeech(
			c.Request.Context(), spokenText, currentWord.TargetLanguage, settings,
		)
		if err != nil {
			return nil, err
		}

		upload, err := services.UploadWordPronunciation(
			c.Request.Context(), userID, wordID, signature, audioBytes,
		)
		if err != nil {
			return nil, err
		}

		oldKey := currentWord.PronunciationAudioKey
		updates := map[string]interface{}{
			"pronunciation_audio_key":       upload.Key,
			"pronunciation_audio_url":       upload.URL,
			"pronunciation_audio_signature": signature,
		}
		if err := database.DB.Model(&currentWord).Updates(updates).Error; err != nil {
			_ = services.DeleteWordPronunciation(c.Request.Context(), upload.Key)
			return nil, err
		}

		if oldKey != "" && oldKey != upload.Key {
			if err := services.DeleteWordPronunciation(c.Request.Context(), oldKey); err != nil {
				log.Printf("stale pronunciation cleanup failed: user_id=%d word_id=%d error=%v", userID, wordID, err)
			}
		}

		log.Printf(
			"word pronunciation generated: user_id=%d word_id=%d target_language=%s bytes=%d latency_ms=%d",
			userID, wordID, currentWord.TargetLanguage, len(audioBytes), time.Since(startedAt).Milliseconds(),
		)
		return pronunciationResponse{
			WordID: wordID, AudioURL: upload.URL, ContentType: "audio/aac",
			CacheKey: signature, Cached: false,
		}, nil
	})
	if err != nil {
		log.Printf("word pronunciation failed: user_id=%d word_id=%d error=%v", userID, wordID, err)
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "Word not found"})
		case errors.Is(err, services.ErrOpenAIAPIKeyMissing), errors.Is(err, services.ErrR2ConfigMissing):
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Pronunciation service is not configured"})
		default:
			c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to generate pronunciation"})
		}
		return
	}

	response := value.(pronunciationResponse)
	log.Printf("word pronunciation success: user_id=%d word_id=%d cache_hit=%t", userID, wordID, response.Cached)
	c.JSON(http.StatusOK, response)
}

func UpdateWordFavorite(c *gin.Context) {
	userIDValue, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	userID, ok := userIDValue.(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid user ID"})
		return
	}

	wordID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || wordID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid word ID"})
		return
	}

	var request updateWordFavoriteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid favorite request"})
		return
	}

	var word models.Word
	if err := database.DB.
		Where("id = ? AND user_id = ?", wordID, userID).
		First(&word).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Word not found"})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch word"})
		return
	}

	word.IsFavorite = request.IsFavorite
	if err := database.DB.Save(&word).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update favorite"})
		return
	}

	c.JSON(http.StatusOK, wordResponse(word))
}

func DeleteWord(c *gin.Context) {
	userIDValue, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	userID, ok := userIDValue.(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid user ID"})
		return
	}

	wordID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || wordID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid word ID"})
		return
	}

	var word models.Word
	if err := database.DB.
		Where("id = ? AND user_id = ?", wordID, userID).
		First(&word).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Word not found"})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch word"})
		return
	}

	if err := services.DeleteWordImage(c.Request.Context(), word.ImageKey); err != nil {
		log.Printf(
			"word image delete failed: user_id=%d word_id=%d image_key=%s error=%v",
			userID,
			word.ID,
			word.ImageKey,
			err,
		)

		if errors.Is(err, services.ErrR2ConfigMissing) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "R2 storage not configured"})
			return
		}

		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to delete image"})
		return
	}

	if err := database.DB.Delete(&word).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete word"})
		return
	}

	if err := services.DeleteWordPronunciation(c.Request.Context(), word.PronunciationAudioKey); err != nil {
		log.Printf(
			"word pronunciation delete failed: user_id=%d word_id=%d error=%v",
			userID, word.ID, err,
		)
	}

	c.JSON(http.StatusOK, gin.H{
		"id":      word.ID,
		"deleted": true,
	})
}

func supportedWordImageExtension(mimeType string) (string, bool) {
	switch strings.ToLower(mimeType) {
	case "image/jpeg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	case "image/webp":
		return ".webp", true
	default:
		return "", false
	}
}

func parseOptionalFloat(value string) float64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}

	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}

	return parsed
}

func wordResponse(word models.Word) gin.H {
	displayWord := strings.TrimSpace(word.DisplayWord)
	if displayWord == "" {
		displayWord = word.Text
	}

	translatedWord := strings.TrimSpace(word.TranslatedWord)
	if translatedWord == "" {
		translatedWord = displayWord
	}

	return gin.H{
		"id":                  word.ID,
		"user_id":             word.UserID,
		"word":                word.Text,
		"object_name_en":      word.ObjectNameEN,
		"target_language":     word.TargetLanguage,
		"translated_word":     translatedWord,
		"article":             word.Article,
		"display_word":        displayWord,
		"pronunciation_guide": word.PronunciationGuide,
		"confidence":          word.Confidence,
		"is_favorite":         word.IsFavorite,
		"image_key":           word.ImageKey,
		"image_url":           word.ImageURL,
		"created_at":          word.CreatedAt,
		"updated_at":          word.UpdatedAt,
	}
}
