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

{{/*
opendepot.iac.value resolves an iac setting. Precedence is iac.<key>, then module.<key>, then provider.<key>,
then the default passed in. Pass a dict with root (the chart context), key, an optional section for nested
keys such as image.repository, and default. The result is a JSON object with the value under "v", so callers
read it with (fromJson (include "opendepot.iac.value" ...)).v and the type is preserved.
*/}}
{{- define "opendepot.iac.value" -}}
{{- $values := .root.Values.AsMap -}}
{{- $value := .default -}}
{{- range $source := list "provider" "module" "iac" -}}
{{- $scope := dig $source (dict) $values -}}
{{- if $.section -}}
{{- $scope = dig $.section (dict) $scope -}}
{{- end -}}
{{- if hasKey $scope $.key -}}
{{- $value = index $scope $.key -}}
{{- end -}}
{{- end -}}
{{- dict "v" $value | toJson -}}
{{- end -}}
