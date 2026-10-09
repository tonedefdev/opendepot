import * as React from "react";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Chip, { type ChipProps } from "@mui/material/Chip";
import IconButton from "@mui/material/IconButton";
import LinearProgress from "@mui/material/LinearProgress";
import Tooltip from "@mui/material/Tooltip";
import Typography from "@mui/material/Typography";
import InfoOutlinedIcon from "@mui/icons-material/InfoOutlined";
import type { JevAssessment, JevThresholds } from "@/lib/api";

interface JevPanelProps {
  assessment: JevAssessment;
  thresholds?: JevThresholds;
}

interface ProbabilityRow {
  label: string;
  description: string;
  value?: number;
  // "max" rows breach when the value exceeds the limit; "min" rows breach when it falls below.
  direction: "max" | "min";
  limit?: number;
}

const RISK_COLORS: Record<string, "success" | "info" | "warning" | "error"> = {
  Minimal: "success",
  Low: "info",
  Moderate: "warning",
  High: "error",
  Critical: "error",
};

function formatPercent(value: number): string {
  return `${(value * 100).toFixed(1)}%`;
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

export default function JevPanel({ assessment, thresholds }: JevPanelProps) {
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

      {assessment.blocked && (
        <Alert severity="error">
          Blocked by Jev policy
          {assessment.blockReasons && assessment.blockReasons.length > 0 && `: ${assessment.blockReasons.join("; ")}`}
        </Alert>
      )}

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
            description="How certain the model is about the risk level, from 0% to 100%. Lower confidence means the rating is less reliable. It turns red when it falls below the policy's minConfidence, which marks the version for review."
            size="small"
            variant="outlined"
            color={confidenceLimit !== undefined && assessment.riskConfidence < confidenceLimit ? "error" : "default"}
          />
        )}
        {assessment.needsReview && (
          <InfoChip
            label="Needs review"
            description="A person should check this version before relying on it. It is set when confidence falls below the policy's minConfidence. It does not block the version on its own."
            size="small"
            color="warning"
          />
        )}
        {assessment.model && (
          <Typography variant="caption" color="text.secondary" sx={{ fontFamily: "monospace" }}>
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
                <Typography variant="body2" sx={{ fontFamily: "monospace", color: breached ? "error.main" : "text.primary" }}>
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
                color={breached ? "error" : "primary"}
                sx={{ height: 6, borderRadius: 3 }}
              />
            </Box>
          );
        })}
      </Box>

      {assessment.blockReasons && assessment.blockReasons.length > 0 && !assessment.blocked && (
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
