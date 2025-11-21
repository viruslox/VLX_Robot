# Implementation Plan: YouTube Live Chat Polling

**Objective:** Implement a polling mechanism for the YouTube Live Chat to support Super Chat alerts and text commands. Unlike Twitch (which uses WebSockets), YouTube requires periodic polling of the API.

**Target File:** `internal/youtube/youtube.go`

## Phase A: Initialization & State Management

- [ ] **Retrieve LiveChatID:**
  - Upon bot startup, query the YouTube `Videos` API.
  - Filter by `eventType=live` and `channelId` to find the current active stream.
  - Extract the `activeLiveChatId`.

- [ ] **Persist State:**
  - Save the `LiveChatID` to the PostgreSQL database (`youtube_state` table).
  - *Rationale:* ensures the bot can resume operations without re-querying the ID if the service restarts.

## Phase B: The Polling Engine

- [ ] **Implement `StartPolling()`:**
  - Create a function to run inside a goroutine (similar to the Twitch implementation).
  - Initialize a `time.Ticker` (recommended interval: 5–10 seconds).

- [ ] **API Interaction:**
  - Call the `liveChatMessages.list` endpoint on every tick.
  - **Critical:** Always utilize the `nextPageToken` returned from the previous API call.
  - Update and save the new `pageToken` to the database after every cycle to prevent processing duplicate messages.

## Phase C: Event Handling Logic

Once the `items` array is retrieved from the API, implement the following handlers:

- [ ] **Handle Super Chats (Monetization):**
  - Check message items for the existence of `snippet.superChatDetails`.
  - Extract data: `amountDisplayString` (e.g., "€5.00") and `userComment`.
  - **Action:** Construct a JSON payload and send it via `hub.Broadcast` to trigger the overlay alert and audio.

- [ ] **Handle Text Commands (Interaction):**
  - Check `snippet.displayMessage` for the command prefix (e.g., `!`).
  - **Logic:** Parse the string (e.g., `!sound`) and cross-reference with the command map.
  - **Action:** Trigger the corresponding audio file or event (mirroring the existing logic used for Twitch).
