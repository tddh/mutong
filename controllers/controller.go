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
	//app.GET("/k8s/deployments", ctrls.Get)
	//app.GET("/k8s/resources/{name:string}", ctrls.Get)
}

func (ctrls *Controllers) Get(ctx *gin.Context) {
	//result := ctrls.K8sResourceCtrl.GET(ctx.Param("name"))
	//ctrls.ResourceNme(ctx)
	//ctx.JSON(http.StatusOK, result)
	//ctx.Status(http.StatusOK)
}
func (ctrls *Controllers) ResourceNme(ctx *gin.Context) {
	ctrls.RoleCtrl.Get(ctx)
}

func (ctrls *Controllers) InitCollect() {
	go ctrls.K8sResourceCtrl.Collect("")
	go ctrls.K8sResourceCtrl.ConsumeKafkaMessages()
}

func (ctrls *Controllers) InitCollectWithContext(ctx context.Context) {
	go ctrls.K8sResourceCtrl.CollectWithContext(ctx, "")
	go ctrls.K8sResourceCtrl.ConsumeKafkaMessagesWithContext(ctx)
}

// 未调用
func (ctrls *Controllers) K8sResourceCollect() {
	ctrls.K8sResourceCtrl.Collect("")
}

// 未调用
func (ctrls *Controllers) K8sResourceRelationship() {
	ctrls.K8sResourceCtrl.Relationship("")
}
