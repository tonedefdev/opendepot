"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Drawer from "@mui/material/Drawer";
import List from "@mui/material/List";
import ListItem from "@mui/material/ListItem";
import ListItemButton from "@mui/material/ListItemButton";
import ListItemText from "@mui/material/ListItemText";
import Typography from "@mui/material/Typography";
import InputBase from "@mui/material/InputBase";
import OutlinedInput from "@mui/material/OutlinedInput";
import InputLabel from "@mui/material/InputLabel";
import FormControl from "@mui/material/FormControl";
import Chip from "@mui/material/Chip";
import Divider from "@mui/material/Divider";
import Tooltip from "@mui/material/Tooltip";
import Button from "@mui/material/Button";
import Avatar from "@mui/material/Avatar";
import CircularProgress from "@mui/material/CircularProgress";
import Alert from "@mui/material/Alert";
import SearchIcon from "@mui/icons-material/Search";
import ClearIcon from "@mui/icons-material/Clear";
import IconButton from "@mui/material/IconButton";
import LogoutIcon from "@mui/icons-material/Logout";
import LoginIcon from "@mui/icons-material/Login";
import WarehouseIcon from "@mui/icons-material/Warehouse";
import GridViewIcon from "@mui/icons-material/GridView";
import Link from "next/link";
import useMediaQuery from "@mui/material/useMediaQuery";
import MenuIcon from "@mui/icons-material/Menu";
import ChevronLeftIcon from "@mui/icons-material/ChevronLeft";
import ChevronRightIcon from "@mui/icons-material/ChevronRight";
import { useTheme } from "@mui/material/styles";
import { useRouter, useSearchParams, usePathname } from "next/navigation";
import { useEffect, useState, useCallback } from "react";

const DRAWER_WIDTH = 260;
const COLLAPSED_WIDTH = 56;
const API_BASE_URL = (process.env.NEXT_PUBLIC_API_BASE_URL ?? "").replace(/\/$/, "");

interface Namespace {
  name: string;
  public: boolean;
}

interface UserInfo {
  email: string;
  name?: string;
}

interface SidebarProps {
  initialNamespaces?: Namespace[];
  userInfo?: UserInfo | null;
  devTokenEnabled?: boolean;
}

