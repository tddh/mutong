package controllers

import (
	"net/http"

	"gitee.com/tddh/mutong/interfaces"
	"github.com/gin-gonic/gin"
)

type UserController struct {
	userSvc interfaces.UserInterface
}

func NewUserController(userSvc interfaces.UserInterface) *UserController {
	return &UserController{userSvc: userSvc}
}

func (uc *UserController) RegisterRoutes(app *gin.Engine) {
	app.GET("/users", uc.GetUserByID)
}

func (uc *UserController) GetUserByID(ctx *gin.Context) {
	user := uc.userSvc.GetUserByID(1)
	if user == nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	ctx.JSON(http.StatusOK, user)
}
