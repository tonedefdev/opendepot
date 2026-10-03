{{- define "opendepot.registryHost" -}}
{{- $baseURL := required "ui.baseUrl is required when assembly.enabled=true" .Values.ui.baseUrl -}}
{{- $parsed := urlParse $baseURL -}}
{{- $scheme := get $parsed "scheme" -}}
{{- $host := get $parsed "host" -}}
{{- if and (ne $scheme "http") (ne $scheme "https") -}}
{{- fail "ui.baseUrl must use an http or https scheme when assembly.enabled=true" -}}
{{- end -}}
{{- if not $host -}}
{{- fail "ui.baseUrl must include a valid host when assembly.enabled=true" -}}
{{- end -}}
{{- $host -}}
{{- end -}}
