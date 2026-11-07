package main

// OpenAI endpoint URLs — hardcoded per spec §2.1, never user-configurable.
// These full-URL constants are the authoritative documented contract; the
// OpenAI client also keeps *Path-suffixed constants for test BaseURL override.
const (
	usageEndpoint        = "https://chatgpt.com/backend-api/wham/usage"
	resetCreditsEndpoint = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"
	consumeEndpoint      = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume"
)

// codexUserAgent mirrors the value used by Cli-Proxy-API-Management-Center
// (spec §2.2). The codex_cli_rs/<version> prefix is the load-bearing part.
const codexUserAgent = "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal"
