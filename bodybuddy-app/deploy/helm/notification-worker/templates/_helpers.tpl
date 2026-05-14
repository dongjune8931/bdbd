{{- define "notification-worker.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "notification-worker.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- include "notification-worker.name" . -}}
{{- end -}}
{{- end -}}

{{- define "notification-worker.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "notification-worker.labels" -}}
helm.sh/chart: {{ include "notification-worker.chart" . }}
app.kubernetes.io/name: {{ include "notification-worker.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: bodybuddy
app.kubernetes.io/component: worker
{{- end -}}

{{- define "notification-worker.selectorLabels" -}}
app.kubernetes.io/name: {{ include "notification-worker.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "notification-worker.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "notification-worker.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}
