package consts

import "time"

const (
	DefaultLoginUpdateMinNotice = 30 * 24 * time.Hour

	AnnotationLoginUpdateAnnouncedAt = K8sGroupNameSoperator + "/login-update-announced-at"
	AnnotationLoginUpdateScheduledAt = K8sGroupNameSoperator + "/login-update-scheduled-at"
	AnnotationLoginUpdateRequested   = K8sGroupNameSoperator + "/login-update-requested"
	AnnotationLoginUpdateInProgress  = K8sGroupNameSoperator + "/login-update-in-progress"
)
