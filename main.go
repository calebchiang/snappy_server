package main

import (
	"github.com/calebchiang/thirdparty_server/database"
	"github.com/calebchiang/thirdparty_server/models"
	"github.com/calebchiang/thirdparty_server/routes"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	database.Connect()
	database.DB.AutoMigrate(
		&models.User{},
	)

	r := gin.Default()
	routes.UserRoutes(r)

	r.Run()
}
