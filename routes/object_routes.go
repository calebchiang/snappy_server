package routes

import (
	"github.com/calebchiang/thirdparty_server/controllers"
	"github.com/calebchiang/thirdparty_server/middleware"
	"github.com/gin-gonic/gin"
)

func ObjectRoutes(r *gin.Engine) {
	auth := r.Group("/objects")
	auth.Use(middleware.RequireAuth())
	{
		auth.POST("/identify", controllers.IdentifyObject)
	}
}
