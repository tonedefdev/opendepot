"use client";

import * as React from "react";
import Dialog from "@mui/material/Dialog";
import DialogTitle from "@mui/material/DialogTitle";
import DialogContent from "@mui/material/DialogContent";
import DialogActions from "@mui/material/DialogActions";
import IconButton from "@mui/material/IconButton";
import Box from "@mui/material/Box";
import Divider from "@mui/material/Divider";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";
import Button from "@mui/material/Button";
import Tooltip from "@mui/material/Tooltip";
import CloseIcon from "@mui/icons-material/Close";
import CloseFullscreenIcon from "@mui/icons-material/CloseFullscreen";
import DataObjectIcon from "@mui/icons-material/DataObject";
import OpenInFullIcon from "@mui/icons-material/OpenInFull";
import { useColorScheme } from "@mui/material/styles";
import { Highlight, type Language, themes } from "prism-react-renderer";
import Prism from "prismjs";
import "prismjs/components/prism-hcl";
import CopyButton from "@/components/CopyButton";
import type { TypeSpec, ValueSpec, VariableValidation } from "./types";
import { renderVariableSpec } from "./typeSpec";
import TypeEditor from "./TypeEditor";
import ValidationsEditor from "./ValidationsEditor";
import ValueEditor from "./ValueEditor";
import { extendHclGrammar } from "./hclConditionHighlight";

(typeof globalThis !== "undefined" ? globalThis : window).Prism = Prism;
extendHclGrammar(Prism.languages.hcl);

const filledTextFieldSx = {
  "& .MuiFilledInput-root": {
    border: "1px solid transparent",
    borderRadius: 1,
    transition: (theme: { transitions: { create: (properties: string[]) => string } }) =>
      theme.transitions.create(["border-color", "background-color"]),
    "&.Mui-focused": { borderColor: "primary.main" },
    "&.Mui-error": { borderColor: "error.main" },
  },
};

interface Props {
  open: boolean;
  onClose: () => void;
  name: string;
  type: TypeSpec;
  description: string;
  validations: VariableValidation[];
  hasDefault: boolean;
  defaultValue: ValueSpec;
  onTypeChange: (type: TypeSpec) => void;
  onDescriptionChange: (description: string) => void;
  onValidationsChange: (validations: VariableValidation[]) => void;
  onHasDefaultChange: (has: boolean) => void;
  onDefaultChange: (value: ValueSpec) => void;
}

/**
 * The type/default editing form for a Variable node, opened from its
 * compact "Type" summary button rather than rendered inline — a fully
 * expanded map(object({...})) editor plus a live preview makes the node
 * unusably tall otherwise, the same problem InputsModal solves for a
 * module's variables. A Dialog also sidesteps the ReactFlow
 * node-drag-vs-MUI-Select interaction issue entirely (portaled content
 * is outside the node's own DOM subtree, so no `nodrag` class is needed
 * here).
 */
