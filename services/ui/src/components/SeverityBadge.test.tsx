// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import * as React from "react";
import { describe, expect, it } from "vitest";
import SeverityBadge from "./SeverityBadge";

describe("SeverityBadge", () => {
  it("shows exempted findings separately from blocking severities", () => {
    render(
      <SeverityBadge
        counts={{ critical: 1, high: 0, medium: 0, low: 0, unknown: 0, exempted: 3 }}
      />,
    );

    expect(screen.getByLabelText("1 CRITICAL")).not.toBeNull();
    expect(screen.getByText("E 3")).not.toBeNull();
  });
});
