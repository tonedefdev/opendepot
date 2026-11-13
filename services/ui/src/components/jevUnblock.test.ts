import { describe, expect, it } from "vitest";
import type { JevUnblock } from "@/lib/api";
import { describeJevUnblock } from "./jevUnblock";

const digest = "d".repeat(64);

function server(overrides: Partial<JevUnblock> & Pick<JevUnblock, "state">): JevUnblock {
  return { governingPriority: 0, matchingPriority: 0, namesHidden: false, ...overrides };
}

function run(unblock: JevUnblock | undefined) {
  return describeJevUnblock({ unblock, namespace: "platform" });
}

describe("describeJevUnblock", () => {
  it("returns null when the server has no jevUnblock", () => {
    expect(run(undefined)).toBeNull();
  });

  it("returns null for the none state", () => {
    expect(run(server({ state: "none" }))).toBeNull();
  });

  it("maps no-digest with the namespace for the forceSync command", () => {
    expect(run(server({ state: "no-digest" }))).toEqual({ state: "no-digest", namespace: "platform" });
  });

  it("maps needs-entry with the governing policy name", () => {
    expect(run(server({ state: "needs-entry", digest, governingPolicy: "platform-skills" }))).toEqual({
      state: "needs-entry",
      digest,
      policy: "platform-skills",
      namesHidden: false,
    });
  });

  it("maps needs-entry without a governing policy to no name, with names visible", () => {
    expect(run(server({ state: "needs-entry", digest }))).toEqual({ state: "needs-entry", digest, policy: undefined, namesHidden: false });
  });

  it("maps superseded with the shadowed policy as the matching policy and the governing policy as the winner", () => {
    expect(
      run(
        server({
          state: "superseded",
          digest,
          governingPolicy: "newer",
          governingPriority: 9,
          matchingPolicy: "older",
          matchingPriority: 2,
        }),
      ),
    ).toEqual({
      state: "superseded",
      digest,
      policy: "older",
      winner: "newer",
      policyPriority: 2,
      winnerPriority: 9,
    });
  });

  it("maps expired with the governing policy and the expiry", () => {
    expect(run(server({ state: "expired", digest, governingPolicy: "platform", expires: "2026-01-01T00:00:00Z" }))).toEqual({
      state: "expired",
      digest,
      policy: "platform",
      expires: "2026-01-01T00:00:00Z",
    });
  });

  it("maps pending with the namespace and the governing policy", () => {
    expect(run(server({ state: "pending", digest, governingPolicy: "platform-skills" }))).toEqual({
      state: "pending",
      namespace: "platform",
      digest,
      policy: "platform-skills",
    });
  });

  it("maps exempted with the governing policy and the expiry", () => {
    expect(run(server({ state: "exempted", governingPolicy: "platform", expires: "2026-02-01T00:00:00Z" }))).toEqual({
      state: "exempted",
      policy: "platform",
      expires: "2026-02-01T00:00:00Z",
    });
  });

  it("maps unknown with the digest and the expiry", () => {
    expect(run(server({ state: "unknown", digest, expires: "2026-03-01T00:00:00Z" }))).toEqual({
      state: "unknown",
      digest,
      expires: "2026-03-01T00:00:00Z",
    });
  });

  it("maps a state this UI does not recognize to unknown", () => {
    expect(run(server({ state: "future-state" as JevUnblock["state"], digest }))).toEqual({ state: "unknown", digest });
  });

  describe("names hidden", () => {
    it("drops the governing policy name for needs-entry when namesHidden is true", () => {
      expect(run(server({ state: "needs-entry", digest, governingPolicy: "platform-skills", namesHidden: true }))).toEqual({
        state: "needs-entry",
        digest,
        policy: undefined,
        namesHidden: true,
      });
    });

    it("drops both names for superseded and keeps the priorities", () => {
      expect(
        run(
          server({
            state: "superseded",
            digest,
            governingPolicy: "newer",
            matchingPolicy: "older",
            governingPriority: 9,
            matchingPriority: 2,
            namesHidden: true,
          }),
        ),
      ).toEqual({
        state: "superseded",
        digest,
        policy: undefined,
        winner: undefined,
        policyPriority: 2,
        winnerPriority: 9,
      });
    });

    it("drops the policy name for expired and keeps the expiry", () => {
      expect(run(server({ state: "expired", digest, governingPolicy: "platform", expires: "2026-01-01T00:00:00Z", namesHidden: true }))).toEqual({
        state: "expired",
        digest,
        policy: undefined,
        expires: "2026-01-01T00:00:00Z",
      });
    });

    it("drops the policy name for pending", () => {
      expect(run(server({ state: "pending", digest, governingPolicy: "platform", namesHidden: true }))).toEqual({
        state: "pending",
        namespace: "platform",
        digest,
        policy: undefined,
      });
    });

    it("drops the policy name for exempted", () => {
      expect(run(server({ state: "exempted", governingPolicy: "platform", expires: "2026-02-01T00:00:00Z", namesHidden: true }))).toEqual({
        state: "exempted",
        policy: undefined,
        expires: "2026-02-01T00:00:00Z",
      });
    });
  });

  describe("empty names", () => {
    it("treats an empty governing policy name as absent", () => {
      expect(run(server({ state: "needs-entry", digest, governingPolicy: "" }))).toEqual({
        state: "needs-entry",
        digest,
        policy: undefined,
        namesHidden: false,
      });
    });

    it("treats an empty matching policy name as absent for superseded", () => {
      expect(
        run(server({ state: "superseded", digest, governingPolicy: "newer", matchingPolicy: "", governingPriority: 9, matchingPriority: 2 })),
      ).toEqual({
        state: "superseded",
        digest,
        policy: undefined,
        winner: "newer",
        policyPriority: 2,
        winnerPriority: 9,
      });
    });
  });

  describe("missing digest", () => {
    it.each(["needs-entry", "superseded", "expired", "pending"] as const)("maps %s without a digest to unknown", (state) => {
      expect(run(server({ state, governingPolicy: "platform" }))).toEqual({ state: "unknown" });
    });
  });
});
