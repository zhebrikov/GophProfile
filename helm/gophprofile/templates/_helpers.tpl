{{- define "gophprofile.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "gophprofile.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "gophprofile.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "gophprofile.labels" -}}
helm.sh/chart: {{ include "gophprofile.chart" . }}
{{ include "gophprofile.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "gophprofile.selectorLabels" -}}
app.kubernetes.io/name: {{ include "gophprofile.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "gophprofile.server.labels" -}}
{{ include "gophprofile.labels" . }}
app.kubernetes.io/component: server
app: gophprofile-server
{{- end }}

{{- define "gophprofile.server.selectorLabels" -}}
{{ include "gophprofile.selectorLabels" . }}
app.kubernetes.io/component: server
app: gophprofile-server
{{- end }}

{{- define "gophprofile.worker.labels" -}}
{{ include "gophprofile.labels" . }}
app.kubernetes.io/component: worker
app: gophprofile-worker
{{- end }}

{{- define "gophprofile.worker.selectorLabels" -}}
{{ include "gophprofile.selectorLabels" . }}
app.kubernetes.io/component: worker
app: gophprofile-worker
{{- end }}

{{- define "gophprofile.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "gophprofile.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "gophprofile.secretName" -}}
{{- if .Values.existingSecret }}
{{- .Values.existingSecret }}
{{- else }}
{{- printf "%s-secrets" (include "gophprofile.fullname" .) }}
{{- end }}
{{- end }}

{{- define "gophprofile.configMapName" -}}
{{- printf "%s-config" (include "gophprofile.fullname" .) }}
{{- end }}

{{- define "gophprofile.postgres.fullname" -}}
{{- printf "%s-postgres" (include "gophprofile.fullname" .) }}
{{- end }}

{{- define "gophprofile.minio.fullname" -}}
{{- printf "%s-minio" (include "gophprofile.fullname" .) }}
{{- end }}

{{- define "gophprofile.rabbitmq.fullname" -}}
{{- printf "%s-rabbitmq" (include "gophprofile.fullname" .) }}
{{- end }}

{{- define "gophprofile.databaseURL" -}}
{{- if .Values.infra.enabled -}}
postgres://{{ .Values.infra.postgres.auth.user }}:{{ .Values.infra.postgres.auth.password }}@{{ include "gophprofile.postgres.fullname" . }}:5432/{{ .Values.infra.postgres.auth.database }}?sslmode=disable
{{- else -}}
{{- .Values.secrets.databaseURL -}}
{{- end -}}
{{- end }}

{{- define "gophprofile.brokerURL" -}}
{{- if .Values.infra.enabled -}}
amqp://guest:guest@{{ include "gophprofile.rabbitmq.fullname" . }}:5672/
{{- else -}}
{{- .Values.secrets.brokerURL -}}
{{- end -}}
{{- end }}

{{- define "gophprofile.s3Endpoint" -}}
{{- if .Values.infra.enabled -}}
{{ include "gophprofile.minio.fullname" . }}:9000
{{- else -}}
{{- .Values.config.s3Endpoint -}}
{{- end -}}
{{- end }}

{{/*
Init container that applies DB migrations before the app starts.
Safe with concurrent pods: goose uses Postgres advisory locks.
Covers the post-install Job race on first install when infra is in-chart.
*/}}
{{- define "gophprofile.migrateInitContainer" -}}
- name: migrate
  image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
  imagePullPolicy: {{ .Values.image.pullPolicy }}
  command: ["/app/migrate", "-command", "up", "-dir", "/app/migrations"]
  env:
    - name: DATABASE_URL
      valueFrom:
        secretKeyRef:
          name: {{ include "gophprofile.secretName" . }}
          key: DATABASE_URL
  securityContext:
    {{- toYaml .Values.containerSecurityContext | nindent 4 }}
  resources:
    requests:
      cpu: 50m
      memory: 64Mi
    limits:
      cpu: 200m
      memory: 128Mi
{{- end }}
