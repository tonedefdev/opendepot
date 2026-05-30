"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Tooltip from "@mui/material/Tooltip";
import {
  SiGithub,
  SiArgo,
  SiKubernetes,
  SiHelm,
  SiVault,
  SiCloudflare,
  SiDatadog,
  SiDigitalocean,
  SiPostgresql,
  SiMysql,
  SiGrafana,
  SiMongodb,
  SiNewrelic,
  SiPagerduty,
  SiOkta,
  SiTerraform,
} from "react-icons/si";

interface Props {
  provider: string;
  size?: number;
}

type ProviderLogoSource =
  | { type: "img"; src: string; alt: string }
  | { type: "icon"; icon: React.ElementType; color: string };

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
            color: "#04cfd0",
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
  // Local SVGs — not available on simple-icons
  aws: { type: "img", src: "/img/aws-dark.svg", alt: "AWS" },
  azure: { type: "img", src: "/img/azure.svg", alt: "Azure" },
  azurerm: { type: "img", src: "/img/azure.svg", alt: "Azure" },
  azuread: { type: "img", src: "/img/azure.svg", alt: "Azure" },
  azapi: { type: "img", src: "/img/azure.svg", alt: "Azure" },
  google: { type: "img", src: "/img/gcp.svg", alt: "Google Cloud" },
  gcp: { type: "img", src: "/img/gcp.svg", alt: "Google Cloud" },
  googlecloud: { type: "img", src: "/img/gcp.svg", alt: "Google Cloud" },
  // Simple Icons
  argocd: { type: "icon", icon: SiArgo, color: "#E6EDF3" },
  github: { type: "icon", icon: SiGithub, color: "#E6EDF3" },
  githubactions: { type: "icon", icon: SiGithub, color: "#E6EDF3" },
  kubernetes: { type: "icon", icon: SiKubernetes, color: "#326CE5" },
  k8s: { type: "icon", icon: SiKubernetes, color: "#326CE5" },
  helm: { type: "icon", icon: SiHelm, color: "#277A9F" },
  vault: { type: "icon", icon: SiVault, color: "#FFCA28" },
  cloudflare: { type: "icon", icon: SiCloudflare, color: "#F38020" },
  datadog: { type: "icon", icon: SiDatadog, color: "#632CA6" },
  digitalocean: { type: "icon", icon: SiDigitalocean, color: "#0080FF" },
  postgresql: { type: "icon", icon: SiPostgresql, color: "#4169E1" },
  postgres: { type: "icon", icon: SiPostgresql, color: "#4169E1" },
  mysql: { type: "icon", icon: SiMysql, color: "#4479A1" },
  grafana: { type: "icon", icon: SiGrafana, color: "#F46800" },
  mongodbatlas: { type: "icon", icon: SiMongodb, color: "#47A248" },
  mongodb: { type: "icon", icon: SiMongodb, color: "#47A248" },
  newrelic: { type: "icon", icon: SiNewrelic, color: "#1CE783" },
  pagerduty: { type: "icon", icon: SiPagerduty, color: "#06AC38" },
  okta: { type: "icon", icon: SiOkta, color: "#007DC1" },
  terraform: { type: "icon", icon: SiTerraform, color: "#7B42BC" },
};

export default function ProviderLogo({ provider, size = 28 }: Props) {
  const key = provider.toLowerCase().replace(/[^a-z0-9]/g, "");
  const entry = KNOWN_PROVIDERS[key];

  return (
    <Tooltip title={provider} placement="top">
      <Box sx={{ display: "inline-flex", alignItems: "center", flexShrink: 0 }}>
        {entry ? (
          <LogoBadge
            size={size}
            child={
              entry.type === "img" ? (
                <Box
                  component="img"
                  src={entry.src}
                  alt={entry.alt}
                  sx={{
                    width: Math.max(14, size * 0.68),
                    height: Math.max(14, size * 0.68),
                    objectFit: "contain",
                    filter: "drop-shadow(0 0 2px rgba(0,0,0,0.35))",
                  }}
                />
              ) : (
                <entry.icon
                  style={{
                    width: Math.max(12, size * 0.65),
                    height: Math.max(12, size * 0.65),
                    color: entry.color,
                    flexShrink: 0,
                  }}
                />
              )
            }
          />
        ) : (
          <GenericProviderBadge size={size} label={provider} />
        )}
      </Box>
    </Tooltip>
  );
}