export default function VariableModal({
  open,
  onClose,
  name,
  type,
  description,
  validations,
  hasDefault,
  defaultValue,
  onTypeChange,
  onDescriptionChange,
  onValidationsChange,
  onHasDefaultChange,
  onDefaultChange,
}: Props) {
  const [previewExpanded, setPreviewExpanded] = React.useState(false);
  const code = React.useMemo(
    () => renderVariableSpec(name, type, hasDefault, defaultValue, description, validations),
    [name, type, hasDefault, defaultValue, description, validations],
  );

  const { mode, systemMode } = useColorScheme();
  const resolvedMode = mode === "system" ? systemMode : mode;
  const prismTheme = resolvedMode === "light" ? themes.github : themes.nightOwl;
  const typeEditor = <TypeEditor value={type} onChange={onTypeChange} />;
  const descriptionEditor = (
    <TextField
      variant="filled"
      label="Description"
      value={description}
      onChange={(event) => onDescriptionChange(event.target.value)}
      multiline
      minRows={2}
      fullWidth
      size="small"
      slotProps={{ input: { disableUnderline: true } }}
      sx={{ ...filledTextFieldSx, mt: 1, mb: 0.5 }}
    />
  );
  const defaultEditor = (
    <>
      <FormControlLabel
        control={<Checkbox size="small" checked={hasDefault} onChange={(e) => onHasDefaultChange(e.target.checked)} />}
        label={<Typography variant="caption">Has a default value</Typography>}
      />
      {hasDefault && <ValueEditor type={type} value={defaultValue} onChange={onDefaultChange} />}
    </>
  );

  return (
    <Dialog
      open={open}
      onClose={onClose}
      maxWidth="lg"
      fullWidth
      slotProps={{ paper: { sx: { height: { xs: "calc(100vh - 32px)", sm: "min(860px, calc(100vh - 64px))" } } } }}
    >
      <DialogTitle sx={{ display: "flex", alignItems: "center", gap: 1 }}>
        <Box sx={{ width: 24, height: 24, display: "grid", placeItems: "center", flexShrink: 0 }}>
          <DataObjectIcon sx={{ fontSize: 20, color: "#04cfd0" }} />
        </Box>
        <Typography component="span" variant="subtitle1" fontWeight={700} sx={{ flex: 1 }}>
          var.{name || "?"} — Variable Definition
        </Typography>
        <IconButton size="small" onClick={onClose} aria-label="Close">
          <CloseIcon fontSize="small" />
        </IconButton>
      </DialogTitle>
      <DialogContent
        dividers
        sx={{
          minHeight: 0,
          overflow: "hidden",
          display: "grid",
          gridTemplateColumns: {
            xs: "minmax(0, 1fr)",
            md: previewExpanded ? "minmax(0, 1fr) minmax(320px, 2fr)" : "minmax(0, 3fr) minmax(320px, 2fr)",
          },
          gridTemplateRows: {
            xs: previewExpanded ? "minmax(120px, 1fr) minmax(0, 2fr)" : "minmax(0, 1fr) minmax(180px, 32%)",
            md: "minmax(0, 1fr)",
          },
          gap: 1.5,
        }}
      >
        <Box data-testid="variable-editor" sx={{ minHeight: 0, overflowY: "auto", pt: 0.75, pr: 0.5 }}>
          {typeEditor}
          {defaultEditor}
          {descriptionEditor}

          <Divider sx={{ my: 2 }} />
          <ValidationsEditor validations={validations} onChange={onValidationsChange} />
        </Box>

        <Box data-testid="hcl-preview" sx={{ position: "relative", minHeight: 0, display: "flex", flexDirection: "column" }}>
          <Highlight prism={Prism as typeof Prism} theme={prismTheme} code={code} language={"hcl" as Language}>
            {({ style, tokens, getLineProps, getTokenProps }) => (
              <Box
                component="pre"
                sx={{
                  m: 0,
                  pl: 1,
                  pr: 9,
                  py: 0.75,
                  minHeight: 0,
                  flex: 1,
                  borderRadius: 1,
                  border: "1px solid",
                  borderColor: "divider",
                  fontFamily: "monospace",
                  fontSize: "0.75rem",
                  lineHeight: 1.5,
                  overflow: "auto",
                  whiteSpace: "pre",
                  ...style,
                }}
              >
                {tokens.map((line, i) => (
                  <div key={i} {...getLineProps({ line })}>
                    {line.map((token, key) => (
                      <span key={key} {...getTokenProps({ token })} />
                    ))}
                  </div>
                ))}
              </Box>
            )}
          </Highlight>
          <Box sx={{ position: "absolute", top: 6, right: 6, display: "flex", alignItems: "center", gap: 0.25 }}>
            <Tooltip title={previewExpanded ? "Restore input pane space" : "Expand HCL preview"}>
              <IconButton
                size="small"
                onClick={() => setPreviewExpanded((expanded) => !expanded)}
                aria-label={previewExpanded ? "Restore HCL preview" : "Expand HCL preview"}
                aria-pressed={previewExpanded}
                sx={{ color: "text.secondary", transition: "color 0.2s", p: 0.4 }}
              >
                {previewExpanded ? <CloseFullscreenIcon sx={{ fontSize: 14 }} /> : <OpenInFullIcon sx={{ fontSize: 14 }} />}
              </IconButton>
            </Tooltip>
            <CopyButton value={code} />
          </Box>
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Done</Button>
      </DialogActions>
    </Dialog>
  );
}
