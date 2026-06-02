package controllers

import (
	"net/http"

	"gitee.com/tddh/mutong/interfaces"
	"github.com/gin-gonic/gin"
)

type RoleController struct {
	roleSvc interfaces.RoleInterface
}

func NewRoleController(roleSvc interfaces.RoleInterface) *RoleController {
	return &RoleController{roleSvc: roleSvc}
}

func (rc *RoleController) RegisterRoutes(app *gin.Engine) {
	app.GET("/roles", rc.Get)
}

func (rc *RoleController) Get(ctx *gin.Context) {
	role := rc.roleSvc.GetRoleByID(1)
	ctx.JSON(http.StatusOK, role)
}
