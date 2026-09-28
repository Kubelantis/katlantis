{{- define "atlantis.name" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "atlantis.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "atlantis.selectorLabels" -}}
app.kubernetes.io/name: {{ include "atlantis.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "atlantis.labels" -}}
{{ include "atlantis.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "atlantis.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "atlantis.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/* An external_stores s3 block for one store. */}}
{{- define "atlantis.s3store" -}}
type: s3
s3:
  bucket: {{ required "externalStores.*.bucket is required" .bucket | quote }}
  region: {{ required "externalStores.*.region is required" .region | quote }}
  {{- with .prefix }}
  prefix: {{ . | quote }}
  {{- end }}
  {{- with .endpoint }}
  endpoint: {{ . | quote }}
  {{- end }}
  {{- if .forcePathStyle }}
  force_path_style: true
  {{- end }}
  {{- with .serverSideEncryption }}
  server_side_encryption: {{ . | quote }}
  {{- end }}
  {{- with .kmsKeyId }}
  kms_key_id: {{ . | quote }}
  {{- end }}
{{- end -}}

{{- define "atlantis.externalStoresEnabled" -}}
{{- if or .Values.externalStores.planStore.enabled .Values.externalStores.logStore.enabled }}true{{ end -}}
{{- end -}}

{{- define "atlantis.tokenSecretName" -}}
{{- default (printf "%s-cluster" (include "atlantis.fullname" .)) .Values.cluster.tokenSecretName -}}
{{- end -}}
