import * as React from "react";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Chip, { type ChipProps } from "@mui/material/Chip";
import IconButton from "@mui/material/IconButton";
import LinearProgress from "@mui/material/LinearProgress";
import Tooltip from "@mui/material/Tooltip";
import Typography from "@mui/material/Typography";
import AttachMoneyOutlinedIcon from "@mui/icons-material/AttachMoneyOutlined";
import InfoOutlinedIcon from "@mui/icons-material/InfoOutlined";
import VerifiedUserOutlinedIcon from "@mui/icons-material/VerifiedUserOutlined";
import Link from "next/link";
import type { JevAssessment, JevThresholds, ScanPolicyTargetKind } from "@/lib/api";
import { RISK_COLORS, probabilitySeverity } from "@/lib/jevSeverity";
import { JEV_INPUT_USD_PER_MILLION_TOKENS, formatJevInputCost } from "@/lib/jevCost";
import CopyButton from "./CopyButton";
import JevChunkTree from "./JevChunkTree";
import type { JevUnblockView } from "./jevUnblock";

// JevExemptionAction offers the Security Policy editor prefilled with a blocked digest. canEdit comes from the server-side capability check.
// targetKind and targetName scope the prefilled policy to this resource.
export interface JevExemptionAction {
  namespace: string;
  versionName?: string;
  targetKind?: ScanPolicyTargetKind;
  targetName?: string;
  canEdit: boolean;
  // governingPolicyHidden is set when browse returned no governing policy name, which is always the case for OIDC callers.
  governingPolicyHidden?: boolean;
}

interface JevPanelProps {
  assessment: JevAssessment;
  thresholds?: JevThresholds;
  exemption?: JevExemptionAction;
  unblock?: JevUnblockView | null;
  // namesHidden mirrors the server's jevUnblock.namesHidden. The unblock view drops it in most states, so the panel takes it directly.
  namesHidden?: boolean;
}

// JEV_MAX_PARTS is the most parts one Version can be scored in. Content needing more is blocked before any Jev call.
const JEV_MAX_PARTS = 16;

interface ProbabilityRow {
  label: string;
  description: string;
  value?: number;
  // "max" rows breach when the value exceeds the limit; "min" rows breach when it falls below.
  direction: "max" | "min";
  limit?: number;
}

function formatPercent(value: number): string {
  return `${(value * 100).toFixed(1)}%`;
}

function describeExemption(assessment: JevAssessment): string {
  const expiry = assessment.exemptionExpires
    ? `Expires ${new Date(assessment.exemptionExpires).toLocaleString()}`
    : "No expiry";
  return `Reason: ${assessment.exemptionReason ?? "none given"}. ${expiry}.`;
}

function formatLimit(row: ProbabilityRow): string | undefined {
  if (row.limit === undefined) return undefined;
  return row.direction === "min" ? `min ${formatPercent(row.limit)}` : `max ${formatPercent(row.limit)}`;
}

function isBreached(row: ProbabilityRow & { value: number }): boolean {
  if (row.limit === undefined) return false;
  return row.direction === "min" ? row.value < row.limit : row.value > row.limit;
}

interface InfoHintProps {
  label: string;
  description: string;
}

function InfoHint({ label, description }: InfoHintProps) {
  return (
    <Tooltip title={description} placement="top" describeChild>
      <IconButton size="small" aria-label={`About ${label}`} sx={{ p: 0.25, color: "text.secondary" }}>
        <InfoOutlinedIcon sx={{ fontSize: 16 }} />
      </IconButton>
    </Tooltip>
  );
}

interface InfoChipProps extends ChipProps {
  description: string;
}

function InfoChip({ description, ...chipProps }: InfoChipProps) {
  return (
    <Tooltip title={description} placement="top" describeChild>
      <Chip tabIndex={0} sx={{ cursor: "help" }} {...chipProps} />
    </Tooltip>
  );
}

// forceSyncCommand re-runs the Version's assessment. A missing Version name keeps a placeholder rather than guessing one.
function forceSyncCommand(namespace: string, versionName?: string): string {
  return `kubectl patch version ${versionName ?? "<name>"} -n ${namespace} --type merge -p '{"spec":{"forceSync":true}}'`;
}

function formatExpiry(value: string): string {
  return new Date(value).toLocaleString();
}

function CopyableValue({ value }: { value: string }) {
  return (
    <Box sx={{ display: "flex", alignItems: "center", gap: 0.5, minWidth: 0 }}>
      <Typography variant="caption" sx={{ fontFamily: "var(--od-font-mono)", wordBreak: "break-all" }}>
        {value}
      </Typography>
      <CopyButton value={value} />
    </Box>
  );
}

