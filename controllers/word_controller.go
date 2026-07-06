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

const maxWordImageBytes = 5 << 20
const maxWordRequestBytes = maxWordImageBytes + (1 << 20)

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

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWordRequestBytes)

	text := strings.TrimSpace(c.PostForm("word"))
	if text == "" {
		text = strings.TrimSpace(c.PostForm("text"))
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

	if fileHeader.Size > maxWordImageBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Image file is too large"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unable to read image file"})
		return
	}
	defer file.Close()

	imageBytes, err := io.ReadAll(io.LimitReader(file, maxWordImageBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unable to read image file"})
		return
	}

	if len(imageBytes) > maxWordImageBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Image file is too large"})
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
		UserID:   userID,
		Text:     text,
		ImageKey: uploadResult.Key,
		ImageURL: uploadResult.URL,
	}

	if err := database.DB.Create(&word).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save word"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":         word.ID,
		"word":       word.Text,
		"image_key":  word.ImageKey,
		"image_url":  word.ImageURL,
		"created_at": word.CreatedAt,
	})
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
	if err := database.DB.
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&words).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch words"})
		return
	}

	response := make([]gin.H, 0, len(words))
	for _, word := range words {
		response = append(response, gin.H{
			"id":         word.ID,
			"word":       word.Text,
			"image_key":  word.ImageKey,
			"image_url":  word.ImageURL,
			"created_at": word.CreatedAt,
			"updated_at": word.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"words": response,
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
