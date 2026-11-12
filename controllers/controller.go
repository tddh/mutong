package controllers

import (
	"context"

	"gitee.com/tddh/mutong/interfaces"
	"github.com/gin-gonic/gin"
)

type Controllers struct {
	UserCtrl        *UserController
	RoleCtrl        *RoleController
	K8sResourceCtrl *K8sResourceController
}

func NewControllers(roleSvc interfaces.RoleInterface, userSvc interfaces.UserInterface, deploymentSvc interfaces.K8sResourceInterface) *Controllers {
	return &Controllers{
		UserCtrl:        NewUserController(userSvc),
		RoleCtrl:        NewRoleController(roleSvc),
		K8sResourceCtrl: NewK8sResourceController(deploymentSvc),
	}
}

func (ctrls *Controllers) RegisterRoutes(app *gin.Engine) {

	ctrls.RoleCtrl.RegisterRoutes(app)
	ctrls.UserCtrl.RegisterRoutes(app)
	ctrls.K8sResourceCtrl.RegisterK8sResourceRoutes(app)
	ctrls.RegisterControllersRoutes(app)
}

func (ctrls *Controllers) RegisterControllersRoutes(app *gin.Engine) {
}

func (ctrls *Controllers) InitCollectWithContext(ctx context.Context) {
	go ctrls.K8sResourceCtrl.CollectWithContext(ctx, "")
	go ctrls.K8sResourceCtrl.ConsumeKafkaMessagesWithContext(ctx)
}
