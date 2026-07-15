{{- define "inference-service.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "inference-service.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- include "inference-service.name" . -}}
{{- end -}}
{{- end -}}

{{- define "inference-service.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "inference-service.labels" -}}
helm.sh/chart: {{ include "inference-service.chart" . }}
app.kubernetes.io/name: {{ include "inference-service.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: bodybuddy
app.kubernetes.io/component: worker
{{- end -}}

{{- define "inference-service.selectorLabels" -}}
app.kubernetes.io/name: {{ include "inference-service.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "inference-service.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "inference-service.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}