export default function Sidebar({
  initialNamespaces = [],
  userInfo = null,
  devTokenEnabled = false,
}: SidebarProps) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  const currentQ = searchParams.get("q") ?? "";
  const currentNs = searchParams.get("namespace") ?? "";
  const currentKind = searchParams.get("kind") ?? "";
  const currentSort = searchParams.get("sort_by") ?? "name";

  const [searchValue, setSearchValue] = useState(currentQ);
  const [namespaces, setNamespaces] = useState<Namespace[]>(initialNamespaces);

  // Dev token input state
  const [devToken, setDevToken] = useState("");
  const [devTokenSaving, setDevTokenSaving] = useState(false);
  const [devTokenMessage, setDevTokenMessage] = useState<{ type: "success" | "error"; text: string } | null>(null);

  // Fetch namespaces client-side if none passed as props
  useEffect(() => {
    if (initialNamespaces.length > 0) return;
    fetch(`${API_BASE_URL}/opendepot/ui/v1/namespaces`)
      .then((r) => r.json())
      .then((data: { items: Namespace[] }) => {
        setNamespaces(data.items ?? []);
      })
      .catch(() => {/* silent */});
  }, [initialNamespaces]);

  const navigate = useCallback(
    (updates: Record<string, string | null>) => {
      const params = new URLSearchParams(searchParams.toString());
      for (const [k, v] of Object.entries(updates)) {
        if (v === null || v === "") {
          params.delete(k);
        } else {
          params.set(k, v);
        }
      }
      // Reset to page 1 on filter change
      params.delete("page");
      router.push(`/?${params.toString()}`);
    },
    [router, searchParams],
  );

  const handleSearchSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    navigate({ q: searchValue });
  };

  const handleClearSearch = () => {
    setSearchValue("");
    navigate({ q: null });
  };

  const handleSaveDevToken = async () => {
    if (!devToken.trim()) return;
    setDevTokenSaving(true);
    setDevTokenMessage(null);
    try {
      const res = await fetch("/auth/dev-token", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token: devToken.trim() }),
      });
      if (res.ok) {
        setDevTokenMessage({ type: "success", text: "Token saved. Refreshing…" });
        setTimeout(() => router.refresh(), 800);
      } else {
        const data = (await res.json()) as { error?: string };
        setDevTokenMessage({ type: "error", text: data.error ?? "Failed to save token." });
      }
    } catch {
      setDevTokenMessage({ type: "error", text: "Network error." });
    } finally {
      setDevTokenSaving(false);
    }
  };

  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down("sm"));
  const [mobileOpen, setMobileOpen] = useState(false);
  const [collapsed, setCollapsed] = useState(false);

  const isOnHome = pathname === "/";

  // Derive avatar initials from name or email
  const avatarInitials = userInfo
    ? userInfo.name
      ? userInfo.name
          .split(" ")
          .slice(0, 2)
          .map((w) => w[0]?.toUpperCase() ?? "")
          .join("")
      : (userInfo.email[0] ?? "").toUpperCase()
    : "";

  const drawerContent = (
    <Box sx={{ display: "flex", flexDirection: "column", height: "100%" }}>
      {/* Logo + Branding */}
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          px: 2,
          py: 2.5,
          background: "linear-gradient(135deg, #047df1 0%, #03deb8 100%)",
          borderBottom: "none",
        }}
      >
        <Box
          component={Link}
          href="/"
          sx={{ display: "flex", alignItems: "center", gap: 1, textDecoration: "none", "&:hover": { opacity: 0.85 } }}
        >
          <Box>
            <Typography
              variant="body1"
              sx={{ fontWeight: 700, color: "#fff", lineHeight: 1.2, fontSize: "0.9375rem" }}
            >
              OpenDepot
            </Typography>
            <Typography
              variant="caption"
              sx={{ color: "rgba(255,255,255,0.85)", fontSize: "0.7rem", fontWeight: 500 }}
            >
              Registry Explorer
            </Typography>
          </Box>
        </Box>
        <Tooltip title="Collapse sidebar" placement="right">
          <IconButton
            size="small"
            onClick={() => setCollapsed(true)}
            sx={{ color: "rgba(255,255,255,0.7)", "&:hover": { color: "#fff" }, flexShrink: 0 }}
          >
            <ChevronLeftIcon sx={{ fontSize: 18 }} />
          </IconButton>
        </Tooltip>
      </Box>

      {/* Search */}
      <Box sx={{ px: 1.5, py: 1.5, borderBottom: "1px solid rgba(240,246,252,0.08)" }}>
        <Box
          component="form"
          onSubmit={handleSearchSubmit}
          sx={{
            display: "flex",
            alignItems: "center",
            gap: 0.5,
            background: "rgba(240,246,252,0.06)",
            border: "1px solid rgba(240,246,252,0.12)",
            borderRadius: "6px",
            px: 1.5,
            py: 0.5,
            "&:focus-within": {
              borderColor: "rgba(4,207,208,0.5)",
              background: "rgba(4,207,208,0.05)",
            },
          }}
        >
          <SearchIcon sx={{ fontSize: 16, color: "text.secondary", flexShrink: 0 }} />
          <InputBase
            placeholder="Search resources…"
            value={searchValue}
            onChange={(e) => setSearchValue(e.target.value)}
            sx={{
              flex: 1,
              fontSize: "0.8125rem",
              color: "text.primary",
              "& input": { padding: 0 },
            }}
          />
          {searchValue && (
            <IconButton size="small" onClick={handleClearSearch} sx={{ p: 0.25 }}>
              <ClearIcon sx={{ fontSize: 14, color: "text.secondary" }} />
            </IconButton>
          )}
        </Box>
      </Box>

      {/* Navigation */}
      <Box sx={{ overflowY: "auto", flex: 1 }}>
        {/* Pages */}
        <Box sx={{ px: 2, pt: 2, pb: 1 }}>
          <Typography
            variant="caption"
            sx={{ textTransform: "uppercase", letterSpacing: "0.08em", color: "text.secondary", fontWeight: 600 }}
          >
            Navigation
          </Typography>
        </Box>
        <List dense disablePadding>
          <ListItem disablePadding>
            <ListItemButton
              component={Link}
              href="/"
              selected={isOnHome}
              sx={{
                mx: 1,
                borderRadius: "6px",
                py: 0.5,
                "&.Mui-selected": { background: "rgba(4,207,208,0.12)", color: "primary.main" },
                "&.Mui-selected:hover": { background: "rgba(4,207,208,0.18)" },
              }}
            >
              <GridViewIcon sx={{ fontSize: 16, mr: 1, opacity: 0.8 }} />
              <ListItemText
                primary="Registry"
                primaryTypographyProps={{ fontSize: "0.8125rem", fontWeight: isOnHome ? 600 : 400 }}
              />
            </ListItemButton>
          </ListItem>
          <ListItem disablePadding>
            <ListItemButton
              component={Link}
              href="/depots"
              selected={pathname === "/depots"}
              sx={{
                mx: 1,
                borderRadius: "6px",
                py: 0.5,
                "&.Mui-selected": { background: "rgba(4,207,208,0.12)", color: "primary.main" },
                "&.Mui-selected:hover": { background: "rgba(4,207,208,0.18)" },
              }}
            >
              <WarehouseIcon sx={{ fontSize: 16, mr: 1, opacity: 0.8 }} />
              <ListItemText
                primary="Depots"
                primaryTypographyProps={{ fontSize: "0.8125rem", fontWeight: pathname === "/depots" ? 600 : 400 }}
              />
            </ListItemButton>
          </ListItem>
        </List>

        <Divider sx={{ mx: 2, my: 1 }} />

        {/* Kind filter */}
        <Box sx={{ px: 2, pt: 1, pb: 1 }}>
          <Typography
            variant="caption"
            sx={{ textTransform: "uppercase", letterSpacing: "0.08em", color: "text.secondary", fontWeight: 600 }}
          >
            Kind
          </Typography>
        </Box>
        <Box sx={{ px: 1.5, pb: 1, display: "flex", flexWrap: "wrap", gap: 0.75 }}>
          {[
            { label: "All", value: "" },
            { label: "Module", value: "module" },
            { label: "Provider", value: "provider" },
          ].map((opt) => (
            <Chip
              key={opt.value || "all"}
              label={opt.label}
              size="small"
              variant={currentKind === opt.value ? "filled" : "outlined"}
              color={currentKind === opt.value ? "primary" : "default"}
              onClick={() => isOnHome && navigate({ kind: opt.value })}
              sx={{ cursor: "pointer" }}
            />
          ))}
        </Box>

        <Divider sx={{ mx: 2, my: 1 }} />

        {/* Sort */}
        <Box sx={{ px: 2, pt: 1, pb: 1 }}>
          <Typography
            variant="caption"
            sx={{ textTransform: "uppercase", letterSpacing: "0.08em", color: "text.secondary", fontWeight: 600 }}
          >
            Sort by
          </Typography>
        </Box>
        <List dense disablePadding>
          {[
            { label: "Name", value: "name" },
            { label: "Namespace", value: "namespace" },
            { label: "Latest Version", value: "latest_version" },
            { label: "Last Scanned", value: "last_scanned" },
          ].map((opt) => (
            <ListItem key={opt.value} disablePadding>
              <ListItemButton
                selected={currentSort === opt.value}
                onClick={() => isOnHome && navigate({ sort_by: opt.value })}
                sx={{
                  mx: 1,
                  borderRadius: "6px",
                  py: 0.5,
                  "&.Mui-selected": {
                    background: "rgba(4,207,208,0.12)",
                    color: "primary.main",
                  },
                  "&.Mui-selected:hover": {
                    background: "rgba(4,207,208,0.18)",
                  },
                }}
              >
                <ListItemText
                  primary={opt.label}
                  primaryTypographyProps={{ fontSize: "0.8125rem", fontWeight: currentSort === opt.value ? 600 : 400 }}
                />
              </ListItemButton>
            </ListItem>
          ))}
        </List>

        <Divider sx={{ mx: 2, my: 1 }} />

        {/* Namespace filter */}
        {namespaces.length > 0 && (
          <>
            <Box sx={{ px: 2, pt: 1, pb: 1, display: "flex", alignItems: "center", justifyContent: "space-between" }}>
              <Typography
                variant="caption"
                sx={{ textTransform: "uppercase", letterSpacing: "0.08em", color: "text.secondary", fontWeight: 600 }}
              >
                Namespace
              </Typography>
              {currentNs && (
                <Tooltip title="Clear filter">
                  <IconButton size="small" onClick={() => navigate({ namespace: null })} sx={{ p: 0 }}>
                    <ClearIcon sx={{ fontSize: 12, color: "text.secondary" }} />
                  </IconButton>
                </Tooltip>
              )}
            </Box>
            <List dense disablePadding>
              {namespaces.map((ns) => (
                <ListItem key={ns.name} disablePadding>
                  <ListItemButton
                    selected={currentNs === ns.name}
                    onClick={() => isOnHome && navigate({ namespace: currentNs === ns.name ? null : ns.name })}
                    sx={{
                      mx: 1,
                      borderRadius: "6px",
                      py: 0.4,
                      "&.Mui-selected": {
                        background: "rgba(4,207,208,0.12)",
                        color: "primary.main",
                      },
                      "&.Mui-selected:hover": {
                        background: "rgba(4,207,208,0.18)",
                      },
                    }}
                  >
                    <ListItemText
                      primary={ns.name}
                      primaryTypographyProps={{
                        fontSize: "0.8125rem",
                        fontFamily: "monospace",
                        fontWeight: currentNs === ns.name ? 600 : 400,
                        noWrap: true,
                      }}
                    />
                    {ns.public && (
                      <Chip
                        label="pub"
                        size="small"
                        sx={{
                          fontSize: "0.65rem",
                          height: 18,
                          "& .MuiChip-label": { px: 0.75 },
                          color: "secondary.light",
                          borderColor: "rgba(3,222,184,0.3)",
                        }}
                        variant="outlined"
                      />
                    )}
                  </ListItemButton>
                </ListItem>
              ))}
            </List>
          </>
        )}
      </Box>

      {/* Footer — Auth + Dev Token */}
      <Box sx={{ borderTop: "1px solid rgba(240,246,252,0.08)" }}>
        {/* Dev token input (only when enabled) */}
        {devTokenEnabled && (
          <Box sx={{ px: 1.5, pt: 1.5, pb: 1 }}>
            <Typography
              variant="caption"
              sx={{ textTransform: "uppercase", letterSpacing: "0.08em", color: "text.secondary", fontWeight: 600, display: "block", mb: 0.75 }}
            >
              Dev Token
            </Typography>
            <FormControl fullWidth size="small" variant="outlined">
              <InputLabel htmlFor="dev-token-input" sx={{ fontSize: "0.75rem" }}>
                Bearer token
              </InputLabel>
              <OutlinedInput
                id="dev-token-input"
                inputProps={{ "data-testid": "dev-token-input" }}
                label="Bearer token"
                value={devToken}
                onChange={(e) => setDevToken(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Enter") { void handleSaveDevToken(); }}}
                sx={{ fontSize: "0.75rem" }}
                endAdornment={
                  devTokenSaving ? (
                    <CircularProgress size={14} />
                  ) : (
                    <Tooltip title="Save token">
                      <IconButton size="small" onClick={() => void handleSaveDevToken()} disabled={!devToken.trim()}>
                        <LoginIcon sx={{ fontSize: 14 }} />
                      </IconButton>
                    </Tooltip>
                  )
                }
              />
            </FormControl>
            {devTokenMessage && (
              <Alert severity={devTokenMessage.type} sx={{ mt: 0.5, py: 0, fontSize: "0.7rem" }}>
                {devTokenMessage.text}
              </Alert>
            )}
          </Box>
        )}

        {/* Auth area */}
        <Box sx={{ px: 1.5, py: 1.25 }}>
          {userInfo ? (
            /* Signed-in state */
            <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
              <Avatar
                sx={{
                  width: 28,
                  height: 28,
                  fontSize: "0.7rem",
                  bgcolor: "primary.main",
                  flexShrink: 0,
                }}
              >
                {avatarInitials}
              </Avatar>
              <Box sx={{ flex: 1, minWidth: 0 }}>
                {userInfo.name && (
                  <Typography variant="caption" sx={{ color: "text.primary", fontWeight: 600, display: "block", lineHeight: 1.2, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                    {userInfo.name}
                  </Typography>
                )}
                <Typography variant="caption" sx={{ color: "text.secondary", fontSize: "0.7rem", display: "block", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                  {userInfo.email}
                </Typography>
              </Box>
              <Tooltip title="Sign out">
                <IconButton
                  component={Link}
                  href="/auth/logout"
                  size="small"
                  aria-label="Sign out"
                  sx={{ flexShrink: 0, color: "text.secondary", "&:hover": { color: "error.main" } }}
                >
                  <LogoutIcon sx={{ fontSize: 16 }} />
                </IconButton>
              </Tooltip>
            </Box>
          ) : (
            /* Signed-out state */
            <Button
              component={Link}
              href="/auth/login"
              variant="outlined"
              size="small"
              fullWidth
              startIcon={<LoginIcon sx={{ fontSize: 14 }} />}
              sx={{ fontSize: "0.8rem", py: 0.5 }}
            >
              Sign in
            </Button>
          )}
        </Box>
      </Box>
    </Box>
  );

  const collapsedContent = (
    <Box sx={{ display: "flex", flexDirection: "column", height: "100%", alignItems: "center", py: 1, gap: 0.5 }}>
      <Tooltip title="Expand sidebar" placement="right">
        <IconButton
          size="small"
          onClick={() => setCollapsed(false)}
          sx={{ color: "text.secondary", "&:hover": { color: "text.primary" }, mb: 0.5 }}
        >
          <ChevronRightIcon sx={{ fontSize: 18 }} />
        </IconButton>
      </Tooltip>
      <Divider sx={{ width: "80%", mb: 0.5 }} />
      <Tooltip title="Registry" placement="right">
        <IconButton
          component={Link}
          href="/"
          sx={{
            color: isOnHome ? "primary.main" : "text.secondary",
            borderRadius: "6px",
            bgcolor: isOnHome ? "rgba(4,207,208,0.12)" : "transparent",
            "&:hover": { bgcolor: "rgba(4,207,208,0.1)" },
          }}
        >
          <GridViewIcon sx={{ fontSize: 20 }} />
        </IconButton>
      </Tooltip>
      <Tooltip title="Depots" placement="right">
        <IconButton
          component={Link}
          href="/depots"
          sx={{
            color: pathname === "/depots" ? "primary.main" : "text.secondary",
            borderRadius: "6px",
            bgcolor: pathname === "/depots" ? "rgba(4,207,208,0.12)" : "transparent",
            "&:hover": { bgcolor: "rgba(4,207,208,0.1)" },
          }}
        >
          <WarehouseIcon sx={{ fontSize: 20 }} />
        </IconButton>
      </Tooltip>
      <Box sx={{ flex: 1 }} />
      <Divider sx={{ width: "80%", mb: 0.5 }} />
      {userInfo ? (
        <Tooltip title={userInfo.name ?? userInfo.email} placement="right">
          <Avatar sx={{ width: 28, height: 28, fontSize: "0.7rem", bgcolor: "primary.main" }}>
            {avatarInitials}
          </Avatar>
        </Tooltip>
      ) : (
        <Tooltip title="Sign in" placement="right">
          <IconButton component={Link} href="/auth/login" size="small" sx={{ color: "text.secondary" }}>
            <LoginIcon sx={{ fontSize: 16 }} />
          </IconButton>
        </Tooltip>
      )}
    </Box>
  );

  return (
    <>
      {/* Mobile hamburger toggle — fixed top-left, only visible on xs */}
      {isMobile && (
        <IconButton
          onClick={() => setMobileOpen((prev) => !prev)}
          aria-label="open sidebar"
          sx={{
            position: "fixed",
            top: 8,
            left: 8,
            zIndex: 1300,
            bgcolor: "background.paper",
            border: "1px solid rgba(240,246,252,0.12)",
            "&:hover": { bgcolor: "rgba(4,207,208,0.1)" },
          }}
        >
          <MenuIcon sx={{ fontSize: 20 }} />
        </IconButton>
      )}
      {/* Temporary drawer for mobile */}
      <Drawer
        variant="temporary"
        open={mobileOpen}
        onClose={() => setMobileOpen(false)}
        ModalProps={{ keepMounted: true }}
        sx={{
          display: { xs: "block", sm: "none" },
          "& .MuiDrawer-paper": { width: DRAWER_WIDTH, boxSizing: "border-box" },
        }}
      >
        {drawerContent}
      </Drawer>
      {/* Permanent drawer for desktop */}
      <Drawer
        variant="permanent"
        sx={{
          display: { xs: "none", sm: "block" },
          width: collapsed ? COLLAPSED_WIDTH : DRAWER_WIDTH,
          flexShrink: 0,
          transition: "width 0.2s ease",
          "& .MuiDrawer-paper": {
            width: collapsed ? COLLAPSED_WIDTH : DRAWER_WIDTH,
            boxSizing: "border-box",
            overflowX: "hidden",
            transition: "width 0.2s ease",
          },
        }}
        open
      >
        {collapsed ? collapsedContent : drawerContent}
      </Drawer>
    </>
  );
}

export { DRAWER_WIDTH, COLLAPSED_WIDTH };
