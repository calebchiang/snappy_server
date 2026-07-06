package models

import "time"

type Word struct {
	ID uint `gorm:"primaryKey"`

	UserID uint `gorm:"not null;index"`
	User   User `gorm:"constraint:OnDelete:CASCADE;"`

	Text string `gorm:"not null"`

	ImageKey string `gorm:"not null"`
	ImageURL string

	CreatedAt time.Time
	UpdatedAt time.Time
}
