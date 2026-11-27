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
opendepot.ociBuilds.validate fails the render when in-cluster OCI builds are enabled without their prerequisites.
Builds require oci.enabled, and when oci.builds.requireScanning is true they also require scanning.enabled.
*/}}
{{- define "opendepot.ociBuilds.validate" -}}
{{- if .Values.oci.builds.enabled -}}
{{- if not .Values.oci.enabled -}}
{{- fail "oci.builds.enabled=true requires oci.enabled=true" -}}
{{- end -}}
{{- if and .Values.oci.builds.requireScanning (not .Values.scanning.enabled) -}}
{{- fail "oci.builds.requireScanning=true requires scanning.enabled=true" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
opendepot.ociBuilds.helperImage resolves the image for the build fetch and upload helpers. It is oci.builds.helperImage
when set, otherwise the version-controller image. The version-deployment flag and the ValidatingAdmissionPolicy both use
this helper so they always agree on the same reference.
*/}}
{{- define "opendepot.ociBuilds.helperImage" -}}
{{- if .Values.oci.builds.helperImage -}}
{{- .Values.oci.builds.helperImage -}}
{{- else -}}
{{- $tag := toString (default (default .Chart.AppVersion .Values.global.image.tag) .Values.version.image.tag) -}}
{{- printf "%s:%s" .Values.version.image.repository $tag -}}
{{- end -}}
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
