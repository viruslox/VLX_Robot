# Project Roadmap & TODO

### 1. Dynamic Rate Limiting
- [ ] **Global Rate Limiter:**
  - Implement a global rate limiting mechanism (e.g., Token Bucket algorithm) acting as a middleware or wrapper around API clients.
  - **Goal:** Prevent the bot from being banned by Twitch or YouTube APIs due to accidental loops, malfunctions, or DoS attacks.
  - **Scope:** `internal/twitch/chat.go`, `internal/youtube/youtube.go`.

### 2. Structured Logging
- [ ] **Migrate to Structured Logger:**
  - Replace the standard Go `log` package with a structured logging library like **Zap** (uber-go/zap) or **Logrus**.
  - **Goal:** Facilitate log analysis and parsing, especially during high traffic events, by outputting logs in JSON format with relevant context (timestamps, levels, module names).
  - **Scope:** Global (`main.go` and all internal packages).

### 3. Automated Testing
- [ ] **Unit & Integration Tests:**
  - Create a test suite for critical modules to ensure stability.
  - **Unit Tests:** Focus on `internal/twitch` (command parsing) and `internal/youtube` (response handling).
  - **Integration Tests:** Verify the interaction between the `websocket/hub` and the API clients.
  - **Goal:** Prevent regressions when adding new features.

### 4. Admin Administration Interface
- [ ] **Web Dashboard:**
  - Develop a lightweight, password-protected web dashboard (e.g., `/admin`).
  - **Features:**
    - View current bot status (uptime, active connections).
    - Manage API Tokens (update/refresh without DB access).
    - Update configuration parameters (e.g., cooldowns, enabled modules) at runtime ("Hot Reload").
  - **Goal:** Allow management without requiring server restarts or raw DB queries.
