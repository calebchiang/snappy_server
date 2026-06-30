package controllers

import (
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/calebchiang/thirdparty_server/services"
	"github.com/gin-gonic/gin"
)

const maxObjectImageBytes = 5 << 20
const maxObjectRequestBytes = maxObjectImageBytes + (1 << 20)

func IdentifyObject(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxObjectRequestBytes)

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

	word, err := services.IdentifyObject(c.Request.Context(), imageBytes, mimeType)
	if err != nil {
		log.Printf(
			"object identification failed: mime_type=%s image_bytes=%d error=%v",
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

	c.JSON(http.StatusOK, gin.H{"word": word})
}

func isSupportedObjectImageType(mimeType string) bool {
	switch strings.ToLower(mimeType) {
	case "image/jpeg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}
