"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Tooltip from "@mui/material/Tooltip";

interface Props {
  provider: string;
  size?: number;
}

interface ProviderLogoSource {
  src: string;
  alt: string;
}

function LogoBadge({ size, child }: { size: number; child: React.ReactNode }) {
  return (
    <Box
      sx={{
        width: size,
        height: size,
        borderRadius: "6px",
        border: "1px solid rgba(240,246,252,0.14)",
        bgcolor: "rgba(240,246,252,0.04)",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        color: "text.primary",
        flexShrink: 0,
      }}
    >
      {child}
    </Box>
  );
}

function GenericProviderBadge({ size, label }: { size: number; label: string }) {
  const letter = label.charAt(0).toUpperCase();
  return (
    <LogoBadge
      size={size}
      child={
        <Box
          sx={{
            fontSize: size * 0.45,
            fontWeight: 700,
            color: "#047df1",
            fontFamily: "monospace",
          }}
        >
          {letter}
        </Box>
      }
    />
  );
}

const KNOWN_PROVIDERS: Record<string, ProviderLogoSource> = {
  aws: { src: "/img/aws-dark.svg", alt: "AWS" },
  azure: { src: "/img/azure.svg", alt: "Azure" },
  azurerm: { src: "/img/azure.svg", alt: "Azure" },
  google: { src: "/img/gcp.svg", alt: "Google Cloud" },
  gcp: { src: "/img/gcp.svg", alt: "Google Cloud" },
  googlecloud: { src: "/img/gcp.svg", alt: "Google Cloud" },
};

export default function ProviderLogo({ provider, size = 28 }: Props) {
  const key = provider.toLowerCase().replace(/[^a-z0-9]/g, "");
  const providerLogo = KNOWN_PROVIDERS[key];

  return (
    <Tooltip title={provider} placement="top">
      <Box sx={{ display: "inline-flex", alignItems: "center", flexShrink: 0 }}>
        {providerLogo ? (
          <LogoBadge
            size={size}
            child={
              <Box
                component="img"
                src={providerLogo.src}
                alt={providerLogo.alt}
                sx={{
                  width: Math.max(14, size * 0.68),
                  height: Math.max(14, size * 0.68),
                  objectFit: "contain",
                  filter: "drop-shadow(0 0 2px rgba(0,0,0,0.35))",
                }}
              />
            }
          />
        ) : (
          <GenericProviderBadge size={size} label={provider} />
        )}
      </Box>
    </Tooltip>
  );
}
