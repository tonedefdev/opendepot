"use client";

import * as React from "react";
import { useState } from "react";
import { Handle, Position, type NodeProps } from "reactflow";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import IconButton from "@mui/material/IconButton";
import TextField from "@mui/material/TextField";
import Tooltip from "@mui/material/Tooltip";
import Chip from "@mui/material/Chip";
import ChevronRightIcon from "@mui/icons-material/ChevronRight";
import CloseIcon from "@mui/icons-material/Close";
import DataObjectIcon from "@mui/icons-material/DataObject";
import DescriptionOutlinedIcon from "@mui/icons-material/DescriptionOutlined";
import ErrorOutlineIcon from "@mui/icons-material/ErrorOutline";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import type { TypeSpec, ValueSpec, VariableValidation } from "./types";
import { renderTypeSpecCompact } from "./typeSpec";
import VariableModal from "./VariableModal";
import { areAssemblyNodePropsEqual } from "./nodeMemo";

export interface VariableNodeData {
  name: string;
  nameError: string | null;
  validationError: string | null;
  type: TypeSpec;
  description: string;
  validations: VariableValidation[];
  hasDefault: boolean;
  default: ValueSpec;
  onRename: (name: string) => void;
  onTypeChange: (type: TypeSpec) => void;
  onDescriptionChange: (description: string) => void;
  onValidationsChange: (validations: VariableValidation[]) => void;
  onHasDefaultChange: (has: boolean) => void;
  onDefaultChange: (value: ValueSpec) => void;
  onRemove: () => void;
}

const NODE_WIDTH = 280;

