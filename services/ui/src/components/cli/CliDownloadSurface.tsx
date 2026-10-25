"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Link from "@mui/material/Link";
import List from "@mui/material/List";
import ListItemButton from "@mui/material/ListItemButton";
import ListItemIcon from "@mui/material/ListItemIcon";
import ListItemText from "@mui/material/ListItemText";
import ToggleButton from "@mui/material/ToggleButton";
import ToggleButtonGroup from "@mui/material/ToggleButtonGroup";
import Typography from "@mui/material/Typography";
import DownloadOutlinedIcon from "@mui/icons-material/DownloadOutlined";
import TerminalOutlinedIcon from "@mui/icons-material/TerminalOutlined";
import VerifiedUserOutlinedIcon from "@mui/icons-material/VerifiedUserOutlined";
import type { IconType } from "react-icons";
import { FaApple, FaWindows } from "react-icons/fa6";
import { HighlightedCodeBlock } from "@/components/HighlightedCodeBlock";
import MobileSidebarButton from "@/components/MobileSidebarButton";
import { SectionCard } from "@/components/SectionCard";
import { CLI_OS_NAMES, CLI_PLATFORMS, type CLIOS, cliBinaryName, detectCLIOS } from "@/lib/cliPlatforms";
import { buildCLIInstallScript, buildCLIVerifyCommand } from "@/lib/cliInstallScripts";

const OS_ICON_COMPONENTS: Record<Exclude<CLIOS, "linux">, IconType> = {
  darwin: FaApple,
  windows: FaWindows,
};

function OSIcon({ os, size }: { os: CLIOS; size: number }) {
  if (os === "linux") {
    return (
      <Box
        component="img"
        src="/img/linux.svg"
        alt=""
        aria-hidden="true"
        sx={{ height: size, width: "auto", display: "block" }}
      />
    );
  }
  const Icon = OS_ICON_COMPONENTS[os];
  return <Icon size={size} aria-hidden="true" />;
}

const INSTALL_OS_OPTIONS = (Object.keys(CLI_OS_NAMES) as CLIOS[]).map((os) => ({
  os,
  label: CLI_OS_NAMES[os],
}));

const PLACEHOLDER_ORIGIN = "https://<registry-host>";

function subscribeNoop() {
  return () => {};
}

function getWindowOrigin() {
  return window.location.origin;
}

function getServerOrigin() {
  return PLACEHOLDER_ORIGIN;
}

export default function CliDownloadSurface() {
  const origin = React.useSyncExternalStore(subscribeNoop, getWindowOrigin, getServerOrigin);
  const [os, setOS] = React.useState<CLIOS>("darwin");
  const script = buildCLIInstallScript(os, origin);
  const shellName = os === "windows" ? "PowerShell" : "Terminal";
  const language = os === "windows" ? "powershell" : "bash";

  React.useEffect(() => {
    const detected = detectCLIOS(navigator.userAgent);
    if (detected) {
      setOS(detected);
    }
  }, []);

  return (
    <Container maxWidth="md" sx={{ py: 4 }}>
      <Box sx={{ display: { xs: "flex", sm: "none" }, justifyContent: "flex-end", mb: 1 }}>
        <MobileSidebarButton />
      </Box>
      <Typography variant="h4" component="h1" sx={{ mb: 1 }}>
        OpenDepot CLI
      </Typography>
      <Typography color="text.secondary" sx={{ mb: 3 }}>
        Choose the binary for your platform. Each download matches the server release it was published with.
      </Typography>

      <SectionCard icon={<DownloadOutlinedIcon fontSize="small" />} title="Downloads">
        <List disablePadding>
          {CLI_PLATFORMS.map((platform) => {
            return (
              <ListItemButton
                key={`${platform.os}/${platform.arch}`}
                component="a"
                href={`/api/cli/${platform.os}/${platform.arch}`}
                download={cliBinaryName(platform.os, platform.arch)}
                sx={{ borderRadius: "6px" }}
              >
                <ListItemIcon sx={{ minWidth: 40, color: "text.primary" }}>
                  <OSIcon os={platform.os} size={20} />
                </ListItemIcon>
                <ListItemText
                  primary={platform.label}
                  secondary={cliBinaryName(platform.os, platform.arch)}
                  secondaryTypographyProps={{ sx: { fontFamily: "monospace" } }}
                />
              </ListItemButton>
            );
          })}
        </List>
      </SectionCard>

      <SectionCard icon={<VerifiedUserOutlinedIcon fontSize="small" />} title="Verify">
        <Typography variant="body2" color="text.secondary">
          Check your download against the{" "}
          <Link href="/api/cli/SHA256SUMS" underline="hover">
            SHA256SUMS
          </Link>{" "}
          file. Run this in {shellName} from the folder that contains your download and SHA256SUMS:
        </Typography>
        <Box sx={{ mt: 1.5 }}>
          <HighlightedCodeBlock code={buildCLIVerifyCommand(os)} language={language} />
        </Box>
      </SectionCard>

      <SectionCard icon={<TerminalOutlinedIcon fontSize="small" />} title="Install">
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          Pick your operating system. The script downloads the binary for your architecture, verifies it against
          SHA256SUMS, and installs it as <code>opendepot</code>.
        </Typography>
        <ToggleButtonGroup
          exclusive
          size="small"
          value={os}
          aria-label="Operating system"
          onChange={(_event, value: CLIOS | null) => {
            if (value) setOS(value);
          }}
          sx={{ mb: 2, flexWrap: "wrap" }}
        >
          {INSTALL_OS_OPTIONS.map(({ os: optionOS, label }) => (
            <ToggleButton
              key={optionOS}
              value={optionOS}
              sx={{ textTransform: "none", gap: 1, px: 2 }}
            >
              <OSIcon os={optionOS} size={16} />
              {label}
            </ToggleButton>
          ))}
        </ToggleButtonGroup>
        <Box sx={{ mb: 0.75, color: "text.secondary", fontSize: "0.75rem", fontWeight: 600 }}>Run in {shellName}</Box>
        <HighlightedCodeBlock code={script} language={language} />
      </SectionCard>
    </Container>
  );
}
