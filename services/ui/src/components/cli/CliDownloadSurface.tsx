"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Link from "@mui/material/Link";
import List from "@mui/material/List";
import ListItemButton from "@mui/material/ListItemButton";
import ListItemText from "@mui/material/ListItemText";
import Typography from "@mui/material/Typography";
import DownloadOutlinedIcon from "@mui/icons-material/DownloadOutlined";
import TerminalOutlinedIcon from "@mui/icons-material/TerminalOutlined";
import PageHeader from "@/components/PageHeader";
import { SectionCard } from "@/components/SectionCard";
import { CLI_PLATFORMS, cliBinaryName } from "@/lib/cliPlatforms";

export default function CliDownloadSurface() {
  return (
    <>
      <PageHeader
        icon={<TerminalOutlinedIcon color="primary" fontSize="small" />}
        title="CLI"
        description="Download the OpenDepot command-line tool."
      />
      <Container maxWidth="md" sx={{ py: 4 }}>
        <Typography variant="h4" component="h1" sx={{ display: { xs: "none", sm: "block" }, mb: 1 }}>
          OpenDepot CLI
        </Typography>
        <Typography color="text.secondary" sx={{ mb: 3 }}>
          Choose the binary for your platform. Each download matches the server release it was published with.
        </Typography>

        <SectionCard icon={<DownloadOutlinedIcon fontSize="small" />} title="Downloads">
          <List disablePadding>
            {CLI_PLATFORMS.map((platform) => (
              <ListItemButton
                key={`${platform.os}/${platform.arch}`}
                component="a"
                href={`/api/cli/${platform.os}/${platform.arch}`}
                download={cliBinaryName(platform.os, platform.arch)}
                sx={{ borderRadius: "6px" }}
              >
                <ListItemText
                  primary={platform.label}
                  secondary={cliBinaryName(platform.os, platform.arch)}
                  secondaryTypographyProps={{ sx: { fontFamily: "monospace" } }}
                />
              </ListItemButton>
            ))}
          </List>
        </SectionCard>

        <SectionCard icon={<TerminalOutlinedIcon fontSize="small" />} title="Install">
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
            On macOS or Linux, make the binary executable and place it on your PATH. Windows users can add the
            .exe to a directory on PATH.
          </Typography>
          <Box
            component="pre"
            sx={{
              m: 0,
              p: 1.5,
              borderRadius: "6px",
              bgcolor: "action.hover",
              fontFamily: "monospace",
              fontSize: "0.8125rem",
              overflowX: "auto",
            }}
          >
            {"chmod +x opendepot_linux_amd64\nsudo mv opendepot_linux_amd64 /usr/local/bin/opendepot\nopendepot --version"}
          </Box>
        </SectionCard>

        <SectionCard icon={<DownloadOutlinedIcon fontSize="small" />} title="Verify">
          <Typography variant="body2" color="text.secondary">
            Check your download against the{" "}
            <Link href="/api/cli/SHA256SUMS" underline="hover">
              SHA256SUMS
            </Link>{" "}
            file with <code>shasum -a 256 -c SHA256SUMS</code>.
          </Typography>
        </SectionCard>
      </Container>
    </>
  );
}
