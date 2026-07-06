package routes

import (
	"github.com/calebchiang/thirdparty_server/controllers"
	"github.com/calebchiang/thirdparty_server/middleware"
	"github.com/gin-gonic/gin"
)

func WordRoutes(r *gin.Engine) {
	auth := r.Group("/words")
	auth.Use(middleware.RequireAuth())
	{
		auth.GET("", controllers.GetWords)
		auth.POST("", controllers.SaveWord)
	}
}
