"use client";

import * as React from "react";
import Container from "@mui/material/Container";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import Button from "@mui/material/Button";
import Alert from "@mui/material/Alert";
import RefreshIcon from "@mui/icons-material/Refresh";
import { useEffect } from "react";

interface ErrorProps {
  error: Error & { digest?: string };
  reset: () => void;
}

export default function ErrorPage({ error, reset }: ErrorProps) {
  useEffect(() => {
    console.error(error);
  }, [error]);

  return (
    <Container maxWidth="md" sx={{ py: 8 }}>
      <Box sx={{ textAlign: "center" }}>
        <Typography variant="h4" gutterBottom>
          Failed to load resource
        </Typography>
        <Alert severity="error" sx={{ mb: 3, textAlign: "left" }}>
          {error.message || "An unexpected error occurred."}
          {error.digest && (
            <Typography variant="caption" display="block" sx={{ mt: 0.5, opacity: 0.7 }}>
              Reference: {error.digest}
            </Typography>
          )}
        </Alert>
        <Button
          variant="contained"
          startIcon={<RefreshIcon />}
          onClick={() => reset()}
        >
          Try again
        </Button>
      </Box>
    </Container>
  );
}
