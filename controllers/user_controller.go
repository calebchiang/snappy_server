package controllers

import (
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/calebchiang/thirdparty_server/database"
	"github.com/calebchiang/thirdparty_server/models"
	"github.com/calebchiang/thirdparty_server/services"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func CreateUser(c *gin.Context) {
	var input struct {
		Name           string `json:"name"`
		Email          string `json:"email"`
		Password       string `json:"password"`
		TargetLanguage string `json:"target_language"`
		NativeLanguage string `json:"native_language"`
		HeardFrom      string `json:"heard_from"`
		AgeGroup       string `json:"age_group"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if input.Name == "" || input.Email == "" || input.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name, email, and password are required"})
		return
	}

	email := strings.ToLower(strings.TrimSpace(input.Email))

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	user := models.User{
		Name:           strings.TrimSpace(input.Name),
		Email:          email,
		Password:       string(hashedPassword),
		TargetLanguage: input.TargetLanguage,
		NativeLanguage: input.NativeLanguage,
		HeardFrom:      input.HeardFrom,
		AgeGroup:       input.AgeGroup,
		PlanTier:       "free",
	}

	if err := database.DB.Create(&user).Error; err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			c.JSON(http.StatusConflict, gin.H{"error": "Email already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	c.JSON(http.StatusCreated, userResponse(user))
}

func LoginUser(c *gin.Context) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if input.Email == "" || input.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email and password required"})
		return
	}

	email := strings.ToLower(strings.TrimSpace(input.Email))

	var user models.User
	if err := database.DB.Where("email = ?", email).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(input.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "JWT secret not configured"})
		return
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID,
		"exp":     time.Now().Add(30 * 24 * time.Hour).Unix(),
	})

	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": tokenString,
	})
}

func GetCurrentUser(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var user models.User

	if err := database.DB.
		Select("id, name, email, credits, target_language, native_language, heard_from, age_group, plan_tier").
		Where("id = ?", userID.(uint)).
		First(&user).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	c.JSON(http.StatusOK, userResponse(user))
}

func UpdateCurrentUser(c *gin.Context) {
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

	var input struct {
		TargetLanguage *string `json:"target_language"`
		NativeLanguage *string `json:"native_language"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	updates := map[string]interface{}{}
	if input.TargetLanguage != nil {
		updates["target_language"] = strings.TrimSpace(*input.TargetLanguage)
	}
	if input.NativeLanguage != nil {
		updates["native_language"] = strings.TrimSpace(*input.NativeLanguage)
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No profile fields provided"})
		return
	}

	var user models.User
	if err := database.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch user"})
		return
	}

	if err := database.DB.Model(&user).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user"})
		return
	}

	if err := database.DB.
		Select("id, name, email, credits, target_language, native_language, heard_from, age_group, plan_tier").
		Where("id = ?", userID).
		First(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch updated user"})
		return
	}

	c.JSON(http.StatusOK, userResponse(user))
}

func DeleteCurrentUser(c *gin.Context) {
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

	var user models.User
	if err := database.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch user"})
		return
	}

	var words []models.Word
	if err := database.DB.
		Select("id, image_key").
		Where("user_id = ?", userID).
		Find(&words).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch user data"})
		return
	}

	for _, word := range words {
		imageKey := strings.TrimSpace(word.ImageKey)
		if imageKey == "" {
			continue
		}

		if err := services.DeleteWordImage(c.Request.Context(), imageKey); err != nil {
			log.Printf(
				"account image delete failed: user_id=%d word_id=%d image_key=%s error=%v",
				userID,
				word.ID,
				imageKey,
				err,
			)

			if errors.Is(err, services.ErrR2ConfigMissing) {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "R2 storage not configured"})
				return
			}

			c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to delete account images"})
			return
		}
	}

	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&models.Word{}).Error; err != nil {
			return err
		}

		return tx.Delete(&user).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete account"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":      userID,
		"deleted": true,
	})
}

func userResponse(user models.User) gin.H {
	return gin.H{
		"id":              user.ID,
		"name":            user.Name,
		"email":           user.Email,
		"credits":         user.Credits,
		"target_language": user.TargetLanguage,
		"native_language": user.NativeLanguage,
		"heard_from":      user.HeardFrom,
		"age_group":       user.AgeGroup,
		"plan_tier":       normalizedPlanTier(user.PlanTier),
	}
}

func normalizedPlanTier(planTier string) string {
	switch strings.ToLower(strings.TrimSpace(planTier)) {
	case "pro":
		return "pro"
	default:
		return "free"
	}
}
