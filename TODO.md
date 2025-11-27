# Project Roadmap & TODO

### 1. Dynamic Rate Limiting
- [x] **Global Rate Limiter:**
  - Implemented Token Bucket algorithm acting as a wrapper around API clients.
  - **Goal:** Prevent the bot from being banned by Twitch or YouTube APIs due to accidental loops or DoS.
  - **Status:** COMPLETED.

### 2. Structured Logging
- [x] **Migrate to Structured Logger:**
  - Replaced standard Go `log` with **Zap** (uber-go/zap).
  - **Goal:** JSON formatted logs for high-traffic analysis and debugging.
  - **Status:** COMPLETED.

### 3. Automated Testing
- [x] **Unit & Integration Tests:**
  - Test suite created for `internal/twitch`, `internal/youtube`, and `internal/websocket`.
  - **Goal:** Prevent regressions and ensure stability.
  - **Status:** COMPLETED.

### 4. Admin Administration Interface
- [ ] **Web Dashboard:**
  - Develop a lightweight, password-protected web dashboard (e.g., `/admin`).
  - **Features:**
    - View current bot status (uptime, active connections).
    - Manage API Tokens (update/refresh without DB access).
    - Update configuration parameters at runtime ("Hot Reload").
  - **Status:** PENDING (Awaiting Directives).