// JevUnblockSection states the one condition that stops a blocked assessment from being exempted, and the exemption state.
function JevUnblockSection({ unblock, versionName }: { unblock: JevUnblockView; versionName?: string }) {
  if (unblock.state === "exempted") {
    return (
      <Alert severity="success" variant="outlined">
        {`${unblock.policy ? `Exempted by ${unblock.policy}` : "Exempted by the governing ScanPolicy"}. ${unblock.expires ? `Expires ${formatExpiry(unblock.expires)}.` : "No expiry."}`}
      </Alert>
    );
  }

  return (
    <Alert severity={unblock.state === "unknown" ? "warning" : "error"} variant="outlined">
      <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
        <Typography variant="subtitle2">Unblock</Typography>
        {unblock.state === "no-digest" && (
          <>
            <Typography variant="body2">
              This assessment has no digest, so no exemption can match it. Set forceSync: true on this Version, then return here.
            </Typography>
            <CopyableValue value={forceSyncCommand(unblock.namespace, versionName)} />
          </>
        )}
        {unblock.state === "needs-entry" && (
          <>
            <Typography variant="body2">
              {unblock.policy
                ? `Add a Jev exemption for this digest to ScanPolicy ${unblock.policy}. It is the policy that governs this Version.`
                : unblock.namesHidden
                  ? "Policy names are hidden for this account, so the governing policy cannot be named. Add a Jev exemption for this digest to the ScanPolicy that governs this Version. Create Jev exemption opens a new policy, and the editor reports which policy governs this Version."
                  : "No ScanPolicy currently governs this Version, so a Jev exemption entry has no effect until a policy with a matching target governs it."}
            </Typography>
            <CopyableValue value={unblock.digest} />
          </>
        )}
        {unblock.state === "superseded" && (
          <>
            <Typography variant="body2">
              {unblock.policy && unblock.winner
                ? `ScanPolicy ${unblock.policy} has a Jev exemption for this digest, but it is ignored. ${unblock.winner} (priority ${unblock.winnerPriority}) wins this Version over ${unblock.policy} (priority ${unblock.policyPriority}), and a shadowed policy's exemptions are ignored in full. Raise ${unblock.policy} to a priority above ${unblock.winnerPriority}, or add the exemption to ${unblock.winner}.`
                : `A Jev exemption for this digest is in a policy that does not govern this Version, so it is ignored. Policy names are hidden for this account. The governing policy has priority ${unblock.winnerPriority} and the policy with the exemption has priority ${unblock.policyPriority}.`}
            </Typography>
            <CopyableValue value={unblock.digest} />
          </>
        )}
        {unblock.state === "expired" && (
          <>
            <Typography variant="body2">
              {`The Jev exemption for this digest in ${unblock.policy ? `ScanPolicy ${unblock.policy}` : "a ScanPolicy"} expired${unblock.expires ? ` at ${formatExpiry(unblock.expires)}` : ""}. Expired exemptions are ignored. Add a new entry with a later expires, or remove expires to make it permanent.`}
            </Typography>
            <CopyableValue value={unblock.digest} />
          </>
        )}
        {unblock.state === "pending" && (
          <>
            <Typography variant="body2">
              {`The Jev exemption for this digest is active in ${unblock.policy ? `ScanPolicy ${unblock.policy}` : "the ScanPolicy"}, which governs this Version, but the Version has not been re-evaluated since. Set forceSync: true on this Version to re-run the check.`}
            </Typography>
            <CopyableValue value={forceSyncCommand(unblock.namespace, versionName)} />
          </>
        )}
        {unblock.state === "unknown" && (
          <>
            <Typography variant="body2">The unblock state for this Version cannot be determined from the server&apos;s response.</Typography>
            {unblock.expires && <Typography variant="body2">{`Expiry: ${formatExpiry(unblock.expires)}.`}</Typography>}
            {unblock.digest && <CopyableValue value={unblock.digest} />}
          </>
        )}
      </Box>
    </Alert>
  );
}

