import type { IronSessionOptions } from "iron-session";

export interface SessionData {
  idToken?: string;
  accessToken?: string;
  // ISO-8601 expiry timestamp from the token set.
  expiresAt?: string;
}

export const sessionOptions: IronSessionOptions = {
  password: process.env.SESSION_PASSWORD as string,
  cookieName: "opendepot_session",
  cookieOptions: {
    // Require HTTPS in production; allow HTTP in local dev.
    secure: process.env.NODE_ENV === "production",
    httpOnly: true,
    sameSite: "lax",
  },
};