function VariableNode({ data }: NodeProps<VariableNodeData>) {
  const {
    name,
    nameError,
    validationError,
    type,
    description,
    validations,
    hasDefault,
    default: defaultValue,
    onRename,
    onTypeChange,
    onDescriptionChange,
    onValidationsChange,
    onHasDefaultChange,
    onDefaultChange,
    onRemove,
  } = data;

  const [typeModalOpen, setTypeModalOpen] = useState(false);
  const attributes = type.kind === "object" ? type.attributes : [];

  return (
    <Box
      sx={{
        width: NODE_WIDTH,
        borderRadius: 2,
        border: "1px solid",
        borderColor: nameError || validationError ? "#f85149" : "transparent",
        bgcolor: "background.paper",
        boxShadow: 3,
        overflow: "hidden",
      }}
    >
      <Box sx={{ px: 1, py: 0.75, bgcolor: "action.hover", display: "flex", alignItems: "center", gap: 0.5 }}>
        <DataObjectIcon sx={{ fontSize: 16, opacity: 0.7, color: "#04cfd0" }} />
        <Typography variant="caption" color="text.secondary">
          var.
        </Typography>
        <TextField
          variant="standard"
          size="small"
          value={name}
          onChange={(e) => onRename(e.target.value)}
          error={!!nameError}
          sx={{ flex: 1, minWidth: 0, "& input": { fontWeight: 700, fontSize: "0.8125rem" } }}
        />
        {description.trim() && (
          <Tooltip
            slotProps={{
              tooltip: {
                sx: {
                  maxWidth: 360,
                  bgcolor: "background.paper",
                  color: "text.primary",
                  border: "1px solid",
                  borderColor: "divider",
                  boxShadow: 4,
                },
              },
            }}
            title={
              <Box
                sx={{
                  maxWidth: 340,
                  color: "text.primary",
                  fontSize: "0.75rem",
                  lineHeight: 1.5,
                  "& p": { m: 0 },
                  "& p:not(:last-child)": { mb: 0.75 },
                  "& ul, & ol": { my: 0.5, pl: 2.5 },
                  "& li": { mb: 0.25 },
                  "& a": { color: "primary.main", textDecoration: "underline" },
                  "& blockquote": { my: 0.5, ml: 0, pl: 1, borderLeft: "2px solid", borderColor: "divider" },
                  "& code": {
                    px: 0.5,
                    py: 0.125,
                    borderRadius: 0.5,
                    bgcolor: "action.selected",
                    color: "warning.main",
                    fontFamily: "monospace",
                    fontSize: "0.6875rem",
                  },
                }}
              >
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{description.trim()}</ReactMarkdown>
              </Box>
            }
          >
            <DescriptionOutlinedIcon aria-label="Variable description" sx={{ fontSize: 16, color: "text.secondary" }} />
          </Tooltip>
        )}
        {validationError && (
          <Tooltip title={validationError}>
            <ErrorOutlineIcon aria-label={validationError} sx={{ fontSize: 16, color: "error.main" }} />
          </Tooltip>
        )}
        <IconButton size="small" onClick={onRemove} aria-label={`Remove ${name}`}>
          <CloseIcon sx={{ fontSize: 14 }} />
        </IconButton>
      </Box>
      {nameError && (
        <Typography variant="caption" sx={{ color: "#f85149", display: "block", px: 1, pt: 0.5 }}>
          {nameError}
        </Typography>
      )}

      <Box
        role="button"
        tabIndex={0}
        onClick={() => setTypeModalOpen(true)}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            setTypeModalOpen(true);
          }
        }}
        sx={{
          display: "flex",
          alignItems: "center",
          gap: 0.75,
          px: 1,
          py: 0.75,
          cursor: "pointer",
          "&:hover": { bgcolor: "action.hover" },
        }}
      >
        <Typography variant="caption" fontWeight={600}>
          Type: {renderTypeSpecCompact(type)}
        </Typography>
        {hasDefault && (
          <Chip label="has default" size="small" color="success" variant="outlined" sx={{ height: 18, fontSize: "0.65rem" }} />
        )}
        <ChevronRightIcon sx={{ fontSize: 16, opacity: 0.6, ml: "auto" }} />
      </Box>

      <Box sx={{ borderTop: "1px solid", borderColor: "divider", py: 0.5 }}>
        <Typography
          variant="caption"
          sx={{ display: "block", px: 1, color: "text.secondary", textTransform: "uppercase", fontSize: "0.6rem", letterSpacing: "0.05em" }}
        >
          Value
        </Typography>
        <Box sx={{ position: "relative", display: "flex", alignItems: "center", justifyContent: "space-between", gap: 0.5, px: 1, py: 0.375 }}>
          <Typography variant="caption" noWrap>
            var.{name || "?"}
          </Typography>
          <Handle
            type="source"
            position={Position.Right}
            id="value"
            isConnectable={false}
            style={{ position: "relative", transform: "none", width: 8, height: 8, background: "#04cfd0" }}
          />
        </Box>
        {attributes.map((attr) => (
          <Box
            key={attr.name}
            sx={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 0.5, px: 1, py: 0.375 }}
          >
            <Tooltip title={attr.type.kind} placement="top">
              <Typography variant="caption" noWrap>
                {attr.name}
              </Typography>
            </Tooltip>
            <Handle
              type="source"
              position={Position.Right}
              id={`attr:${attr.name}`}
              isConnectable={false}
              style={{ position: "relative", transform: "none", width: 8, height: 8, background: "#04cfd0" }}
            />
          </Box>
        ))}
      </Box>

      <VariableModal
        open={typeModalOpen}
        onClose={() => setTypeModalOpen(false)}
        name={name}
        type={type}
        description={description}
        validations={validations}
        hasDefault={hasDefault}
        defaultValue={defaultValue}
        onTypeChange={onTypeChange}
        onDescriptionChange={onDescriptionChange}
        onValidationsChange={onValidationsChange}
        onHasDefaultChange={onHasDefaultChange}
        onDefaultChange={onDefaultChange}
      />
    </Box>
  );
}

export default React.memo(VariableNode, areAssemblyNodePropsEqual);