export default function JevPanel({ assessment, thresholds, exemption, unblock, namesHidden }: JevPanelProps) {
  const allRows: ProbabilityRow[] = [
    {
      label: "Safe",
      description: "Model probability that this skill or agent is safe to use. Higher is safer.",
      value: assessment.safeProbability,
      direction: "min",
      limit: thresholds?.minSafeProbability,
    },
    {
      label: "Injection",
      description: "Model probability that this skill or agent contains instructions an AI model would follow from untrusted input, such as fetched pages, issues, or files.",
      value: assessment.injectionProbability,
      direction: "max",
      limit: thresholds?.maxInjectionProbability,
    },
    {
      label: "Exfiltration",
      description: "Model probability that this skill or agent directs the agent to send data, files, or credentials to an external destination.",
      value: assessment.exfiltrationProbability,
      direction: "max",
      limit: thresholds?.maxExfiltrationProbability,
    },
    {
      label: "Destructive actions",
      description: "Model probability that this skill or agent directs the agent to take destructive or irreversible actions, such as deleting data or force-pushing, without asking the user first.",
      value: assessment.destructiveProbability,
      direction: "max",
      limit: thresholds?.maxDestructiveProbability,
    },
    {
      label: "Hidden instructions",
      description: "Model probability that this skill or agent contains hidden or obfuscated instructions, such as encoded or invisible text, or instructions to conceal actions from the user.",
      value: assessment.hiddenInstructionsProbability,
      direction: "max",
      limit: thresholds?.maxHiddenInstructionsProbability,
    },
    {
      label: "Scope mismatch",
      description: "Model probability that this skill or agent asks the agent to do things beyond what its description says it does.",
      value: assessment.scopeMismatchProbability,
      direction: "max",
      limit: thresholds?.maxScopeMismatchProbability,
    },
    {
      label: "Remote execution",
      description: "Model probability that this skill or agent directs the agent to download and run remote code or scripts, or to install unpinned dependencies.",
      value: assessment.remoteExecutionProbability,
      direction: "max",
      limit: thresholds?.maxRemoteExecutionProbability,
    },
  ];
  const rows = allRows.filter((row): row is ProbabilityRow & { value: number } => row.value !== undefined);

  const riskLimit = thresholds?.maxRiskScore;
  const confidenceLimit = thresholds?.minConfidence;
  const riskLevelColor = assessment.riskLevel ? RISK_COLORS[assessment.riskLevel] ?? "default" : "default";

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
      <Alert severity="info" variant="outlined">
        <Typography variant="body2">
          Skill or agent content was sent to TypeSafe for this assessment. The scores are model probabilities, not a certification or an OpenDepot verdict.
        </Typography>
      </Alert>

      {assessment.error && <Alert severity="error">Assessment error: {assessment.error}</Alert>}

      {assessment.chunkCount !== undefined && assessment.chunkCount > JEV_MAX_PARTS && (assessment.chunks ?? []).length === 0 && (
        <Alert severity="error">
          {`Content needs at least ${assessment.chunkCount} parts; the limit is ${JEV_MAX_PARTS}; no Jev calls were made`}
        </Alert>
      )}

      {assessment.blocked && (
        <Alert severity="error">
          Blocked by Jev
          {!assessment.exempted && assessment.blockReasons && assessment.blockReasons.length > 0 && `: ${assessment.blockReasons.join("; ")}`}
        </Alert>
      )}

      {assessment.contentDigest && (
        <Box sx={{ display: "flex", alignItems: "center", gap: 0.5, minWidth: 0 }}>
          <Typography variant="caption" color="text.secondary">Digest</Typography>
          <Typography variant="caption" sx={{ fontFamily: "var(--od-font-mono)", wordBreak: "break-all" }}>
            {assessment.contentDigest}
          </Typography>
          <CopyButton value={assessment.contentDigest} />
        </Box>
      )}

      {assessment.blocked && assessment.contentDigest && exemption && (
        <Box sx={{ display: "flex", flexDirection: "column", alignItems: "flex-start", gap: 0.5 }}>
          {exemption.canEdit ? (
            <Button
              component={Link}
              href={`/security-policies/new?${new URLSearchParams({
                namespace: exemption.namespace,
                jevDigest: assessment.contentDigest,
                ...(exemption.versionName ? { jevVersion: exemption.versionName } : {}),
                ...(exemption.targetKind && exemption.targetName ? { jevKind: exemption.targetKind, jevName: exemption.targetName } : {}),
                ...(exemption.governingPolicyHidden ? { jevGoverningHidden: "true" } : {}),
              })}`}
              size="small"
              variant="outlined"
              startIcon={<VerifiedUserOutlinedIcon fontSize="small" />}
            >
              Create Jev exemption
            </Button>
          ) : (
            <>
              <Button size="small" variant="outlined" disabled startIcon={<VerifiedUserOutlinedIcon fontSize="small" />}>
                Create Jev exemption
              </Button>
              <Typography variant="caption" color="text.secondary">
                Only namespace-wide policy managers can create Jev exemptions in {exemption.namespace}.
              </Typography>
            </>
          )}
        </Box>
      )}

      {unblock && <JevUnblockSection unblock={unblock} versionName={assessment.versionName} />}

      <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1, alignItems: "center" }}>
        {assessment.riskLevel && (
          <InfoChip
            label={`Risk: ${assessment.riskLevel}`}
            description="How much harm this skill or agent could cause if an AI agent follows it. The level shown is the one the model rated most likely, on a scale from Minimal to Critical."
            size="small"
            color={riskLevelColor}
            variant="outlined"
          />
        )}
        {assessment.riskScore !== undefined && (
          <InfoChip
            label={`Risk score: ${assessment.riskScore.toFixed(2)}`}
            description="Where the risk falls on a 0 to 4 scale, from Minimal (0) to Critical (4). Fractional scores fall between levels. It turns red when it exceeds the policy's maxRiskScore, which blocks the version."
            size="small"
            variant="outlined"
            color={riskLimit !== undefined && assessment.riskScore > riskLimit ? "error" : "default"}
          />
        )}
        {assessment.riskConfidence !== undefined && (
          <InfoChip
            label={`Confidence: ${formatPercent(assessment.riskConfidence)}`}
            description="How certain the model is about the risk level, from 0% to 100%. Lower confidence means the rating is less reliable. When the policy sets minConfidence, a part below it blocks the version and marks it for review. It turns red when it falls below that limit."
            size="small"
            variant="outlined"
            color={confidenceLimit !== undefined && assessment.riskConfidence < confidenceLimit ? "error" : "default"}
          />
        )}
        {assessment.inputTokens !== undefined && assessment.inputTokens > 0 && (
          <InfoChip
            label={`Cost: ${formatJevInputCost(assessment.inputTokens)}`}
            description={`Estimated Jev input cost: the input tokens Jev reported for every part it processed, ${assessment.inputTokens.toLocaleString("en-US")} in total, at $${JEV_INPUT_USD_PER_MILLION_TOKENS} per million input tokens. Jev does not charge for output tokens.`}
            icon={<AttachMoneyOutlinedIcon />}
            size="small"
            variant="outlined"
            color="success"
            sx={{
              cursor: "help",
              "& .MuiChip-icon": { fontSize: 16, ml: 0.5, mr: -0.125 },
              "& .MuiChip-label": { pl: 0 },
            }}
          />
        )}
        {assessment.exempted && (
          <InfoChip
            label={assessment.exemptedBy && namesHidden !== true ? `Exempted by ${assessment.exemptedBy}` : "Exempted"}
            description={describeExemption(assessment)}
            size="small"
            color="info"
            variant="outlined"
          />
        )}
        {assessment.needsReview && (
          <InfoChip
            label="Needs review"
            description="A person should check this version before relying on it. It is set when the risk level is Critical, and when confidence falls below the policy's minConfidence, which also blocks the version. An exemption does not clear it."
            size="small"
            color="warning"
          />
        )}
        {assessment.model && (
          <Typography variant="caption" color="text.secondary" sx={{ fontFamily: "var(--od-font-mono)" }}>
            Model: {assessment.model}
          </Typography>
        )}
      </Box>

      <Box sx={{ display: "flex", flexDirection: "column", gap: 1.25 }}>
        {rows.map((row) => {
          const breached = isBreached(row);
          const limitText = formatLimit(row);
          return (
            <Box key={row.label}>
              <Box sx={{ display: "flex", justifyContent: "space-between", gap: 1, mb: 0.5 }}>
                <Box sx={{ display: "flex", alignItems: "center", gap: 0.5, minWidth: 0 }}>
                  <Typography variant="body2">{row.label}</Typography>
                  <InfoHint label={row.label} description={row.description} />
                </Box>
                <Typography variant="body2" sx={{ fontFamily: "var(--od-font-mono)", color: breached ? "error.main" : "text.primary" }}>
                  {formatPercent(row.value)}
                  {limitText && (
                    <Typography component="span" variant="caption" color="text.secondary" sx={{ ml: 1 }}>
                      ({limitText})
                    </Typography>
                  )}
                </Typography>
              </Box>
              <LinearProgress
                variant="determinate"
                value={Math.min(100, Math.max(0, row.value * 100))}
                color={breached ? "error" : probabilitySeverity(row.value, row.direction === "min")}
                sx={{ height: 6, borderRadius: 3 }}
              />
            </Box>
          );
        })}
      </Box>

      <JevChunkTree assessment={assessment} />

      {assessment.exempted && assessment.blockReasons && assessment.blockReasons.length > 0 && (
        <Typography variant="caption" color="text.secondary">
          Exempted block reasons: {assessment.blockReasons.join("; ")}
        </Typography>
      )}

      {!assessment.exempted && assessment.blockReasons && assessment.blockReasons.length > 0 && !assessment.blocked && (
        <Typography variant="caption" color="text.secondary">
          Flags: {assessment.blockReasons.join("; ")}
        </Typography>
      )}

      <Typography variant="caption" color="text.secondary">
        Evaluated {new Date(assessment.evaluatedAt).toLocaleString()}
      </Typography>
    </Box>
  );
}
