package models

import "time"

type User struct {
	ID        uint   `gorm:"primaryKey"`
	Name      string `gorm:"not null"`
	Email     string `gorm:"uniqueIndex;not null"`
	Password  string `gorm:"not null"`
	Credits   int    `gorm:"not null;default:1"`
	CreatedAt time.Time
	UpdatedAt time.Time
}
