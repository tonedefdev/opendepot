---
tags:
  - authentication
  - registry-explorer
  - ui
---

# Registry Explorer UI Authentication

The Registry Explorer UI has a browser-based authentication flow separate from
the `tofu login` CLI flow. The UI registers its own OIDC client and issues its
own access tokens.

For UI deployment, routing, session secrets, and client registration, see the
[Registry Explorer guide](../guides/registry-explorer/index.md).

## Sign In and Sign Out

When `ui.oidc.enabled: true`, the Sidebar displays a **Sign in** button. It
redirects the browser to `/auth/login`, which starts the OIDC authorization code
flow. After the identity provider redirects back, the UI stores the access token
in an encrypted server-side session cookie.

The Sidebar footer shows the authenticated user's display name from the `name`
or `preferred_username` JWT claim and a **Sign out** button. Sign-out calls
`/auth/logout` and clears the session cookie.

## Session Token Forwarding

The UI is a Next.js application with server components. On each page render,
the server component reads the access token from the session cookie and forwards
it as an `Authorization: Bearer <token>` header to browse API calls under
`/opendepot/ui/v1/*`.

The Server evaluates the token against `GroupBinding` resources. Unauthenticated
users receive only publicly labelled resources.

## Provider Unavailability

If the OIDC provider is unreachable when a user attempts to sign in,
`/auth/login` returns `HTTP 503`. Load balancers, health checks, and monitoring
can distinguish provider unavailability from an application error (`HTTP 500`).

## Developer Token Input

!!! danger "Local development only"
    Set `ui.auth.devTokenInput.enabled: false` in every production environment.

When enabled, the Sidebar shows a **Dev Bearer Token** input. A developer can
paste a raw Kubernetes ServiceAccount token, such as the output of
`kubectl create token`, into the field. The UI posts it to `/auth/dev-token`
and stores it as `devToken` in the encrypted session. Subsequent server renders
prefer it over the OIDC access token. Submit an empty value to clear it.

See [Developer Token Input](../guides/registry-explorer/index.md#enabling-the-ui)
for Helm configuration details.
