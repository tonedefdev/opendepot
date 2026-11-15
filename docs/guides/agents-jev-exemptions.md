# Jev Digest, Exemptions, and Retries

Read the Jev digest for a Version, clear a Jev block with an exemption, and understand how failed Jev calls are retried. For how Jev is enabled and scored, see [Jev assessment](agents-jev.md).

## Reading the digest

Each fresh Jev assessment records the SHA-256 of the archive it assessed in `status.jevAssessment.contentDigest`. A `jevExemptions` entry uses this value. Read it with `kubectl`:

```bash
kubectl get version <version-name> \
  --namespace opendepot-system \
  --output jsonpath='{.status.jevAssessment.contentDigest}'
```

The Jev panel shows the same digest with a copy button.

`status.checksum` holds the same SHA-256 bytes as base64. `contentDigest` holds them as lowercase hex. To convert `status.checksum`:

```bash
kubectl get version <version-name> \
  --namespace opendepot-system \
  --output jsonpath='{.status.checksum}' | base64 --decode | xxd -p -c 64
```

A legacy assessment, recorded before `contentDigest` existed, has no digest, so no exemption can match it. Set `forceSync: true` on the Version once, as on line 282. The new assessment records the digest.

## Exemptions

A `ScanPolicy` clears a Jev block with a `jevExemptions` entry. Each entry matches one exact archive digest. For the fields, see [Jev exemptions with ScanPolicy](../configuration/scanning.md#jev-exemptions-with-scanpolicy).

**What an exemption clears.** A matching entry clears every Jev block reason for the exact digest. That covers the verdict reasons (`Critical`, each of the eight thresholds, and `minConfidence`) and the coverage reasons: skipped files, the token limit, content that needs more than 16 parts, a failed part, retries that are exhausted, an unreadable token Secret, a read error on the extracted content, and Jev disabled on the controller. An exemption does not change `needsReview`. The Workshop Needs review queue and the Stats Needs review count leave out a Version while its exemption is active, so a Version that is still flagged shows up again once `exemptionExpires` passes. An exemption with no expiry stays out of the queue until it is removed.

**When the Jev call failed.** If the assessment ends with a Service error, the exemption is still recorded. `exempted` is `true` and `blocked` is `false`. The Version still stays unsynced with `status.synced: false` and a `JevUnavailable:` sync status. It publishes only after a Jev run completes without an error. When retries are exhausted, the Version stays unsynced until you set `forceSync: true` or change the spec. Check `status.synced` and `syncStatus` before you treat a Version as published.

**Entries are not tied to the reasons they were granted for.** An entry clears whatever reasons the assessment has while the digest matches. If you tighten `jevPolicy` and then force a sync, the same entry also clears the new reasons, as long as the archive digest is unchanged.

**The digest depends on the toolchain and the packing code.** Archives are packed with a deterministic tar and gzip writer that uses the Go `compress/flate` implementation. A Go toolchain upgrade, or a change to how archives are packed, produces a new digest. The old entry no longer matches, so the Version blocks again. Add an entry for the new digest after you review the assessment.

**Audit.** Use these to see which entries apply:

- `status.jevAssessment` fields: `exempted`, `exemptedBy`, `exemptionReason`, `exemptionExpires`, `blockReasons`, and `blocked`.
- `syncStatus` on a published exempted Version names the policy, the number of cleared reasons, the first 12 characters of the digest, and the expiry.
- Events: `JevExemptionApplied` (Normal) when an entry starts to apply, and `JevExemptionRevoked` (Warning) when it stops. Events are emitted on a change only. The revoked message does not say why the entry stopped applying, so check the policy's `expires` and its exemption list. Run `kubectl describe version <version-name>` to view them. `JevExemptionApplied` also appears when the Jev call failed and the Version stays unsynced.
- ScanPolicy `status.activeJevExemptions` and `status.expiredJevExemptions` count the entries.

**Branch sources.** A Rejected branch Version that has no assigned version is re-opened when the winning ScanPolicy has an unexpired `jevExemptions` entry for its stored digest. The controller downloads the archive again. It checks the digest against the entry after the download, so only the exact archive publishes.

## Retries

A Service failure is retried every 15 seconds. The controller counts each failure in controller memory, keyed by the Version, its generation, and its archive checksum, and copies the count to `status.jevAssessment.retryCount`. The count does not depend on the status write, so retries stop even when the API server rejects that write. Retries stop once the count exceeds `jevMaxRetries`. With the default of `3`, a Version gets 4 attempts. After that, the error stays recorded until you set `forceSync: true` or change the spec. Either one bumps the generation and starts a new count. A controller restart also clears the in-memory count, and the next run starts again from the stored `retryCount`. Each attempt makes at most 16 Jev requests.

A `401` or `403` from Jev is retried every 15 seconds and is not counted. These retries continue until the token is fixed.

Set `scanning.jev.maxRetries` to change the limit. It maps to the `--jev-max-retries` flag on the `version` controller.

## Next steps

- [Jev assessment](agents-jev.md): enable Jev and review how Versions are scored.
- [Scanning configuration](../configuration/scanning.md): ScanPolicy fields, including `jevExemptions`.
