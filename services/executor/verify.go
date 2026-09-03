package executor

import (
	"context"
	"fmt"
	"time"

	ex "gitee.com/tddh/mutong/models/executor"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// 执行后验证参数：执行成功后等待一小段时间让控制器开始调和，再轮询目标就绪状态
const (
	verifyInitialDelay = 10 * time.Second
	verifyInterval     = 5 * time.Second
	verifyTimeout      = 4 * time.Minute
	// verifyIterTimeout 给每次轮询的 K8s 调用一个独立超时，避免共享 client 限流器
	// 在父 context 接近超时时报 "would exceed context deadline" 干扰失败判定
	verifyIterTimeout = 15 * time.Second
)

// verifyResult 验证结论
type verifyResult struct {
	ok bool
	// transient 表示本次结论源于瞬时读取错误（API 不可达、限流等），
	// 而非目标确定性地"未就绪"；瞬时结果不作为最终失败原因，降低误判回滚风险
	transient bool
	message   string
}

// startVerification 后台验证一次已执行的动作，并把结果回写到审计记录。
// oldPod / oldTemplateJSON 是执行前抓取的快照，分别用于识别"新 Pod"和失败自动回滚。
func (e *K8sExecutor) startVerification(ctx context.Context, plan ex.ExecutionPlan, execMsg string, oldPod podSnapshot, oldTemplateJSON string) {
	if !actionNeedsVerification(plan.Action) {
		return
	}
	ns := plan.Namespace
	if ns == "" && e.cfg != nil {
		ns = e.cfg.GetExecutorConf().DefaultNamespace
	}

	// 进程关闭时终止所有在途验证
	verifyCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-e.stopCh:
			cancel()
		case <-verifyCtx.Done():
		}
	}()

	time.Sleep(verifyInitialDelay)

	// lastDef 记录最近一次"确定性"结论（就绪 或 明确未就绪），
	// 瞬时读取错误不覆盖它，保证超时后报告的失败原因真实可信
	var lastDef verifyResult
	var sawDef bool
	err := wait.PollUntilContextTimeout(verifyCtx, verifyInterval, verifyTimeout, true, func(c context.Context) (bool, error) {
		iterCtx, cancel := context.WithTimeout(c, verifyIterTimeout)
		defer cancel()
		r := e.checkExecutionOutcome(iterCtx, plan.Action, ns, plan.ResourceName, oldPod)
		if r.ok {
			lastDef, sawDef = r, true
			return true, nil
		}
		if !r.transient {
			lastDef, sawDef = r, true
		}
		return false, nil
	})
	vr := lastDef
	if err != nil && !vr.ok {
		detail := vr.message
		if !sawDef || detail == "" {
			detail = "目标在超时窗口内未达到就绪状态"
		}
		vr = verifyResult{ok: false, message: fmt.Sprintf("超时（%v）未就绪: %s", verifyTimeout, detail)}
	}

	var finalMsg string
	var finalSuccess bool
	switch {
	case vr.ok:
		finalMsg = execMsg + " | 执行验证通过: " + vr.message
		finalSuccess = true
		// 验证通过即确认当前模板健康，记录为"最近已知健康版本"，供将来回滚优先使用
		if isDeploymentLevelAction(plan.Action) {
			e.snapshotDeploymentTemplate(ctx, ns, plan.ResourceName)
		}
	case canAutoRollback(plan.Action):
		// 可回滚动作（改镜像/调资源）验证失败：优先回滚到"最近已知健康版本"，
		// 没有才退回变更前快照（快照本身可能就是坏状态，已知健康版本更可靠）
		rollbackTarget, source := e.pickRollbackTarget(ns, plan.ResourceName, oldTemplateJSON)
		if rollbackTarget == "" {
			e.logger.Error("Verification failed but no rollback template available",
				zap.String("action", string(plan.Action)),
				zap.String("deployment", ns+"/"+plan.ResourceName))
			finalMsg = fmt.Sprintf("%s | 执行验证失败: %s（无可用回滚模板，需人工介入）", execMsg, vr.message)
			finalSuccess = false
			break
		}
		rbCtx, rbCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer rbCancel()
		if rbErr := e.restorePodTemplate(rbCtx, ns, plan.ResourceName, rollbackTarget); rbErr != nil {
			e.logger.Error("Auto rollback after failed verification failed",
				zap.String("action", string(plan.Action)),
				zap.String("deployment", ns+"/"+plan.ResourceName),
				zap.String("rollbackSource", source),
				zap.Error(rbErr))
			finalMsg = fmt.Sprintf("%s | 执行验证失败: %s；自动回滚（%s）也失败: %v（需人工介入）", execMsg, vr.message, source, rbErr)
		} else {
			e.logger.Warn("Verification failed, deployment auto-rolled back",
				zap.String("action", string(plan.Action)),
				zap.String("deployment", ns+"/"+plan.ResourceName),
				zap.String("rollbackSource", source),
				zap.String("reason", vr.message))
			finalMsg = fmt.Sprintf("%s | 执行验证失败已自动回滚（%s）: %s", execMsg, source, vr.message)
		}
		finalSuccess = false
	default:
		finalMsg = fmt.Sprintf("%s | 执行验证失败: %s（需人工介入）", execMsg, vr.message)
		finalSuccess = false
	}

	if uErr := e.auditStore.UpdateResult(plan.ID, finalSuccess, finalMsg); uErr != nil {
		e.logger.Error("Update audit with verification result failed",
			zap.String("planId", plan.ID), zap.Error(uErr))
		return
	}
	e.logger.Info("Post-execution verification finished",
		zap.String("action", string(plan.Action)),
		zap.String("target", ns+"/"+plan.ResourceName),
		zap.Bool("verified", vr.ok),
		zap.Bool("rollbackable", !vr.ok && canAutoRollback(plan.Action)))
}

