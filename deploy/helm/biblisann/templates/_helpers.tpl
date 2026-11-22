{{- define "biblisann.name" -}}
{{- .Chart.Name -}}
{{- end -}}

{{- define "biblisann.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "biblisann.labels" -}}
app.kubernetes.io/name: {{ include "biblisann.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "biblisann.selectorLabels" -}}
app.kubernetes.io/name: {{ include "biblisann.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "biblisann.image" -}}
{{- if .Values.image.digest -}}
{{- printf "%s@%s" .Values.image.repository .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) -}}
{{- end -}}
{{- end -}}

{{/* Name of the CNPG cluster and of the Secret CNPG creates for the application user. */}}
{{- define "biblisann.cnpgName" -}}
{{- printf "%s-db" (include "biblisann.fullname" .) -}}
{{- end -}}

{{- define "biblisann.dbSecretName" -}}
{{- if eq .Values.database.mode "cnpg" -}}
{{- printf "%s-app" (include "biblisann.cnpgName" .) -}}
{{- else -}}
{{- required "database.external.existingSecret is required when database.mode=external" .Values.database.external.existingSecret -}}
{{- end -}}
{{- end -}}

{{- define "biblisann.dbSecretKey" -}}
{{- if eq .Values.database.mode "cnpg" -}}uri{{- else -}}{{ .Values.database.external.key }}{{- end -}}
{{- end -}}
