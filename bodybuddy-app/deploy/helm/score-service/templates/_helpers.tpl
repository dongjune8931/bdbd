{{- define "score-service.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "score-service.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- include "score-service.name" . -}}
{{- end -}}
{{- end -}}

{{- define "score-service.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "score-service.labels" -}}
helm.sh/chart: {{ include "score-service.chart" . }}
app.kubernetes.io/name: {{ include "score-service.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: bodybuddy
app.kubernetes.io/component: api
{{- end -}}

{{- define "score-service.selectorLabels" -}}
app.kubernetes.io/name: {{ include "score-service.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "score-service.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "score-service.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}