// isDeploymentLevelAction 判断动作是否作用于 Deployment（其目标模板可作为已知健康版本记录）
func isDeploymentLevelAction(a ex.ActionType) bool {
	switch a {
	case ex.ActionRolloutRestart, ex.ActionUpdateDeploymentImage,
		ex.ActionUpdateResourceLimits, ex.ActionScaleDeployment,
		ex.ActionRolloutUndo:
		return true
	}
	return false
}

// pickRollbackTarget 选择回滚目标模板：优先"最近已知健康版本"，否则退回变更前快照
func (e *K8sExecutor) pickRollbackTarget(ns, name, snapshot string) (target, source string) {
	if kg, ok := e.knownGood.Load(ns + "/" + name); ok {
		if s, _ := kg.(string); s != "" {
			return s, "最近已知健康版本"
		}
	}
	if snapshot != "" {
		return snapshot, "变更前快照"
	}
	return "", ""
}

// actionNeedsVerification 哪些动作执行成功后需要验证就绪状态
func actionNeedsVerification(a ex.ActionType) bool {
	switch a {
	case ex.ActionRestartPod, ex.ActionDeletePod,
		ex.ActionRolloutRestart, ex.ActionUpdateDeploymentImage,
		ex.ActionUpdateResourceLimits, ex.ActionScaleDeployment,
		ex.ActionRolloutUndo:
		return true
	}
	return false
}

// canAutoRollback 验证失败时可自动回滚的动作（有变更前模板快照可用）
func canAutoRollback(a ex.ActionType) bool {
	return a == ex.ActionUpdateDeploymentImage || a == ex.ActionUpdateResourceLimits
}

// checkExecutionOutcome 检查动作目标是否达到就绪状态
func (e *K8sExecutor) checkExecutionOutcome(ctx context.Context, action ex.ActionType, ns, name string, oldPod podSnapshot) verifyResult {
	switch action {
	case ex.ActionRestartPod, ex.ActionDeletePod:
		return e.checkNewPodReady(ctx, ns, name, oldPod)
	default:
		return e.checkDeploymentRollout(ctx, ns, name)
	}
}

// checkDeploymentRollout 检查 Deployment rollout 是否完成：
// observedGeneration 追上、新模板副本全部更新且可用、无旧版本副本残留
func (e *K8sExecutor) checkDeploymentRollout(ctx context.Context, ns, name string) verifyResult {
	dep, err := e.k8sClient.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return verifyResult{ok: false, transient: true, message: fmt.Sprintf("获取 Deployment 失败: %v", err)}
	}
	return deploymentRolloutStatus(dep)
}

// deploymentRolloutStatus 基于 Deployment 对象判定 rollout 是否健康完成（纯函数，便于快照路径复用）
func deploymentRolloutStatus(dep *appsv1.Deployment) verifyResult {
	desired := int32(1)
	if dep.Spec.Replicas != nil {
		desired = *dep.Spec.Replicas
	}
	st := dep.Status
	if st.ObservedGeneration < dep.Generation {
		return verifyResult{ok: false, message: fmt.Sprintf("控制器尚未处理最新变更（observedGeneration %d < %d）", st.ObservedGeneration, dep.Generation)}
	}
	if desired == 0 {
		if st.Replicas == 0 {
			return verifyResult{ok: true, message: "副本已全部缩容"}
		}
		return verifyResult{ok: false, message: fmt.Sprintf("缩容中（当前 %d 个副本）", st.Replicas)}
	}
	if st.UpdatedReplicas < desired {
		return verifyResult{ok: false, message: fmt.Sprintf("滚动更新中（已更新 %d/%d）", st.UpdatedReplicas, desired)}
	}
	if st.AvailableReplicas < desired {
		return verifyResult{ok: false, message: fmt.Sprintf("等待可用副本（%d/%d 可用）", st.AvailableReplicas, desired)}
	}
	if st.Replicas > st.UpdatedReplicas {
		return verifyResult{ok: false, message: fmt.Sprintf("旧版本副本回收中（总副本 %d，新副本 %d）", st.Replicas, st.UpdatedReplicas)}
	}
	return verifyResult{ok: true, message: fmt.Sprintf("rollout 完成（%d/%d 副本就绪）", st.AvailableReplicas, desired)}
}

