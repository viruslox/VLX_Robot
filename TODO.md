# Project Roadmap & TODO

## Feature Implementation: YouTube Live Chat Polling

**Objective:** Implement a polling mechanism for the YouTube Live Chat to support Super Chat alerts and text commands. Unlike Twitch (which uses WebSockets), YouTube requires periodic polling of the API.

**Target File:** `internal/youtube/youtube.go`

### Phase A: Initialization & State Management

- [ ] **Retrieve LiveChatID:**
  - Upon bot startup, query the YouTube `Videos` API.
  - Filter by `eventType=live` and `channelId` to find the current active stream.
  - Extract the `activeLiveChatId`.

- [ ] **Persist State:**
  - Save the `LiveChatID` to the PostgreSQL database (`youtube_state` table).
  - *Rationale:* ensures the bot can resume operations without re-querying the ID if the service restarts.

### Phase B: The Polling Engine

- [ ] **Implement `StartPolling()`:**
  - Create a function to run inside a goroutine (similar to the Twitch implementation).
  - Initialize a `time.Ticker` (recommended interval: 5–10 seconds).

- [ ] **API Interaction:**
  - Call the `liveChatMessages.list` endpoint on every tick.
  - **Critical:** Always utilize the `nextPageToken` returned from the previous API call.
  - Update and save the new `pageToken` to the database after every cycle to prevent processing duplicate messages.

### Phase C: Event Handling Logic

Once the `items` array is retrieved from the API, implement the following handlers:

- [ ] **Handle Super Chats (Monetization):**
  - Check message items for the existence of `snippet.superChatDetails`.
  - Extract data: `amountDisplayString` (e.g., "€5.00") and `userComment`.
  - **Action:** Construct a JSON payload and send it via `hub.Broadcast` to trigger the overlay alert and audio.

- [ ] **Handle Text Commands (Interaction):**
  - Check `snippet.displayMessage` for the command prefix (e.g., `!`).
  - **Logic:** Parse the string (e.g., `!sound`) and cross-reference with the command map.
  - **Action:** Trigger the corresponding audio file or event (mirroring the existing logic used for Twitch).

---

## System Hardening & Architecture

**Objective:** Improve the reliability, maintainability, and observability of the codebase to support increased traffic and future scalability.

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
