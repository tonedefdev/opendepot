import * as React from "react";
import Container from "@mui/material/Container";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import Button from "@mui/material/Button";
import Link from "next/link";
import ArrowBackIcon from "@mui/icons-material/ArrowBack";

export default function NotFound() {
  return (
    <Container maxWidth="md" sx={{ py: 8 }}>
      <Box sx={{ textAlign: "center" }}>
        <Typography
          variant="h1"
          sx={{ fontSize: "6rem", fontWeight: 900, color: "primary.main", lineHeight: 1 }}
        >
          404
        </Typography>
        <Typography variant="h5" gutterBottom sx={{ mt: 2 }}>
          Resource not found
        </Typography>
        <Typography variant="body1" color="text.secondary" sx={{ mb: 4 }}>
          The module or provider you&apos;re looking for doesn&apos;t exist or may have been removed.
        </Typography>
        <Button
          component={Link}
          href="/"
          variant="contained"
          startIcon={<ArrowBackIcon />}
        >
          Back to Registry
        </Button>
      </Box>
    </Container>
  );
}