// checkNewPodReady 检查旧 Pod（按 UID 识别）被替换后，新 Pod 是否 Ready
func (e *K8sExecutor) checkNewPodReady(ctx context.Context, ns, name string, oldPod podSnapshot) verifyResult {
	pod, err := e.k8sClient.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		if oldPod.uid != "" && pod.UID == oldPod.uid {
			return verifyResult{ok: false, message: fmt.Sprintf("原 Pod 仍在（状态 %s），尚未被替换", pod.Status.Phase)}
		}
		// 名字相同但 UID 不同（或驱逐原地重启）：直接判断就绪
		if isPodReady(pod) {
			return verifyResult{ok: true, message: fmt.Sprintf("Pod %s 已就绪", pod.Name)}
		}
		return verifyResult{ok: false, message: fmt.Sprintf("Pod %s 未就绪（状态 %s）", pod.Name, pod.Status.Phase)}
	}

	// 原 Pod 已消失：用执行前快照的控制器选择器找替代 Pod
	if !oldPod.controller || oldPod.selector == "" {
		// 快照缺失时兜底：重新按名字取不到 Pod，无法定位控制器
		return verifyResult{ok: false, message: fmt.Sprintf("原 Pod 已删除，且无控制器快照可定位替代 Pod（%s）", name)}
	}
	pods, listErr := e.k8sClient.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: oldPod.selector})
	if listErr != nil {
		return verifyResult{ok: false, transient: true, message: fmt.Sprintf("列出 %s 的 Pod 失败: %v", oldPod.ownerName, listErr)}
	}
	for i := range pods.Items {
		p := &pods.Items[i]
		if oldPod.uid != "" && p.UID == oldPod.uid {
			continue
		}
		if p.DeletionTimestamp != nil {
			continue
		}
		if isPodReady(p) {
			return verifyResult{ok: true, message: fmt.Sprintf("新 Pod %s 已就绪", p.Name)}
		}
		return verifyResult{ok: false, message: fmt.Sprintf("新 Pod %s 未就绪（状态 %s）", p.Name, p.Status.Phase)}
	}
	return verifyResult{ok: false, message: fmt.Sprintf("%s 尚未创建替代 Pod", oldPod.ownerName)}
}

// podOwnerSelector 找到 Pod 的控制器（ReplicaSet→Deployment / StatefulSet / DaemonSet）及其标签选择器
func (e *K8sExecutor) podOwnerSelector(ctx context.Context, ns string, pod *corev1.Pod) (selector, ownerName string, err error) {
	for _, ref := range pod.OwnerReferences {
		switch ref.Kind {
		case "ReplicaSet":
			rs, rsErr := e.k8sClient.AppsV1().ReplicaSets(ns).Get(ctx, ref.Name, metav1.GetOptions{})
			if rsErr != nil {
				return "", "", fmt.Errorf("获取 ReplicaSet 失败: %w", rsErr)
			}
			for _, rsOwner := range rs.OwnerReferences {
				if rsOwner.Kind == "Deployment" {
					return metav1.FormatLabelSelector(rs.Spec.Selector), "Deployment " + rsOwner.Name, nil
				}
			}
			return metav1.FormatLabelSelector(rs.Spec.Selector), "ReplicaSet " + ref.Name, nil
		case "StatefulSet":
			sts, stsErr := e.k8sClient.AppsV1().StatefulSets(ns).Get(ctx, ref.Name, metav1.GetOptions{})
			if stsErr != nil {
				return "", "", fmt.Errorf("获取 StatefulSet 失败: %w", stsErr)
			}
			return metav1.FormatLabelSelector(sts.Spec.Selector), "StatefulSet " + ref.Name, nil
		case "DaemonSet":
			ds, dsErr := e.k8sClient.AppsV1().DaemonSets(ns).Get(ctx, ref.Name, metav1.GetOptions{})
			if dsErr != nil {
				return "", "", fmt.Errorf("获取 DaemonSet 失败: %w", dsErr)
			}
			return metav1.FormatLabelSelector(ds.Spec.Selector), "DaemonSet " + ref.Name, nil
		}
	}
	return "", "", fmt.Errorf("Pod %s 没有控制器（裸 Pod 删除后不会重建）", pod.Name)
}

func isPodReady(pod *corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
