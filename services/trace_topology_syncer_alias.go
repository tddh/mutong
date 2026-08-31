package services

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// stsPodPattern matches StatefulSet pod names like "kafka-0", "kafka-controller-0".
var stsPodPattern = regexp.MustCompile(`^(.+)-\d+$`)

// parseK8sFQDN extracts (appName, namespace) from a K8s DNS name.
//   - namespace = the label immediately preceding "svc" (kafka-0.kafka-headless.base.svc -> "base")
//   - StatefulSet pod names strip their ordinal suffix (kafka-0 -> "kafka")
//   - legacy two-segment names without "svc" fall back to service.namespace
func parseK8sFQDN(host string) (appName, namespace string) {
	labels := strings.Split(host, ".")
	if len(labels) == 0 || labels[0] == "" {
		return "", ""
	}

	for i := 1; i < len(labels); i++ {
		if labels[i] == "svc" && i > 0 {
			namespace = labels[i-1]
			break
		}
	}
	if namespace == "" && len(labels) > 1 {
		namespace = labels[1]
	}

	first := labels[0]
	if m := stsPodPattern.FindStringSubmatch(first); m != nil {
		return m[1], namespace
	}
	return first, namespace
}

// extractPodNameFromAddress returns the pod name if rawPeer is a K8s pod FQDN
// (e.g. "kafka-controller-0.kafka-controller-headless.base.svc.cluster.local"
//
//	-> "kafka-controller-0"), otherwise "".
func extractPodNameFromAddress(rawPeer string) string {
	host := rawPeer
	if idx := strings.LastIndex(rawPeer, ":"); idx > 0 {
		host = rawPeer[:idx]
	}
	if net.ParseIP(host) != nil {
		return ""
	}
	labels := strings.Split(host, ".")
	if len(labels) == 0 {
		return ""
	}
	if stsPodPattern.MatchString(labels[0]) {
		return labels[0]
	}
	return ""
}

// lookupByOwnerName resolves a BusinessApp by its workload owner name + namespace.
// It returns the canonical app (uid + app_name) that BLS created from labels.
func (s *TraceTopologySyncer) lookupByOwnerName(ownerName, namespace string) (businessAppRef, bool) {
	if ownerName == "" || namespace == "" {
		return businessAppRef{}, false
	}
	cacheKey := "bizowner:" + namespace + ":" + ownerName
	if cached, err := s.relationCache.Get(cacheKey); err == nil {
		parts := strings.SplitN(string(cached), "|", 2)
		if len(parts) == 2 {
			s.logger.Debug("OWNER_LOOKUP cache hit", zap.String("owner", ownerName), zap.String("app", parts[1]))
			return businessAppRef{uid: parts[0], appName: parts[1], namespace: namespace}, true
		}
	}

	query := fmt.Sprintf(
		`MATCH (v:BusinessApp) WHERE v.BusinessApp.owner_name == %s AND v.BusinessApp.namespace == %s RETURN v.BusinessApp.uid as uid, v.BusinessApp.app_name as app_name LIMIT 1`,
		strconv.Quote(ownerName), strconv.Quote(namespace),
	)
	resultSet, err := s.graphDB.ExecuteAndCheck(query)
	if err != nil || resultSet == nil || resultSet.GetRowSize() == 0 {
		return businessAppRef{}, false
	}
	row, rowErr := resultSet.GetRowValuesByIndex(0)
	if rowErr != nil {
		return businessAppRef{}, false
	}
	uidV, e1 := row.GetValueByColName("uid")
	appV, e2 := row.GetValueByColName("app_name")
	if e1 != nil || e2 != nil {
		return businessAppRef{}, false
	}
	uid, _ := uidV.AsString()
	app, _ := appV.AsString()
	if uid == "" || app == "" {
		return businessAppRef{}, false
	}
	_ = s.relationCache.Set(cacheKey, []byte(uid+"|"+app))
	return businessAppRef{uid: uid, appName: app, namespace: namespace}, true
}
