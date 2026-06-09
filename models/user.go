package models

import "time"

type User struct {
	ID uint `gorm:"primaryKey"`

	// User's display name shown throughout the app
	Name string `gorm:"not null"`

	Email    string `gorm:"uniqueIndex;not null"`
	Password string `gorm:"not null"`

	// Language the user is currently learning
	TargetLanguage string

	// User's native language for translations and explanations
	NativeLanguage string

	// Marketing attribution (TikTok, Instagram, Friend, etc.)
	HeardFrom string

	// Age range selected during onboarding
	AgeGroup string

	Credits int `gorm:"not null;default:1"`

	CreatedAt time.Time
	UpdatedAt time.Time
}
