# Stage 1: Build the MkDocs site from the repository docs.
FROM python:3.13-slim-bookworm@sha256:a5a7017efc5969e00ae2b91c8eb0631049a569652aa4ffc4501769049ae45c72 AS builder

# mkdocs-material's imaging extra links against cairo at runtime.
RUN apt-get update && \
    apt-get install -y --no-install-recommends libcairo2 && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

COPY mkdocs.yml ./
COPY docs ./docs
COPY overrides ./overrides
# The build context has no .git, so the git-revision-date plugin would stamp build time on every page.
# Drop that plugin from a build-time copy of mkdocs.yml. --strict still applies to everything else.
RUN sed '/^  - git-revision-date-localized:/,/fallback_to_build_date: true/d' mkdocs.yml > mkdocs.image.yml && \
    ! grep -q 'git-revision-date-localized' mkdocs.image.yml && \
    mkdocs build --strict -f mkdocs.image.yml -d /site

# Stage 2: Runtime — unprivileged NGINX serving the static site under /docs/.
FROM nginx:1.29.8-alpine3.23@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de AS runner

ARG APK_PATCH_STAMP=unset
RUN echo "apk patch stamp: ${APK_PATCH_STAMP}" && apk upgrade --no-cache

COPY services/docs/nginx.conf /etc/nginx/nginx.conf
# The UI NGINX forwards /docs/* without rewriting it, so the site lives under /docs here too.
COPY --from=builder /site /usr/share/nginx/html/docs

EXPOSE 8080

USER 101

ENTRYPOINT ["nginx", "-g", "daemon off;"]
