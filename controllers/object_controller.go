package controllers

import (
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/calebchiang/thirdparty_server/database"
	"github.com/calebchiang/thirdparty_server/models"
	"github.com/calebchiang/thirdparty_server/services"
	"github.com/gin-gonic/gin"
)

const maxObjectImageBytes = 5 << 20
const maxObjectRequestBytes = maxObjectImageBytes + (1 << 20)

func IdentifyObject(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxObjectRequestBytes)

	userID, targetLanguage, ok := objectRequestUserContext(c)
	if !ok {
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

	if fileHeader.Size > maxObjectImageBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Image file is too large"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unable to read image file"})
		return
	}
	defer file.Close()

	imageBytes, err := io.ReadAll(io.LimitReader(file, maxObjectImageBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unable to read image file"})
		return
	}

	if len(imageBytes) > maxObjectImageBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Image file is too large"})
		return
	}

	mimeType := http.DetectContentType(imageBytes)
	if !isSupportedObjectImageType(mimeType) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported image type"})
		return
	}

	result, err := services.IdentifyObject(c.Request.Context(), imageBytes, mimeType, targetLanguage)
	if err != nil {
		log.Printf(
			"object identification failed: user_id=%d target_language=%s mime_type=%s image_bytes=%d error=%v",
			userID,
			targetLanguage,
			mimeType,
			len(imageBytes),
			err,
		)

		status := http.StatusBadGateway
		message := "Failed to identify object"

		switch {
		case errors.Is(err, services.ErrOpenAIAPIKeyMissing):
			status = http.StatusInternalServerError
			message = "OpenAI API key not configured"
		case errors.Is(err, services.ErrOpenAIInvalidOutput):
			status = http.StatusBadGateway
			message = "Invalid object identification response"
		}

		c.JSON(status, gin.H{"error": message})
		return
	}

	c.JSON(http.StatusOK, objectTranslationResponse(result))
}

func TranslateObject(c *gin.Context) {
	userID, targetLanguage, ok := objectRequestUserContext(c)
	if !ok {
		return
	}

	var input struct {
		ObjectNameEN string `json:"object_name_en"`
		Word         string `json:"word"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	objectNameEN := strings.TrimSpace(input.ObjectNameEN)
	if objectNameEN == "" {
		objectNameEN = strings.TrimSpace(input.Word)
	}
	if objectNameEN == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Object name is required"})
		return
	}

	result, err := services.TranslateObject(c.Request.Context(), objectNameEN, targetLanguage)
	if err != nil {
		log.Printf(
			"object translation failed: user_id=%d target_language=%s object_name_en=%s error=%v",
			userID,
			targetLanguage,
			objectNameEN,
			err,
		)

		status := http.StatusBadGateway
		message := "Failed to translate object"

		switch {
		case errors.Is(err, services.ErrOpenAIAPIKeyMissing):
			status = http.StatusInternalServerError
			message = "OpenAI API key not configured"
		case errors.Is(err, services.ErrOpenAIInvalidOutput):
			status = http.StatusBadGateway
			message = "Invalid object translation response"
		}

		c.JSON(status, gin.H{"error": message})
		return
	}

	c.JSON(http.StatusOK, objectTranslationResponse(result))
}

func isSupportedObjectImageType(mimeType string) bool {
	switch strings.ToLower(mimeType) {
	case "image/jpeg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

func objectRequestUserContext(c *gin.Context) (uint, string, bool) {
	userIDValue, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return 0, "", false
	}

	userID, ok := userIDValue.(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid user ID"})
		return 0, "", false
	}

	var user models.User
	if err := database.DB.
		Select("id, target_language").
		Where("id = ?", userID).
		First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return 0, "", false
	}

	targetLanguage := strings.TrimSpace(user.TargetLanguage)
	if targetLanguage == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Target language is required"})
		return 0, "", false
	}

	return userID, targetLanguage, true
}

func objectTranslationResponse(result services.ObjectTranslationResult) gin.H {
	return gin.H{
		"object_name_en":      result.ObjectNameEN,
		"target_language":     result.TargetLanguage,
		"translated_word":     result.TranslatedWord,
		"article":             result.Article,
		"display_word":        result.DisplayWord,
		"pronunciation_guide": result.PronunciationGuide,
		"confidence":          result.Confidence,
		"word":                result.DisplayWord,
	}
}
