// The registry layout test runs against its own UI server, which reads resources from the stub API below.
export const LAYOUT_STUB_PORT = 8787;
export const LAYOUT_UI_PORT = 3400;
export const LAYOUT_STUB_URL = `http://127.0.0.1:${LAYOUT_STUB_PORT}`;
export const LAYOUT_UI_URL = `http://localhost:${LAYOUT_UI_PORT}`;
