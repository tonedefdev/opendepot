"use client";

import * as React from "react";
import Dialog from "@mui/material/Dialog";
import DialogTitle from "@mui/material/DialogTitle";
import DialogContent from "@mui/material/DialogContent";
import DialogActions from "@mui/material/DialogActions";
import IconButton from "@mui/material/IconButton";
import Box from "@mui/material/Box";
import Divider from "@mui/material/Divider";
import Stack from "@mui/material/Stack";
import TextField from "@mui/material/TextField";
import Tooltip from "@mui/material/Tooltip";
import Typography from "@mui/material/Typography";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";
import Button from "@mui/material/Button";
import CloseIcon from "@mui/icons-material/Close";
import DataObjectIcon from "@mui/icons-material/DataObject";
import AddIcon from "@mui/icons-material/Add";
import DeleteOutlineIcon from "@mui/icons-material/DeleteOutline";
import { useColorScheme } from "@mui/material/styles";
import CodeEditor from "@uiw/react-textarea-code-editor";
import { Highlight, type Language, themes } from "prism-react-renderer";
import Prism from "prismjs";
import "prismjs/components/prism-hcl";
import CopyButton from "@/components/CopyButton";
import type { TypeSpec, ValueSpec, VariableValidation } from "./types";
import { isComplexVariableType, renderVariableSpec } from "./typeSpec";
import TypeEditor from "./TypeEditor";
import ValueEditor from "./ValueEditor";

// Make prism-react-renderer use the full prismjs instance so it picks up the
// HCL grammar we registered above via the side-effectful import (mirrors
// ContractTable.tsx / UsageSnippet.tsx).
(typeof globalThis !== "undefined" ? globalThis : window).Prism = Prism;

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
  const code = React.useMemo(
    () => renderVariableSpec(name, type, hasDefault, defaultValue, description, validations),
    [name, type, hasDefault, defaultValue, description, validations],
  );

  const { mode, systemMode } = useColorScheme();
  const resolvedMode = mode === "system" ? systemMode : mode;
  const prismTheme = resolvedMode === "light" ? themes.github : themes.nightOwl;
  const editorMode = resolvedMode === "dark" ? "dark" : "light";
  const complexType = isComplexVariableType(type);

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
  const complexTypeDivider = <Divider sx={{ my: 1.5 }} />;

  const changeValidation = (index: number, patch: Partial<VariableValidation>) => {
    onValidationsChange(
      validations.map((validation, validationIndex) =>
        validationIndex === index ? { ...validation, ...patch } : validation,
      ),
    );
  };

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
          gridTemplateColumns: { xs: "minmax(0, 1fr)", md: "minmax(0, 3fr) minmax(320px, 2fr)" },
          gridTemplateRows: { xs: "minmax(0, 1fr) minmax(180px, 32%)", md: "minmax(0, 1fr)" },
          gap: 1.5,
        }}
      >
        <Box data-testid="variable-editor" sx={{ minHeight: 0, overflowY: "auto", pt: 0.75, pr: 0.5 }}>
          {complexType ? <>{defaultEditor}{descriptionEditor}{complexTypeDivider}{typeEditor}</> : <>{typeEditor}{defaultEditor}{descriptionEditor}</>}

          <Divider sx={{ my: 2 }} />
          <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mb: 1 }}>
            <Typography variant="subtitle2">Validations</Typography>
            <Button
              size="small"
              startIcon={<AddIcon />}
              onClick={() => onValidationsChange([...validations, { condition: "", errorMessage: "" }])}
            >
              Add validation
            </Button>
          </Stack>

          <Stack spacing={1.5}>
            {validations.map((validation, index) => {
              const conditionMissing = !validation.condition.trim();
              const messageMissing = !validation.errorMessage.trim();

              return (
                <Box
                  key={index}
                  sx={{
                    border: "1px solid",
                    borderColor: conditionMissing || messageMissing ? "error.main" : "divider",
                    borderRadius: 1,
                    p: 1.25,
                  }}
                >
                  <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mb: 0.75 }}>
                    <Typography variant="caption" fontWeight={700}>
                      Validation {index + 1}
                    </Typography>
                    <Tooltip title="Remove validation">
                      <IconButton
                        size="small"
                        aria-label={`Remove validation ${index + 1}`}
                        onClick={() =>
                          onValidationsChange(validations.filter((_, validationIndex) => validationIndex !== index))
                        }
                      >
                        <DeleteOutlineIcon fontSize="small" />
                      </IconButton>
                    </Tooltip>
                  </Stack>
                  <Typography component="label" htmlFor={`variable-validation-${index}`} variant="caption" fontWeight={600}>
                    Condition (HCL)
                  </Typography>
                  <Box
                    sx={{
                      mt: 0.5,
                      mb: 1,
                      border: "1px solid",
                      borderColor: conditionMissing ? "error.main" : "transparent",
                      borderRadius: 1,
                      bgcolor: "action.hover",
                      overflow: "hidden",
                      transition: (theme) => theme.transitions.create(["border-color", "background-color"]),
                      "&:hover": { bgcolor: "action.selected" },
                      "&:focus-within": {
                        borderColor: conditionMissing ? "error.main" : "primary.main",
                        bgcolor: "action.selected",
                      },
                      "& .w-tc-editor": {
                        "--color-prettylights-syntax-sublimelinter-gutter-mark":
                          editorMode === "dark" ? "#b1bac4" : "#57606a",
                        "--color-prettylights-syntax-string-regexp": editorMode === "dark" ? "#a5d6ff" : "#0a3069",
                      },
                    }}
                  >
                    <CodeEditor
                      id={`variable-validation-${index}`}
                      value={validation.condition}
                      language="hcl"
                      data-color-mode={editorMode}
                      minHeight={72}
                      indentWidth={2}
                      placeholder="length(var.name) > 0"
                      onChange={(event) => changeValidation(index, { condition: event.target.value })}
                      aria-invalid={conditionMissing}
                      style={{ fontSize: 12, fontFamily: "monospace", backgroundColor: "transparent", minHeight: 72 }}
                    />
                  </Box>
                  {conditionMissing && (
                    <Typography variant="caption" color="error">
                      Condition is required.
                    </Typography>
                  )}
                  <TextField
                    variant="filled"
                    label="Error message"
                    value={validation.errorMessage}
                    onChange={(event) => changeValidation(index, { errorMessage: event.target.value })}
                    error={messageMissing}
                    helperText={messageMissing ? "Error message is required." : undefined}
                    multiline
                    minRows={2}
                    fullWidth
                    size="small"
                    slotProps={{ input: { disableUnderline: true } }}
                    sx={{ ...filledTextFieldSx, mt: conditionMissing ? 0.75 : 0 }}
                  />
                </Box>
              );
            })}
          </Stack>
        </Box>

        <Box data-testid="hcl-preview" sx={{ position: "relative", minHeight: 0, display: "flex", flexDirection: "column" }}>
          <Highlight prism={Prism as typeof Prism} theme={prismTheme} code={code} language={"hcl" as Language}>
            {({ style, tokens, getLineProps, getTokenProps }) => (
              <Box
                component="pre"
                sx={{
                  m: 0,
                  pl: 1,
                  pr: 5,
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
          <Box sx={{ position: "absolute", top: 6, right: 6 }}>
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
