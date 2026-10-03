"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import Collapse from "@mui/material/Collapse";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import { useColorScheme } from "@mui/material/styles";
import CodeEditor from "@uiw/react-textarea-code-editor";
import type { VariableValidation } from "./types";
import { hclConditionPlugins, hclEditorColorVariables } from "./hclConditionHighlight";
import {
  AddButton,
  RemoveButton,
  RowSpacer,
  RowToggle,
  childBranchSx,
  compactFieldSx,
  monoSx,
  rowActionsSx,
  rowLabelSx,
  rowSx,
  rowsSx,
} from "./editorRows";

const fieldRowSx = { display: "flex", alignItems: "flex-start", flexWrap: "wrap", gap: 1, px: 0.5, py: 0.25 };
const fieldLabelSx = { ...rowLabelSx, py: 0.75 };

interface Props {
  validations: VariableValidation[];
  onChange: (validations: VariableValidation[]) => void;
}

export default function ValidationsEditor({ validations, onChange }: Props) {
  const [expanded, setExpanded] = React.useState<number | false>(false);
  const { mode, systemMode } = useColorScheme();
  const editorMode = (mode === "system" ? systemMode : mode) === "light" ? "light" : "dark";

  const change = (index: number, patch: Partial<VariableValidation>) =>
    onChange(validations.map((validation, i) => (i === index ? { ...validation, ...patch } : validation)));

  const remove = (index: number) => {
    setExpanded((current) => {
      if (current === index) return false;
      return current !== false && current > index ? current - 1 : current;
    });
    onChange(validations.filter((_, i) => i !== index));
  };

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 0.75 }}>
      <Typography variant="subtitle2">Validations</Typography>
      <Box sx={rowsSx}>
        {validations.map((validation, index) => {
          const isExpanded = expanded === index;
          const toggle = () => setExpanded(isExpanded ? false : index);
          const incomplete = !validation.condition.trim() || !validation.errorMessage.trim();

          return (
            <Box key={index}>
              <Box sx={rowSx}>
                <RowToggle label={`Validation ${index + 1}`} expanded={isExpanded} onToggle={toggle} />
                <Box
                  onClick={toggle}
                  sx={{ flex: "1 1 0", minWidth: 0, display: "flex", alignItems: "baseline", gap: 1.5, cursor: "pointer" }}
                >
                  {validation.condition.trim() ? (
                    <Typography noWrap sx={{ ...monoSx, flex: "0 1 auto", maxWidth: "60%" }}>
                      {validation.condition}
                    </Typography>
                  ) : (
                    <Typography noWrap color="text.secondary" sx={{ ...monoSx, fontStyle: "italic" }}>
                      new validation
                    </Typography>
                  )}
                  <Typography noWrap variant="caption" color="text.secondary" sx={{ flex: "1 1 0", minWidth: 0 }}>
                    {validation.errorMessage}
                  </Typography>
                </Box>
                <Box sx={rowActionsSx}>
                  {incomplete && (
                    <Chip size="small" label="incomplete" color="error" variant="outlined" sx={{ height: 22, fontSize: "0.7rem" }} />
                  )}
                  <RemoveButton label={`Remove validation ${index + 1}`} onClick={() => remove(index)} />
                </Box>
              </Box>
              <Collapse in={isExpanded} unmountOnExit>
                <Box sx={childBranchSx}>
                  <Box sx={fieldRowSx}>
                    <RowSpacer />
                    <Typography component="label" htmlFor={`variable-validation-${index}`} sx={fieldLabelSx}>
                      condition
                    </Typography>
                    <Box
                      sx={{
                        flex: "1 1 240px",
                        minWidth: 0,
                        px: 1.5,
                        py: 0.75,
                        borderRadius: 1,
                        bgcolor: "action.hover",
                        overflow: "hidden",
                        transition: (theme) => theme.transitions.create("background-color"),
                        "&:hover, &:focus-within": { bgcolor: "action.selected" },
                        "& .w-tc-editor": {
                          ...hclEditorColorVariables(editorMode),
                        },
                      }}
                    >
                      <CodeEditor
                        id={`variable-validation-${index}`}
                        value={validation.condition}
                        language="hcl"
                        rehypePlugins={hclConditionPlugins}
                        data-color-mode={editorMode}
                        minHeight={20}
                        padding={0}
                        indentWidth={2}
                        placeholder="length(var.name) > 0"
                        onChange={(event) => change(index, { condition: event.target.value })}
                        aria-invalid={!validation.condition.trim()}
                        style={{ ...monoSx, backgroundColor: "transparent" }}
                      />
                    </Box>
                  </Box>
                  <Box sx={fieldRowSx}>
                    <RowSpacer />
                    <Typography component="label" htmlFor={`variable-validation-${index}-message`} sx={fieldLabelSx}>
                      error_message
                    </Typography>
                    <TextField
                      id={`variable-validation-${index}-message`}
                      variant="filled"
                      size="small"
                      hiddenLabel
                      multiline
                      maxRows={4}
                      placeholder="Name must not be empty."
                      value={validation.errorMessage}
                      onChange={(event) => change(index, { errorMessage: event.target.value })}
                      slotProps={{ input: { disableUnderline: true }, htmlInput: { "aria-invalid": !validation.errorMessage.trim() } }}
                      sx={{
                        ...compactFieldSx,
                        "& .MuiFilledInput-root": { borderRadius: 1, py: 0.75 },
                        "& .MuiFilledInput-input": { ...monoSx, py: 0 },
                        flex: "1 1 240px",
                        minWidth: 0,
                      }}
                    />
                  </Box>
                </Box>
              </Collapse>
            </Box>
          );
        })}
        <AddButton
          onClick={() => {
            onChange([...validations, { condition: "", errorMessage: "" }]);
            setExpanded(validations.length);
          }}
        >
          Add validation
        </AddButton>
      </Box>
    </Box>
  );
}
