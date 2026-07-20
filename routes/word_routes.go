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
		auth.GET("/:id", controllers.GetWord)
		auth.POST("/:id/pronunciation", controllers.GetWordPronunciation)
		auth.POST("", controllers.SaveWord)
		auth.PATCH("/:id/favorite", controllers.UpdateWordFavorite)
		auth.DELETE("/:id", controllers.DeleteWord)
	}
}
